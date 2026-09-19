package tasks

import (
	"context"
	"slices"
	"testing"
	"time"

	"voltis/db/dbtest"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	return dbtest.Pool(t)
}

func waitUntil(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func dbStatus(t *testing.T, pool *pgxpool.Pool, id string) int {
	t.Helper()
	var status int
	if err := pool.QueryRow(context.Background(), "SELECT status FROM tasks WHERE id = $1", id).Scan(&status); err != nil {
		t.Fatalf("read status of %s: %v", id, err)
	}
	return status
}

func newManager(t *testing.T, publish func(Snapshot)) (*Manager, *pgxpool.Pool) {
	t.Helper()
	pool := newTestPool(t)
	m := NewManager(pool, publish)
	t.Cleanup(m.Close)
	return m, pool
}

func pushTask(t *testing.T, m *Manager, def *TaskDef, input any) *TaskHandle {
	t.Helper()
	handle, err := m.Push(def, input)
	if err != nil {
		t.Fatalf("push %s: %v", def.Name, err)
	}
	return handle
}

func waitTask(t *testing.T, handle *TaskHandle) any {
	t.Helper()
	result, err := handle.Wait()
	if err != nil {
		t.Fatalf("wait %s: %v", handle.ID(), err)
	}
	return result
}

func liveHas(m *Manager, id string) bool {
	return slices.ContainsFunc(m.Live(), func(s Snapshot) bool { return s.ID == id })
}
