package routes

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"voltis/models"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

type LibraryRoutes struct {
	db *sqlx.DB
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

	var rows []struct {
		models.Library
		ContentCount     *int `db:"content_count"`
		RootContentCount *int `db:"root_content_count"`
	}
	err := lr.db.Select(&rows, `
		SELECT l.*,
			(SELECT COUNT(*) FROM content WHERE library_id = l.id) AS content_count,
			(SELECT COUNT(*) FROM content WHERE library_id = l.id AND parent_id IS NULL) AS root_content_count
		FROM libraries l
		ORDER BY l.name
	`)
	if err != nil {
		return err
	}

	result := make([]LibraryDTO, len(rows))
	for i, r := range rows {
		result[i] = libraryToDTO(r.Library, r.ContentCount, r.RootContentCount)
	}
	return c.JSON(http.StatusOK, result)
}

func (lr *LibraryRoutes) scan(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	idParam := c.QueryParam("id")
	force := c.QueryParam("force") == "true"

	var libraries []models.Library
	if idParam != "" {
		ids := strings.Split(idParam, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		query, args, err := sqlx.In("SELECT * FROM libraries WHERE id IN (?)", ids)
		if err != nil {
			return err
		}
		err = lr.db.Select(&libraries, lr.db.Rebind(query), args...)
		if err != nil {
			return err
		}
	} else {
		if err := lr.db.Select(&libraries, "SELECT * FROM libraries"); err != nil {
			return err
		}
	}

	for _, lib := range libraries {
		if err := enqueueScan(lr.db, lib.ID, force); err != nil {
			return err
		}
	}

	return okResponse(c)
}

func (lr *LibraryRoutes) upsert(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

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
		_, err = lr.db.Exec(`
			INSERT INTO libraries (id, created_at, updated_at, name, type, sources)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, id, now, now, req.Name, req.Type, sourcesJSON)
		if err != nil {
			return err
		}

		var lib models.Library
		if err := lr.db.Get(&lib, "SELECT * FROM libraries WHERE id = $1", id); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, libraryToDTO(lib, nil, nil))
	}

	var lib models.Library
	err = lr.db.Get(&lib, "SELECT * FROM libraries WHERE id = $1", idOrNew)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}

	_, err = lr.db.Exec(`
		UPDATE libraries SET name = $1, sources = $2, updated_at = $3 WHERE id = $4
	`, req.Name, sourcesJSON, now, idOrNew)
	if err != nil {
		return err
	}

	if err := lr.db.Get(&lib, "SELECT * FROM libraries WHERE id = $1", idOrNew); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, libraryToDTO(lib, nil, nil))
}

func (lr *LibraryRoutes) delete(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	id := c.Param("id")
	result, err := lr.db.Exec("DELETE FROM libraries WHERE id = $1", id)
	if err != nil {
		return err
	}

	rows, _ := result.RowsAffected()
	if rows == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}
	return okResponse(c)
}

func enqueueScan(db *sqlx.DB, libraryID string, force bool) error {
	// TODO
	_ = db
	_ = libraryID
	_ = force
	return nil
}
