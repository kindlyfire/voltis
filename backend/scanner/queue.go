package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueBroadcaster is implemented by the WebSocket hub.
type QueueBroadcaster interface {
	db.TaskEventBroadcaster
	BroadcastScanQueue(libraryIDs []string)
}

type scanJob struct {
	LibraryID   string
	Force       bool
	FilterPaths []string
}

type Queue struct {
	pool    *pgxpool.Pool
	hub     QueueBroadcaster
	mu      sync.Mutex
	jobs    []scanJob
	running bool
}

func NewQueue(pool *pgxpool.Pool, hub QueueBroadcaster) *Queue {
	return &Queue{pool: pool, hub: hub}
}

func (q *Queue) Enqueue(libraryID string, force bool, filterPaths []string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Skip duplicate full scans
	if len(filterPaths) == 0 {
		for _, j := range q.jobs {
			if j.LibraryID == libraryID && len(j.FilterPaths) == 0 {
				slog.Info("[scanner] scan already queued", "library", libraryID)
				return
			}
		}
	}

	q.jobs = append(q.jobs, scanJob{
		LibraryID:   libraryID,
		Force:       force,
		FilterPaths: filterPaths,
	})
	slog.Info("[scanner] scan enqueued", "library", libraryID, "queue_size", len(q.jobs))

	if !q.running {
		q.running = true
		go q.process()
	}
}

func (q *Queue) broadcastQueue() {
	q.mu.Lock()
	ids := fp.Map(q.jobs, func(j scanJob) string { return j.LibraryID })
	q.mu.Unlock()
	q.hub.BroadcastScanQueue(ids)
}

func (q *Queue) process() {
	stopBroadcast := fp.NewTicker(1000, func() {
		q.broadcastQueue()
	})
	defer stopBroadcast()

	for {
		var job *scanJob
		fp.WithMutex(&q.mu, func() {
			if len(q.jobs) == 0 {
				q.running = false
			} else {
				job = &q.jobs[0]
				q.jobs = q.jobs[1:]
			}
		})
		if job == nil {
			return
		}

		q.broadcastQueue()
		q.runJob(*job)
	}
}

func (q *Queue) runJob(job scanJob) {
	ctx := context.Background()

	lib, err := db.SelectOne[models.Library](ctx, q.pool,
		"SELECT * FROM libraries WHERE id = $1", job.LibraryID)
	if err != nil {
		slog.Error("[scanner] library not found", "library", job.LibraryID, "err", err)
		return
	}

	// Parse sources
	type source struct {
		PathURI string `json:"path_uri"`
	}
	var sources []source
	_ = json.Unmarshal(lib.Sources, &sources)

	paths := make([]string, len(sources))
	for i, s := range sources {
		paths[i] = s.PathURI
	}

	result, err := Scan(ctx, q.pool, lib.ID, lib.Type, paths, ScanOptions{
		Force:       job.Force,
		FilterPaths: job.FilterPaths,
		Hub:         q.hub,
	})
	if err != nil {
		slog.Error("[scanner] scan failed", "library", lib.ID, "err", err)
		return
	}
	if result != nil {
		slog.Info("[scanner] scan complete",
			"library", lib.ID,
			"added", result.Added,
			"updated", result.Updated,
			"removed", result.Removed,
			"unchanged", result.Unchanged,
		)
	}
}
