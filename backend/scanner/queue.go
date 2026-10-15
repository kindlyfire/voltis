package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/lib/notify"
	"voltis/lib/tasks"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Queue struct {
	manager  *tasks.Manager
	pool     *pgxpool.Pool
	def      *tasks.TaskDef
	notifier Notifier

	// mu guards libs. Each library's mutex serialises its pending check and push in Enqueue.
	mu   sync.Mutex
	libs map[string]*sync.Mutex
}

func NewQueue(manager *tasks.Manager, pool *pgxpool.Pool, notifier Notifier, store *metadata.Store) *Queue {
	def := NewScanTask(notifier, store)
	manager.Register(def)
	return &Queue{manager: manager, pool: pool, def: def, notifier: notifier, libs: map[string]*sync.Mutex{}}
}

// Scanning reports whether a scan of the library runs or waits.
func (q *Queue) Scanning(libraryID string) bool {
	return slices.ContainsFunc(q.manager.Pending(TaskName), func(p tasks.Pending) bool {
		si, ok := p.Input.(ScanInput)
		return ok && si.LibraryID == libraryID
	})
}

// libraryLock returns the library's mutex. Entries are never removed; there are few libraries.
func (q *Queue) libraryLock(id string) *sync.Mutex {
	q.mu.Lock()
	defer q.mu.Unlock()
	l, ok := q.libs[id]
	if !ok {
		l = &sync.Mutex{}
		q.libs[id] = l
	}
	return l
}

func (q *Queue) Enqueue(libraryID string, force bool, filterPaths []string) (string, error) {
	return q.enqueue(libraryID, force, filterPaths, "")
}

var (
	ErrTaskNotFound = errors.New("task not found")
	ErrNotFinished  = errors.New("task hasn't finished")
	ErrLibraryGone  = errors.New("library not found")
)

// Retry queues the same scan again, and the old task's items follow it. A retried full scan
// joins another that's already pending.
func (q *Queue) Retry(ctx context.Context, oldTaskID string) (string, error) {
	old, err := db.SelectOne[models.Task](ctx, q.pool, "SELECT * FROM tasks WHERE id = $1", oldTaskID)
	switch {
	case errors.Is(err, pgx.ErrNoRows) || err == nil && old.Name != TaskName:
		return "", ErrTaskNotFound
	case err != nil:
		return "", err
	case old.Status < models.TaskStatusCompleted:
		return "", ErrNotFinished
	}
	var in ScanInput
	if err := json.Unmarshal(old.Input, &in); err != nil {
		return "", err
	}
	id, err := q.enqueue(in.LibraryID, in.Force, in.FilterPaths, oldTaskID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrLibraryGone
	}
	return id, err
}

// joinScan moves retryOf's items onto the pending scan target. It locks target's row, which its
// terminal persist updates, so the items either exist before that or the scan is seen as ended.
func (q *Queue) joinScan(ctx context.Context, target, libraryID, retryOf string) (bool, error) {
	var users []string
	joined := false
	err := db.WithTx(ctx, q.pool, func(tx pgx.Tx) error {
		var status int
		err := tx.QueryRow(ctx, "SELECT status FROM tasks WHERE id = $1 FOR UPDATE", target).Scan(&status)
		if err != nil || status >= models.TaskStatusCompleted {
			return err
		}
		lib, err := db.SelectOne[models.Library](ctx, tx, "SELECT * FROM libraries WHERE id = $1", libraryID)
		if err != nil {
			return fmt.Errorf("library not found: %s: %w", libraryID, err)
		}
		if users, err = notify.DropScanItems(ctx, tx, retryOf); err != nil {
			return err
		}
		created, err := notify.CreateScanItems(ctx, tx, target, notify.ScanData{LibraryID: lib.ID, LibraryName: lib.Name})
		users = append(users, created...)
		joined = true
		return err
	})
	if err != nil {
		return false, err
	}
	if joined {
		q.notifier.NotificationsChanged(users)
	}
	return joined, nil
}

// enqueue pushes a scan of the library with an item per admin. retryOf is the task whose items
// the new scan takes over.
func (q *Queue) enqueue(libraryID string, force bool, filterPaths []string, retryOf string) (string, error) {
	l := q.libraryLock(libraryID)
	l.Lock()
	defer l.Unlock()

	ctx := context.Background()
	if len(filterPaths) == 0 {
		for _, p := range q.manager.Pending(TaskName) {
			si, ok := p.Input.(ScanInput)
			// The retried task may still be pending right after its terminal commit.
			if ok && p.ID != retryOf && si.LibraryID == libraryID && len(si.FilterPaths) == 0 {
				slog.Info("[scanner] scan already queued", "library", libraryID)
				if retryOf == "" {
					return p.ID, nil
				}
				joined, err := q.joinScan(ctx, p.ID, libraryID, retryOf)
				if err != nil {
					return "", err
				}
				if joined {
					return p.ID, nil
				}
				// The target ended meanwhile: start a fresh scan that takes over the items.
				break
			}
		}
	}

	lib, err := db.SelectOne[models.Library](ctx, q.pool,
		"SELECT * FROM libraries WHERE id = $1", libraryID)
	if err != nil {
		return "", fmt.Errorf("library not found: %s: %w", libraryID, err)
	}

	paths := fp.Map(models.ParseLibrarySources(lib.Sources), func(s models.LibrarySource) string { return s.PathURI })

	var users []string
	handle, err := q.manager.Push(q.def, ScanInput{
		LibraryID:   lib.ID,
		LibraryType: lib.Type,
		Settings:    models.ParseLibrarySettings(lib.Settings),
		Sources:     paths,
		Force:       force,
		FilterPaths: filterPaths,
	}, func(ctx context.Context, tx pgx.Tx, id string) (err error) {
		data := notify.ScanData{LibraryID: lib.ID, LibraryName: lib.Name, Files: len(filterPaths)}
		if retryOf != "" {
			if users, err = notify.RebindScanItems(ctx, tx, retryOf, id, data); err != nil {
				return err
			}
		}
		created, err := notify.CreateScanItems(ctx, tx, id, data)
		users = append(users, created...)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("push scan task: %w", err)
	}
	q.notifier.NotificationsChanged(users)

	return handle.ID(), nil
}
