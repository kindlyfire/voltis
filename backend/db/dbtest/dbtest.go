package dbtest

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"voltis/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

// GenericPlans is another pool on pool's database whose connections always run generic plans.
func GenericPlans(t *testing.T, pool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	cfg := pool.Config()
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_generic_plan"
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("generic pool: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := Unmigrated(t)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

// Unmigrated creates an empty test database.
func Unmigrated(t *testing.T) *pgxpool.Pool {
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
	if _, err := admin.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
		t.Logf("cleanup drop database: %v", err)
	}
}
