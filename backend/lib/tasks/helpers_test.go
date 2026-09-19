package tasks

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"slices"
	"testing"
	"time"

	"voltis/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := cmp.Or(os.Getenv("APP_TESTS_DATABASE_URL"),
		"postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable")

	ctx := context.Background()

	buf := make([]byte, 8)
	rand.Read(buf)
	dbName := "voltis_tests_" + hex.EncodeToString(buf)

	admin, err := db.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+dbName); err != nil {
		admin.Close()
		t.Fatalf("create database: %v", err)
	}
	admin.Close()

	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	parsed.Path = "/" + dbName

	pool, err := db.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		admin, err := db.Connect(ctx, adminURL)
		if err != nil {
			t.Logf("cleanup connect admin: %v", err)
			return
		}
		defer admin.Close()
		if _, err := admin.Exec(ctx, "DROP DATABASE "+dbName); err != nil {
			t.Logf("cleanup drop database: %v", err)
		}
	})

	return pool
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
