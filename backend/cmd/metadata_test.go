package cmd

import (
	"context"
	"testing"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/linking"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/mangabaka"

	"github.com/jackc/pgx/v5/pgxpool"
)

func exec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func TestMatchLibrarySkipsSeriesWhileScanned(t *testing.T) {
	pool := dbtest.Pool(t)
	reg := providers.NewRegistry(mangabaka.New())
	links := linking.New(pool, metadata.NewStore(reg), reg, nil, func(string) {})
	ctx := context.Background()
	scanning := scanRunning(ctx, pool)
	exec(t, pool, "INSERT INTO libraries (id, name, type) VALUES ('l1', 'lib', 'comics')")
	exec(t, pool, "INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ('s', 's', 'comic/s', 'comic_series', 'l1')")
	if scanning("l1") {
		t.Fatal("scanning without a scan")
	}
	exec(t, pool, `INSERT INTO tasks (id, created_at, updated_at, name, status, input, output)
		VALUES ('t1', now(), now(), 'scan_library', 1, '{"library_id": "l1"}', '{}')`)
	if !scanning("l1") || scanning("l2") {
		t.Fatal("the scan of l1 is not seen live")
	}
	// Skipped before any search, so no provider is asked.
	if err := MatchLibrary(ctx, pool, links, "l1", false); err != nil {
		t.Fatal(err)
	}
	if n, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM metadata_links"); err != nil || n != 0 {
		t.Fatalf("links = %d (%v)", n, err)
	}
}
