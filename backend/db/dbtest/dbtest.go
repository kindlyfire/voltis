package dbtest

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
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

// AssertFacetsConsistent fails t unless content_facets holds exactly facet_rows of every valid
// root. EXCEPT ALL both ways, so a duplicated row also fails.
func AssertFacetsConsistent(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	diff, err := db.SelectScalars[string](context.Background(), pool, `
		WITH want AS (
			SELECT c.id, c.library_id, r.kind, r.key, r.roles, r.labels
			FROM content c, public.facet_rows(c.data) r
			WHERE c.parent_id IS NULL AND c.valid
		), got AS (
			SELECT content_id, library_id, kind, key, roles, labels FROM content_facets
		)
		SELECT format('missing %s', x) FROM (TABLE want EXCEPT ALL TABLE got) x
		UNION ALL
		SELECT format('extra %s', x) FROM (TABLE got EXCEPT ALL TABLE want) x
		LIMIT 20`)
	if err != nil {
		t.Fatalf("facets consistency: %v", err)
	}
	if len(diff) > 0 {
		t.Errorf("content_facets out of sync:\n%s", strings.Join(diff, "\n"))
	}
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
