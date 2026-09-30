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
		return store.WriteFileLayers(context.Background(), tx, []metadata.FileLayer{{ContentID: "c_s", Fields: f}}, time.Now())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func data(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	d, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT data::text FROM content WHERE id = 'c_s' AND data_version = $1", metadata.DataVersion)
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
	exec(t, pool, `INSERT INTO metadata_links (library_id, content_id, provider, state, external_id, origin)
		VALUES ($1, 'c_s', 'a', 'linked', '1', 'manual')`, lib)
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("File")})
	if got := data(t, pool); got != `{"count": 0, "title": "Remote", "alt_titles": ["File"]}` {
		t.Fatalf("data = %s", got)
	}

	layers, err := store.Load(ctx, pool, metadata.Target{ContentID: "c_s"})
	if err != nil {
		t.Fatal(err)
	}
	if layers.OverridesRev != 1 || layers.Overrides.Description.P != metadata.Null || len(layers.Providers) != 1 {
		t.Fatalf("layers = %+v", layers)
	}
	if err := inTx(t, pool, func(tx pgx.Tx) error {
		_, err := store.Lock(ctx, tx, "c_missing")
		return err
	}); !errors.Is(err, metadata.ErrNotFound) {
		t.Fatalf("unknown content: err = %v", err)
	}

	// A scan writes a linkless leaf's NewLeafDoc values with its row, which must be what Recompute derives.
	exec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, parent_id)
		VALUES ('c_1', '1', 'comic/S/1', 'comic', $1, 'c_s')`, lib)
	doc, changed, err := metadata.NewLeafDoc(metadata.Doc{V: 2, Rev: 1, Overrides: metadata.Fields{Title: metadata.Val("Mine")}},
		metadata.Fields{Title: metadata.Val("File"), Volume: metadata.Val("1")})
	if err != nil || !changed {
		t.Fatalf("changed = %v (%v)", changed, err)
	}
	exec(t, pool, "UPDATE content SET data_raw = $1, data = $2, data_version = $3 WHERE id = 'c_1'",
		doc.Raw, doc.Data, metadata.DataVersion)
	var recomputed bool
	if err := inTx(t, pool, func(tx pgx.Tx) (err error) {
		recomputed, err = store.Recompute(ctx, tx, []string{"c_1"})
		return err
	}); err != nil || recomputed {
		t.Fatalf("Recompute changed %s (%v)", doc.Data, err)
	}
	if doc.Data != `{"title":"Mine","alt_titles":["File"],"volume":"1"}` {
		t.Fatalf("leaf data = %s", doc.Data)
	}
}

// A snapshot that no longer decodes drops out of the merge, and its link records why until it
// decodes again.
func TestStoreSkipsASnapshotThatNoLongerDecodes(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at) VALUES
		('a', '1', '1', '"broken"', now(), now()), ('b', '2', '2', '{"description": "From b"}', now(), now())`)
	exec(t, pool, `INSERT INTO metadata_links (library_id, content_id, provider, state, external_id, origin) VALUES
		($1, 'c_s', 'a', 'linked', '1', 'manual'), ($1, 'c_s', 'b', 'linked', '2', 'manual')`, lib)
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

func TestLocalMatchInputChangesMakePendingLinksDue(t *testing.T) {
	pool := setup(t)
	exec(t, pool, `INSERT INTO metadata_links (library_id, content_id, provider, state) VALUES
		($1, 'c_s', 'a', 'review'), ($1, 'c_s', 'b', 'ignored')`, lib)
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
			return store.WriteFileLayers(context.Background(), tx, []metadata.FileLayer{{ContentID: "c_1", Fields: f}}, time.Now())
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

// A row that comes out the same is not written; one an older version derived only gets the
// version, and keeps its meta_updated_at.
func TestRecomputeSkipsUnchangedRows(t *testing.T) {
	pool := setup(t)
	ctx := context.Background()
	writeFile(t, pool, metadata.Fields{Title: metadata.Val("S")})
	exec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, data_raw, data, data_version, meta_updated_at)
		VALUES ('c_t', 'T', 'comic/T', 'comic_series', $1, '{"v": 2, "file": {"title": "T"}}', '{"title": "Old"}', 0, '2000-01-01'),
		       ('c_u', 'U', 'comic/U', 'comic_series', $1, '{"v": 2, "file": {"title": "U"}}', '{"title": "U"}', 0, '2000-01-01')`, lib)
	type row struct {
		Xmin      string    `db:"xmin"`
		Data      string    `db:"data"`
		Version   int       `db:"data_version"`
		UpdatedAt time.Time `db:"meta_updated_at"`
	}
	read := func() []row {
		t.Helper()
		rows, err := db.Select[row](ctx, pool,
			"SELECT xmin::text AS xmin, data::text AS data, data_version, meta_updated_at FROM content ORDER BY id")
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	recompute := func() bool {
		t.Helper()
		var changed bool
		err := inTx(t, pool, func(tx pgx.Tx) (err error) {
			changed, err = store.Recompute(ctx, tx, []string{"c_s", "c_t", "c_u"})
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
