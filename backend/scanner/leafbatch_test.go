package scanner

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var testLeafDoc = metadata.LeafDoc{Raw: `{"v": 2}`, Data: `{"title": "t"}`}

func batchLeaf(lib, id, part, uri string) models.Content {
	return models.Content{ID: id, LibraryID: lib, CreatedAt: baseTime, UpdatedAt: baseTime, Type: "comic",
		URIPart: part, URI: uri, Valid: true}
}

type batchWrite struct {
	c    models.Content
	meta metadata.LeafDoc
}

// writeLeaves sends leaves through one leafBatch, flushing after each when single is set.
func writeLeaves(ctx context.Context, tx pgx.Tx, single bool, ws ...batchWrite) error {
	var b leafBatch
	for _, w := range ws {
		if err := b.add(ctx, tx, w.c, &w.meta); err != nil {
			return err
		}
		if single {
			if err := b.flush(ctx, tx); err != nil {
				return err
			}
		}
	}
	// Staged out of step order, so only ORDER BY seq restores it.
	slices.Reverse(b.rows)
	return b.flush(ctx, tx)
}

func batchTx(t *testing.T, pool *pgxpool.Pool, single bool, cs ...models.Content) error {
	t.Helper()
	ws := make([]batchWrite, len(cs))
	for i, c := range cs {
		ws[i] = batchWrite{c, testLeafDoc}
	}
	return db.WithTx(context.Background(), pool, func(tx pgx.Tx) error {
		return writeLeaves(context.Background(), tx, single, ws...)
	})
}

func assertUniqueViolation(t *testing.T, err error) {
	t.Helper()
	if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != "23505" {
		t.Fatalf("err = %v, want unique violation", err)
	}
}

func TestLeafBatchReleasesKeysBeforeClaimingThem(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	// idx_content_unique alone: uri_part moves, uri stays.
	a, b := batchLeaf(lib, "c_a", "a", "/ua"), batchLeaf(lib, "c_b", "b", "/ub")
	// idx_content_uri_unique alone: uri moves, uri_part stays.
	c, d := batchLeaf(lib, "c_c", "c", "/x"), batchLeaf(lib, "c_d", "d", "/y")
	seedContent(t, pool, a, b, c, d)

	a.URIPart, b.URIPart = "z", "a"
	c.URI, d.URI = "/z", "/x"
	must(t, batchTx(t, pool, false, a, b, c, d))
	for id, want := range map[string][2]string{"c_a": {"z", "/ua"}, "c_b": {"a", "/ub"}, "c_c": {"c", "/z"}, "c_d": {"d", "/x"}} {
		if got := readContent(t, pool, id); [2]string{got.URIPart, got.URI} != want {
			t.Fatalf("%s = %s %s, want %v", id, got.URIPart, got.URI, want)
		}
	}

	// A swap holds each key while the other claims it, row by row or batched.
	a.URIPart, b.URIPart = "a", "z"
	for _, single := range []bool{true, false} {
		assertUniqueViolation(t, batchTx(t, pool, single, a, b))
	}
}

func TestLeafBatchDuplicateFlushesAndLastWriteWins(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	a1, b, a2, c := batchLeaf(lib, "c_a", "a1", "/a1"), batchLeaf(lib, "c_b", "b", "/b"),
		batchLeaf(lib, "c_a", "a2", "/a2"), batchLeaf(lib, "c_c", "c", "/c")
	// c claims the keys a2 frees, in the same flush.
	c.URIPart, c.URI = "a1", "/a1"

	ctx := context.Background()
	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		var batch leafBatch
		for i, l := range []models.Content{a1, b, a2} {
			if err := batch.add(ctx, tx, l, &testLeafDoc); err != nil {
				return err
			}
			if i == 2 && len(batch.rows) != 1 {
				t.Fatalf("buffered %d rows after the duplicate, want 1", len(batch.rows))
			}
		}
		// Two multi-row flushes in one transaction share the staging table.
		if err := batch.add(ctx, tx, c, &testLeafDoc); err != nil {
			return err
		}
		return batch.flush(ctx, tx)
	})
	must(t, err)
	assertCatalog(t, pool, lib, []string{"/a1", "/a2", "/b"})
	if got := readContent(t, pool, "c_a"); got.URI != "/a2" {
		t.Fatalf("c_a uri = %s, want /a2", got.URI)
	}
}

// rowJSON is every persisted column but the keys, which tests vary between rows.
func rowJSON(t *testing.T, pool *pgxpool.Pool, id string) map[string]any {
	t.Helper()
	var row map[string]any
	err := pool.QueryRow(context.Background(),
		"SELECT to_jsonb(c) - 'id' - 'uri' - 'uri_part' FROM content c WHERE id = $1", id).Scan(&row)
	if err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	return row
}

func TestLeafBatchMatchesSingleRowUpsert(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	seedContent(t, pool, models.Content{ID: "c_s", LibraryID: lib, Type: "comic_series", URIPart: "s", URI: "/s", Valid: true})

	two, three, pages := float32(2.5), float32(3), 12
	full := func(id string, at time.Time) models.Content {
		c := batchLeaf(lib, id, id, "/s/"+id)
		c.UpdatedAt, c.ParentID = at, new("c_s")
		c.FileURI, c.FileMtime, c.CoverURI = new("/f"), &baseTime, new("/cover")
		c.OrderParts = []*float32{&two, nil, &three}
		c.FileData, c.PageCount = models.JSONB(`{"pages": [1]}`), &pages
		return c
	}
	sparse := func(id string, at time.Time) models.Content {
		c := batchLeaf(lib, id, id, "/"+id)
		c.UpdatedAt, c.Valid = at, false
		c.Order, c.FileSize, c.WordCount = new(4), new(100), new(7)
		return c
	}
	later := baseTime.Add(time.Hour)
	for _, round := range []struct {
		at    time.Time
		build func(string, time.Time) models.Content
	}{{baseTime, full}, {later, sparse}, {later, full}} {
		must(t, batchTx(t, pool, true, round.build("one", round.at)))
		must(t, batchTx(t, pool, false, round.build("m1", round.at), round.build("m2", round.at)))
		want := rowJSON(t, pool, "one")
		for _, id := range []string{"m1", "m2"} {
			if got := rowJSON(t, pool, id); !reflect.DeepEqual(got, want) {
				t.Fatalf("%s:\n got %v\nwant %v", id, got, want)
			}
		}
	}
	if got := rowJSON(t, pool, "m1")["order_parts"]; !reflect.DeepEqual(got, []any{2.5, nil, 3.0}) {
		t.Fatalf("order_parts = %v", got)
	}
}

func TestLeafBatchMetaUpdatedAt(t *testing.T) {
	for _, single := range []bool{true, false} {
		pool := newTestPool(t)
		lib := newTestLibrary(t, pool, "comics")
		ids := []string{"same", "raw", "derived", "stale"}
		ws := make([]batchWrite, len(ids))
		for i, id := range ids {
			ws[i] = batchWrite{batchLeaf(lib, id, id, "/"+id), testLeafDoc}
		}
		ctx := context.Background()
		must(t, db.WithTx(ctx, pool, func(tx pgx.Tx) error { return writeLeaves(ctx, tx, single, ws...) }))
		exec(t, pool, "UPDATE content SET data_version = 0 WHERE id = 'stale'")

		later := baseTime.Add(time.Hour)
		for i := range ws {
			ws[i].c.UpdatedAt = later
		}
		ws[1].meta.Raw = `{"v": 2, "file": {"title": "x"}}`
		ws[2].meta.Data = `{"title": "x"}`
		must(t, db.WithTx(ctx, pool, func(tx pgx.Tx) error { return writeLeaves(ctx, tx, single, ws...) }))

		want := map[string]time.Time{"same": baseTime, "raw": later, "derived": later, "stale": baseTime}
		for _, id := range ids {
			var at time.Time
			var version int
			err := pool.QueryRow(ctx, "SELECT meta_updated_at, data_version FROM content WHERE id = $1", id).
				Scan(&at, &version)
			must(t, err)
			if !at.Equal(want[id]) || version != metadata.DataVersion {
				t.Fatalf("single=%v %s: meta_updated_at %v version %d, want %v version %d",
					single, id, at, version, want[id], metadata.DataVersion)
			}
		}
	}
}

func TestLeafBatchRetryOnReusedConnectionStartsClean(t *testing.T) {
	cfg := newTestPool(t).Config()
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	must(t, err)
	t.Cleanup(pool.Close)
	lib := newTestLibrary(t, pool, "comics")
	seedContent(t, pool, batchLeaf(lib, "c_e", "e", "/e"))

	clash := batchLeaf(lib, "c_b", "b", "/e")
	assertUniqueViolation(t, batchTx(t, pool, false, batchLeaf(lib, "c_a", "a", "/a"), clash))

	ctx := context.Background()
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		cs := []batchWrite{{batchLeaf(lib, "c_c", "c", "/c"), testLeafDoc}, {batchLeaf(lib, "c_d", "d", "/d"), testLeafDoc}}
		if err := writeLeaves(ctx, tx, false, cs...); err != nil {
			return err
		}
		staged, err := db.SelectScalars[string](ctx, tx, "SELECT id FROM pg_temp.scan_leaves ORDER BY seq")
		if err == nil && !slices.Equal(staged, []string{"c_c", "c_d"}) {
			t.Fatalf("staged = %v", staged)
		}
		return err
	})
	must(t, err)
	assertCatalog(t, pool, lib, []string{"/c", "/d", "/e"})
	var table *string
	must(t, pool.QueryRow(ctx, "SELECT to_regclass('pg_temp.scan_leaves')::text").Scan(&table))
	if table != nil {
		t.Fatalf("staging table outlived its transaction: %s", *table)
	}
}
