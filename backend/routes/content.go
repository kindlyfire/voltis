package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ContentRoutes struct {
	pool *pgxpool.Pool
}

func (cr *ContentRoutes) Register(g *echo.Group) {
	g.GET("", cr.list)
	g.GET("/recently-read", cr.recentlyRead)
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

// contentRowColumns selects a contentListRow from content c, user_to_content utc (for @user_id)
// and content_metadata cm.
const contentRowColumns = `c.*,
	(SELECT COUNT(*) FROM content child WHERE child.parent_id = c.id) AS children_count,
	(SELECT COUNT(*) ` + unfinishedChildren + `) AS unread_children_count,
	utc.id AS utc_id, utc.user_id AS utc_user_id, utc.library_id AS utc_library_id,
	utc.uri AS utc_uri, utc.starred AS utc_starred, utc.status AS utc_status,
	utc.status_updated_at AS utc_status_updated_at, utc.notes AS utc_notes,
	utc.rating AS utc_rating, utc.progress AS utc_progress,
	utc.progress_updated_at AS utc_progress_updated_at,
	cm.data AS meta_data, cm.updated_at AS meta_updated_at`

func selectContentRows(ctx context.Context, pool *pgxpool.Pool, userID string, ids []string) (map[string]contentListRow, error) {
	rows, err := db.Select[contentListRow](ctx, pool, `
		SELECT `+contentRowColumns+`
		FROM content c
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
		LEFT JOIN content_metadata cm
			ON cm.uri = c.uri AND cm.library_id = c.library_id
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
		FROM content c
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
		LEFT JOIN content_metadata cm
			ON cm.uri = c.uri AND cm.library_id = c.library_id
		WHERE c.id = @id
	`, pgx.NamedArgs{"user_id": user.ID, "id": contentID})
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
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
	Search        string   `query:"search"`
	Limit         *int     `query:"limit"           validate:"omitempty,min=0"`
	Offset        int      `query:"offset"          validate:"min=0"`
	Sort          string   `query:"sort"            validate:"omitempty,oneof=progress_updated_at created_at order rating user_rating unread_children_count release_date title"`
	SortOrder     string   `query:"sort_order"      validate:"omitempty,oneof=asc desc" default:"desc"`
	Include       string   `query:"include"`
	ListID        string   // set by code only
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

	items, total, err := queryContent(reqCtx(c), cr.pool, user.ID, q, nil)
	if err != nil {
		return err
	}

	dtos := make([]ContentDTO, len(items))
	for i, r := range items {
		dtos[i] = contentToDTO(r.Content, contentDTOOpts{
			meta:                r.MetaData,
			childrenCount:       r.ChildrenCount,
			unreadChildrenCount: r.UnreadChildrenCount,
			userToContent:       r.utc(),
			includeFileData:     includes["file_data"],
			includeMeta:         includes["meta"],
		})
	}

	return c.JSON(http.StatusOK, PaginatedResponse[ContentDTO]{Data: dtos, Total: total})
}

type kindCounts struct{ Series, Comics, Books int }

func (k kindCounts) items() int { return k.Comics + k.Books }

// kindCountColumns selects kindCounts over content c.
const kindCountColumns = `COUNT(*) FILTER (WHERE c.type IN ('comic_series', 'book_series')) AS series,
	COUNT(*) FILTER (WHERE c.type = 'comic') AS comics,
	COUNT(*) FILTER (WHERE c.type = 'book') AS books`

// contentFrom builds the FROM and WHERE clauses shared by queryContent and countContentKinds.
func contentFrom(userID string, f contentListQuery) (string, pgx.NamedArgs) {
	args := pgx.NamedArgs{"user_id": userID, "valid": f.Valid != "false"}
	where := []string{"c.valid = @valid"}

	if f.ParentID != "" {
		if f.ParentID == "null" {
			where = append(where, "c.parent_id IS NULL")
		} else {
			args["parent_id"] = f.ParentID
			where = append(where, "c.parent_id = @parent_id")
		}
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
	}
	if f.Starred != "" {
		args["starred"] = f.Starred == "true"
		where = append(where, "utc.starred = @starred")
	}
	switch f.HasStatus {
	case "true":
		where = append(where, "utc.status IS NOT NULL")
	case "false":
		where = append(where, "(utc.user_id IS NULL OR utc.status IS NULL)")
	}
	switch f.HasRating {
	case "true":
		where = append(where, "utc.rating IS NOT NULL")
	case "false":
		where = append(where, "(utc.user_id IS NULL OR utc.rating IS NULL)")
	}
	if f.Search != "" {
		args["search"] = f.Search
		where = append(where, metadata.Matches("cm", "search_text", false, f.Search))
	}

	listJoin := ""
	if f.ListID != "" {
		args["list_id"] = f.ListID
		listJoin = `JOIN custom_list_to_content clc ON clc.custom_list_id = @list_id
			AND clc.library_id = c.library_id AND clc.uri = c.uri`
	}

	return fmt.Sprintf(`
		FROM content c
		%s
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
		LEFT JOIN content_metadata cm
			ON cm.uri = c.uri AND cm.library_id = c.library_id
		WHERE %s
	`, listJoin, strings.Join(where, " AND ")), args
}

func countContentKinds(ctx context.Context, q db.Querier, userID string, f contentListQuery) (kindCounts, error) {
	from, args := contentFrom(userID, f)
	return db.SelectOne[kindCounts](ctx, q, "SELECT "+kindCountColumns+from, args)
}

// queryContent runs the content list filters for userID. ListID, set only by code, joins the
// list's entries and orders by list order unless Sort is set. counts, when non-nil, holds
// countContentKinds for the same filters and saves the count query.
func queryContent(ctx context.Context, q db.Querier, userID string, f contentListQuery, counts *kindCounts) ([]contentListRow, int, error) {
	if counts == nil {
		k, err := countContentKinds(ctx, q, userID, f)
		if err != nil {
			return nil, 0, err
		}
		counts = &k
	}
	total := counts.Series + counts.items()

	from, args := contentFrom(userID, f)

	nullsOrder := "NULLS LAST"
	if f.SortOrder == "asc" {
		nullsOrder = "NULLS FIRST"
	}

	var order string
	switch f.Sort {
	case "progress_updated_at":
		order = fmt.Sprintf("utc.progress_updated_at %s %s", f.SortOrder, nullsOrder)
	case "created_at":
		order = fmt.Sprintf("c.created_at %s", f.SortOrder)
	case "order":
		order = fmt.Sprintf("c.\"order\" %s", f.SortOrder)
	case "rating":
		order = fmt.Sprintf("cm.rating %s %s", f.SortOrder, nullsOrder)
	case "user_rating":
		order = fmt.Sprintf("utc.rating %s %s", f.SortOrder, nullsOrder)
	case "unread_children_count":
		order = fmt.Sprintf("unread_children_count %s", f.SortOrder)
	case "release_date":
		order = fmt.Sprintf("cm.release_date %s %s", f.SortOrder, nullsOrder)
	case "title":
		order = fmt.Sprintf("cm.data->>'title' %s %s", f.SortOrder, nullsOrder)
	default:
		if f.Search != "" {
			order = "paradedb.score(cm.id) DESC"
		} else if f.ListID != "" {
			order = `(clc."order" IS NULL), clc."order", clc.created_at`
		}
	}
	// The c.id tiebreaker keeps offset pages from repeating or skipping tied rows.
	orderBy := "ORDER BY c.id"
	if order != "" {
		orderBy = "ORDER BY " + order + ", c.id"
	}

	dataQuery := "SELECT " + contentRowColumns + from + orderBy
	if f.Limit != nil {
		dataQuery += fmt.Sprintf(" LIMIT %d", *f.Limit)
	}
	if f.Offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := db.Select[contentListRow](ctx, q, dataQuery, args)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
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
		), kids AS (
			SELECT c.id, c.parent_id,
				row_number() OVER (PARTITION BY c.parent_id ORDER BY c."order" ASC NULLS LAST, c.id) AS pos,
				c.valid AND (utc.status IS NULL OR utc.status IN ('reading', 'plan_to_read')) AS eligible
			FROM content c
			LEFT JOIN user_to_content utc
				ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
			WHERE c.parent_id IN (SELECT series_id FROM last) AND c.type IN ('book', 'comic')
		), pick AS (
			-- Children from L onwards first (false < true), then the earlier ones.
			SELECT DISTINCT ON (k.parent_id) k.parent_id AS series_id, k.id AS item_id
			FROM kids k
			JOIN last l ON l.series_id = k.parent_id
			JOIN kids lk ON lk.id = l.last_id
			WHERE k.eligible
			ORDER BY k.parent_id, (k.pos < lk.pos), k.pos
		), series_pick AS (
			SELECT l.series_id, l.sort_at, p.item_id
			FROM last l
			JOIN pick p USING (series_id)
			JOIN content s ON s.id = l.series_id
			LEFT JOIN user_to_content sutc
				ON sutc.library_id = s.library_id AND sutc.uri = s.uri AND sutc.user_id = @user_id
			WHERE @ignore_series_status OR sutc.status IS NULL OR sutc.status NOT IN ('dropped', 'on_hold')
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
	content, err := db.SelectOne[models.Content](ctx, pool, "SELECT * FROM content WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Content{}, echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	return content, err
}
