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

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := db.LockMetadata(ctx, tx, lib); err != nil {
		t.Fatal(err)
	}

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

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := db.LockMetadata(ctx, tx, lib); err != nil {
		t.Fatal(err)
	}

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
