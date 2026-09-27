package routes

import (
	"context"
	"testing"

	"voltis/db"
	"voltis/db/dbtest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func contentLibrary(t *testing.T, pool *pgxpool.Pool, contentID string) string {
	t.Helper()
	lib, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT library_id FROM content WHERE id = $1", contentID)
	if err != nil {
		t.Fatalf("read library: %v", err)
	}
	return lib
}

func TestRefWritesLandOnTheURIAScanMovedUnderTheLock(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	contentID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, contentID)
	ctx := context.Background()
	list := s(c.Post("/api/custom-lists", map[string]any{"name": "l", "visibility": "private"}).Assert(t, 200).JSON()["id"])

	requests := map[string]func() *response{
		"user_to_content": func() *response {
			return c.Post("/api/content/"+contentID+"/user-data", map[string]any{"starred": true})
		},
		"custom_list_to_content": func() *response {
			return c.Post("/api/custom-lists/"+list+"/entries", map[string]any{"content_id": contentID})
		},
	}
	for table, request := range requests {
		tx := holdLock(t, pool, lib)
		done := make(chan *response, 1)
		go func() { done <- request() }()
		dbtest.WaitForBlockedLock(t, pool)

		uri := "comic/moved-" + table
		if _, err := tx.Exec(ctx, "UPDATE content SET uri = $2 WHERE id = $1", contentID, uri); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		(<-done).Assert(t, 200)
		n, err := db.SelectScalar[int](ctx, pool, "SELECT COUNT(*) FROM "+table+" WHERE uri = $1", uri)
		if err != nil || n != 1 {
			t.Fatalf("%s rows at %s = %d (%v), want the write at the moved uri", table, uri, n, err)
		}
	}
}

// holdLock takes a library's metadata lock in an open transaction, released by its commit.
func holdLock(t *testing.T, pool *pgxpool.Pool, lib string) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	if err := db.LockMetadata(ctx, tx, lib); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestBulkListEntriesLockLibrariesInOrder(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	a, b := newTestContent(t, pool), newTestContent(t, pool)
	if contentLibrary(t, pool, a) > contentLibrary(t, pool, b) {
		a, b = b, a
	}
	list := s(c.Post("/api/custom-lists", map[string]any{"name": "l", "visibility": "private"}).Assert(t, 200).JSON()["id"])

	// Both batches queue behind the first library's lock. Locking per entry, the reversed batch
	// would take the second library first, and the two would deadlock once it is released.
	tx := holdLock(t, pool, contentLibrary(t, pool, a))
	done := make(chan *response, 2)
	for _, ids := range [][]string{{a, b}, {b, a}} {
		go func() {
			done <- c.Post("/api/custom-lists/entries", map[string]any{"entries": []map[string]any{
				{"list_id": list, "content_id": ids[0]}, {"list_id": list, "content_id": ids[1]},
			}})
		}()
	}
	dbtest.WaitForBlockedLocks(t, pool, 2)
	if err := tx.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	(<-done).Assert(t, 200)
	(<-done).Assert(t, 200)
}

func TestFixBrokenRefsWaitsForAScanRename(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	contentID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, contentID)
	ctx := context.Background()
	mustExec(t, pool, `INSERT INTO user_to_content (id, user_id, library_id, uri, starred)
		SELECT 'stale', id, $1, 'comic/old', true FROM users`, lib)

	tx := holdLock(t, pool, lib)
	done := make(chan *response, 1)
	go func() {
		done <- c.Post("/api/content/broken-refs/"+lib, map[string]any{"update": map[string]string{"stale": "comic/new"}})
	}()
	dbtest.WaitForBlockedLock(t, pool)
	if _, err := tx.Exec(ctx, "UPDATE content SET uri = 'comic/new' WHERE id = $1", contentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	(<-done).Assert(t, 200)
	if uri, err := db.SelectScalar[string](ctx, pool, "SELECT uri FROM user_to_content WHERE id = 'stale'"); err != nil || uri != "comic/new" {
		t.Fatalf("ref uri = %q (%v), want the repair validated against the committed rename", uri, err)
	}
}
