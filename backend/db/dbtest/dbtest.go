package dbtest

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"
	"time"

	"voltis/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	adminURL := cmp.Or(os.Getenv("APP_TESTS_DATABASE_URL"),
		"postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable")

	ctx := context.Background()

	buf := make([]byte, 8)
	rand.Read(buf)
	name := "voltis_tests_" + hex.EncodeToString(buf)

	admin, err := db.Connect(ctx, adminURL)
	if err != nil {
		t.Fatalf("connect admin: %v", err)
	}
	defer admin.Close()

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { drop(t, adminURL, name) })

	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatalf("parse admin url: %v", err)
	}
	parsed.Path = "/" + name

	pool, err := db.Connect(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect test db: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func drop(t *testing.T, adminURL, name string) {
	t.Helper()
	ctx := context.Background()
	admin, err := db.Connect(ctx, adminURL)
	if err != nil {
		t.Logf("cleanup connect admin: %v", err)
		return
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, "DROP DATABASE "+name); err != nil {
		t.Logf("cleanup drop database: %v", err)
	}
}

func WaitForBlockedLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		n, err := db.SelectScalar[int](context.Background(), pool, `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`)
		if err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for a blocked advisory lock")
}
