package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/models/contentmeta"
	"voltis/scanner"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ContentRoutes struct {
	pool      *pgxpool.Pool
	scanQueue *scanner.Queue
}

func (cr *ContentRoutes) Register(g *echo.Group) {
	g.GET("", cr.list)
	g.GET("/:content_id", cr.get)
	g.GET("/:content_id/lists", cr.listsForContent)
	g.POST("/:content_id/user-data", cr.updateUserData)
	g.POST("/:content_id/series-item-statuses", cr.setSeriesItemStatuses)
	g.GET("/:content_id/metadata-layers", cr.getMetadataLayers)
	g.POST("/:content_id/metadata-override", cr.updateMetadataOverride)
	g.POST("/:content_id/scan", cr.scanContent)
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

	title := ""
	if opts.meta != nil {
		var m map[string]json.RawMessage
		if json.Unmarshal(opts.meta, &m) == nil {
			if t, ok := m["title"]; ok {
				_ = json.Unmarshal(t, &title)
			}
		}
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
		Title:               title,
		Valid:               c.Valid,
		FileURI:             c.FileURI,
		FileMtime:           c.FileMtime,
		FileSize:            c.FileSize,
		CoverURI:            c.CoverURI,
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

func (cr *ContentRoutes) get(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	r, err := db.SelectOne[contentWithUTCRow](ctx, cr.pool, `
		SELECT c.*,
			utc.id AS utc_id, utc.user_id AS utc_user_id, utc.library_id AS utc_library_id,
			utc.uri AS utc_uri, utc.starred AS utc_starred, utc.status AS utc_status,
			utc.status_updated_at AS utc_status_updated_at, utc.notes AS utc_notes,
			utc.rating AS utc_rating, utc.progress AS utc_progress,
			utc.progress_updated_at AS utc_progress_updated_at,
			cm.data AS meta_data
		FROM content c
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $1
		LEFT JOIN content_metadata cm
			ON cm.uri = c.uri AND cm.library_id = c.library_id
		WHERE c.id = $2
	`, user.ID, contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, contentToDTO(r.Content, contentDTOOpts{
		meta:            r.MetaData,
		userToContent:   r.utc(),
		includeFileData: true,
		includeMeta:     true,
	}))
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
}

func (cr *ContentRoutes) list(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)

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

	// Build query
	args := pgx.NamedArgs{"user_id": user.ID, "valid": q.Valid != "false"}
	where := []string{"c.valid = @valid"}

	if q.ParentID != "" {
		if q.ParentID == "null" {
			where = append(where, "c.parent_id IS NULL")
		} else {
			args["parent_id"] = q.ParentID
			where = append(where, "c.parent_id = @parent_id")
		}
	}
	if q.LibraryID != "" {
		args["library_id"] = q.LibraryID
		where = append(where, "c.library_id = @library_id")
	}
	if len(q.Type) > 0 {
		args["types"] = q.Type
		where = append(where, "c.type = ANY(@types)")
	}
	if q.ReadingStatus != "" {
		args["reading_status"] = q.ReadingStatus
		where = append(where, "utc.status = @reading_status")
	}
	if q.Starred != "" {
		args["starred"] = q.Starred == "true"
		where = append(where, "utc.starred = @starred")
	}
	switch q.HasStatus {
	case "true":
		where = append(where, "utc.status IS NOT NULL")
	case "false":
		where = append(where, "(utc.user_id IS NULL OR utc.status IS NULL)")
	}
	switch q.HasRating {
	case "true":
		where = append(where, "utc.rating IS NOT NULL")
	case "false":
		where = append(where, "(utc.user_id IS NULL OR utc.rating IS NULL)")
	}
	if q.Search != "" {
		fuzzyDist := 1
		if len(q.Search) < 3 {
			fuzzyDist = 0
		}
		args["search"] = q.Search
		where = append(where, fmt.Sprintf(
			"cm.data->>'title' ||| (@search)::pdb.fuzzy(%d, t)", fuzzyDist,
		))
	}

	whereClause := strings.Join(where, " AND ")

	baseFrom := fmt.Sprintf(`
		FROM content c
		LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = @user_id
		LEFT JOIN content_metadata cm
			ON cm.uri = c.uri AND cm.library_id = c.library_id
		WHERE %s
	`, whereClause)

	// Count query
	var total int
	err = cr.pool.QueryRow(ctx, "SELECT COUNT(*) "+baseFrom, args).Scan(&total)
	if err != nil {
		return err
	}

	// Sorting
	nullsOrder := "NULLS LAST"
	if q.SortOrder == "asc" {
		nullsOrder = "NULLS FIRST"
	}

	var orderClause string
	switch q.Sort {
	case "progress_updated_at":
		orderClause = fmt.Sprintf("ORDER BY utc.progress_updated_at %s %s", q.SortOrder, nullsOrder)
	case "created_at":
		orderClause = fmt.Sprintf("ORDER BY c.created_at %s", q.SortOrder)
	case "order":
		orderClause = fmt.Sprintf("ORDER BY c.\"order\" %s", q.SortOrder)
	case "rating":
		orderClause = fmt.Sprintf("ORDER BY (cm.data->>'community_rating')::numeric %s %s", q.SortOrder, nullsOrder)
	case "user_rating":
		orderClause = fmt.Sprintf("ORDER BY utc.rating %s %s", q.SortOrder, nullsOrder)
	case "unread_children_count":
		orderClause = fmt.Sprintf("ORDER BY unread_children_count %s", q.SortOrder)
	case "release_date":
		orderClause = fmt.Sprintf("ORDER BY (cm.data->>'publication_date')::date %s %s", q.SortOrder, nullsOrder)
	case "title":
		orderClause = fmt.Sprintf("ORDER BY cm.data->>'title' %s %s", q.SortOrder, nullsOrder)
	default:
		if q.Search != "" {
			orderClause = "ORDER BY paradedb.score(cm.id) DESC"
		}
	}

	// Data query with children counts
	dataQuery := fmt.Sprintf(`
		SELECT c.*,
			(SELECT COUNT(*) FROM content child WHERE child.parent_id = c.id) AS children_count,
			(SELECT COUNT(*) FROM content child
				LEFT JOIN user_to_content child_utc
					ON child_utc.library_id = child.library_id
					AND child_utc.uri = child.uri
					AND child_utc.user_id = @user_id
				WHERE child.parent_id = c.id
					AND (child_utc.id IS NULL OR child_utc.status IS NULL
						OR child_utc.status NOT IN ('completed', 'dropped'))
			) AS unread_children_count,
			utc.id AS utc_id, utc.user_id AS utc_user_id, utc.library_id AS utc_library_id,
			utc.uri AS utc_uri, utc.starred AS utc_starred, utc.status AS utc_status,
			utc.status_updated_at AS utc_status_updated_at, utc.notes AS utc_notes,
			utc.rating AS utc_rating, utc.progress AS utc_progress,
			utc.progress_updated_at AS utc_progress_updated_at,
			cm.data AS meta_data
		%s
		%s
	`, baseFrom, orderClause)

	if q.Limit != nil {
		dataQuery += fmt.Sprintf(" LIMIT %d", *q.Limit)
	}
	if q.Offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", q.Offset)
	}

	items, err := db.Select[contentListRow](ctx, cr.pool, dataQuery, args)
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

	content, err := getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

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

	// Get or create user_to_content
	var utcID string
	err = cr.pool.QueryRow(ctx, `
		SELECT id FROM user_to_content
		WHERE user_id = $1 AND library_id = $2 AND uri = $3
	`, user.ID, content.LibraryID, content.URI).Scan(&utcID)

	now := time.Now().UTC()

	if errors.Is(err, pgx.ErrNoRows) {
		utcID = models.MakeUserToContentID()
		_, err = cr.pool.Exec(ctx, `
			INSERT INTO user_to_content (id, user_id, library_id, uri)
			VALUES ($1, $2, $3, $4)
		`, utcID, user.ID, content.LibraryID, content.URI)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	// Apply updates
	sets := []string{}
	args := pgx.NamedArgs{"utc_id": utcID}

	if _, ok := rawBody["starred"]; ok && req.Starred != nil {
		sets = append(sets, "starred = @starred")
		args["starred"] = *req.Starred
	}
	if _, ok := rawBody["status"]; ok {
		sets = append(sets, "status = @status", "status_updated_at = @status_updated_at")
		args["status"] = req.Status
		args["status_updated_at"] = now
	}
	if _, ok := rawBody["notes"]; ok {
		sets = append(sets, "notes = @notes")
		args["notes"] = req.Notes
	}
	if _, ok := rawBody["rating"]; ok {
		sets = append(sets, "rating = @rating")
		args["rating"] = req.Rating
	}
	if _, ok := rawBody["progress"]; ok {
		sets = append(sets, "progress = @progress", "progress_updated_at = @progress_updated_at")
		args["progress"] = []byte(*req.Progress)
		if req.Progress != nil && string(*req.Progress) != "{}" && string(*req.Progress) != "null" {
			args["progress_updated_at"] = now
		} else {
			args["progress_updated_at"] = nil
		}
	}

	if len(sets) > 0 {
		query := "UPDATE user_to_content SET " + strings.Join(sets, ", ") + " WHERE id = @utc_id"
		if _, err := cr.pool.Exec(ctx, query, args); err != nil {
			return err
		}
	}

	// Fetch updated record
	utc, err := db.SelectOne[models.UserToContent](ctx, cr.pool, "SELECT * FROM user_to_content WHERE id = $1", utcID)
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

	_, err = getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

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
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return okResponse(c)
}

type MetadataLayerDTO struct {
	Source string          `json:"source"`
	Data   json.RawMessage `json:"data"`
	Raw    json.RawMessage `json:"raw"`
}

type MetadataLayersResponse struct {
	Merged json.RawMessage    `json:"merged"`
	Layers []MetadataLayerDTO `json:"layers"`
}

func (cr *ContentRoutes) getMetadataLayers(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

	var data, dataRaw json.RawMessage
	err = cr.pool.QueryRow(ctx, `
		SELECT data, data_raw FROM content_metadata
		WHERE uri = $1 AND library_id = $2
	`, content.URI, content.LibraryID).Scan(&data, &dataRaw)
	if errors.Is(err, pgx.ErrNoRows) {
		data = json.RawMessage("{}")
		dataRaw = json.RawMessage("{}")
	} else if err != nil {
		return err
	}

	var rawMap map[string]json.RawMessage
	_ = json.Unmarshal(dataRaw, &rawMap)
	if rawMap == nil {
		rawMap = map[string]json.RawMessage{}
	}

	layers := make([]MetadataLayerDTO, len(contentmeta.MergeOrder))
	for i, source := range contentmeta.MergeOrder {
		entry := rawMap[source]
		layerData := json.RawMessage("{}")
		layerRaw := json.RawMessage("{}")

		if entry != nil {
			var entryMap map[string]json.RawMessage
			if json.Unmarshal(entry, &entryMap) == nil {
				if d, ok := entryMap["data"]; ok {
					layerData = d
				}
				if r, ok := entryMap["raw"]; ok {
					layerRaw = r
				}
			}
		}

		layers[i] = MetadataLayerDTO{Source: source, Data: layerData, Raw: layerRaw}
	}

	return c.JSON(http.StatusOK, MetadataLayersResponse{Merged: data, Layers: layers})
}

type metadataOverrideRequest struct {
	Data json.RawMessage `json:"data"`
}

func (cr *ContentRoutes) updateMetadataOverride(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

	var req metadataOverrideRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	now := time.Now().UTC()

	// Get existing data_raw or create new
	var dataRaw json.RawMessage
	err = cr.pool.QueryRow(ctx, `
		SELECT data_raw FROM content_metadata WHERE uri = $1 AND library_id = $2
	`, content.URI, content.LibraryID).Scan(&dataRaw)

	if errors.Is(err, pgx.ErrNoRows) {
		// Create new row
		overrideEntry, _ := json.Marshal(map[string]json.RawMessage{"data": req.Data, "raw": json.RawMessage("{}")})
		newDataRaw, _ := json.Marshal(map[string]json.RawMessage{"overrides": overrideEntry})
		merged := req.Data

		_, err = cr.pool.Exec(ctx, `
			INSERT INTO content_metadata (id, uri, library_id, data, data_raw, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, models.MakeContentID(), content.URI, content.LibraryID, merged, newDataRaw, now)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// Update existing
		var rawMap map[string]json.RawMessage
		_ = json.Unmarshal(dataRaw, &rawMap)
		if rawMap == nil {
			rawMap = map[string]json.RawMessage{}
		}

		overrideEntry, _ := json.Marshal(map[string]json.RawMessage{"data": req.Data, "raw": json.RawMessage("{}")})
		rawMap["overrides"] = overrideEntry

		// Recompute merged
		mergedJSON, _ := json.Marshal(contentmeta.MergeRawLayers(rawMap))
		updatedRaw, _ := json.Marshal(rawMap)

		_, err = cr.pool.Exec(ctx, `
			UPDATE content_metadata SET data = $1, data_raw = $2, updated_at = $3
			WHERE uri = $4 AND library_id = $5
		`, mergedJSON, updatedRaw, now, content.URI, content.LibraryID)
		if err != nil {
			return err
		}
	}

	return cr.getMetadataLayers(c)
}

func (cr *ContentRoutes) scanContent(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, cr.pool, contentID)
	if err != nil {
		return err
	}

	var fileURIs []string
	if content.FileURI != nil {
		fileURIs = append(fileURIs, *content.FileURI)
	}

	childURIs, err := db.SelectScalars[string](ctx, cr.pool, "SELECT file_uri FROM content WHERE parent_id = $1 AND file_uri IS NOT NULL", contentID)
	if err != nil {
		return err
	}
	fileURIs = append(fileURIs, childURIs...)

	if len(fileURIs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "No files to scan")
	}

	cr.scanQueue.Enqueue(content.LibraryID, true, fileURIs)

	return c.JSON(http.StatusOK, map[string]string{"status": "queued"})
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

func getContent(ctx context.Context, pool *pgxpool.Pool, id string) (models.Content, error) {
	content, err := db.SelectOne[models.Content](ctx, pool, "SELECT * FROM content WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Content{}, echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	return content, err
}
