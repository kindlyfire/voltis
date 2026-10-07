package routes

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"voltis/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type FacetRoutes struct {
	pool *pgxpool.Pool
}

func (fr *FacetRoutes) Register(g *echo.Group) {
	g.GET("/:kind", fr.list)
	g.GET("/:kind/:key", fr.entry)
}

// facetKinds maps the URL kinds to content_facets' kinds.
var facetKinds = map[string]string{"genres": "genre", "tags": "tag", "people": "person", "publishers": "publisher"}

type facetListQuery struct {
	Search    string `query:"q"          validate:"max=200"`
	Sort      string `query:"sort"       default:"count" validate:"oneof=count name"`
	Order     string `query:"order"      validate:"omitempty,oneof=asc desc"` // default: count desc, name asc
	LibraryID string `query:"library_id"`
	Limit     int    `query:"limit"      default:"100" validate:"min=1,max=500"`
	Offset    int    `query:"offset"     validate:"min=0"`
}

type FacetDTO struct {
	Key   string `json:"key"   db:"key"`   // folded; the URL segment replaces its spaces with '-'
	Name  string `json:"name"  db:"name"`  // most common label (sampled), global; genres: the slug
	Count int    `json:"count" db:"count"` // valid roots, in library_id when set
}

type FacetEntryDTO struct {
	FacetDTO
	Roles []RoleCountDTO `json:"roles" db:"roles"` // people only, else []; count desc, role
}

type RoleCountDTO struct {
	Role  string `json:"role"`
	Count int    `json:"count"`
}

// facetName is the SQL for the display name of key k: the most common label among the first 500
// roots in idx_content_facets_value order, one vote per root and spelling. With bitmap scans off it
// is an ordered index scan that stops at 500 rows; a bitmap scan would sort every row of the key
// first.
func facetName(k string) string {
	return `(SELECT l FROM (SELECT labels FROM content_facets
		WHERE kind = @kind AND key = ` + k + ` ORDER BY library_id, content_id LIMIT 500) s, unnest(s.labels) l
	GROUP BY l ORDER BY count(*) DESC, l LIMIT 1)`
}

// facetRow is a list row; total is the number of keys, on the count sort only.
type facetRow struct {
	FacetDTO
	Total int `db:"total" json:"-"`
}

// facetTx runs fn with the settings every facet query relies on.
func facetTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SET LOCAL work_mem = '64MB'; SET LOCAL enable_bitmapscan = off"); err != nil {
			return err
		}
		return fn(tx)
	})
}

func facetKind(c echo.Context) (string, error) {
	kind, ok := facetKinds[c.Param("kind")]
	if !ok {
		return "", echo.NewHTTPError(http.StatusNotFound, "Unknown facet kind")
	}
	return kind, nil
}

func (fr *FacetRoutes) list(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}
	kind, err := facetKind(c)
	if err != nil {
		return err
	}
	q, err := BindQuery[facetListQuery](c)
	if err != nil {
		return err
	}

	args := pgx.NamedArgs{"kind": kind, "limit": q.Limit, "offset": q.Offset}
	scope := "kind = @kind"
	if q.LibraryID != "" {
		args["lib"] = q.LibraryID
		scope += " AND library_id = @lib"
	}
	if q.Search != "" {
		// The subquery folds q once; a bare facet_key(@q) is re-folded per row under generic plans.
		args["q"] = q.Search
		scope += " AND strpos(key, (SELECT public.facet_key(@q))) > 0"
	}
	order := q.Order
	if order == "" {
		order = map[string]string{"count": "desc", "name": "asc"}[q.Sort]
	}
	var pageSQL string
	if q.Sort == "count" {
		// The aggregate covers the whole kind anyway, so total comes with it.
		sortBy := "n " + order + ", key"
		pageSQL = `
			WITH g AS (
				SELECT key, count(*) AS n, count(*) OVER () AS total
				FROM content_facets WHERE ` + scope + ` GROUP BY key
			), page AS (SELECT * FROM g ORDER BY ` + sortBy + ` LIMIT @limit OFFSET @offset)
			SELECT p.key, ` + facetName("p.key") + ` AS name, p.n AS count, p.total
			FROM page p ORDER BY ` + sortBy
	} else {
		// GROUP BY key ORDER BY key streams the index and stops after the page.
		pageSQL = `
			WITH page AS (
				SELECT key, count(*) AS n FROM content_facets WHERE ` + scope + `
				GROUP BY key ORDER BY key ` + order + ` LIMIT @limit OFFSET @offset
			)
			SELECT p.key, ` + facetName("p.key") + ` AS name, p.n AS count, 0 AS total
			FROM page p ORDER BY p.key ` + order
	}

	ctx := reqCtx(c)
	var page []facetRow
	var total int
	err = facetTx(ctx, fr.pool, func(tx pgx.Tx) (err error) {
		if page, err = db.Select[facetRow](ctx, tx, pageSQL, args); err != nil {
			return err
		}
		if q.Sort == "count" && len(page) > 0 {
			total = page[0].Total
			return nil
		}
		total, err = db.SelectScalar[int](ctx, tx, "SELECT count(DISTINCT key) FROM content_facets WHERE "+scope, args)
		return err
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, PaginatedResponse[facetRow]{Data: page, Total: total})
}

func (fr *FacetRoutes) entry(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}
	kind, err := facetKind(c)
	if err != nil {
		return err
	}
	// Echo leaves the param escaped when it routes on RawPath (set for escapes such as %2B).
	key := c.Param("key")
	if c.Request().URL.RawPath != "" {
		key, _ = url.PathUnescape(key)
	}

	args := pgx.NamedArgs{"kind": kind, "key": key}
	lib := ""
	if id := c.QueryParam("library_id"); id != "" {
		args["lib"] = id
		lib = " AND library_id = @lib"
	}

	// Roles read the heap for every row of the key, and only people have them.
	roles := "'[]'::jsonb"
	if kind == "person" {
		roles = `(SELECT coalesce(jsonb_agg(jsonb_build_object('role', r, 'count', n) ORDER BY n DESC, r), '[]')
			FROM (SELECT r, count(*) n FROM content_facets, unnest(roles) r
				WHERE kind = @kind AND key = k.key` + lib + ` GROUP BY r) x)`
	}

	ctx := reqCtx(c)
	var e FacetEntryDTO
	err = facetTx(ctx, fr.pool, func(tx pgx.Tx) (err error) {
		e, err = db.SelectOne[FacetEntryDTO](ctx, tx, `
			WITH k AS (SELECT public.facet_key(@key) AS key)
			SELECT k.key, `+facetName("k.key")+` AS name,
				(SELECT count(*) FROM content_facets WHERE kind = @kind AND key = k.key`+lib+`) AS count,
				`+roles+` AS roles
			FROM k WHERE EXISTS (SELECT 1 FROM content_facets WHERE kind = @kind AND key = k.key)
		`, args)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Not found")
	}
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, e)
}
