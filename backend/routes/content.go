package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/comic"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
)

type ContentRoutes struct {
	pool *pgxpool.Pool
}

func (cr *ContentRoutes) Register(g *echo.Group) {
	g.GET("", cr.list)
	g.GET("/continue-reading", cr.continueList)
	g.GET("/buckets", cr.buckets)
	g.GET("/ids", cr.ids)
	g.POST("/bulk/user-data", cr.bulkUserData)
	g.GET("/:content_id", cr.get)
	g.GET("/:content_id/lists", cr.listsForContent)
	g.POST("/:content_id/user-data", cr.updateUserData)
	g.GET("/:content_id/reading", cr.readingGet)
	g.POST("/:content_id/reading", cr.readingPost)
	g.POST("/:content_id/series-reading", cr.seriesReading)
	g.GET("/:content_id/continue", cr.continueOne)
}

type UserToContentDTO struct {
	Starred           bool            `json:"starred"`
	Status            *string         `json:"status"`
	StatusUpdatedAt   *time.Time      `json:"status_updated_at"`
	Notes             *string         `json:"notes"`
	Rating            *int            `json:"rating"`
	Progress          json.RawMessage `json:"progress"`
	ProgressUpdatedAt *time.Time      `json:"progress_updated_at"`
	Revision          *string         `json:"revision"`
	LastReadAt        *time.Time      `json:"last_read_at"`
	// ReadingSeq orders this state against others of the same content; see models.ReadingSeq.
	ReadingSeq models.ReadingSeq `json:"reading_seq"`
}

func utcToDTO(u *models.UserToContent) *UserToContentDTO {
	if u == nil {
		return nil
	}
	progress := u.Progress
	if progress == nil {
		progress = json.RawMessage("{}")
	}
	return &UserToContentDTO{
		Starred:           u.Starred,
		Status:            u.Status,
		StatusUpdatedAt:   u.StatusUpdatedAt,
		Notes:             u.Notes,
		Rating:            u.Rating,
		Progress:          progress,
		ProgressUpdatedAt: u.ProgressUpdatedAt,
		Revision:          u.Revision,
		LastReadAt:        u.LastReadAt,
		ReadingSeq:        u.ReadingSeq,
	}
}

type ContentDTO struct {
	ID           string          `json:"id"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
	URIPart      string          `json:"uri_part"`
	URI          string          `json:"uri"`
	Title        string          `json:"title"`
	Valid        bool            `json:"valid"`
	FileURI      *string         `json:"file_uri"`
	FileMtime    *time.Time      `json:"file_mtime"`
	FileSize     *int            `json:"file_size"`
	CoverURI     *string         `json:"cover_uri"`
	CoverVersion *string         `json:"cover_version"`
	Type         string          `json:"type"`
	Order        *int            `json:"order"`
	OrderParts   []*float32      `json:"order_parts"`
	Meta         json.RawMessage `json:"meta"`
	FileData     json.RawMessage `json:"file_data"`
	ParentID     *string         `json:"parent_id"`
	LibraryID    string          `json:"library_id"`
	childCounts
	UserData *UserToContentDTO `json:"user_data"`
	Length   *ContentLength    `json:"length,omitempty"`
	Continue *ContinueDTO      `json:"continue"`
	// FacetKeys holds the value-page keys of meta's staff, genres, tags and publishers, index for
	// index, or null where a value has no page. Only get sets it, for valid roots.
	FacetKeys json.RawMessage `json:"facet_keys,omitempty"`
}

// childCounts counts a series' valid children. new_children_count is the unfinished volumes
// added since the user completed the series, or 0 past newGuard.
type childCounts struct {
	ChildrenCount          *int `json:"children_count"           db:"children_count"`
	UnreadChildrenCount    *int `json:"unread_children_count"    db:"unread_children_count"`
	CompletedChildrenCount *int `json:"completed_children_count" db:"completed_children_count"`
	DroppedChildrenCount   *int `json:"dropped_children_count"   db:"dropped_children_count"`
	NewChildrenCount       *int `json:"new_children_count"       db:"new_children_count"`
}

// ContinueDTO is set on the rows of the continue and recently_updated sorts.
type ContinueDTO struct {
	Action string      `json:"action"`
	IsNew  bool        `json:"is_new"`
	Series *ContentDTO `json:"series"`
}

// ContentLength is in words for books and pages for comics; the frontend converts it to time.
type ContentLength struct {
	Unit      string `json:"unit"`
	Total     int64  `json:"total"`
	Remaining int64  `json:"remaining"`
}

type contentDTOOpts struct {
	meta            json.RawMessage
	counts          childCounts
	userToContent   *models.UserToContent
	includeFileData bool
	includeMeta     bool
}

func contentToDTO(c models.Content, opts contentDTOOpts) ContentDTO {
	meta := json.RawMessage("{}")
	if opts.includeMeta && opts.meta != nil {
		meta = opts.meta
	}
	fileData := json.RawMessage("{}")
	if opts.includeFileData && c.FileData != nil {
		fileData = c.FileData
	}

	var m struct {
		Title string             `json:"title"`
		Cover *metadata.CoverRef `json:"cover"`
	}
	if opts.meta != nil {
		_ = json.Unmarshal(opts.meta, &m)
	}

	orderParts := c.OrderParts
	if orderParts == nil {
		orderParts = []*float32{}
	}

	return ContentDTO{
		ID:           c.ID,
		CreatedAt:    c.CreatedAt,
		UpdatedAt:    c.UpdatedAt,
		URIPart:      c.URIPart,
		URI:          c.URI,
		Title:        m.Title,
		Valid:        c.Valid,
		FileURI:      c.FileURI,
		FileMtime:    c.FileMtime,
		FileSize:     c.FileSize,
		CoverURI:     c.CoverURI,
		CoverVersion: covers.Version(m.Cover, c.CoverURI != nil, c.FileMtime),
		Type:         c.Type,
		Order:        c.Order,
		OrderParts:   orderParts,
		Meta:         meta,
		FileData:     fileData,
		ParentID:     c.ParentID,
		LibraryID:    c.LibraryID,
		childCounts:  opts.counts,
		UserData:     utcToDTO(opts.userToContent),
	}
}

// dto maps a hydrated row, with its continue info when it has one.
func (r contentListRow) dto(includeFileData, includeMeta bool) ContentDTO {
	dto := contentToDTO(r.Content, contentDTOOpts{
		meta:            r.MetaData,
		counts:          r.childCounts,
		userToContent:   r.utc(),
		includeFileData: includeFileData,
		includeMeta:     includeMeta,
	})
	if r.Continue != nil {
		dto.Continue = &ContinueDTO{Action: r.Continue.Action, IsNew: r.Continue.IsNew}
		if r.Continue.Series != nil {
			series := r.Continue.Series.dto(false, false)
			dto.Continue.Series = &series
		}
	}
	return dto
}

// childCountsJoin counts the valid children of content c in cc, for @user_id, with utc joined.
const childCountsJoin = `
	LEFT JOIN LATERAL (
		SELECT COUNT(*) AS children_count,
			COUNT(*) FILTER (WHERE child_utc.status = 'completed') AS completed_children_count,
			COUNT(*) FILTER (WHERE child_utc.status = 'dropped') AS dropped_children_count,
			COUNT(*) FILTER (WHERE utc.status = 'completed' AND child.created_at > utc.status_updated_at
				AND (child_utc.status IS NULL OR child_utc.status NOT IN ('completed', 'dropped'))) AS new_count
		FROM content child
		LEFT JOIN user_to_content child_utc ON child_utc.library_id = child.library_id
			AND child_utc.uri = child.uri AND child_utc.user_id = @user_id
		WHERE child.parent_id = c.id AND child.valid
	) cc ON true`

// contentRowColumns selects a contentListRow from content c, user_to_content utc (for @user_id)
// and childCountsJoin.
var contentRowColumns = models.ContentColumns("c") + `,
	cc.children_count, cc.completed_children_count, cc.dropped_children_count,
	cc.children_count - cc.completed_children_count - cc.dropped_children_count AS unread_children_count,
	CASE WHEN ` + newGuard("cc.new_count", "cc.children_count") + ` THEN cc.new_count ELSE 0 END
		AS new_children_count,
	utc.id AS utc_id, utc.user_id AS utc_user_id, utc.library_id AS utc_library_id,
	utc.uri AS utc_uri, utc.starred AS utc_starred, utc.status AS utc_status,
	utc.status_updated_at AS utc_status_updated_at, utc.notes AS utc_notes,
	utc.rating AS utc_rating, utc.progress AS utc_progress,
	utc.progress_updated_at AS utc_progress_updated_at, utc.revision AS utc_revision,
	utc.last_read_at AS utc_last_read_at, utc.reading_seq AS utc_reading_seq, c.data AS meta_data, c.meta_updated_at`

func selectContentRows(ctx context.Context, q db.Querier, userID string, ids []string) (map[string]contentListRow, error) {
	if len(ids) == 0 { // ANY(NULL) fails pg_search's pushdown
		return map[string]contentListRow{}, nil
	}
	rows, err := db.Select[contentListRow](ctx, q, `
		SELECT `+contentRowColumns+`
		FROM content c`+utcJoin+childCountsJoin+`
		WHERE c.id = ANY(@ids)
	`, pgx.NamedArgs{"user_id": userID, "ids": ids})
	if err != nil {
		return nil, err
	}
	byID := make(map[string]contentListRow, len(rows))
	for _, r := range rows {
		byID[r.ID] = r
	}
	return byID, nil
}

func (cr *ContentRoutes) get(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	r, err := db.SelectOne[contentListRow](ctx, cr.pool, `
		SELECT `+contentRowColumns+`
		FROM content c`+utcJoin+childCountsJoin+`
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": user.ID, "id": contentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	if c.QueryParam("page_sizes") == "1" && r.Type == "comic" && r.FileURI != nil && !pagesSized(r.FileData) {
		fd, err := pageSizesFor(ctx, cr.pool, r.ID)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			names, err := comicPageNames(r.FileData)
			if err != nil {
				return err
			}
			fd, err = sizedFileData(r.FileData, fp.Map(names, func(n string) comic.PageInfo { return comic.PageInfo{Name: n} }))
			if err != nil {
				return err
			}
		}
		r.FileData = fd
	}

	length, err := contentLength(ctx, cr.pool, user.ID, r.Content, r.UTCStatus)
	if err != nil {
		return err
	}

	dto := r.dto(true, true)
	dto.Length = length
	if r.ParentID == nil && r.Valid {
		dto.FacetKeys, err = db.SelectScalar[json.RawMessage](ctx, cr.pool, "SELECT public.facet_links($1::jsonb)", r.MetaData)
		if err != nil {
			return err
		}
	}
	return c.JSON(http.StatusOK, dto)
}

var (
	pageSizesGroup singleflight.Group
	// Swapped in tests.
	pageSizes        = comic.PageSizes
	pageSizesTimeout = 5 * time.Minute
)

type pageSizesRow struct {
	FileURI   string       `db:"file_uri"`
	FileMtime *time.Time   `db:"file_mtime"`
	FileSize  *int         `db:"file_size"`
	FileData  models.JSONB `db:"file_data"`
}

// pageSizesFor runs one flight per comic, which outlives cancelled callers so its result is kept.
func pageSizesFor(ctx context.Context, pool *pgxpool.Pool, contentID string) (models.JSONB, error) {
	ch := pageSizesGroup.DoChan(contentID, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pageSizesTimeout)
		defer cancel()
		row, err := db.SelectOne[pageSizesRow](ctx, pool, `
			SELECT file_uri, file_mtime, file_size, file_data FROM content WHERE id = $1 AND file_uri IS NOT NULL
		`, contentID)
		if err == nil && !pagesSized(row.FileData) {
			row.FileData, err = savePageSizes(ctx, pool, contentID, row)
		}
		if err != nil {
			slog.Warn("compute comic page sizes", "content_id", contentID, "err", err)
		}
		return row.FileData, err
	})
	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(models.JSONB), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func savePageSizes(ctx context.Context, pool *pgxpool.Pool, contentID string, row pageSizesRow) (models.JSONB, error) {
	names, err := comicPageNames(row.FileData)
	if err != nil {
		return nil, err
	}
	pages, err := pageSizes(ctx, row.FileURI, names)
	if err != nil {
		return nil, err
	}
	fd, err := sizedFileData(row.FileData, pages)
	if err != nil {
		return nil, err
	}
	_, err = pool.Exec(ctx, `
		UPDATE content SET file_data = $5
		WHERE id = $1 AND file_uri = $2
			AND file_mtime IS NOT DISTINCT FROM $3 AND file_size IS NOT DISTINCT FROM $4
			AND file_data = $6
	`, contentID, row.FileURI, row.FileMtime, row.FileSize, fd, row.FileData)
	return fd, err
}

func pagesSized(fileData []byte) bool {
	var fd struct {
		Pages [][]json.RawMessage `json:"pages"`
	}
	_ = json.Unmarshal(fileData, &fd)
	return !slices.ContainsFunc(fd.Pages, func(p []json.RawMessage) bool { return len(p) < 3 })
}

// sizedFileData replaces file_data's pages with [name, width, height] tuples, keeping other keys.
func sizedFileData(fileData []byte, pages []comic.PageInfo) (models.JSONB, error) {
	var fd map[string]any
	if err := json.Unmarshal(fileData, &fd); err != nil {
		return nil, err
	}
	fd["pages"] = fp.Map(pages, func(p comic.PageInfo) any { return []any{p.Name, p.Width, p.Height} })
	return json.Marshal(fd)
}

// contentLength sums the length of a leaf, or of a series' valid children, and what the user has
// left of it given the item's own status. It returns nil when any counted item lacks a count.
func contentLength(ctx context.Context, pool *pgxpool.Pool, userID string, c models.Content,
	status *string) (*ContentLength, error) {
	row, err := db.SelectOne[struct {
		Missing   int64 `db:"missing"`
		Total     int64 `db:"total"`
		Remaining int64 `db:"remaining"`
	}](ctx, pool, `
		SELECT
			COUNT(*) FILTER (WHERE l.n IS NULL) AS missing,
			COALESCE(SUM(l.n), 0)::bigint AS total,
			COALESCE(ROUND(SUM(CASE
				WHEN utc.status IN ('completed', 'dropped') THEN 0
				WHEN x.type = 'comic' AND jsonb_typeof(utc.progress->'current_page') = 'number' THEN
					GREATEST(x.page_count - (utc.progress->>'current_page')::float8, 0)
				WHEN x.type = 'book' AND jsonb_typeof(utc.progress->'progress_percent') = 'number' THEN
					l.n * (1 - LEAST(GREATEST((utc.progress->>'progress_percent')::float8, 0), 100) / 100)
				ELSE l.n
			END)), 0)::bigint AS remaining
		FROM content x
		CROSS JOIN LATERAL (SELECT COALESCE(x.word_count, x.page_count) AS n) l
		LEFT JOIN user_to_content utc
			ON utc.library_id = x.library_id AND utc.uri = x.uri AND utc.user_id = @user_id
		WHERE x.valid AND x.type IN ('book', 'comic') AND (x.id = @id OR x.parent_id = @id)
	`, pgx.NamedArgs{"user_id": userID, "id": c.ID})
	if err != nil || row.Missing > 0 || row.Total == 0 {
		return nil, err
	}
	unit := "words"
	if c.Type == "comic" || c.Type == "comic_series" {
		unit = "pages"
	}
	// A series' own status is set without touching its children.
	if status != nil && (*status == "completed" || *status == "dropped") {
		row.Remaining = 0
	}
	return &ContentLength{Unit: unit, Total: row.Total, Remaining: row.Remaining}, nil
}

func (cr *ContentRoutes) listsForContent(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

	rows, err := cr.pool.Query(ctx, `
		SELECT cl.id FROM custom_lists cl
		JOIN custom_list_to_content clc ON clc.custom_list_id = cl.id
		WHERE cl.user_id = $1 AND clc.library_id = $2 AND clc.uri = $3
		ORDER BY cl.created_at DESC
	`, user.ID, content.LibraryID, content.URI)
	if err != nil {
		return err
	}

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if ids == nil {
		ids = []string{}
	}
	return c.JSON(http.StatusOK, ids)
}

type contentListQuery struct {
	ParentID      string   `query:"parent_id"`
	LibraryID     string   `query:"library_id"`
	Type          []string `query:"type"`
	Valid         string   `query:"valid"           validate:"omitempty,oneof=true false"`
	ReadingStatus string   `query:"reading_status"  validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
	Starred       string   `query:"starred"         validate:"omitempty,oneof=true false"`
	HasStatus     string   `query:"has_status"      validate:"omitempty,oneof=true false"`
	HasRating     string   `query:"has_rating"      validate:"omitempty,oneof=true false"`
	Search        string   `query:"search"          validate:"max=200"`
	Limit         *int     `query:"limit"           validate:"omitempty,min=0"`
	Offset        int      `query:"offset"          validate:"min=0"`
	Sort          string   `query:"sort"            validate:"omitempty,oneof=last_read_at progress_updated_at history continue recently_updated created_at order rating user_rating unread_children_count release_date title relevance"`
	SortOrder     string   `query:"sort_order"      validate:"omitempty,oneof=asc desc" default:"desc"`
	Count         string   `query:"count"           validate:"omitempty,oneof=true false"`
	Include       string   `query:"include"`
	// omitempty stops validation of an empty field, so required_with goes first.
	FacetKind string `query:"facet_kind" validate:"required_with=Facet,omitempty,oneof=genres tags people publishers"`
	Facet     string `query:"facet"      validate:"required_with=FacetKind,omitempty,max=1000"`
	FacetRole string `query:"facet_role" validate:"omitempty,max=200,excluded_without=Facet"`
	ListID    string // set by code only
	// IgnoreSeriesStatus is the user's home preference, set by code for the continue sorts.
	IgnoreSeriesStatus bool
}

// filter is the query's filter. The history, continue and recently_updated sorts also restrict
// the rows; the last two list continue targets, wherever they sit, so they ignore parent_id.
func (q contentListQuery) filter() contentFilter {
	f := contentFilter{
		ParentID:           q.ParentID,
		LibraryID:          q.LibraryID,
		Type:               q.Type,
		Valid:              optBool(q.Valid),
		ReadingStatus:      q.ReadingStatus,
		Starred:            optBool(q.Starred),
		HasStatus:          optBool(q.HasStatus),
		HasRating:          optBool(q.HasRating),
		Search:             strings.TrimSpace(q.Search),
		ListID:             q.ListID,
		IgnoreSeriesStatus: q.IgnoreSeriesStatus,
		FacetKind:          facetKinds[q.FacetKind],
		Facet:              q.Facet,
		FacetRole:          q.FacetRole,
	}
	switch q.Sort {
	case "history":
		f.LastReadOnly = true
	case "continue":
		f.Continue, f.ParentID = true, ""
	case "recently_updated":
		f.RecentlyUpdated, f.ParentID = true, ""
	}
	return f
}

// order resolves the sort: an explicit one; else relevance for a search, list order in a list, or
// title asc (sort_order defaults to desc, so it is ignored). progress_updated_at is the old name
// of last_read_at, and history sorts by it.
func (q contentListQuery) order() (sort, dir string) {
	switch {
	case q.Sort == "progress_updated_at" || q.Sort == "history":
		return "last_read_at", q.SortOrder
	case q.Sort != "" && q.Sort != "relevance":
		return q.Sort, q.SortOrder
	case strings.TrimSpace(q.Search) != "":
		return "relevance", ""
	case q.ListID != "":
		return "list", "asc"
	}
	return "title", "asc"
}

// optBool parses a validated "true" or "false"; "" is nil.
func optBool(s string) *bool {
	if s == "" {
		return nil
	}
	b := s == "true"
	return &b
}

// contentFilter is the content list's filter, shared by the grid endpoints and OPDS.
type contentFilter struct {
	ParentID      string
	LibraryID     string
	Type          []string
	Valid         *bool
	ReadingStatus string
	Starred       *bool
	HasStatus     *bool
	HasRating     *bool
	Search        string
	ListID        string // joins the list's entries as clc
	LastReadOnly  bool   // only content the user has read
	// Continue keeps only continue targets, joined as ct; RecentlyUpdated only the new ones.
	Continue, RecentlyUpdated bool
	IgnoreSeriesStatus        bool
	// FacetKind is content_facets' singular kind; Facet any spelling of the value; FacetRole a
	// role the value must carry.
	FacetKind, Facet, FacetRole string
}

// joins are the optional joins of a grid query.
type joins struct{ utc, unread, lastRead bool }

func (j joins) or(o joins) joins {
	return joins{j.utc || o.utc, j.unread || o.unread, j.lastRead || o.lastRead}
}

const (
	utcJoin = `
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id`
	listJoin = `
		JOIN custom_list_to_content clc ON clc.custom_list_id = @list_id
			AND clc.library_id = c.library_id AND clc.uri = c.uri`
	// unreadJoin groups every child once, where a correlated subquery would run per sorted row.
	unreadJoin = `
		LEFT JOIN (
			SELECT child.parent_id, COUNT(*) FILTER (WHERE child_utc.status IS NULL
				OR child_utc.status NOT IN ('completed', 'dropped')) AS unread
			FROM content child
			LEFT JOIN user_to_content child_utc ON child_utc.library_id = child.library_id
				AND child_utc.uri = child.uri AND child_utc.user_id = @user_id
			WHERE child.parent_id IS NOT NULL AND child.valid%s
			GROUP BY child.parent_id
		) uc ON uc.parent_id = c.id`
	// lastReadSQL is when the user last read each item, or for a series its children's latest
	// (else its own), as lr. It starts from the user's rows, not the catalog, so the history,
	// which joins it, reads only what the user has read. The group key is an expression over
	// content's columns: grouping by a VALUES column is estimated at 2 rows.
	lastReadSQL = `
		SELECT r.id, COALESCE(MAX(cu.last_read_at) FILTER (WHERE v.child),
			MAX(cu.last_read_at) FILTER (WHERE NOT v.child)) AS last_read_at
		FROM user_to_content cu
		JOIN content k ON k.library_id = cu.library_id AND k.uri = cu.uri
		CROSS JOIN (VALUES (false), (true)) v(child)
		CROSS JOIN LATERAL (SELECT CASE WHEN v.child THEN k.parent_id ELSE k.id END AS id) r
		WHERE cu.user_id = @user_id AND cu.last_read_at IS NOT NULL AND r.id IS NOT NULL
		GROUP BY r.id`
	lastReadJoin = " LEFT JOIN (" + lastReadSQL + ") lr ON lr.id = c.id"
	historyFrom  = "FROM (" + lastReadSQL + ") lr JOIN content c ON c.id = lr.id"
	lastReadExpr = "lr.last_read_at"
)

// where appends the filter's conditions and args, and returns the joins they read.
func (f contentFilter) where(args pgx.NamedArgs) (cond string, j joins) {
	needsUTC := false
	args["valid"] = f.Valid == nil || *f.Valid
	where := []string{"c.valid = @valid"}

	switch f.ParentID {
	case "":
	case "null":
		where = append(where, "c.parent_id IS NULL")
	default:
		args["parent_id"] = f.ParentID
		where = append(where, "c.parent_id = @parent_id")
	}
	if f.LibraryID != "" {
		args["library_id"] = f.LibraryID
		where = append(where, "c.library_id = @library_id")
	}
	if len(f.Type) > 0 {
		args["types"] = f.Type
		where = append(where, "c.type = ANY(@types)")
	}
	if f.ReadingStatus != "" {
		args["reading_status"] = f.ReadingStatus
		where = append(where, "utc.status = @reading_status")
		needsUTC = true
	}
	if f.Starred != nil {
		args["starred"] = *f.Starred
		where = append(where, "utc.starred = @starred")
		needsUTC = true
	}
	if f.HasStatus != nil {
		where = append(where, fmt.Sprintf("(utc.status IS NOT NULL) = %t", *f.HasStatus))
		needsUTC = true
	}
	if f.HasRating != nil {
		where = append(where, fmt.Sprintf("(utc.rating IS NOT NULL) = %t", *f.HasRating))
		needsUTC = true
	}
	if f.Search != "" {
		args["search"] = f.Search
		where = append(where, metadata.TitleMatch("c", f.ParentID == "null"))
	}
	if f.RecentlyUpdated {
		where = append(where, "ct.is_new AND ct.series_status = 'reading'")
	}
	if f.Facet != "" {
		args["facet_kind"], args["facet"] = f.FacetKind, f.Facet
		sub := "SELECT cf.content_id FROM content_facets cf WHERE cf.kind = @facet_kind AND cf.key = public.facet_key(@facet)"
		if f.FacetRole != "" {
			args["facet_role"] = f.FacetRole
			sub += " AND @facet_role = ANY(cf.roles)"
		}
		where = append(where, "c.parent_id IS NULL AND c.id IN ("+sub+")")
	}
	j.utc = needsUTC
	return strings.Join(where, " AND "), j
}

// queryArgs are the arguments of a query built on f.from. A facet filter skips the statement cache
// so each run is planned for its key; a generic plan sizes every key at the kind's average.
func (f contentFilter) queryArgs(args pgx.NamedArgs) []any {
	if f.Facet == "" {
		return []any{args}
	}
	return []any{pgx.QueryExecModeDescribeExec, args}
}

// from returns the FROM and WHERE clauses of a grid query, with the joins that the filter or the
// caller reads.
func (f contentFilter) from(args pgx.NamedArgs, j joins) string {
	cond, fj := f.where(args)
	j = j.or(fj)
	from := "FROM content c"
	if f.LastReadOnly {
		from = historyFrom
	}
	if f.Continue || f.RecentlyUpdated {
		args["ignore_series_status"] = f.IgnoreSeriesStatus
		from += " JOIN (" + continueTargetsSQL + ") ct ON ct.target_id = c.id"
	}
	if f.ListID != "" {
		args["list_id"] = f.ListID
		from += listJoin
	}
	if j.utc {
		from += utcJoin
	}
	if j.unread {
		lib := ""
		if f.LibraryID != "" {
			lib = " AND child.library_id = @library_id"
		}
		from += fmt.Sprintf(unreadJoin, lib)
	}
	if j.lastRead && !f.LastReadOnly {
		from += lastReadJoin
	}
	return from + " WHERE " + cond
}

// nullsOrder places NULLs as the list always has: first ascending, last descending.
func nullsOrder(dir string) string {
	if dir == "asc" {
		return "NULLS FIRST"
	}
	return "NULLS LAST"
}

// contentOrder returns the ORDER BY for a resolved sort (see contentListQuery.order), always
// ending in c.id, and the joins it reads. The continue sorts read ct, which their filter joins.
func contentOrder(sort, dir string) (clause string, j joins) {
	col, nullable := "", true
	switch sort {
	case "relevance":
		return metadata.TitleScore("c") + ` DESC, c.search_title_len, c.sort_title, c.id COLLATE "C"`, j
	case "list":
		return `(clc."order" IS NULL), clc."order", clc.created_at, c.id`, j
	case "continue":
		return continueOrder(dir), j
	case "recently_updated":
		return "ct.new_added_at " + dir + " " + nullsOrder(dir) + ", ct.target_id " + dir, j
	case "title":
		col = "c.sort_title"
	case "rating":
		col = "c.rating"
	case "release_date":
		col = "c.release_date"
	case "user_rating":
		col, j.utc = "utc.rating", true
	case "last_read_at":
		col, j.lastRead = lastReadExpr, true
	case "created_at":
		col, nullable = "c.created_at", false
	case "order":
		col, nullable = `c."order"`, false
	case "unread_children_count":
		col, nullable, j.unread = "COALESCE(uc.unread, 0)", false, true
	}
	clause = col + " " + dir
	if nullable {
		clause += " " + nullsOrder(dir)
	}
	return clause + ", c.id " + dir, j
}

// listContentIDs sorts and paginates the filtered ids, reading only the joins the filter and the
// order need; a nil limit means all, or maxContentIDs for a search.
func listContentIDs(ctx context.Context, q db.Querier, userID string, f contentFilter,
	sort, dir string, limit *int, offset int,
) ([]string, error) {
	args := pgx.NamedArgs{"user_id": userID}
	return db.SelectScalars[string](ctx, q, contentIDsSQL("c.id", f, sort, dir, limit, offset, args), f.queryArgs(args)...)
}

// continueID is a row of the continue sorts, with its target info.
type continueID struct {
	ID       string  `db:"id"`
	Action   string  `db:"action"`
	IsNew    bool    `db:"is_new"`
	SeriesID *string `db:"series_id"`
}

func listContinueIDs(ctx context.Context, q db.Querier, userID string, f contentFilter,
	sort, dir string, limit *int, offset int,
) ([]continueID, error) {
	args := pgx.NamedArgs{"user_id": userID}
	return db.Select[continueID](ctx, q,
		contentIDsSQL("c.id, ct.action, ct.is_new, ct.series_id", f, sort, dir, limit, offset, args), f.queryArgs(args)...)
}

func contentIDsSQL(cols string, f contentFilter, sort, dir string, limit *int, offset int, args pgx.NamedArgs) string {
	order, j := contentOrder(sort, dir)
	sql := "SELECT " + cols + " " + f.from(args, j) + " ORDER BY " + order
	if limit == nil && f.Search != "" {
		// pg_search 0.25.10's scan fails a generic plan without a LIMIT ("unrecognized node type"),
		// and allocates its top-k heap by the LIMIT, so it can't be huge either.
		limit = new(maxContentIDs)
	}
	if limit != nil {
		sql += fmt.Sprintf(" LIMIT %d", *limit)
	}
	if offset > 0 {
		sql += fmt.Sprintf(" OFFSET %d", offset)
	}
	return sql
}

func countContent(ctx context.Context, q db.Querier, userID string, f contentFilter) (int, error) {
	args := pgx.NamedArgs{"user_id": userID}
	return db.SelectScalar[int](ctx, q, "SELECT COUNT(*) "+f.from(args, joins{}), f.queryArgs(args)...)
}

// SearchEvalResult is one root-level search's first page of ids, its total and the time each took.
type SearchEvalResult struct {
	IDs                 []string
	Total               int
	PageTime, CountTime time.Duration
}

// SearchEvalQuery runs a root-level content search, as the header (libraryID "") or a library's
// grid runs it, for the relevance eval.
func SearchEvalQuery(ctx context.Context, q db.Querier, search, libraryID string, limit int) (SearchEvalResult, error) {
	lq := contentListQuery{Search: search, LibraryID: libraryID, ParentID: "null"}
	f := lq.filter()
	sort, dir := lq.order()
	start := time.Now()
	ids, err := listContentIDs(ctx, q, "", f, sort, dir, &limit, 0)
	if err != nil {
		return SearchEvalResult{}, err
	}
	pageTime, start := time.Since(start), time.Now()
	total, err := countContent(ctx, q, "", f)
	if err != nil {
		return SearchEvalResult{}, err
	}
	return SearchEvalResult{ids, total, pageTime, time.Since(start)}, nil
}

type contentPageResponse struct {
	Data  []ContentDTO `json:"data"`
	Total *int         `json:"total"`
}

func (cr *ContentRoutes) list(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	q, err := BindQuery[contentListQuery](c)
	if err != nil {
		return err
	}
	q.IgnoreSeriesStatus = homePrefs(user.Preferences)

	includes := map[string]bool{}
	for part := range strings.SplitSeq(q.Include, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			includes[part] = true
		}
	}

	g, ctx := errgroup.WithContext(reqCtx(c))
	var total *int
	if q.Count != "false" {
		g.Go(func() error {
			n, err := countContent(ctx, cr.pool, user.ID, q.filter())
			total = &n
			return err
		})
	}
	var rows []contentListRow
	g.Go(func() (err error) {
		rows, err = listContent(ctx, cr.pool, user.ID, q)
		return err
	})
	if err := g.Wait(); err != nil {
		return err
	}

	dtos := make([]ContentDTO, len(rows))
	for i, r := range rows {
		dtos[i] = r.dto(includes["file_data"], includes["meta"])
	}

	return c.JSON(http.StatusOK, contentPageResponse{Data: dtos, Total: total})
}

const maxContentIDs = 200_000

func (cr *ContentRoutes) ids(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	q, err := BindQuery[contentListQuery](c)
	if err != nil {
		return err
	}
	if q.Limit == nil || *q.Limit > maxContentIDs {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("limit must be at most %d", maxContentIDs))
	}
	q.IgnoreSeriesStatus = homePrefs(user.Preferences)

	sort, dir := q.order()
	ids, err := listContentIDs(reqCtx(c), cr.pool, user.ID, q.filter(), sort, dir, q.Limit, q.Offset)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string][]string{"ids": ids})
}

// bucketKey groups a resolved sort's rows into rail segments. Each key is a prefix of the sort
// key (or monotonic in it), so segments are contiguous in the list. It is "" for sorts without
// segments.
func bucketKey(sort string) (key string, j joins) {
	year := func(col string) string { return "to_char(" + col + " AT TIME ZONE 'UTC', 'YYYY')" }
	switch sort {
	case "title":
		// Rows without metadata sort next to '#'.
		return "COALESCE(left(c.sort_title, 1), '#')", j
	case "created_at":
		return year("c.created_at"), j
	case "last_read_at":
		return year(lastReadExpr), joins{lastRead: true}
	case "continue":
		return year("ct.recency"), j
	case "recently_updated":
		return year("ct.new_added_at"), j
	case "release_date":
		return "left(c.release_date, 4)", j
	}
	return "", j
}

type contentBucket struct {
	Key   *string `json:"key"   db:"key"`
	Count int     `json:"count" db:"count"`
}

type contentBucketsResponse struct {
	Total   int             `json:"total"`
	Buckets []contentBucket `json:"buckets"`
}

func (cr *ContentRoutes) buckets(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	ctx := reqCtx(c)
	q, err := BindQuery[contentListQuery](c)
	if err != nil {
		return err
	}
	q.IgnoreSeriesStatus = homePrefs(user.Preferences)

	f := q.filter()
	sort, dir := q.order()
	key, j := bucketKey(sort)
	res := contentBucketsResponse{Buckets: []contentBucket{}}
	if key == "" {
		res.Total, err = countContent(ctx, cr.pool, user.ID, f)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, res)
	}

	args := pgx.NamedArgs{"user_id": user.ID}
	res.Buckets, err = db.Select[contentBucket](ctx, cr.pool, fmt.Sprintf(
		"SELECT %s AS key, COUNT(*) AS count %s GROUP BY 1 ORDER BY 1 %s %s",
		key, f.from(args, j), dir, nullsOrder(dir)), f.queryArgs(args)...)
	if err != nil {
		return err
	}
	for _, b := range res.Buckets {
		res.Total += b.Count
	}
	return c.JSON(http.StatusOK, res)
}

type kindCounts struct{ Series, Comics, Books int }

func (k kindCounts) items() int { return k.Comics + k.Books }

// kindCountColumns selects kindCounts over content c.
const kindCountColumns = `COUNT(*) FILTER (WHERE c.type IN ('comic_series', 'book_series')) AS series,
	COUNT(*) FILTER (WHERE c.type = 'comic') AS comics,
	COUNT(*) FILTER (WHERE c.type = 'book') AS books`

func countContentKinds(ctx context.Context, pool *pgxpool.Pool, userID string, f contentFilter) (k kindCounts, err error) {
	args := pgx.NamedArgs{"user_id": userID}
	sql := "SELECT " + kindCountColumns + " " + f.from(args, joins{})
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		// pg_search 0.25.10's aggregate scan miscounts FILTER aggregates under generic plans.
		if _, err := tx.Exec(ctx, "SET LOCAL paradedb.enable_aggregate_custom_scan = off"); err != nil {
			return err
		}
		k, err = db.SelectOne[kindCounts](ctx, tx, sql, f.queryArgs(args)...)
		return err
	})
	return k, err
}

// listContent runs one page of lq: the ids query sorts and pages, then only those rows hydrate.
// Rows of the continue sorts carry their continue info, with the series hydrated alongside.
func listContent(ctx context.Context, q db.Querier, userID string, lq contentListQuery) ([]contentListRow, error) {
	sort, dir := lq.order()
	f := lq.filter()
	var cids []continueID
	var ids []string
	var err error
	if f.Continue || f.RecentlyUpdated {
		if cids, err = listContinueIDs(ctx, q, userID, f, sort, dir, lq.Limit, lq.Offset); err != nil {
			return nil, err
		}
		for _, r := range cids {
			ids = append(ids, r.ID)
			if r.SeriesID != nil {
				ids = append(ids, *r.SeriesID)
			}
		}
	} else if ids, err = listContentIDs(ctx, q, userID, f, sort, dir, lq.Limit, lq.Offset); err != nil {
		return nil, err
	}
	byID, err := selectContentRows(ctx, q, userID, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]contentListRow, 0, len(ids))
	if cids == nil {
		for _, id := range ids {
			if r, ok := byID[id]; ok { // gone if deleted since the ids query
				rows = append(rows, r)
			}
		}
		return rows, nil
	}
	for _, cid := range cids {
		// A scan may have deleted, invalidated or reparented the target since the ids query.
		r, ok := byID[cid.ID]
		if !ok || !r.Valid || !fp.PtrEq(r.ParentID, cid.SeriesID) {
			continue
		}
		r.Continue = &continueInfo{Action: cid.Action, IsNew: cid.IsNew}
		if cid.SeriesID != nil {
			series, ok := byID[*cid.SeriesID]
			if !ok {
				continue
			}
			r.Continue.Series = &series
		}
		rows = append(rows, r)
	}
	return rows, nil
}

// queryContent runs one page of lq with its total. counts, when non-nil, holds countContentKinds
// for the same filter and saves the count query.
func queryContent(ctx context.Context, pool *pgxpool.Pool, userID string, lq contentListQuery, counts *kindCounts) ([]contentListRow, int, error) {
	if counts == nil {
		k, err := countContentKinds(ctx, pool, userID, lq.filter())
		if err != nil {
			return nil, 0, err
		}
		counts = &k
	}
	rows, err := listContent(ctx, pool, userID, lq)
	return rows, counts.Series + counts.items(), err
}

type userToContentRequest struct {
	Starred *bool   `json:"starred"`
	Notes   *string `json:"notes"`
	Rating  *int    `json:"rating"  validate:"omitempty,min=1,max=10"`
}

// updateUserData sets the user's star, notes and rating. Status and progress go through the
// reading endpoints.
func (cr *ContentRoutes) updateUserData(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	// Parse raw JSON to detect which fields were sent
	body := c.Request().Body
	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(body).Decode(&rawBody); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	for _, field := range []string{"status", "progress"} {
		if _, ok := rawBody[field]; ok {
			return echo.NewHTTPError(http.StatusBadRequest, field+" is set through /reading")
		}
	}

	var req userToContentRequest
	rawFull, _ := json.Marshal(rawBody)
	if err := json.Unmarshal(rawFull, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	cols := []string{"id", "user_id", "library_id", "uri"}
	vals := []string{"@utc_id", "@user_id", "@library_id", "@uri"}
	sets := []string{"uri = EXCLUDED.uri"}
	args := pgx.NamedArgs{
		"utc_id":  models.MakeUserToContentID(),
		"user_id": user.ID,
	}
	set := func(col string, val any) {
		cols = append(cols, col)
		vals = append(vals, "@"+col)
		sets = append(sets, col+" = EXCLUDED."+col)
		args[col] = val
	}

	if _, ok := rawBody["starred"]; ok && req.Starred != nil {
		set("starred", *req.Starred)
	}
	if _, ok := rawBody["notes"]; ok {
		set("notes", req.Notes)
	}
	if _, ok := rawBody["rating"]; ok {
		set("rating", req.Rating)
	}

	var utc models.UserToContent
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if err := lockUserData(ctx, tx, user.ID, contentID); err != nil {
			return err
		}
		libraryID, uri, err := contentURI(ctx, tx, contentID)
		if err != nil {
			return err
		}
		args["library_id"], args["uri"] = libraryID, uri
		utc, err = db.SelectOne[models.UserToContent](ctx, tx, fmt.Sprintf(`
			INSERT INTO user_to_content (%s)
			VALUES (%s)
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET %s
			RETURNING *
		`, strings.Join(cols, ", "), strings.Join(vals, ", "), strings.Join(sets, ", ")), args)
		return err
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, utcToDTO(&utc))
}

type bulkUserDataRequest struct {
	IDs             []string `json:"ids"              validate:"required,min=1"`
	Action          string   `json:"action"           validate:"required,oneof=set_status reset"`
	Status          *string  `json:"status"           validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
	IncludeChildren bool     `json:"include_children"`
}

// bulkUserData sets or clears the user's status on the given content, as the reading commands
// do. Completing also ends the items, and with include_children the unfinished volumes of
// selected series; reset clears the content and the children of selected series.
func (cr *ContentRoutes) bulkUserData(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	var req bulkUserDataRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	ids := fp.Dedup(req.IDs)
	rev, now := db.ServerRevision(), time.Now().UTC()
	args := pgx.NamedArgs{"user_id": user.ID, "ids": ids, "status": req.Status, "now": now, "rev": rev}
	completing := req.Action == "set_status" && req.Status != nil && *req.Status == "completed"

	ctx := reqCtx(c)
	var count int
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if err := lockUserData(ctx, tx, user.ID, ids...); err != nil {
			return err
		}
		rows, err := db.Select[completionRow](ctx, tx,
			"SELECT "+completionColumns+" FROM content c WHERE c.id = ANY(@ids)", args)
		if err != nil {
			return err
		}
		count = len(rows)

		switch {
		case req.Action == "reset":
			return clearContent(ctx, tx, user.ID, ids, rev)
		case completing:
			if req.IncludeChildren {
				children, err := db.Select[completionRow](ctx, tx, unfinishedChildRows, args)
				if err != nil {
					return err
				}
				// A selected child is also a child of a selected series.
				rows = fp.Dedup(append(rows, children...))
			}
			if err := markCompleted(ctx, tx, user.ID, rows, rev, now); err != nil {
				return err
			}
		default:
			args["utc_ids"] = fp.Map(ids, func(string) string { return models.MakeUserToContentID() })
			_, err := tx.Exec(ctx, `
				INSERT INTO user_to_content AS utc (id, user_id, library_id, uri, status, status_updated_at, revision)
				SELECT r.id, @user_id, c.library_id, c.uri, @status, @now, @rev
				FROM unnest(@utc_ids::text[], @ids::text[]) r(id, cid)
				JOIN content c ON c.id = r.cid
				ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status,
					status_updated_at = CASE WHEN utc.status IS DISTINCT FROM EXCLUDED.status
						THEN EXCLUDED.status_updated_at ELSE utc.status_updated_at END,
					revision = EXCLUDED.revision
			`, args)
			if err != nil {
				return err
			}
			if req.Status == nil || *req.Status != "reading" {
				return nil
			}
		}
		// Reading or completed volumes start their series, after any series status set above.
		parents, err := db.SelectScalars[string](ctx, tx,
			"SELECT DISTINCT parent_id FROM content WHERE id = ANY(@ids) AND parent_id IS NOT NULL", args)
		if err != nil {
			return err
		}
		for _, p := range parents {
			if _, err := startSeries(ctx, tx, user.ID, p, rev, now, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]int{"count": count})
}

type contentWithUTCRow struct {
	models.Content
	UTCId                *string    `db:"utc_id"`
	UTCUserID            *string    `db:"utc_user_id"`
	UTCLibraryID         *string    `db:"utc_library_id"`
	UTCURI               *string    `db:"utc_uri"`
	UTCStarred           *bool      `db:"utc_starred"`
	UTCStatus            *string    `db:"utc_status"`
	UTCStatusUpdatedAt   *time.Time `db:"utc_status_updated_at"`
	UTCNotes             *string    `db:"utc_notes"`
	UTCRating            *int       `db:"utc_rating"`
	UTCProgress          []byte     `db:"utc_progress"`
	UTCProgressUpdatedAt *time.Time `db:"utc_progress_updated_at"`
	UTCRevision          *string    `db:"utc_revision"`
	UTCLastReadAt        *time.Time `db:"utc_last_read_at"`
	UTCReadingSeq        *int64     `db:"utc_reading_seq"`
	MetaData             []byte     `db:"meta_data"`
	MetaUpdatedAt        *time.Time `db:"meta_updated_at"`
}

func (r *contentWithUTCRow) utc() *models.UserToContent {
	if r.UTCId == nil {
		return nil
	}
	return &models.UserToContent{
		ID:                *r.UTCId,
		UserID:            *r.UTCUserID,
		LibraryID:         r.UTCLibraryID,
		URI:               *r.UTCURI,
		Starred:           *r.UTCStarred,
		Status:            r.UTCStatus,
		StatusUpdatedAt:   r.UTCStatusUpdatedAt,
		Notes:             r.UTCNotes,
		Rating:            r.UTCRating,
		Progress:          r.UTCProgress,
		ProgressUpdatedAt: r.UTCProgressUpdatedAt,
		Revision:          r.UTCRevision,
		LastReadAt:        r.UTCLastReadAt,
		ReadingSeq:        models.ReadingSeq(*r.UTCReadingSeq),
	}
}

type contentListRow struct {
	contentWithUTCRow
	childCounts
	Continue *continueInfo `db:"-"`
}

type continueInfo struct {
	Action string
	IsNew  bool
	Series *contentListRow
}

// lockUserData takes db.LockUserData for the libraries holding the content, in sorted order so
// that concurrent batches cannot deadlock.
func lockUserData(ctx context.Context, tx pgx.Tx, userID string, contentIDs ...string) error {
	libs, err := db.SelectScalars[string](ctx, tx,
		"SELECT DISTINCT library_id FROM content WHERE id = ANY($1) ORDER BY library_id", contentIDs)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if err := db.LockUserData(ctx, tx, userID, lib); err != nil {
			return err
		}
	}
	return nil
}

// contentURI reads the content's current URI; under lockUserData, a ref written with it
// cannot land on a URI a scan has since moved away from.
func contentURI(ctx context.Context, tx pgx.Tx, contentID string) (libraryID, uri string, err error) {
	err = tx.QueryRow(ctx, "SELECT library_id, uri FROM content WHERE id = $1", contentID).Scan(&libraryID, &uri)
	if errors.Is(err, pgx.ErrNoRows) {
		err = echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	return libraryID, uri, err
}

func getContent(ctx context.Context, pool *pgxpool.Pool, id string) (models.Content, error) {
	content, err := db.SelectOne[models.Content](ctx, pool, "SELECT "+models.ContentColumns("")+" FROM content WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Content{}, echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	return content, err
}
