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
	"golang.org/x/sync/singleflight"
)

type ContentRoutes struct {
	pool *pgxpool.Pool
}

func (cr *ContentRoutes) Register(g *echo.Group) {
	g.GET("", cr.list)
	g.GET("/recently-read", cr.recentlyRead)
	g.GET("/buckets", cr.buckets)
	g.GET("/ids", cr.ids)
	g.POST("/bulk/user-data", cr.bulkUserData)
	g.GET("/:content_id", cr.get)
	g.GET("/:content_id/lists", cr.listsForContent)
	g.POST("/:content_id/user-data", cr.updateUserData)
	g.POST("/:content_id/series-item-statuses", cr.setSeriesItemStatuses)
}

type UserToContentDTO struct {
	Starred           bool            `json:"starred"`
	Status            *string         `json:"status"`
	StatusUpdatedAt   *time.Time      `json:"status_updated_at"`
	Notes             *string         `json:"notes"`
	Rating            *int            `json:"rating"`
	Progress          json.RawMessage `json:"progress"`
	ProgressUpdatedAt *time.Time      `json:"progress_updated_at"`
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
	}
}

type ContentDTO struct {
	ID                  string            `json:"id"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	URIPart             string            `json:"uri_part"`
	Title               string            `json:"title"`
	Valid               bool              `json:"valid"`
	FileURI             *string           `json:"file_uri"`
	FileMtime           *time.Time        `json:"file_mtime"`
	FileSize            *int              `json:"file_size"`
	CoverURI            *string           `json:"cover_uri"`
	CoverVersion        *string           `json:"cover_version"`
	Type                string            `json:"type"`
	Order               *int              `json:"order"`
	OrderParts          []*float32        `json:"order_parts"`
	Meta                json.RawMessage   `json:"meta"`
	FileData            json.RawMessage   `json:"file_data"`
	ParentID            *string           `json:"parent_id"`
	LibraryID           string            `json:"library_id"`
	ChildrenCount       *int              `json:"children_count"`
	UnreadChildrenCount *int              `json:"unread_children_count"`
	UserData            *UserToContentDTO `json:"user_data"`
	Length              *ContentLength    `json:"length,omitempty"`
}

// ContentLength is in words for books and pages for comics; the frontend converts it to time.
type ContentLength struct {
	Unit      string `json:"unit"`
	Total     int64  `json:"total"`
	Remaining int64  `json:"remaining"`
}

type contentDTOOpts struct {
	meta                json.RawMessage
	childrenCount       *int
	unreadChildrenCount *int
	userToContent       *models.UserToContent
	includeFileData     bool
	includeMeta         bool
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
		ID:                  c.ID,
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
		URIPart:             c.URIPart,
		Title:               m.Title,
		Valid:               c.Valid,
		FileURI:             c.FileURI,
		FileMtime:           c.FileMtime,
		FileSize:            c.FileSize,
		CoverURI:            c.CoverURI,
		CoverVersion:        covers.Version(m.Cover, c.CoverURI != nil, c.FileMtime),
		Type:                c.Type,
		Order:               c.Order,
		OrderParts:          orderParts,
		Meta:                meta,
		FileData:            fileData,
		ParentID:            c.ParentID,
		LibraryID:           c.LibraryID,
		ChildrenCount:       opts.childrenCount,
		UnreadChildrenCount: opts.unreadChildrenCount,
		UserData:            utcToDTO(opts.userToContent),
	}
}

// unfinishedChildren selects the children of content c that @user_id hasn't completed or dropped.
const unfinishedChildren = `FROM content child
	LEFT JOIN user_to_content child_utc
		ON child_utc.library_id = child.library_id
		AND child_utc.uri = child.uri
		AND child_utc.user_id = @user_id
	WHERE child.parent_id = c.id
		AND (child_utc.status IS NULL OR child_utc.status NOT IN ('completed', 'dropped'))`

// contentRowColumns selects a contentListRow from content c and user_to_content utc (for @user_id).
var contentRowColumns = models.ContentColumns("c") + `,
	(SELECT COUNT(*) FROM content child WHERE child.parent_id = c.id) AS children_count,
	(SELECT COUNT(*) ` + unfinishedChildren + `) AS unread_children_count,
	utc.id AS utc_id, utc.user_id AS utc_user_id, utc.library_id AS utc_library_id,
	utc.uri AS utc_uri, utc.starred AS utc_starred, utc.status AS utc_status,
	utc.status_updated_at AS utc_status_updated_at, utc.notes AS utc_notes,
	utc.rating AS utc_rating, utc.progress AS utc_progress,
	utc.progress_updated_at AS utc_progress_updated_at,
	c.data AS meta_data, c.meta_updated_at`

func selectContentRows(ctx context.Context, q db.Querier, userID string, ids []string) (map[string]contentListRow, error) {
	rows, err := db.Select[contentListRow](ctx, q, `
		SELECT `+contentRowColumns+`
		FROM content c`+utcJoin+`
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
		FROM content c`+utcJoin+`
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

	dto := contentToDTO(r.Content, contentDTOOpts{
		meta:                r.MetaData,
		childrenCount:       r.ChildrenCount,
		unreadChildrenCount: r.UnreadChildrenCount,
		userToContent:       r.utc(),
		includeFileData:     true,
		includeMeta:         true,
	})
	dto.Length = length
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
	Sort          string   `query:"sort"            validate:"omitempty,oneof=progress_updated_at created_at order rating user_rating unread_children_count release_date title relevance"`
	SortOrder     string   `query:"sort_order"      validate:"omitempty,oneof=asc desc" default:"desc"`
	Count         string   `query:"count"           validate:"omitempty,oneof=true false"`
	Include       string   `query:"include"`
	ListID        string   // set by code only
}

func (q contentListQuery) filter() contentFilter {
	return contentFilter{
		ParentID:      q.ParentID,
		LibraryID:     q.LibraryID,
		Type:          q.Type,
		Valid:         optBool(q.Valid),
		ReadingStatus: q.ReadingStatus,
		Starred:       optBool(q.Starred),
		HasStatus:     optBool(q.HasStatus),
		HasRating:     optBool(q.HasRating),
		Search:        strings.TrimSpace(q.Search),
		ListID:        q.ListID,
	}
}

// order resolves the sort: an explicit one; else relevance for a search, list order in a list, or
// title asc (sort_order defaults to desc, so it is ignored).
func (q contentListQuery) order() (sort, dir string) {
	switch {
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
			WHERE child.parent_id IS NOT NULL%s
			GROUP BY child.parent_id
		) uc ON uc.parent_id = c.id`
)

// where appends the filter's conditions and args; needsUTC says whether it reads utc.
func (f contentFilter) where(args pgx.NamedArgs) (cond string, needsUTC bool) {
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
	return strings.Join(where, " AND "), needsUTC
}

// from returns the FROM and WHERE clauses of a grid query, with the joins that the filter or the
// caller (utc, unread) reads.
func (f contentFilter) from(args pgx.NamedArgs, utc, unread bool) string {
	cond, fUTC := f.where(args)
	from := "FROM content c"
	if f.ListID != "" {
		args["list_id"] = f.ListID
		from += listJoin
	}
	if utc || fUTC {
		from += utcJoin
	}
	if unread {
		lib := ""
		if f.LibraryID != "" {
			lib = " AND child.library_id = @library_id"
		}
		from += fmt.Sprintf(unreadJoin, lib)
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
// ending in c.id, and the joins it reads.
func contentOrder(sort, dir string) (clause string, needsUTC, needsUnread bool) {
	col, nullable := "", true
	switch sort {
	case "relevance":
		return metadata.TitleScore("c") + ` DESC, c.search_title_len, c.sort_title, c.id COLLATE "C"`, false, false
	case "list":
		return `(clc."order" IS NULL), clc."order", clc.created_at, c.id`, false, false
	case "title":
		col = "c.sort_title"
	case "rating":
		col = "c.rating"
	case "release_date":
		col = "c.release_date"
	case "user_rating":
		col, needsUTC = "utc.rating", true
	case "progress_updated_at":
		col, needsUTC = "utc.progress_updated_at", true
	case "created_at":
		col, nullable = "c.created_at", false
	case "order":
		col, nullable = `c."order"`, false
	case "unread_children_count":
		col, nullable, needsUnread = "COALESCE(uc.unread, 0)", false, true
	}
	clause = col + " " + dir
	if nullable {
		clause += " " + nullsOrder(dir)
	}
	return clause + ", c.id " + dir, needsUTC, needsUnread
}

// listContentIDs sorts and paginates the filtered ids, reading only the joins the filter and the
// order need; a nil limit means all, or maxContentIDs for a search.
func listContentIDs(ctx context.Context, q db.Querier, userID string, f contentFilter,
	sort, dir string, limit *int, offset int,
) ([]string, error) {
	args := pgx.NamedArgs{"user_id": userID}
	order, utc, unread := contentOrder(sort, dir)
	sql := "SELECT c.id " + f.from(args, utc, unread) + " ORDER BY " + order
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
	return db.SelectScalars[string](ctx, q, sql, args)
}

func countContent(ctx context.Context, q db.Querier, userID string, f contentFilter) (int, error) {
	args := pgx.NamedArgs{"user_id": userID}
	return db.SelectScalar[int](ctx, q, "SELECT COUNT(*) "+f.from(args, false, false), args)
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

	includes := map[string]bool{}
	for part := range strings.SplitSeq(q.Include, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			includes[part] = true
		}
	}

	ctx := reqCtx(c)
	var total *int
	if q.Count != "false" {
		n, err := countContent(ctx, cr.pool, user.ID, q.filter())
		if err != nil {
			return err
		}
		total = &n
	}

	rows, err := listContent(ctx, cr.pool, user.ID, q)
	if err != nil {
		return err
	}

	dtos := make([]ContentDTO, len(rows))
	for i, r := range rows {
		dtos[i] = contentToDTO(r.Content, contentDTOOpts{
			meta:                r.MetaData,
			childrenCount:       r.ChildrenCount,
			unreadChildrenCount: r.UnreadChildrenCount,
			userToContent:       r.utc(),
			includeFileData:     includes["file_data"],
			includeMeta:         includes["meta"],
		})
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
func bucketKey(sort string) (key string, needsUTC bool) {
	switch sort {
	case "title":
		// Rows without metadata sort next to '#'.
		return "COALESCE(left(c.sort_title, 1), '#')", false
	case "created_at":
		return "to_char(c.created_at AT TIME ZONE 'UTC', 'YYYY')", false
	case "progress_updated_at":
		return "to_char(utc.progress_updated_at AT TIME ZONE 'UTC', 'YYYY')", true
	case "release_date":
		return "left(c.release_date, 4)", false
	}
	return "", false
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

	f := q.filter()
	sort, dir := q.order()
	key, utc := bucketKey(sort)
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
		key, f.from(args, utc, false), dir, nullsOrder(dir)), args)
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
	sql := "SELECT " + kindCountColumns + " " + f.from(args, false, false)
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		// pg_search 0.25.10's aggregate scan miscounts FILTER aggregates under generic plans.
		if _, err := tx.Exec(ctx, "SET LOCAL paradedb.enable_aggregate_custom_scan = off"); err != nil {
			return err
		}
		k, err = db.SelectOne[kindCounts](ctx, tx, sql, args)
		return err
	})
	return k, err
}

// listContent runs one page of lq: the ids query sorts and pages, then only those rows hydrate.
func listContent(ctx context.Context, q db.Querier, userID string, lq contentListQuery) ([]contentListRow, error) {
	sort, dir := lq.order()
	ids, err := listContentIDs(ctx, q, userID, lq.filter(), sort, dir, lq.Limit, lq.Offset)
	if err != nil {
		return nil, err
	}
	byID, err := selectContentRows(ctx, q, userID, ids)
	if err != nil {
		return nil, err
	}
	rows := make([]contentListRow, 0, len(ids))
	for _, id := range ids {
		if r, ok := byID[id]; ok { // gone if deleted since the ids query
			rows = append(rows, r)
		}
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

type RecentlyReadEntryDTO struct {
	Item   ContentDTO  `json:"item"`
	Series *ContentDTO `json:"series"`
}

type recentlyReadQuery struct {
	Limit int `query:"limit" default:"10" validate:"min=1,max=50"`
}

// homePrefs reads the user's home preferences; malformed preferences mean the defaults.
func homePrefs(raw models.JSONB) (ignoreSeriesStatus bool) {
	var p struct {
		Home struct {
			IgnoreSeriesStatus bool `json:"ignoreSeriesStatus"`
		} `json:"home"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return false
	}
	return p.Home.IgnoreSeriesStatus
}

func (cr *ContentRoutes) recentlyRead(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	q, err := BindQuery[recentlyReadQuery](c)
	if err != nil {
		return err
	}
	picks, err := continueReading(reqCtx(c), cr.pool, user, q.Limit)
	if err != nil {
		return err
	}
	toDTO := func(r contentListRow) ContentDTO {
		return contentToDTO(r.Content, contentDTOOpts{
			meta:                r.MetaData,
			childrenCount:       r.ChildrenCount,
			unreadChildrenCount: r.UnreadChildrenCount,
			userToContent:       r.utc(),
		})
	}
	entries := make([]RecentlyReadEntryDTO, len(picks))
	for i, p := range picks {
		entries[i] = RecentlyReadEntryDTO{Item: toDTO(p.Item)}
		if p.Series != nil {
			dto := toDTO(*p.Series)
			entries[i].Series = &dto
		}
	}
	return c.JSON(http.StatusOK, entries)
}

type readingPick struct {
	Item   contentListRow
	Series *contentListRow // nil for a standalone item
}

// continueReading lists the items in progress, one per series. A series' item is its first
// eligible (unread, valid, not on hold) child from the anchor onwards, wrapping to the start,
// where the anchor is the active child with the latest status change or, while eligible, activity.
func continueReading(ctx context.Context, pool *pgxpool.Pool, user *models.User, limit int) ([]readingPick, error) {
	type pickRow struct {
		ItemID   string  `db:"item_id"`
		SeriesID *string `db:"series_id"`
	}
	picks, err := db.Select[pickRow](ctx, pool, `
		WITH last AS (
			SELECT DISTINCT ON (c.parent_id)
				c.parent_id AS series_id, c.id AS last_id,
				MAX(COALESCE(utc.progress_updated_at, utc.status_updated_at))
					OVER (PARTITION BY c.parent_id) AS sort_at
			FROM user_to_content utc
			JOIN content c ON c.library_id = utc.library_id AND c.uri = utc.uri
			WHERE utc.user_id = @user_id
				AND c.parent_id IS NOT NULL AND c.type IN ('book', 'comic')
				AND (utc.progress_updated_at IS NOT NULL OR utc.status IN ('reading', 'completed'))
			ORDER BY c.parent_id,
				CASE WHEN utc.status IS NULL OR utc.status IN ('reading', 'plan_to_read')
					THEN GREATEST(utc.progress_updated_at, utc.status_updated_at)
					ELSE utc.status_updated_at END DESC NULLS LAST,
				c."order" DESC NULLS FIRST, c.id DESC
		), series_pick AS (
			-- Ordering last by recency lets an incremental-sort plan stop picking children early.
			SELECT l.series_id, l.sort_at, p.item_id
			FROM (SELECT * FROM last ORDER BY sort_at DESC NULLS LAST) l
			JOIN content s ON s.id = l.series_id
			LEFT JOIN user_to_content sutc
				ON sutc.library_id = s.library_id AND sutc.uri = s.uri AND sutc.user_id = @user_id
			CROSS JOIN LATERAL (
				-- Children from the anchor onwards first (false < true), then the earlier ones.
				SELECT k.id AS item_id FROM (
					SELECT k.*, max(k.pos) FILTER (WHERE k.id = l.last_id) OVER () AS last_pos
					FROM (
						SELECT c.id,
							row_number() OVER (ORDER BY c."order" ASC NULLS LAST, c.id) AS pos,
							c.valid AND (utc.status IS NULL OR utc.status IN ('reading', 'plan_to_read')) AS eligible
						FROM content c
						LEFT JOIN user_to_content utc
							ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
						WHERE c.parent_id = l.series_id AND c.type IN ('book', 'comic')
					) k
				) k
				WHERE k.eligible
				ORDER BY k.pos < k.last_pos, k.pos
				LIMIT 1
			) p
			WHERE @ignore_series_status OR sutc.status IS NULL OR sutc.status NOT IN ('dropped', 'on_hold')
			ORDER BY l.sort_at DESC NULLS LAST, p.item_id
			LIMIT @limit
		), standalone AS (
			SELECT NULL::text AS series_id,
				COALESCE(utc.progress_updated_at, utc.status_updated_at) AS sort_at, c.id AS item_id
			FROM user_to_content utc
			JOIN content c ON c.library_id = utc.library_id AND c.uri = utc.uri
			WHERE utc.user_id = @user_id AND utc.status = 'reading'
				AND c.parent_id IS NULL AND c.valid AND c.type IN ('book', 'comic')
		)
		SELECT item_id, series_id FROM (TABLE series_pick UNION ALL TABLE standalone) g
		ORDER BY sort_at DESC NULLS LAST, item_id
		LIMIT @limit
	`, pgx.NamedArgs{"user_id": user.ID, "limit": limit, "ignore_series_status": homePrefs(user.Preferences)})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, 2*len(picks))
	for _, p := range picks {
		ids = append(ids, p.ItemID)
		if p.SeriesID != nil {
			ids = append(ids, *p.SeriesID)
		}
	}
	rows, err := selectContentRows(ctx, pool, user.ID, ids)
	if err != nil {
		return nil, err
	}

	out := []readingPick{}
	for _, p := range picks {
		// A scan may have deleted, invalidated or reparented the item, or deleted the series, since
		// the pick.
		item, ok := rows[p.ItemID]
		if !ok || !item.Valid || !fp.PtrEq(item.ParentID, p.SeriesID) {
			continue
		}
		pick := readingPick{Item: item}
		if p.SeriesID != nil {
			series, ok := rows[*p.SeriesID]
			if !ok {
				continue
			}
			pick.Series = &series
		}
		out = append(out, pick)
	}
	return out, nil
}

type userToContentRequest struct {
	Starred  *bool            `json:"starred"`
	Status   *string          `json:"status"   validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
	Notes    *string          `json:"notes"`
	Rating   *int             `json:"rating"   validate:"omitempty,min=1,max=10"`
	Progress *json.RawMessage `json:"progress"`
}

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

	var req userToContentRequest
	rawFull, _ := json.Marshal(rawBody)
	if err := json.Unmarshal(rawFull, &req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	now := time.Now().UTC()

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
	if _, ok := rawBody["status"]; ok {
		set("status", req.Status)
		set("status_updated_at", now)
	}
	if _, ok := rawBody["notes"]; ok {
		set("notes", req.Notes)
	}
	if _, ok := rawBody["rating"]; ok {
		set("rating", req.Rating)
	}
	if _, ok := rawBody["progress"]; ok {
		progress := []byte("{}")
		if req.Progress != nil {
			var m map[string]json.RawMessage
			if err := json.Unmarshal(*req.Progress, &m); err != nil {
				return echo.NewHTTPError(http.StatusBadRequest, "progress must be a JSON object")
			}
			progress = []byte(*req.Progress)
		}
		set("progress", progress)
		if string(progress) == "{}" {
			set("progress_updated_at", nil)
		} else {
			set("progress_updated_at", now)
		}
	}

	var utc models.UserToContent
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if err := lockContentLibraries(ctx, tx, contentID); err != nil {
			return err
		}
		libraryID, uri, err := contentURI(ctx, tx, contentID)
		if err != nil {
			return err
		}
		args["library_id"], args["uri"] = libraryID, uri
		var parentID, prevStatus *string
		if req.Status != nil {
			err = tx.QueryRow(ctx, `
				SELECT c.parent_id, utc.status FROM content c
				LEFT JOIN user_to_content utc
					ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $2
				WHERE c.id = $1
			`, contentID, user.ID).Scan(&parentID, &prevStatus)
			if err != nil {
				return err
			}
		}
		utc, err = db.SelectOne[models.UserToContent](ctx, tx, fmt.Sprintf(`
			INSERT INTO user_to_content (%s)
			VALUES (%s)
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET %s
			RETURNING *
		`, strings.Join(cols, ", "), strings.Join(vals, ", "), strings.Join(sets, ", ")), args)
		if err != nil {
			return err
		}
		// Only a change propagates: readers resend 'reading' on every progress save, which
		// would otherwise restore a series status the user cleared.
		if parentID != nil && (prevStatus == nil || *prevStatus != *req.Status) {
			return propagateSeriesStatus(ctx, tx, user.ID, *parentID, *req.Status, now)
		}
		return nil
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, utcToDTO(&utc))
}

type seriesItemStatusesRequest struct {
	Status  *string `json:"status"   validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
	UntilID *string `json:"until_id"`
}

func (cr *ContentRoutes) setSeriesItemStatuses(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	var req seriesItemStatusesRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockContentLibraries(ctx, tx, contentID); err != nil {
		return err
	}
	if _, _, err := contentURI(ctx, tx, contentID); err != nil {
		return err
	}

	type childRow struct {
		ID        string `db:"id"`
		LibraryID string `db:"library_id"`
		URI       string `db:"uri"`
	}
	children, err := db.Select[childRow](ctx, tx, `
		SELECT id, library_id, uri FROM content
		WHERE parent_id = $1 ORDER BY "order" ASC
	`, contentID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return okResponse(c)
	}

	setChildren := children
	if req.UntilID != nil {
		splitIdx := -1
		for i, ch := range children {
			if ch.ID == *req.UntilID {
				splitIdx = i
				break
			}
		}
		if splitIdx == -1 {
			return echo.NewHTTPError(http.StatusNotFound, "Target child not found")
		}
		setChildren = children[:splitIdx+1]
	}

	now := time.Now().UTC()

	// Clear statuses if status is nil or until_id is set
	if req.Status == nil || req.UntilID != nil {
		allLibIDs := make([]string, len(children))
		allURIs := make([]string, len(children))
		for i, ch := range children {
			allLibIDs[i] = ch.LibraryID
			allURIs[i] = ch.URI
		}
		_, err = tx.Exec(ctx, `
			UPDATE user_to_content
			SET status = NULL, status_updated_at = $1, progress = '{}', progress_updated_at = NULL
			WHERE user_id = $2
				AND (library_id, uri) IN (SELECT UNNEST($3::text[]), UNNEST($4::text[]))
		`, now, user.ID, allLibIDs, allURIs)
		if err != nil {
			return err
		}
	}

	// Upsert target items with the given status
	if req.Status != nil {
		for _, ch := range setChildren {
			_, err = tx.Exec(ctx, `
				INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at)
				VALUES ($1, $2, $3, $4, $5, $6)
				ON CONFLICT (user_id, library_id, uri)
				DO UPDATE SET status = $5, status_updated_at = $6
			`, models.MakeUserToContentID(), user.ID, ch.LibraryID, ch.URI, *req.Status, now)
			if err != nil {
				return err
			}
		}
		if err := propagateSeriesStatus(ctx, tx, user.ID, contentID, *req.Status, now); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return okResponse(c)
}

// propagateSeriesStatus applies a child's new status to its series: a 'reading' or 'completed'
// child starts a series without a status, and completing the last unfinished child completes a
// 'reading' series. Other series statuses are the user's choice and stay untouched.
func propagateSeriesStatus(ctx context.Context, tx pgx.Tx, userID, seriesID, childStatus string, now time.Time) error {
	if childStatus != "reading" && childStatus != "completed" {
		return nil
	}
	args := pgx.NamedArgs{"utc_id": models.MakeUserToContentID(), "user_id": userID, "series_id": seriesID, "now": now}
	_, err := tx.Exec(ctx, `
		INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at)
		SELECT @utc_id, @user_id, library_id, uri, 'reading', @now FROM content WHERE id = @series_id
		ON CONFLICT (user_id, library_id, uri) DO UPDATE
			SET status = 'reading', status_updated_at = EXCLUDED.status_updated_at
			WHERE user_to_content.status IS NULL
	`, args)
	if err != nil || childStatus != "completed" {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE user_to_content utc SET status = 'completed', status_updated_at = @now
		FROM content c
		WHERE c.id = @series_id AND utc.user_id = @user_id
			AND utc.library_id = c.library_id AND utc.uri = c.uri AND utc.status = 'reading'
			AND NOT EXISTS (SELECT 1 `+unfinishedChildren+`)
	`, args)
	return err
}

type bulkUserDataRequest struct {
	IDs    []string `json:"ids"    validate:"required,min=1"`
	Action string   `json:"action" validate:"required,oneof=set_status reset"`
	Status *string  `json:"status" validate:"omitempty,oneof=reading completed on_hold dropped plan_to_read"`
}

// bulkUserData sets or resets the user's status on the given content. Reset also clears progress,
// and the statuses and progress of the children of selected series.
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
	utcIDs := make([]string, len(ids))
	for i := range utcIDs {
		utcIDs[i] = models.MakeUserToContentID()
	}
	now := time.Now().UTC()
	args := pgx.NamedArgs{
		"user_id": user.ID, "content_ids": ids, "utc_ids": utcIDs, "status": req.Status, "now": now,
	}
	sets := "status = EXCLUDED.status, status_updated_at = EXCLUDED.status_updated_at"
	reset := req.Action == "reset"
	if reset {
		args["status"] = nil
		sets += ", progress = EXCLUDED.progress, progress_updated_at = EXCLUDED.progress_updated_at"
	}

	ctx := reqCtx(c)
	var count int64
	err = db.WithTx(ctx, cr.pool, func(tx pgx.Tx) error {
		if err := lockContentLibraries(ctx, tx, ids...); err != nil {
			return err
		}
		// As in updateUserData, only a changed child status propagates to its series.
		var parents []string
		if !reset && req.Status != nil {
			var err error
			parents, err = db.SelectScalars[string](ctx, tx, `
				SELECT DISTINCT c.parent_id FROM content c
				LEFT JOIN user_to_content utc
					ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
				WHERE c.id = ANY(@content_ids) AND c.parent_id IS NOT NULL
					AND utc.status IS DISTINCT FROM @status
			`, args)
			if err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at)
			SELECT r.id, @user_id, c.library_id, c.uri, @status, @now
			FROM unnest(@utc_ids::text[], @content_ids::text[]) r(id, cid)
			JOIN content c ON c.id = r.cid
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET `+sets, args)
		if err != nil {
			return err
		}
		count = tag.RowsAffected()
		for _, p := range parents {
			if err := propagateSeriesStatus(ctx, tx, user.ID, p, *req.Status, now); err != nil {
				return err
			}
		}
		if !reset {
			return nil
		}
		_, err = tx.Exec(ctx, `
			UPDATE user_to_content utc
			SET status = NULL, status_updated_at = @now, progress = '{}', progress_updated_at = NULL
			FROM content child
			WHERE child.parent_id = ANY(@content_ids) AND utc.user_id = @user_id
				AND utc.library_id = child.library_id AND utc.uri = child.uri
		`, args)
		return err
	})
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, map[string]int64{"count": count})
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
	}
}

type contentListRow struct {
	contentWithUTCRow
	ChildrenCount       *int `db:"children_count"`
	UnreadChildrenCount *int `db:"unread_children_count"`
}

// lockContentLibraries takes the metadata locks, under which scans move refs, of the libraries
// holding the content, in sorted order so that concurrent batches cannot deadlock.
func lockContentLibraries(ctx context.Context, tx pgx.Tx, contentIDs ...string) error {
	libs, err := db.SelectScalars[string](ctx, tx,
		"SELECT DISTINCT library_id FROM content WHERE id = ANY($1) ORDER BY library_id", contentIDs)
	if err != nil {
		return err
	}
	for _, lib := range libs {
		if err := db.LockMetadata(ctx, tx, lib); err != nil {
			return err
		}
	}
	return nil
}

// contentURI reads the content's current URI; under lockContentLibraries, a ref written with it
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
