package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"context"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ContentRefRoutes struct {
	pool *pgxpool.Pool
}

func (cr *ContentRefRoutes) Register(g *echo.Group) {
	g.GET("/refs/:library_id", cr.listLibraryURIs)
	g.GET("/broken-refs", cr.brokenRefsSummary)
	g.GET("/broken-refs/:library_id", cr.listBrokenRefs)
	g.POST("/broken-refs/:library_id", cr.fixBrokenRefs)
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

	contentURIs, err := collectStrings(ctx, cr.pool,
		"SELECT uri FROM content WHERE library_id = $1", libraryID)
	if err != nil {
		return err
	}

	userURIs, err := collectStrings(ctx, cr.pool,
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
	LibraryID *string `json:"library_id"`
	Count     int     `json:"count"`
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

func (cr *ContentRefRoutes) listBrokenRefs(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	ctx := reqCtx(c)
	libraryID := c.Param("library_id")
	search := c.QueryParam("search")
	limitParam := c.QueryParam("limit")
	offsetParam := c.QueryParam("offset")

	offset := 0
	if offsetParam != "" {
		offset, _ = strconv.Atoi(offsetParam)
	}
	var limit *int
	if limitParam != "" {
		v, _ := strconv.Atoi(limitParam)
		if v > 0 {
			limit = &v
		}
	}

	args := pgx.NamedArgs{
		"user_id":    user.ID,
		"library_id": libraryID,
	}

	searchClause := ""
	if search != "" {
		args["search"] = "%" + search + "%"
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
	if limit != nil {
		dataQuery += fmt.Sprintf(" LIMIT %d", *limit)
	}
	if offset > 0 {
		dataQuery += fmt.Sprintf(" OFFSET %d", offset)
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

func collectStrings(ctx context.Context, pool *pgxpool.Pool, query string, args ...any) ([]string, error) {
	rows, err := pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, nil
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
	defer tx.Rollback(ctx)

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

	return c.NoContent(http.StatusOK)
}
