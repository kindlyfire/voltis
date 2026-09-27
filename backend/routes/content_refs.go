package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"voltis/db"
	"voltis/linking"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ContentRefRoutes struct {
	pool  *pgxpool.Pool
	links *linking.Service
}

func (cr *ContentRefRoutes) Register(g *echo.Group) {
	g.GET("/refs/:library_id", cr.listLibraryURIs)
	g.GET("/broken-refs", cr.brokenRefsSummary)
	g.GET("/broken-refs/:library_id", cr.listBrokenRefs)
	g.POST("/broken-refs/:library_id", cr.fixBrokenRefs)
	g.GET("/orphaned-metadata", adminOnly(cr.orphansSummary))
	g.GET("/orphaned-metadata/:library_id", adminOnly(cr.listOrphans))
	g.GET("/orphaned-metadata/:library_id/targets", adminOnly(cr.listOrphanTargets))
	g.POST("/orphaned-metadata/:library_id", adminOnly(cr.fixOrphans))
}

type libraryURIsResponse struct {
	ContentURIs []string `json:"content_uris"`
	UserURIs    []string `json:"user_uris"`
}

func (cr *ContentRefRoutes) listLibraryURIs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	contentURIs, err := db.SelectScalars[string](ctx, cr.pool,
		"SELECT uri FROM content WHERE library_id = $1", libraryID)
	if err != nil {
		return err
	}

	userURIs, err := db.SelectScalars[string](ctx, cr.pool,
		"SELECT uri FROM user_to_content WHERE user_id = $1 AND library_id = $2",
		user.ID, libraryID)
	if err != nil {
		return err
	}

	if contentURIs == nil {
		contentURIs = []string{}
	}
	if userURIs == nil {
		userURIs = []string{}
	}

	return c.JSON(http.StatusOK, libraryURIsResponse{
		ContentURIs: contentURIs,
		UserURIs:    userURIs,
	})
}

type brokenRefsSummaryItem struct {
	LibraryID *string `json:"library_id" db:"library_id"`
	Count     int     `json:"count"      db:"count"`
}

func (cr *ContentRefRoutes) brokenRefsSummary(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)

	rows, err := cr.pool.Query(ctx, `
		SELECT utc.library_id, COUNT(*)
		FROM user_to_content utc
		LEFT JOIN content c ON c.uri = utc.uri AND c.library_id = utc.library_id
		WHERE utc.user_id = $1 AND c.id IS NULL
		GROUP BY utc.library_id
	`, user.ID)
	if err != nil {
		return err
	}
	defer rows.Close()

	var items []brokenRefsSummaryItem
	for rows.Next() {
		var item brokenRefsSummaryItem
		if err := rows.Scan(&item.LibraryID, &item.Count); err != nil {
			return err
		}
		items = append(items, item)
	}
	if items == nil {
		items = []brokenRefsSummaryItem{}
	}
	return c.JSON(http.StatusOK, items)
}

type brokenUserToContentDTO struct {
	ID                string          `json:"id"`
	URI               string          `json:"uri"`
	LibraryID         *string         `json:"library_id"`
	Starred           bool            `json:"starred"`
	Status            *string         `json:"status"`
	StatusUpdatedAt   *time.Time      `json:"status_updated_at"`
	Notes             *string         `json:"notes"`
	Rating            *int            `json:"rating"`
	Progress          json.RawMessage `json:"progress"`
	ProgressUpdatedAt *time.Time      `json:"progress_updated_at"`
}

func brokenUTCToDTO(u models.UserToContent) brokenUserToContentDTO {
	progress := json.RawMessage(u.Progress)
	if progress == nil {
		progress = json.RawMessage("{}")
	}
	return brokenUserToContentDTO{
		ID:                u.ID,
		URI:               u.URI,
		LibraryID:         u.LibraryID,
		Starred:           u.Starred,
		Status:            u.Status,
		StatusUpdatedAt:   u.StatusUpdatedAt,
		Notes:             u.Notes,
		Rating:            u.Rating,
		Progress:          progress,
		ProgressUpdatedAt: u.ProgressUpdatedAt,
	}
}

type brokenRefsQuery struct {
	Search string `query:"search"`
	Limit  *int   `query:"limit"  validate:"omitempty,min=1"`
	Offset int    `query:"offset" validate:"min=0"`
}

func (cr *ContentRefRoutes) listBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	q, err := BindQuery[brokenRefsQuery](c)
	if err != nil {
		return err
	}

	args := pgx.NamedArgs{
		"user_id":    user.ID,
		"library_id": libraryID,
	}

	searchClause := ""
	if q.Search != "" {
		args["search"] = "%" + q.Search + "%"
		searchClause = " AND utc.uri ILIKE @search"
	}

	baseQuery := fmt.Sprintf(`
		FROM user_to_content utc
		LEFT JOIN content c ON c.uri = utc.uri AND c.library_id = utc.library_id
		WHERE utc.user_id = @user_id AND utc.library_id = @library_id AND c.id IS NULL%s
	`, searchClause)

	var total int
	err = cr.pool.QueryRow(ctx, "SELECT COUNT(*) "+baseQuery, args).Scan(&total)
	if err != nil {
		return err
	}

	dataQuery := "SELECT utc.* " + baseQuery + " ORDER BY utc.uri"
	if q.Limit != nil {
		dataQuery += fmt.Sprintf(" LIMIT %d", *q.Limit)
	}
	if q.Offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", q.Offset)
	}

	items, err := db.Select[models.UserToContent](ctx, cr.pool, dataQuery, args)
	if err != nil {
		return err
	}

	dtos := make([]brokenUserToContentDTO, len(items))
	for i, item := range items {
		dtos[i] = brokenUTCToDTO(item)
	}

	return c.JSON(http.StatusOK, PaginatedResponse[brokenUserToContentDTO]{Data: dtos, Total: total})
}

type brokenRefsFixRequest struct {
	Delete []string          `json:"delete"`
	Update map[string]string `json:"update"`
}

func (cr *ContentRefRoutes) fixBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")

	var req brokenRefsFixRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Scans move refs under this lock; without it a target could be validated mid-rename.
	if err := db.LockMetadata(ctx, tx, libraryID); err != nil {
		return err
	}

	// Delete
	if len(req.Delete) > 0 {
		_, err := tx.Exec(ctx, `
			DELETE FROM user_to_content
			WHERE id = ANY($1) AND user_id = $2 AND library_id = $3
		`, req.Delete, user.ID, libraryID)
		if err != nil {
			return err
		}
	}

	// Update
	if len(req.Update) > 0 {
		// Validate target URIs exist
		targetURIs := make([]string, 0, len(req.Update))
		seen := map[string]bool{}
		for _, uri := range req.Update {
			if !seen[uri] {
				targetURIs = append(targetURIs, uri)
				seen[uri] = true
			}
		}

		rows, err := tx.Query(ctx,
			"SELECT uri FROM content WHERE uri = ANY($1) AND library_id = $2",
			targetURIs, libraryID)
		if err != nil {
			return err
		}
		validSet := map[string]bool{}
		for rows.Next() {
			var u string
			if err := rows.Scan(&u); err != nil {
				return err
			}
			validSet[u] = true
		}
		var invalid []string
		for _, u := range targetURIs {
			if !validSet[u] {
				invalid = append(invalid, u)
			}
		}
		if len(invalid) > 0 {
			sort.Strings(invalid)
			return echo.NewHTTPError(http.StatusBadRequest,
				fmt.Sprintf("No content with URIs %v in library '%s'", invalid, libraryID))
		}

		// Delete existing entries at target URIs to avoid conflicts
		_, err = tx.Exec(ctx, `
			DELETE FROM user_to_content
			WHERE user_id = $1 AND library_id = $2 AND uri = ANY($3)
		`, user.ID, libraryID, targetURIs)
		if err != nil {
			return err
		}

		// Update each ref
		for utcID, newURI := range req.Update {
			_, err = tx.Exec(ctx, `
				UPDATE user_to_content SET uri = $1
				WHERE id = $2 AND user_id = $3 AND library_id = $4
			`, newURI, utcID, user.ID, libraryID)
			if err != nil {
				return err
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return okResponse(c)
}

// orphanedURIs are the URIs holding metadata or links that outlive what they describe: metadata
// without content, and links without series content.
var orphanedURIs = `
	SELECT library_id, uri FROM content_metadata m WHERE ` + metadata.KeptMetadata + ` AND NOT ` + metadata.HeldMetadata + `
	UNION SELECT library_id, uri FROM metadata_links l WHERE ` + metadata.KeptLink + ` AND NOT ` + metadata.HeldLink

func (cr *ContentRefRoutes) orphansSummary(c echo.Context) error {
	items, err := db.Select[brokenRefsSummaryItem](reqCtx(c), cr.pool,
		"SELECT library_id, COUNT(*) AS count FROM ("+orphanedURIs+") o GROUP BY library_id ORDER BY library_id")
	if err != nil {
		return err
	}
	if items == nil {
		items = []brokenRefsSummaryItem{}
	}
	return c.JSON(http.StatusOK, items)
}

type orphanDTO struct {
	URI       string          `json:"uri"       db:"uri"`
	Title     *string         `json:"title"     db:"title"`
	Overrides []string        `json:"overrides" db:"overrides"` // overridden field keys
	Links     []orphanLinkDTO `json:"links"     db:"links"`
}

type orphanLinkDTO struct {
	Provider   string   `json:"provider"`
	State      string   `json:"state"`
	ExternalID *string  `json:"external_id"`
	Rejected   []string `json:"rejected"`
}

func (cr *ContentRefRoutes) listOrphans(c echo.Context) error {
	q, err := BindQuery[brokenRefsQuery](c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	args := pgx.NamedArgs{"library_id": c.Param("library_id"), "search": "%" + q.Search + "%"}
	from := "FROM (" + orphanedURIs + ") o"
	where := " WHERE o.library_id = @library_id AND o.uri ILIKE @search"

	total, err := db.SelectScalar[int](ctx, cr.pool, "SELECT COUNT(*) "+from+where, args)
	if err != nil {
		return err
	}
	dataQuery := `
		SELECT o.uri, m.data->>'title' AS title,
			ARRAY(SELECT k FROM jsonb_object_keys(COALESCE(m.data_raw->'overrides', '{}')) k ORDER BY k) AS overrides,
			COALESCE((
				SELECT jsonb_agg(jsonb_build_object('provider', l.provider, 'state', l.state,
					'external_id', l.external_id, 'rejected', l.rejected) ORDER BY l.provider)
				FROM metadata_links l WHERE l.library_id = o.library_id AND l.uri = o.uri
			), '[]') AS links
		` + from + ` LEFT JOIN content_metadata m ON m.library_id = o.library_id AND m.uri = o.uri AND NOT ` + metadata.HeldMetadata +
		where + ` ORDER BY o.uri`
	if q.Limit != nil {
		dataQuery += fmt.Sprintf(" LIMIT %d", *q.Limit)
	}
	if q.Offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", q.Offset)
	}
	items, err := db.Select[orphanDTO](ctx, cr.pool, dataQuery, args)
	if err != nil {
		return err
	}
	if items == nil {
		items = []orphanDTO{}
	}
	return c.JSON(http.StatusOK, PaginatedResponse[orphanDTO]{Data: items, Total: total})
}

type orphanTargetsQuery struct {
	Search string `query:"q"`
	Series bool   `query:"series"` // for orphans with links, which attach to series only
	Limit  int    `query:"limit" default:"50" validate:"min=1,max=100"`
}

type orphanTargetDTO struct {
	URI   string  `json:"uri"   db:"uri"`
	Title *string `json:"title" db:"title"`
}

// listOrphanTargets searches the content orphans can move to, by title or URI, an exact match first.
func (cr *ContentRefRoutes) listOrphanTargets(c echo.Context) error {
	q, err := BindQuery[orphanTargetsQuery](c)
	if err != nil {
		return err
	}
	args := pgx.NamedArgs{"library_id": c.Param("library_id"), "series": q.Series, "search": q.Search,
		"like": "%" + likeEscaper.Replace(q.Search) + "%", "limit": q.Limit}
	from, order := "content c", "c.uri"
	if q.Search != "" {
		// Titles through the bm25 index, URIs by pattern: bm25 on both at once crashed ParadeDB.
		from = `(SELECT m.uri FROM content_metadata m WHERE m.library_id = @library_id AND ` +
			metadata.Matches("m", "search_text", true, q.Search) + `
			UNION SELECT uri FROM content WHERE library_id = @library_id AND uri ILIKE @like) h
			JOIN content c ON c.library_id = @library_id AND c.uri = h.uri`
		order = "(c.uri = @search OR " + metadata.ExactTitle + ") DESC, c.uri"
	}
	items, err := db.Select[orphanTargetDTO](reqCtx(c), cr.pool, `
		SELECT c.uri, m.data->>'title' AS title
		FROM `+from+` LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri
		WHERE c.library_id = @library_id AND (NOT @series OR `+metadata.SeriesContent+`)
		ORDER BY `+order+` LIMIT @limit`, args)
	if err != nil {
		return err
	}
	if items == nil {
		items = []orphanTargetDTO{}
	}
	return c.JSON(http.StatusOK, map[string]any{"data": items})
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

type orphansFixRequest struct {
	Delete []string          `json:"delete"`
	Move   map[string]string `json:"move"`
}

func (cr *ContentRefRoutes) fixOrphans(c echo.Context) error {
	var req orphansFixRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := cr.links.FixOrphans(reqCtx(c), c.Param("library_id"), req.Delete, req.Move); err != nil {
		return metadataError(err)
	}
	return okResponse(c)
}
