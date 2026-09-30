package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sync"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/linking"
	"voltis/models"
	"voltis/providers"
	"voltis/scanner"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type LibraryRoutes struct {
	pool      *pgxpool.Pool
	scanQueue *scanner.Queue
	links     *linking.Service
	reg       *providers.Registry
	ctx       context.Context // the server's
}

func (lr *LibraryRoutes) Register(g *echo.Group) {
	g.GET("", lr.list)
	g.POST("/scan", adminOnly(lr.scan))
	g.POST("/:id_or_new", adminOnly(lr.upsert))
	g.DELETE("/:id", adminOnly(lr.delete))
}

type LibraryDTO struct {
	ID               string                 `json:"id"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Name             string                 `json:"name"`
	Type             string                 `json:"type"`
	ContentCount     *int                   `json:"content_count"`
	RootContentCount *int                   `json:"root_content_count"`
	ScannedAt        *time.Time             `json:"scanned_at"`
	Sources          []models.LibrarySource `json:"sources"`
	Settings         models.LibrarySettings `json:"settings"`
}

func libraryToDTO(lib models.Library, contentCount, rootContentCount *int) LibraryDTO {
	return LibraryDTO{
		ID:               lib.ID,
		CreatedAt:        lib.CreatedAt,
		UpdatedAt:        lib.UpdatedAt,
		Name:             lib.Name,
		Type:             lib.Type,
		ContentCount:     contentCount,
		RootContentCount: rootContentCount,
		ScannedAt:        lib.ScannedAt,
		Sources:          models.ParseLibrarySources(lib.Sources),
		Settings:         models.ParseLibrarySettings(lib.Settings),
	}
}

type upsertLibraryRequest struct {
	Name     string                 `json:"name"`
	Type     string                 `json:"type"`
	Sources  []models.LibrarySource `json:"sources"`
	Settings json.RawMessage        `json:"settings"`
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

type scanResponse struct {
	TaskIDs []string `json:"task_ids"`
}

type scanRequest struct {
	IDs        []string `json:"ids"`
	ContentIDs []string `json:"content_ids"`
	Force      bool     `json:"force"`
}

func (lr *LibraryRoutes) scan(c echo.Context) error {
	ctx := reqCtx(c)

	var req scanRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	if len(req.IDs) > 0 && len(req.ContentIDs) > 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "ids and content_ids are mutually exclusive")
	}

	// An explicit empty list must not fall through to scanning every library.
	if req.ContentIDs != nil {
		if len(req.ContentIDs) == 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "content_ids is empty")
		}
		ids, err := lr.scanContentIDs(ctx, fp.Dedup(req.ContentIDs))
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, scanResponse{TaskIDs: ids})
	}

	var (
		libraries []models.Library
		err       error
	)
	if len(req.IDs) > 0 {
		libraries, err = db.Select[models.Library](ctx, lr.pool, "SELECT * FROM libraries WHERE id = ANY($1)", req.IDs)
	} else {
		libraries, err = db.Select[models.Library](ctx, lr.pool, "SELECT * FROM libraries")
	}
	if err != nil {
		return err
	}

	taskIDs := []string{}
	for _, lib := range libraries {
		id, err := lr.scanQueue.Enqueue(lib.ID, req.Force, nil)
		if err != nil {
			return err
		}
		taskIDs = append(taskIDs, id)
	}

	return c.JSON(http.StatusOK, scanResponse{TaskIDs: taskIDs})
}

// maxScanFileURIs caps a content scan, which lists each file, children included, as an explicit
// URI in a forced scan.
const maxScanFileURIs = 5000

func (lr *LibraryRoutes) scanContentIDs(ctx context.Context, contentIDs []string) ([]string, error) {
	rows, err := db.Select[models.Content](ctx, lr.pool, "SELECT "+models.ContentColumns("")+" FROM content WHERE id = ANY($1)", contentIDs)
	if err != nil {
		return nil, err
	}
	if len(rows) != len(contentIDs) {
		return nil, echo.NewHTTPError(http.StatusNotFound, "One or more content IDs not found")
	}

	libraryID := rows[0].LibraryID
	if slices.ContainsFunc(rows[1:], func(r models.Content) bool { return r.LibraryID != libraryID }) {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "All content IDs must belong to the same library")
	}

	var fileURIs []string
	for _, r := range rows {
		if r.FileURI != nil {
			fileURIs = append(fileURIs, *r.FileURI)
		}
	}

	childURIs, err := db.SelectScalars[string](ctx, lr.pool,
		"SELECT file_uri FROM content WHERE parent_id = ANY($1) AND file_uri IS NOT NULL", contentIDs)
	if err != nil {
		return nil, err
	}
	fileURIs = append(fileURIs, childURIs...)

	if len(fileURIs) == 0 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "No files to scan")
	}
	if len(fileURIs) > maxScanFileURIs {
		return nil, echo.NewHTTPError(http.StatusBadRequest,
			fmt.Sprintf("Too many files to scan (%d, at most %d)", len(fileURIs), maxScanFileURIs))
	}

	taskID, err := lr.scanQueue.Enqueue(libraryID, true, fileURIs)
	if err != nil {
		return nil, err
	}
	return []string{taskID}, nil
}

func (lr *LibraryRoutes) upsert(c echo.Context) error {
	ctx := reqCtx(c)
	idOrNew := c.Param("id_or_new")

	var req upsertLibraryRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	prefixes := map[string]bool{}
	for _, source := range req.Sources {
		info, err := os.Stat(source.PathURI)
		if err != nil || !info.IsDir() {
			return echo.NewHTTPError(http.StatusBadRequest,
				"Source path does not exist or is not a directory: "+source.PathURI)
		}
		// Their overrides would compete for the same files.
		if p, ok := source.Prefix(); ok {
			if prefixes[p] {
				return echo.NewHTTPError(http.StatusBadRequest, "Duplicate source: "+source.PathURI)
			}
			prefixes[p] = true
		}
	}

	if req.Sources == nil {
		req.Sources = []models.LibrarySource{}
	}
	sourcesJSON, err := json.Marshal(req.Sources)
	if err != nil {
		return err
	}

	// Omitted settings keep the stored ones, or the defaults for a new library; omitted keys take
	// their defaults.
	var settingsJSON []byte
	if len(req.Settings) > 0 && string(req.Settings) != "null" {
		settings := models.DefaultLibrarySettings()
		if err := json.Unmarshal(req.Settings, &settings); err != nil {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid settings: "+err.Error())
		}
		mode := settings.BookSeriesInference
		if mode != models.BookSeriesInferenceOff && mode != models.BookSeriesInferenceConservative {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid book_series_inference: "+mode)
		}
		if settings.AutoMatch == nil {
			settings.AutoMatch = models.Switches{}
		}
		if settingsJSON, err = json.Marshal(settings); err != nil {
			return err
		}
	}

	now := time.Now().UTC()

	id := idOrNew
	var before models.Library // a new library matches nothing
	if idOrNew == "new" {
		id = models.MakeLibraryID()
		_, err = lr.pool.Exec(ctx, `
			INSERT INTO libraries (id, created_at, updated_at, name, type, sources, settings)
			VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7::jsonb, '{}'))
		`, id, now, now, req.Name, req.Type, sourcesJSON, settingsJSON)
	} else {
		if before, err = getLibrary(ctx, lr.pool, idOrNew); err != nil {
			return err
		}
		_, err = lr.pool.Exec(ctx, `
			UPDATE libraries SET name = $1, sources = $2, updated_at = $3, settings = COALESCE($5::jsonb, settings)
			WHERE id = $4
		`, req.Name, sourcesJSON, now, idOrNew, settingsJSON)
	}
	if err != nil {
		return err
	}

	lib, err := getLibrary(ctx, lr.pool, id)
	if err != nil {
		return err
	}
	// Series newly covered are due already if never matched, but those unmatched are not. Coverage
	// that only shrank leaves links alone.
	var grown []string
	for _, p := range lr.reg.All() {
		if linking.AutoMatchGrew(before, lib, p.Name()) {
			grown = append(grown, p.Name())
		}
	}
	// After responding: it rewrites every covered link, which takes a while in a large library, and
	// only makes series due early.
	if len(grown) > 0 {
		autoMatching.Go(func() {
			ctx, cancel := context.WithTimeout(lr.ctx, 5*time.Minute)
			defer cancel()
			if err := lr.links.MatchNow(ctx, []string{id}, grown); err != nil {
				slog.Warn("[libraries] failed to make newly auto-matched series due", "library", id, "err", err)
			}
		})
	}
	return c.JSON(http.StatusOK, libraryToDTO(lib, nil, nil))
}

// autoMatching tracks the saves' background MatchNow runs, for tests to wait on.
var autoMatching sync.WaitGroup

func (lr *LibraryRoutes) delete(c echo.Context) error {
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
