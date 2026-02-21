package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/scanner"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type LibraryRoutes struct {
	pool      *pgxpool.Pool
	scanQueue *scanner.Queue
}

func (lr *LibraryRoutes) Register(g *echo.Group) {
	g.GET("", lr.list)
	g.POST("/scan", lr.scan)
	g.POST("/:id_or_new", lr.upsert)
	g.DELETE("/:id", lr.delete)
}

type LibrarySourceDTO struct {
	PathURI string `json:"path_uri"`
}

type LibraryDTO struct {
	ID               string             `json:"id"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
	Name             string             `json:"name"`
	Type             string             `json:"type"`
	ContentCount     *int               `json:"content_count"`
	RootContentCount *int               `json:"root_content_count"`
	ScannedAt        *time.Time         `json:"scanned_at"`
	Sources          []LibrarySourceDTO `json:"sources"`
}

func libraryToDTO(lib models.Library, contentCount, rootContentCount *int) LibraryDTO {
	var sources []LibrarySourceDTO
	_ = json.Unmarshal(lib.Sources, &sources)
	if sources == nil {
		sources = []LibrarySourceDTO{}
	}
	return LibraryDTO{
		ID:               lib.ID,
		CreatedAt:        lib.CreatedAt,
		UpdatedAt:        lib.UpdatedAt,
		Name:             lib.Name,
		Type:             lib.Type,
		ContentCount:     contentCount,
		RootContentCount: rootContentCount,
		ScannedAt:        lib.ScannedAt,
		Sources:          sources,
	}
}

type upsertLibraryRequest struct {
	Name    string             `json:"name"`
	Type    string             `json:"type"`
	Sources []LibrarySourceDTO `json:"sources"`
}

func (lr *LibraryRoutes) list(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	type libraryRow struct {
		models.Library
		ContentCount     *int `db:"content_count"`
		RootContentCount *int `db:"root_content_count"`
	}
	items, err := db.Select[libraryRow](ctx, lr.pool, `
		SELECT l.*,
			(SELECT COUNT(*) FROM content WHERE library_id = l.id) AS content_count,
			(SELECT COUNT(*) FROM content WHERE library_id = l.id AND parent_id IS NULL) AS root_content_count
		FROM libraries l
		ORDER BY l.name
	`)
	if err != nil {
		return err
	}

	result := make([]LibraryDTO, len(items))
	for i, r := range items {
		result[i] = libraryToDTO(r.Library, r.ContentCount, r.RootContentCount)
	}
	return c.JSON(http.StatusOK, result)
}

func (lr *LibraryRoutes) scan(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	idParam := c.QueryParam("id")
	force := c.QueryParam("force") == "true"

	var (
		libraries []models.Library
		err       error
	)
	if idParam != "" {
		ids := strings.Split(idParam, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		libraries, err = db.Select[models.Library](ctx, lr.pool, "SELECT * FROM libraries WHERE id = ANY($1)", ids)
		if err != nil {
			return err
		}
	} else {
		libraries, err = db.Select[models.Library](ctx, lr.pool, "SELECT * FROM libraries")
		if err != nil {
			return err
		}
	}

	for _, lib := range libraries {
		lr.scanQueue.Enqueue(lib.ID, force, nil)
	}

	return okResponse(c)
}

func (lr *LibraryRoutes) upsert(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	idOrNew := c.Param("id_or_new")

	var req upsertLibraryRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	for _, source := range req.Sources {
		info, err := os.Stat(source.PathURI)
		if err != nil || !info.IsDir() {
			return echo.NewHTTPError(http.StatusBadRequest,
				"Source path does not exist or is not a directory: "+source.PathURI)
		}
	}

	sourcesJSON, err := json.Marshal(req.Sources)
	if err != nil {
		return err
	}

	now := time.Now().UTC()

	if idOrNew == "new" {
		id := models.MakeLibraryID()
		_, err = lr.pool.Exec(ctx, `
			INSERT INTO libraries (id, created_at, updated_at, name, type, sources)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, id, now, now, req.Name, req.Type, sourcesJSON)
		if err != nil {
			return err
		}

		lib, err := getLibrary(ctx, lr.pool, id)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, libraryToDTO(lib, nil, nil))
	}

	_, err = getLibrary(ctx, lr.pool, idOrNew)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}

	_, err = lr.pool.Exec(ctx, `
		UPDATE libraries SET name = $1, sources = $2, updated_at = $3 WHERE id = $4
	`, req.Name, sourcesJSON, now, idOrNew)
	if err != nil {
		return err
	}

	lib, err := getLibrary(ctx, lr.pool, idOrNew)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, libraryToDTO(lib, nil, nil))
}

func (lr *LibraryRoutes) delete(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	id := c.Param("id")
	result, err := lr.pool.Exec(ctx, "DELETE FROM libraries WHERE id = $1", id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}
	return okResponse(c)
}

func getLibrary(ctx context.Context, pool *pgxpool.Pool, id string) (models.Library, error) {
	lib, err := db.SelectOne[models.Library](ctx, pool, "SELECT * FROM libraries WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.Library{}, echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}
	return lib, err
}


