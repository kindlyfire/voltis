package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type CustomListRoutes struct {
	pool *pgxpool.Pool
}

func (cr *CustomListRoutes) Register(g *echo.Group) {
	g.GET("", cr.list)
	g.GET("/:list_id", cr.get)
	g.POST("", cr.create)
	g.POST("/:list_id", cr.update)
	g.DELETE("/:list_id", cr.delete)
	g.POST("/:list_id/entries", cr.createEntry)
	g.POST("/:list_id/entries/reorder", cr.reorderEntries)
	g.POST("/:list_id/entries/:entry_id", cr.updateEntry)
	g.DELETE("/:list_id/entries/:entry_id", cr.deleteEntry)
}

// DTOs

type customListDTO struct {
	ID              string    `json:"id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	Name            string    `json:"name"`
	Description     *string   `json:"description"`
	Visibility      string    `json:"visibility"`
	UserID          string    `json:"user_id"`
	EntryCount      *int      `json:"entry_count"`
	CoverContentIDs []string  `json:"cover_content_ids"`
}

type customListEntryDTO struct {
	ID        string      `json:"id"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
	LibraryID string      `json:"library_id"`
	URI       string      `json:"uri"`
	Content   *ContentDTO `json:"content"`
	Notes     *string     `json:"notes"`
	Order     *int        `json:"order"`
}

type customListDetailDTO struct {
	ID          string               `json:"id"`
	CreatedAt   time.Time            `json:"created_at"`
	UpdatedAt   time.Time            `json:"updated_at"`
	Name        string               `json:"name"`
	Description *string              `json:"description"`
	Visibility  string               `json:"visibility"`
	UserID      string               `json:"user_id"`
	EntryCount  int                  `json:"entry_count"`
	Entries     []customListEntryDTO `json:"entries"`
}

// Helpers

func (cr *CustomListRoutes) getListForUser(c echo.Context, user *models.User, requireOwner bool) (models.CustomList, error) {
	ctx := reqCtx(c)
	listID := c.Param("list_id")

	cl, err := db.SelectOne[models.CustomList](ctx, cr.pool, "SELECT * FROM custom_lists WHERE id = $1", listID)
	if errors.Is(err, pgx.ErrNoRows) {
		return cl, echo.NewHTTPError(http.StatusNotFound, "List not found")
	}
	if err != nil {
		return cl, err
	}

	if cl.UserID != user.ID {
		if requireOwner {
			return cl, echo.NewHTTPError(http.StatusForbidden, "Not allowed")
		}
		if cl.Visibility == "private" {
			return cl, echo.NewHTTPError(http.StatusNotFound, "List not found")
		}
	}
	return cl, nil
}

// Handlers

func (cr *CustomListRoutes) list(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	userFilter := c.QueryParam("user")
	if userFilter == "" {
		userFilter = "all"
	}

	var whereClause string
	switch userFilter {
	case "me":
		whereClause = "WHERE cl.user_id = $1"
	case "others":
		whereClause = "WHERE cl.user_id != $1 AND cl.visibility != 'private'"
	default:
		whereClause = "WHERE (cl.user_id = $1 OR (cl.user_id != $1 AND cl.visibility != 'private'))"
	}

	type listRow struct {
		models.CustomList
		EntryCount      *int   `db:"entry_count"`
		CoverContentIDs []byte `db:"cover_content_ids"`
	}

	rows, err := db.Select[listRow](ctx, cr.pool, `
		SELECT cl.*,
			(SELECT COUNT(*) FROM custom_list_to_content clc WHERE clc.custom_list_id = cl.id) AS entry_count,
			(
				SELECT array_to_json(
					(array_agg(c.id ORDER BY (clc."order" IS NULL), clc."order", clc.created_at)
					 FILTER (WHERE c.cover_uri IS NOT NULL))[1:4]
				)
				FROM custom_list_to_content clc
				LEFT JOIN content c ON c.library_id = clc.library_id AND c.uri = clc.uri
				WHERE clc.custom_list_id = cl.id
			) AS cover_content_ids
		FROM custom_lists cl
		`+whereClause+`
		ORDER BY cl.created_at DESC
	`, user.ID)
	if err != nil {
		return err
	}

	dtos := make([]customListDTO, len(rows))
	for i, r := range rows {
		var coverIDs []string
		if r.CoverContentIDs != nil {
			_ = json.Unmarshal(r.CoverContentIDs, &coverIDs)
		}
		if coverIDs == nil {
			coverIDs = []string{}
		}
		dtos[i] = customListDTO{
			ID:              r.ID,
			CreatedAt:       r.CreatedAt,
			UpdatedAt:       r.UpdatedAt,
			Name:            r.Name,
			Description:     r.Description,
			Visibility:      r.Visibility,
			UserID:          r.UserID,
			EntryCount:      r.EntryCount,
			CoverContentIDs: coverIDs,
		}
	}
	return c.JSON(http.StatusOK, dtos)
}

func (cr *CustomListRoutes) get(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, false)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)

	entryCount, err := db.SelectScalar[int](ctx, cr.pool, "SELECT COUNT(*) FROM custom_list_to_content WHERE custom_list_id = $1", cl.ID)
	if err != nil {
		return err
	}

	type entryRow struct {
		models.CustomListToContent
		ContentID   *string         `db:"content_id"`
		ContentData json.RawMessage `db:"content_data"`
		MetaData    json.RawMessage `db:"meta_data"`
	}

	entryRows, err := db.Select[entryRow](ctx, cr.pool, `
		SELECT clc.*,
			c.id AS content_id,
			row_to_json(c.*) AS content_data,
			cm.data AS meta_data
		FROM custom_list_to_content clc
		LEFT JOIN content c ON c.library_id = clc.library_id AND c.uri = clc.uri
		LEFT JOIN content_metadata cm ON cm.uri = clc.uri AND cm.library_id = clc.library_id
		WHERE clc.custom_list_id = $1
		ORDER BY (clc."order" IS NULL), clc."order", clc.created_at
	`, cl.ID)
	if err != nil {
		return err
	}

	entries := make([]customListEntryDTO, len(entryRows))
	for i, r := range entryRows {
		var contentDTO *ContentDTO
		if r.ContentID != nil && r.ContentData != nil {
			var content models.Content
			if err := json.Unmarshal(r.ContentData, &content); err != nil {
				return fmt.Errorf("unmarshal content %s: %w", *r.ContentID, err)
			}
			dto := contentToDTO(content, contentDTOOpts{
				meta:        r.MetaData,
				includeMeta: true,
			})
			contentDTO = &dto
		}
		entries[i] = customListEntryDTO{
			ID:        r.ID,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			LibraryID: r.LibraryID,
			URI:       r.URI,
			Content:   contentDTO,
			Notes:     r.Notes,
			Order:     r.Order,
		}
	}

	return c.JSON(http.StatusOK, customListDetailDTO{
		ID:          cl.ID,
		CreatedAt:   cl.CreatedAt,
		UpdatedAt:   cl.UpdatedAt,
		Name:        cl.Name,
		Description: cl.Description,
		Visibility:  cl.Visibility,
		UserID:      cl.UserID,
		EntryCount:  entryCount,
		Entries:     entries,
	})
}

type customListUpsertRequest struct {
	Name        string  `json:"name"        validate:"notblank,max=100"`
	Description *string `json:"description" validate:"omitempty,max=5000"`
	Visibility  string  `json:"visibility"  validate:"required,oneof=public private unlisted"`
}

func (cr *CustomListRoutes) create(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	var req customListUpsertRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	name := strings.TrimSpace(req.Name)

	ctx := reqCtx(c)
	now := time.Now().UTC()
	id := models.MakeCustomListID()

	_, err = cr.pool.Exec(ctx, `
		INSERT INTO custom_lists (id, created_at, updated_at, name, description, visibility, user_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, id, now, now, name, req.Description, req.Visibility, user.ID)
	if err != nil {
		return err
	}

	return c.JSON(http.StatusOK, customListDTO{
		ID:              id,
		CreatedAt:       now,
		UpdatedAt:       now,
		Name:            name,
		Description:     req.Description,
		Visibility:      req.Visibility,
		UserID:          user.ID,
		EntryCount:      new(0),
		CoverContentIDs: []string{},
	})
}

func (cr *CustomListRoutes) update(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	var req customListUpsertRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	name := strings.TrimSpace(req.Name)

	ctx := reqCtx(c)
	now := time.Now().UTC()

	_, err = cr.pool.Exec(ctx, `
		UPDATE custom_lists SET name = $1, description = $2, visibility = $3, updated_at = $4
		WHERE id = $5
	`, name, req.Description, req.Visibility, now, cl.ID)
	if err != nil {
		return err
	}

	return okResponse(c)
}

func (cr *CustomListRoutes) delete(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	_, err = cr.pool.Exec(ctx, "DELETE FROM custom_lists WHERE id = $1", cl.ID)
	if err != nil {
		return err
	}

	return okResponse(c)
}

type createEntryRequest struct {
	ContentID string  `json:"content_id"`
	Notes     *string `json:"notes"`
}

func (cr *CustomListRoutes) createEntry(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	var req createEntryRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	ctx := reqCtx(c)

	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	content, err := db.SelectOne[models.Content](ctx, tx, "SELECT * FROM content WHERE id = $1", req.ContentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	var maxOrder *int
	err = tx.QueryRow(ctx,
		"SELECT MAX(\"order\") FROM custom_list_to_content WHERE custom_list_id = $1", cl.ID).Scan(&maxOrder)
	if err != nil {
		return err
	}
	orderValue := 1
	if maxOrder != nil {
		orderValue = *maxOrder + 1
	}

	now := time.Now().UTC()
	entryID := models.MakeCustomListContentID()

	_, err = tx.Exec(ctx, `
		INSERT INTO custom_list_to_content (id, created_at, updated_at, custom_list_id, library_id, uri, notes, "order")
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, entryID, now, now, cl.ID, content.LibraryID, content.URI, req.Notes, orderValue)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Content already in list")
	}

	// Update list timestamp
	_, err = tx.Exec(ctx, "UPDATE custom_lists SET updated_at = $1 WHERE id = $2", now, cl.ID)
	if err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return okResponse(c)
}

type reorderEntriesRequest struct {
	CTCIDs []string `json:"ctc_ids"`
}

func (cr *CustomListRoutes) reorderEntries(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	var req reorderEntriesRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if len(req.CTCIDs) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "ctc_ids are required")
	}

	ctx := reqCtx(c)

	// Verify all entries belong to the list
	var count int
	err = cr.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM custom_list_to_content
		WHERE custom_list_id = $1 AND id = ANY($2)
	`, cl.ID, req.CTCIDs).Scan(&count)
	if err != nil {
		return err
	}
	if count != len(req.CTCIDs) {
		return echo.NewHTTPError(http.StatusBadRequest, "Some entries do not belong to the list")
	}

	tx, err := cr.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i, id := range req.CTCIDs {
		_, err = tx.Exec(ctx, `UPDATE custom_list_to_content SET "order" = $1 WHERE id = $2`, i, id)
		if err != nil {
			return err
		}
	}

	now := time.Now().UTC()
	_, _ = tx.Exec(ctx, "UPDATE custom_lists SET updated_at = $1 WHERE id = $2", now, cl.ID)

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	return okResponse(c)
}

func (cr *CustomListRoutes) updateEntry(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	entryID := c.Param("entry_id")

	_, err = db.SelectOne[models.CustomListToContent](ctx, cr.pool,
		"SELECT * FROM custom_list_to_content WHERE id = $1 AND custom_list_id = $2", entryID, cl.ID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Entry not found")
	}
	if err != nil {
		return err
	}

	var rawBody map[string]json.RawMessage
	if err := json.NewDecoder(c.Request().Body).Decode(&rawBody); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}

	now := time.Now().UTC()
	args := pgx.NamedArgs{"id": entryID, "now": now}
	sets := "updated_at = @now"

	if v, ok := rawBody["notes"]; ok {
		var notes *string
		_ = json.Unmarshal(v, &notes)
		args["notes"] = notes
		sets += ", notes = @notes"
	}
	if v, ok := rawBody["order"]; ok {
		var order *int
		_ = json.Unmarshal(v, &order)
		args["order"] = order
		sets += `, "order" = @order`
	}

	_, err = cr.pool.Exec(ctx,
		"UPDATE custom_list_to_content SET "+sets+" WHERE id = @id", args)
	if err != nil {
		return err
	}

	_, _ = cr.pool.Exec(ctx, "UPDATE custom_lists SET updated_at = $1 WHERE id = $2", now, cl.ID)

	return okResponse(c)
}

func (cr *CustomListRoutes) deleteEntry(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	cl, err := cr.getListForUser(c, user, true)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	entryID := c.Param("entry_id")

	result, err := cr.pool.Exec(ctx,
		"DELETE FROM custom_list_to_content WHERE id = $1 AND custom_list_id = $2",
		entryID, cl.ID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Entry not found")
	}

	now := time.Now().UTC()
	_, _ = cr.pool.Exec(ctx, "UPDATE custom_lists SET updated_at = $1 WHERE id = $2", now, cl.ID)

	return okResponse(c)
}
