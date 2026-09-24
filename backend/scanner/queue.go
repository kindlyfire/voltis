package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/lib/tasks"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Queue struct {
	manager *tasks.Manager
	pool    *pgxpool.Pool
	def     *tasks.TaskDef
}

func NewQueue(manager *tasks.Manager, pool *pgxpool.Pool, notify Notifier) *Queue {
	def := NewScanTask(notify)
	manager.Register(def)
	return &Queue{manager: manager, pool: pool, def: def}
}

func (q *Queue) Enqueue(libraryID string, force bool, filterPaths []string) (string, error) {
	if len(filterPaths) == 0 {
		for _, p := range q.manager.Pending("scan_library") {
			si, ok := p.Input.(ScanInput)
			if ok && si.LibraryID == libraryID && len(si.FilterPaths) == 0 {
				slog.Info("[scanner] scan already queued", "library", libraryID)
				return p.ID, nil
			}
		}
	}

	ctx := context.Background()
	lib, err := db.SelectOne[models.Library](ctx, q.pool,
		"SELECT * FROM libraries WHERE id = $1", libraryID)
	if err != nil {
		return "", fmt.Errorf("library not found: %s: %w", libraryID, err)
	}

	type source struct {
		PathURI string `json:"path_uri"`
	}
	var sources []source
	_ = json.Unmarshal(lib.Sources, &sources)

	paths := fp.Map(sources, func(s source) string { return s.PathURI })

	handle, err := q.manager.Push(q.def, ScanInput{
		LibraryID:   lib.ID,
		LibraryType: lib.Type,
		Settings:    models.ParseLibrarySettings(lib.Settings),
		Sources:     paths,
		Force:       force,
		FilterPaths: filterPaths,
	})
	if err != nil {
		return "", fmt.Errorf("push scan task: %w", err)
	}

	go func() {
		resultAny, err := handle.Wait()
		if err != nil {
			slog.Error("[scanner] scan failed", "library", lib.ID, "err", err)
			return
		}
		result, ok := resultAny.(ScanResult)
		if !ok {
			return
		}
		slog.Info("[scanner] scan complete",
			"library", lib.ID,
			"added", result.Added,
			"updated", result.Updated,
			"removed", result.Removed,
			"failed", result.Failed,
			"unchanged", result.Unchanged,
			"duration", result.Duration,
		)
	}()

	return handle.ID(), nil
}
