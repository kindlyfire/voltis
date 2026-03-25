package scanner

import (
	"context"
	"encoding/json"
	"log/slog"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/lib/tasks"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueBroadcaster is implemented by the WebSocket hub.
type QueueBroadcaster interface {
	BroadcastTaskEvent(task *models.Task, progress json.RawMessage)
	BroadcastScanQueue(libraryIDs []string)
}

type Queue struct {
	manager *tasks.Manager
	pool    *pgxpool.Pool
	hub     QueueBroadcaster
}

func NewQueue(manager *tasks.Manager, pool *pgxpool.Pool, hub QueueBroadcaster) *Queue {
	q := &Queue{manager: manager, pool: pool, hub: hub}
	ScanTask.OnUpdate = func(task *models.Task, progress json.RawMessage) {
		hub.BroadcastTaskEvent(task, progress)
		if task.Status != models.TaskStatusInProgress && task.Status != models.TaskStatusPending {
			q.broadcastQueue()
		}
	}
	return q
}

func (q *Queue) Enqueue(libraryID string, force bool, filterPaths []string) {
	if len(filterPaths) == 0 {
		for _, input := range q.manager.Pending("scan_library") {
			si := input.(ScanInput)
			if si.LibraryID == libraryID && len(si.FilterPaths) == 0 {
				slog.Info("[scanner] scan already queued", "library", libraryID)
				return
			}
		}
	}

	ctx := context.Background()
	lib, err := db.SelectOne[models.Library](ctx, q.pool,
		"SELECT * FROM libraries WHERE id = $1", libraryID)
	if err != nil {
		slog.Error("[scanner] library not found", "library", libraryID, "err", err)
		return
	}

	type source struct {
		PathURI string `json:"path_uri"`
	}
	var sources []source
	_ = json.Unmarshal(lib.Sources, &sources)

	paths := fp.Map(sources, func(s source) string { return s.PathURI })

	handle, err := q.manager.Push(ScanTask, ScanInput{
		LibraryID:   lib.ID,
		LibraryType: lib.Type,
		Sources:     paths,
		Force:       force,
		FilterPaths: filterPaths,
	}, nil)
	if err != nil {
		slog.Error("[scanner] failed to push scan task", "library", lib.ID, "err", err)
		return
	}

	q.broadcastQueue()

	go func() {
		resultAny, err := handle.Wait()
		if err != nil {
			slog.Error("[scanner] scan failed", "library", lib.ID, "err", err)
			return
		}
		result := resultAny.(ScanResult)
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
}

func (q *Queue) broadcastQueue() {
	pending := q.manager.Pending("scan_library")
	ids := fp.Dedup(fp.Map(pending, func(input any) string {
		return input.(ScanInput).LibraryID
	}))
	q.hub.BroadcastScanQueue(ids)
}
