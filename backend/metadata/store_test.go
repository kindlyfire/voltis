package metadata_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/metadata"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// jsonDecoder reads snapshots that are stored as layers.
type jsonDecoder struct{}

func (jsonDecoder) Layer(_ metadata.EntryKey, raw json.RawMessage) (metadata.Fields, error) {
	var f metadata.Fields
	err := json.Unmarshal(raw, &f)
	return f, err
}

func (jsonDecoder) Order(provider string) int { return slices.Index([]string{"a", "b"}, provider) }

var store = metadata.NewStore(jsonDecoder{})

const lib = "l_test"

func setup(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.Pool(t)
	exec(t, pool, "INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", lib)
	exec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ('c_s', 'S', 'comic/S', 'comic_series', $1)`, lib)
	return pool
}

func exec(t *testing.T, q db.Querier, sql string, args ...any) {
	t.Helper()
	if _, err := q.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func inTx(t *testing.T, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) error {
	t.Helper()
	return db.WithTx(context.Background(), pool, fn)
}

func setOverrides(t *testing.T, pool *pgxpool.Pool, rev int64, o metadata.Fields) error {
	t.Helper()
	return inTx(t, pool, func(tx pgx.Tx) error {
		target, err := store.Lock(context.Background(), tx, "c_s")
		if err != nil {
			return err
		}
		return store.SetOverrides(context.Background(), tx, target, rev, o)
	})
}

func writeFile(t *testing.T, pool *pgxpool.Pool, f metadata.Fields) {
	t.Helper()
	err := inTx(t, pool, func(tx pgx.Tx) error {
		return store.WriteFileLayers(context.Background(), tx, lib, []metadata.FileLayer{{URI: "comic/S", Fields: f}}, time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func data(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	d, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT data::text FROM content_metadata WHERE library_id = $1 AND uri = 'comic/S' AND data_version = $2",
		lib, metadata.DataVersion)
	if err != nil {
		t.Fatalf("read data: %v", err)
	}
	return d
}

func TestStoreDerivesDataFromLayers(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("File"), Description: metadata.Val("Blurb"), Count: metadata.Val(3)})

	o := metadata.Fields{Description: metadata.Opt[string]{P: metadata.Null}, Count: metadata.Val(0)}
	if err := setOverrides(t, pool, 0, o); err != nil {
		t.Fatal(err)
	}
	if err := setOverrides(t, pool, 0, o); !errors.Is(err, metadata.ErrConflict) {
		t.Fatalf("stale rev: err = %v", err)
	}
	if got := data(t, pool); got != `{"count": 0, "title": "File"}` {
		t.Fatalf("data = %s", got)
	}

	// A provider layer sits between the file and the overrides.
	exec(t, pool, `INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
		VALUES ('a', '1', '1', '{"title": "Remote", "count": 9}', now(), now())`)
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin)
		VALUES ($1, 'comic/S', 'a', 'linked', '1', 'manual')`, lib)
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("File")})
	if got := data(t, pool); got != `{"count": 0, "title": "Remote", "alt_titles": ["File"]}` {
		t.Fatalf("data = %s", got)
	}

	layers, err := store.Load(ctx, pool, metadata.Target{LibraryID: lib, URI: "comic/S"})
	if err != nil {
		t.Fatal(err)
	}
	if layers.OverridesRev != 1 || layers.Overrides.Description.P != metadata.Null || len(layers.Providers) != 1 {
		t.Fatalf("layers = %+v", layers)
	}
}

// A snapshot that no longer decodes drops out of the merge, and its link records why until it
// decodes again.
func TestStoreSkipsASnapshotThatNoLongerDecodes(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at) VALUES
		('a', '1', '1', '"broken"', now(), now()), ('b', '2', '2', '{"description": "From b"}', now(), now())`)
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin) VALUES
		($1, 'comic/S', 'a', 'linked', '1', 'manual'), ($1, 'comic/S', 'b', 'linked', '2', 'manual')`, lib)
	lastErrors := func() []*string {
		t.Helper()
		errs, err := db.SelectScalars[*string](context.Background(), pool,
			"SELECT last_error FROM metadata_links ORDER BY provider")
		if err != nil {
			t.Fatal(err)
		}
		return errs
	}

	writeFile(t, pool, metadata.Fields{Title: metadata.Val("File")})
	if got := data(t, pool); got != `{"title": "File", "description": "From b"}` {
		t.Fatalf("data = %s", got)
	}
	if errs := lastErrors(); errs[0] == nil || errs[1] != nil {
		t.Fatalf("last errors = %v", errs)
	}

	exec(t, pool, `UPDATE provider_entries SET raw = '{"title": "Remote"}' WHERE provider = 'a'`)
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("File")})
	if errs := lastErrors(); errs[0] != nil {
		t.Fatalf("last errors = %v", errs)
	}
}

// A kept link on a leaf moves up to its series unless the series has one; a disposable one goes.
func TestCollectOrphansMovesLeafLinksToTheirSeries(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, parent_id) VALUES
		('c_1', 'ch1', 'comic/S/ch1', 'comic', $1, 'c_s'), ('c_2', 'ch2', 'comic/S/ch2', 'comic', $1, 'c_s')`, lib)
	exec(t, pool, `INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at) VALUES
		('a', '1', '1', '{"title": "Remote"}', now(), now())`)
	// A linked leaf keeps its entry off the series; a pending one without rejections goes; the series'
	// own row for d stays as it is.
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin, rejected) VALUES
		($1, 'comic/S/ch1', 'a', 'linked', '1', 'manual', '{}'), ($1, 'comic/S/ch2', 'b', 'review', NULL, NULL, '{}'),
		($1, 'comic/S/ch2', 'c', 'review', NULL, NULL, '{9}'), ($1, 'comic/S/ch1', 'd', 'ignored', NULL, NULL, '{}'),
		($1, 'comic/S', 'd', 'unmatched', NULL, NULL, '{8}')`, lib)
	err := inTx(t, pool, func(tx pgx.Tx) error { return store.CollectOrphans(context.Background(), tx, lib) })
	if err != nil {
		t.Fatal(err)
	}
	links, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri || ':' || provider || ':' || state || ':' || rejected::text FROM metadata_links ORDER BY uri COLLATE \"C\", provider")
	want := []string{"comic/S:c:review:{9}", "comic/S:d:unmatched:{8}", "comic/S/ch1:a:linked:{}", "comic/S/ch1:d:ignored:{}"}
	if err != nil || !slices.Equal(links, want) {
		t.Fatalf("links = %v (%v), want %v", links, err, want)
	}
	// Matching reads the series now.
	if due, err := db.SelectScalar[bool](context.Background(), pool,
		"SELECT retry_at IS NOT NULL FROM metadata_links WHERE provider = 'c'"); err != nil || !due {
		t.Fatalf("moved review link due = %v (%v)", due, err)
	}
}

func TestLocalMatchInputChangesMakePendingLinksDue(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state) VALUES
		($1, 'comic/S', 'a', 'review'), ($1, 'comic/S', 'b', 'ignored')`, lib)
	due := func() []string {
		t.Helper()
		got, err := db.SelectScalars[string](context.Background(), pool,
			"SELECT provider FROM metadata_links WHERE retry_at IS NOT NULL ORDER BY provider")
		if err != nil {
			t.Fatal(err)
		}
		exec(t, pool, "UPDATE metadata_links SET retry_at = NULL")
		return got
	}

	writeFile(t, pool, metadata.Fields{Title: metadata.Val("S")})
	due()
	exec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, parent_id)
		VALUES ('c_1', '1', 'comic/S/1', 'comic', $1, 'c_s')`, lib)
	writeChild := func(f metadata.Fields) {
		t.Helper()
		err := inTx(t, pool, func(tx pgx.Tx) error {
			return store.WriteFileLayers(context.Background(), tx, lib, []metadata.FileLayer{{URI: "comic/S/1", Fields: f}}, time.Now())
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		name string
		do   func()
		want []string
	}{
		{"same inputs", func() { writeFile(t, pool, metadata.Fields{Title: metadata.Val("S"), Notes: metadata.Val("n")}) }, nil},
		{"new file title", func() { writeFile(t, pool, metadata.Fields{Title: metadata.Val("T")}) }, []string{"a"}},
		{"description override", func() {
			_ = setOverrides(t, pool, 0, metadata.Fields{Description: metadata.Val("d")})
		}, nil},
		{"title override", func() { _ = setOverrides(t, pool, 1, metadata.Fields{Title: metadata.Val("U")}) }, []string{"a"}},
		{"new child", func() { writeChild(metadata.Fields{Series: metadata.Val("S"), Volume: metadata.Val("1")}) }, []string{"a"}},
		{"same child", func() {
			writeChild(metadata.Fields{Series: metadata.Val("S"), Volume: metadata.Val("1"), Notes: metadata.Val("n")})
		}, nil},
		{"child volume", func() { writeChild(metadata.Fields{Series: metadata.Val("S"), Volume: metadata.Val("2")}) }, []string{"a"}},
	} {
		step.do()
		if got := due(); !slices.Equal(got, step.want) {
			t.Errorf("%s: due = %v, want %v", step.name, got, step.want)
		}
	}
}

func TestLockReadsTheURIAScanMoved(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	held, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Rollback(ctx) }()
	if err := db.LockMetadata(ctx, held, lib); err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	go func() { done <- setOverrides(t, pool, 0, metadata.Fields{Title: metadata.Val("edited")}) }()
	dbtest.WaitForBlockedLock(t, pool)
	exec(t, held, "UPDATE content SET uri = 'comic/moved' WHERE id = 'c_s'")
	if err := held.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	uris, err := db.SelectScalars[string](ctx, pool, "SELECT uri FROM content_metadata")
	if err != nil || !slices.Equal(uris, []string{"comic/moved"}) {
		t.Fatalf("metadata uris = %v (%v)", uris, err)
	}

	if err := inTx(t, pool, func(tx pgx.Tx) error {
		_, err := store.Lock(ctx, tx, "c_missing")
		return err
	}); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("unknown content: err = %v", err)
	}
}

func TestCollectOrphans(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO content_metadata (library_id, uri, data_raw) VALUES
		($1, 'comic/S', '{"v": 2}'), ($1, 'gone/plain', '{"v": 2, "file": {}}'),
		($1, 'gone/edited', '{"v": 2, "overrides": {"title": "x"}}')`, lib)
	exec(t, pool, `INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
		VALUES ('a', '1', '1', '{}', now(), now())`)
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state, rejected, external_id, origin) VALUES
		($1, 'comic/S', 'a', 'review', '{}', NULL, NULL),
		($1, 'gone/pending', 'a', 'unmatched', '{}', NULL, NULL),
		($1, 'gone/rejected', 'a', 'review', '{2}', NULL, NULL),
		($1, 'gone/ignored', 'a', 'ignored', '{}', NULL, NULL),
		($1, 'gone/linked', 'a', 'linked', '{}', '1', 'auto')`, lib)
	if err := inTx(t, pool, func(tx pgx.Tx) error { return store.CollectOrphans(context.Background(), tx, lib) }); err != nil {
		t.Fatal(err)
	}
	for table, want := range map[string][]string{
		"content_metadata": {"comic/S", "gone/edited"},
		"metadata_links":   {"comic/S", "gone/ignored", "gone/linked", "gone/rejected"},
	} {
		got, err := db.SelectScalars[string](context.Background(), pool, "SELECT uri FROM "+table+" ORDER BY uri")
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s = %v (%v), want %v", table, got, err, want)
		}
	}
}

// A row that comes out the same is not written; one an older version derived only gets the version.
func TestRecomputeSkipsUnchangedRows(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("S")})
	exec(t, pool, `INSERT INTO content_metadata (uri, library_id, data_raw, data, data_version, updated_at)
		VALUES ('comic/T', $1, '{"v": 2, "file": {"title": "T"}}', '{"title": "Old"}', 0, '2000-01-01'),
		       ('comic/U', $1, '{"v": 2, "file": {"title": "U"}}', '{"title": "U"}', 0, '2000-01-01')`, lib)
	type row struct {
		URI       string    `db:"uri"`
		Data      string    `db:"data"`
		Version   int       `db:"data_version"`
		UpdatedAt time.Time `db:"updated_at"`
	}
	read := func() []row {
		t.Helper()
		rows, err := db.Select[row](ctx, pool,
			"SELECT uri, data::text AS data, data_version, updated_at FROM content_metadata ORDER BY uri")
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	recompute := func() bool {
		t.Helper()
		var changed bool
		err := inTx(t, pool, func(tx pgx.Tx) (err error) {
			changed, err = store.Recompute(ctx, tx, lib, []string{"comic/S", "comic/T", "comic/U"})
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		return changed
	}
	before := read()
	if !recompute() {
		t.Fatal("reported no change")
	}
	after := read()
	if after[0] != before[0] || after[1].Data != `{"title": "T"}` || after[1].UpdatedAt.Equal(before[1].UpdatedAt) {
		t.Fatalf("rows = %+v, want S untouched and T written", after)
	}
	if u := after[2]; u.Data != before[2].Data || !u.UpdatedAt.Equal(before[2].UpdatedAt) || u.Version != metadata.DataVersion {
		t.Fatalf("U = %+v, want only its version bumped", u)
	}
	if recompute() || !slices.Equal(read(), after) {
		t.Fatal("recomputing again wrote rows")
	}
}
