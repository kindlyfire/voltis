package routes

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
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

func overrideTitle(t *testing.T, pool *pgxpool.Pool, libraryID, uri string) string {
	t.Helper()
	var raw json.RawMessage
	err := pool.QueryRow(context.Background(),
		"SELECT data_raw FROM content_metadata WHERE library_id = $1 AND uri = $2", libraryID, uri).Scan(&raw)
	if err != nil {
		return ""
	}
	mr := metaraw.From(raw)
	if mr.Overrides == nil {
		return ""
	}
	return mr.Overrides.Raw.Title
}

func TestMetadataLockResolvesRenamedURI(t *testing.T) {
	pool := newTestPool(t)
	contentID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, contentID)
	ctx := context.Background()

	oldURI, err := db.SelectScalar[string](ctx, pool, "SELECT uri FROM content WHERE id = $1", contentID)
	if err != nil {
		t.Fatal(err)
	}

	tx := holdLock(t, pool, lib)

	done := make(chan error, 1)
	go func() {
		done <- editMetadataRaw(ctx, pool, contentID, lib, func(mr *metaraw.MetadataRaw) bool {
			mr.Overrides = &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "edited"}}
			return true
		})
	}()

	dbtest.WaitForBlockedLock(t, pool)
	select {
	case err := <-done:
		t.Fatalf("edit ran while the lock was held: %v", err)
	default:
	}

	if _, err := tx.Exec(ctx, "UPDATE content SET uri = 'comic/renamed' WHERE id = $1", contentID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := overrideTitle(t, pool, lib, "comic/renamed"); got != "edited" {
		t.Fatalf("override at the new uri = %q", got)
	}
	if got := overrideTitle(t, pool, lib, oldURI); got != "" {
		t.Fatalf("override at the old uri = %q", got)
	}
}

func TestMetadataLockRejectsUnknownContent(t *testing.T) {
	pool := newTestPool(t)
	contentID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, contentID)

	err := editMetadataRaw(context.Background(), pool, "c_missing", lib, func(*metaraw.MetadataRaw) bool {
		t.Fatal("fn must not run for unknown content")
		return true
	})
	httpErr, ok := err.(*echo.HTTPError)
	if !ok || httpErr.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}

	err = editMetadataRaw(context.Background(), pool, contentID, "l_other", func(*metaraw.MetadataRaw) bool {
		t.Fatal("fn must not run for another library")
		return true
	})
	if httpErr, ok := err.(*echo.HTTPError); !ok || httpErr.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want 404", err)
	}
}

func TestMetadataLockSkipsWriteWhenUnchanged(t *testing.T) {
	pool := newTestPool(t)
	contentID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, contentID)
	ctx := context.Background()

	err := editMetadataRaw(ctx, pool, contentID, lib, func(*metaraw.MetadataRaw) bool { return false })
	if err != nil {
		t.Fatalf("edit: %v", err)
	}
	n, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM content_metadata WHERE library_id = $1", lib)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rows = %d, want none", n)
	}

}

func TestMetadataLockEditFollowsASeriesRename(t *testing.T) {
	pool := newTestPool(t)
	seriesID := newTestContent(t, pool)
	lib := contentLibrary(t, pool, seriesID)
	ctx := context.Background()

	mustExec(t, pool, "UPDATE content SET type = 'comic_series', uri = 'comic/S', uri_part = 'S' WHERE id = $1", seriesID)
	leafID := "c_leaf"
	mustExec(t, pool, `
		INSERT INTO content (id, uri_part, uri, type, library_id, parent_id)
		VALUES ($1, 'ch1', 'comic/S/ch1', 'comic', $2, $3)
	`, leafID, lib, seriesID)

	tx := holdLock(t, pool, lib)

	done := make(chan error, 1)
	go func() {
		done <- editMetadataRaw(ctx, pool, leafID, lib, func(mr *metaraw.MetadataRaw) bool {
			mr.Overrides = &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "late"}}
			return true
		})
	}()

	dbtest.WaitForBlockedLock(t, pool)
	select {
	case err := <-done:
		t.Fatalf("edit ran while the lock was held: %v", err)
	default:
	}

	if _, err := tx.Exec(ctx,
		"UPDATE content SET uri = 'comic/S_2019', uri_part = 'S_2019' WHERE id = $1", seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx,
		"UPDATE content SET uri = 'comic/S_2019' || '/' || uri_part WHERE parent_id = $1", seriesID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	if err := <-done; err != nil {
		t.Fatalf("edit: %v", err)
	}
	if got := overrideTitle(t, pool, lib, "comic/S_2019/ch1"); got != "late" {
		t.Fatalf("override at the renamed uri = %q", got)
	}
	if got := overrideTitle(t, pool, lib, "comic/S/ch1"); got != "" {
		t.Fatalf("the edit landed at the old uri: %q", got)
	}
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
