package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/epub"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func withMeta(r Result, m metadata.Fields) Result {
	r.Item.MetaRaw = m
	return r
}

func withOrder(r Result, parts ...float32) Result {
	r.Item.OrderParts = make([]*float32, len(parts))
	for i := range parts {
		r.Item.OrderParts[i] = &parts[i]
	}
	return r
}

func libraryScannedAt(t *testing.T, pool *pgxpool.Pool, lib string) *time.Time {
	t.Helper()
	var at *time.Time
	err := pool.QueryRow(context.Background(), "SELECT scanned_at FROM libraries WHERE id = $1", lib).Scan(&at)
	if err != nil {
		t.Fatalf("read scanned_at: %v", err)
	}
	return at
}

func TestFingerprintRelativePathsIndexAbsolutelyAndKeepTheirSpelling(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	t.Chdir(t.TempDir())

	leaf := filepath.Join("S", "ch1.cbz")
	seedContent(t, pool,
		models.Content{ID: "p1", LibraryID: lib, Type: "comic_series", URI: "comic/S", URIPart: "S", Valid: true, FileURI: new("S")},
		models.Content{ID: "l1", LibraryID: lib, Type: "comic", URI: "comic/S/ch1", URIPart: "ch1", Valid: true,
			FileURI: new(leaf), ParentID: new("p1")},
	)

	cwd, err := os.Getwd()
	must(t, err)
	res := newResolver()
	fps, err := loadFingerprints(context.Background(), pool, lib)
	must(t, err)
	wantKey := filepath.Join(cwd, "S", "ch1.cbz")
	if len(fps) != 1 || fps[0].Path != leaf {
		t.Fatalf("fingerprints = %v, want the stored spelling %s", fps, leaf)
	}

	refs, err := loadSeries(context.Background(), pool, lib)
	must(t, err)
	if len(refs) != 1 || deref(refs[0].FileURI) != "S" {
		t.Fatalf("series = %+v, want the stored spelling", refs)
	}

	w := newWriter(ScanInput{LibraryID: lib}, nil, nil, res, fps, refs)
	if id, ok := w.dirSeries(filepath.Join(cwd, "S")); !ok || id != "p1" {
		t.Fatalf("dirSeries = %q, %v, want the series indexed by its absolute directory", id, ok)
	}
	if _, ok := w.fps[wantKey]; !ok {
		t.Fatalf("fingerprint index = %v, want a key at %s", w.fps, wantKey)
	}
	if _, hadOld := w.at(leaf); !hadOld {
		t.Fatalf("fingerprints = %v, want %s to resolve through the walk spelling", fps, leaf)
	}
}

func TestFingerprintNarrowRead(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")

	mtime := baseTime
	seedContent(t, pool,
		models.Content{ID: "p1", LibraryID: lib, Type: "comic_series", URI: "comic/S", URIPart: "S", Valid: true, FileURI: new("/lib/S")},
		models.Content{ID: "l1", LibraryID: lib, Type: "comic", URI: "comic/S/ch1", URIPart: "ch1", Valid: true,
			FileURI: new("/lib/S/ch1.cbz"), FileMtime: &mtime, FileSize: new(42), ParentID: new("p1"),
			FileData: json.RawMessage(`{"pages":[["001.jpg",4,2]]}`)},
		models.Content{ID: "l2", LibraryID: lib, Type: "comic", URI: "comic/S/ch2", URIPart: "ch2", Valid: false,
			FileURI: new("/lib/S/ch2.cbz"), ParentID: new("p1")},
	)

	q := &recordingQuerier{Querier: pool}
	res := newResolver()
	loaded, err := loadFingerprints(context.Background(), q, lib)
	if err != nil {
		t.Fatalf("load fingerprints: %v", err)
	}
	wantCols := []string{"file_mtime", "file_size", "file_uri", "id", "parent_id", "uri_part", "valid"}
	if got := selectedColumns(t, q.sql[0]); !slices.Equal(got, wantCols) {
		t.Fatalf("fingerprint columns = %v, want %v", got, wantCols)
	}
	if len(loaded) != 2 {
		t.Fatalf("fingerprints = %v, want the two leaves only", loaded)
	}
	fps := map[string]Fingerprint{}
	for _, f := range loaded {
		fps[mustResolveFile(t, res, f.Path)] = f
	}
	got := fps[mustResolveFile(t, res, "/lib/S/ch1.cbz")]
	if got.ID != "l1" || got.URIPart != "ch1" || deref(got.ParentID) != "p1" || !got.Valid {
		t.Fatalf("fingerprint = %+v", got)
	}
	if got.Size == nil || *got.Size != 42 || got.Mtime == nil || !got.Mtime.Equal(baseTime) {
		t.Fatalf("fingerprint file data = %+v", got)
	}
	ch2 := fps[mustResolveFile(t, res, "/lib/S/ch2.cbz")]
	if ch2.Valid {
		t.Fatal("invalid row must report Valid false")
	}
	if ch2.Mtime != nil || ch2.Size != nil {
		t.Fatal("null columns must stay nil")
	}

	refs, err := loadSeries(context.Background(), q, lib)
	if err != nil {
		t.Fatalf("load series: %v", err)
	}
	wantCols = []string{"file_uri", "id", "type", "uri", "uri_part"}
	if got := selectedColumns(t, q.sql[1]); !slices.Equal(got, wantCols) {
		t.Fatalf("series columns = %v, want %v", got, wantCols)
	}
	if len(refs) != 1 || refs[0].ID != "p1" || refs[0].URI != "comic/S" || deref(refs[0].FileURI) != "/lib/S" {
		t.Fatalf("series = %+v", refs)
	}
}

func TestFlushHydratesOnlyTheRowsTheFlushTouches(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.place(comicResult("/lib/T/ch2.cbz", "ch2", "T", "/lib/T"))
	r.place(comicResult("/lib/solo.cbz", "solo", "", ""))
	r.commit(false)

	touched := r.w.byURI["comic/S"]
	untouched := r.w.byURI["comic/T"]
	untouchedLeaf := r.w.keys[Key{untouched, "ch1"}]
	solo := r.w.keys[Key{"", "solo"}]

	r.reload()
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))
	rec, counts, _, err := r.recordCommit(false)
	if err != nil || counts.Added != 1 {
		t.Fatalf("commit = %+v, %v", counts, err)
	}

	leaf := r.w.keys[Key{touched, "ch2"}]
	if got, want := rec.idsFor(t, "WHERE library_id = $1 AND id = ANY"), []string{leaf, touched}; !slices.Equal(got, slices.Sorted(slices.Values(want))) {
		t.Fatalf("hydrated %v, want only the flushed series and its write %v", got, want)
	}
	if got := rec.idsFor(t, "c.parent_id = ANY"); !slices.Equal(got, []string{touched}) {
		t.Fatalf("hydrated children of %v, want only %s", got, touched)
	}
	for _, q := range rec.queries {
		for _, arg := range q.args {
			ids, ok := arg.([]string)
			if !ok {
				continue
			}
			if slices.Contains(ids, untouched) || slices.Contains(ids, untouchedLeaf) || slices.Contains(ids, solo) {
				t.Fatalf("query %q reached outside the flush: %v", q.sql, ids)
			}
		}
	}
}

func TestFlushKeepsManifestThroughInvalidationAndSeriesPatch(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicItem("/lib/S", "1", metadata.Fields{}))
	if counts := r.commit(false); counts.Added != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	seriesID := r.w.byURI["comic/S"]
	leafID := r.w.keys[Key{seriesID, "ch1"}]
	seriesManifest := `{"pages": [["cover.jpg", 2, 3]]}`
	exec(t, r.pool, "UPDATE content SET file_data = $2, file_uri = $3 WHERE id = $1", seriesID, seriesManifest, "/lib/Old")

	leafBefore, seriesBefore := readContent(t, r.pool, leafID), readContent(t, r.pool, seriesID)
	var stored map[string]any
	if err := json.Unmarshal(leafBefore.FileData, &stored); err != nil || stored["pages"] == nil {
		t.Fatalf("file_data = %s", leafBefore.FileData)
	}

	r.reload()
	r.place(Result{File: fsFile("/lib/S/ch1.cbz", baseTime, 10)})
	r.place(comicResult("/lib/S2/ch2.cbz", "ch2", "S", "/lib/S2"))
	r.commit(false)

	leafAfter, seriesAfter := readContent(t, r.pool, leafID), readContent(t, r.pool, seriesID)
	if leafAfter.Valid {
		t.Fatal("row must be invalidated")
	}
	if string(leafAfter.FileData) != string(leafBefore.FileData) {
		t.Fatalf("leaf file_data = %s, want %s", leafAfter.FileData, leafBefore.FileData)
	}
	if deref(leafBefore.CoverURI) != "/lib/S/ch1.cbz/001.jpg" || deref(leafAfter.CoverURI) != deref(leafBefore.CoverURI) {
		t.Fatalf("cover = %v, want %v", leafAfter.CoverURI, leafBefore.CoverURI)
	}
	if !leafAfter.UpdatedAt.After(leafBefore.UpdatedAt) {
		t.Fatalf("updated_at = %v, want later than %v", leafAfter.UpdatedAt, leafBefore.UpdatedAt)
	}
	if deref(seriesAfter.FileURI) != "/lib/S" {
		t.Fatalf("series file_uri = %v, want the patch to land", deref(seriesAfter.FileURI))
	}
	if string(seriesAfter.FileData) != string(seriesBefore.FileData) {
		t.Fatalf("series file_data = %s, want %s", seriesAfter.FileData, seriesBefore.FileData)
	}
	if seriesAfter.URI != "comic/S" || seriesAfter.URIPart != "S" {
		t.Fatalf("series = %+v, want no rename", seriesAfter)
	}
}

func TestFlushInvalidationKeepsTheRowAndItsKey(t *testing.T) {
	r := newTestScan(t, "comics")

	chapter := comicItem("/lib/S", "1", metadata.Fields{})
	r.place(chapter)
	r.commit(false)

	seriesID := r.w.byURI["comic/S"]
	leafID := r.w.keys[Key{seriesID, "ch1"}]
	before := readContent(t, r.pool, leafID)

	r.reload()
	r.place(Result{File: fsFile("/lib/S/ch1.cbz", baseTime, 10)})
	r.commit(false)

	after := readContent(t, r.pool, leafID)
	want := before
	want.Valid = false
	want.UpdatedAt = after.UpdatedAt
	if !reflect.DeepEqual(after, want) || !after.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("row = %+v, want %+v with only its validity and timestamp changed", after, want)
	}
	if got := readMeta(t, r.pool, r.lib, "comic/S/ch1").File.Title.V; got != "Ch. 1" {
		t.Fatalf("leaf metadata title = %q, want the invalidated leaf's metadata left alone", got)
	}

	r.reload()
	r.place(comicResult("/lib/S/ch1 (v2).cbz", "ch1", "S", "/lib/S"))
	if r.w.prog.Failed != 1 {
		t.Fatalf("failed = %d, want the invalid row to still reserve its uri_part", r.w.prog.Failed)
	}
	r.commit(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/S", "comic/S/ch1"})
	if held := readContent(t, r.pool, leafID); held.Valid || deref(held.FileURI) != "/lib/S/ch1.cbz" {
		t.Fatalf("row = %+v, want the invalid row still holding comic/S/ch1", held)
	}

	r.reload()
	r.place(chapter)
	if counts := r.commit(false); counts.Added != 0 || counts.Updated != 1 {
		t.Fatalf("counts = %+v, want the original path to recover the row", counts)
	}

	recovered := readContent(t, r.pool, leafID)
	if !recovered.Valid || !recovered.CreatedAt.Equal(before.CreatedAt) || deref(recovered.FileURI) != "/lib/S/ch1.cbz" {
		t.Fatalf("row = %+v, want the invalid row recovered in place", recovered)
	}
}

func TestFlushReReducesSeriesAfterLaterMember(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"),
		metadata.Fields{Series: metadata.Val("S"), Publishers: metadata.Val([]string{"Beta"}), Language: metadata.Val("fr")}), 0, 2))
	r.commit(false)

	if got := readMeta(t, r.pool, r.lib, "comic/S").File; !slices.Equal(got.Publishers.V, []string{"Beta"}) || got.Title.V != "S" {
		t.Fatalf("series metadata = %+v", got)
	}

	r.reload()
	r.place(withOrder(withMeta(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"),
		metadata.Fields{Series: metadata.Val("S"), Publishers: metadata.Val([]string{"Alpha"})}), 0, 1))
	r.commit(false)

	got := readMeta(t, r.pool, r.lib, "comic/S").File
	if !slices.Equal(got.Publishers.V, []string{"Alpha"}) {
		t.Fatalf("publishers = %v, want the earlier member to win", got.Publishers.V)
	}
	if got.Language.V != "fr" {
		t.Fatalf("language = %q, want the later member to still contribute", got.Language.V)
	}

	seriesID := r.w.byURI["comic/S"]
	kids, err := db.SelectScalars[string](context.Background(), r.pool,
		`SELECT uri_part FROM content WHERE parent_id = $1 ORDER BY "order"`, seriesID)
	must(t, err)
	if !slices.Equal(kids, []string{"ch1", "ch2"}) {
		t.Fatalf("ordering = %v", kids)
	}
}

func TestFlushRanksTiedChildrenByURIPart(t *testing.T) {
	r := newTestScan(t, "comics")

	parts := []string{"ch4", "ch1", "ch6", "ch2", "ch5", "ch3"}
	for _, part := range parts {
		r.place(withOrder(comicResult("/lib/S/"+part+".cbz", part, "S", "/lib/S"), 0))
	}
	r.commit(false)

	kids, err := db.Select[models.Content](context.Background(), r.pool,
		`SELECT `+models.ContentColumns("")+` FROM content WHERE parent_id = $1 ORDER BY "order"`, r.w.byURI["comic/S"])
	must(t, err)
	if len(kids) != len(parts) {
		t.Fatalf("persisted children = %+v, want %d", kids, len(parts))
	}
	ranked := make([]string, len(kids))
	for i, kid := range kids {
		if kid.Order == nil || *kid.Order != i {
			t.Fatalf("child %d = %+v, want rank %d persisted", i, kid, i)
		}
		ranked[i] = kid.URIPart
	}
	if want := slices.Sorted(slices.Values(parts)); !slices.Equal(ranked, want) {
		t.Fatalf("children = %v, want tied siblings ranked by URI part", ranked)
	}
}

func TestFlushCrossSeriesKeyReuse(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "A", "/lib/A"))
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "B", "/lib/B"))
	r.commit(false)

	seriesB := r.w.byURI["comic/B"]
	displaced := r.w.keys[Key{seriesB, "ch1"}]

	r.reload()
	r.w.gone[displaced] = true
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/B"))
	counts := r.commit(true)

	if counts.Removed != 1 || counts.Updated != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	assertCatalog(t, r.pool, r.lib, []string{"comic/B", "comic/B/ch1"})
	moved := readContent(t, r.pool, r.w.keys[Key{seriesB, "ch1"}])
	if deref(moved.FileURI) != "/lib/A/ch1.cbz" || deref(moved.ParentID) != seriesB {
		t.Fatalf("moved row = %+v", moved)
	}
}

func TestFlushStandaloneKeyReleasedBeforeSeriesInsert(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/X/ch1.cbz", "ch1", "X", "/lib/X"))
	r.place(comicResult("/lib/Foo.cbz", "Foo", "", ""))
	r.commit(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/Foo", "comic/X", "comic/X/ch1"})

	r.reload()
	r.place(comicResult("/lib/Foo.cbz", "Foo", "X", "/lib/X"))
	r.place(comicResult("/lib/Foo/ch1.cbz", "ch1", "Foo", "/lib/Foo"))
	r.commit(false)

	want := []string{"comic/Foo", "comic/Foo/ch1", "comic/X", "comic/X/ch1", "comic/X/Foo"}
	assertCatalog(t, r.pool, r.lib, want)
	newSeries, err := db.SelectOne[models.Content](context.Background(), r.pool,
		"SELECT "+models.ContentColumns("")+" FROM content WHERE library_id = $1 AND uri = 'comic/Foo'", r.lib)
	must(t, err)
	if newSeries.Type != "comic_series" {
		t.Fatalf("comic/Foo = %+v, want the new series", newSeries)
	}
}

func TestFlushRenameMovesAnnotationsWithChildKeyChange(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch9.cbz", "ch9", "S", "/lib/S"))
	r.commit(false)

	seedRefs(t, r, "comic/S", "comic/S/ch1")
	series := r.w.byURI["comic/S"]
	leaf := r.w.keys[Key{series, "ch1"}]
	exec(t, r.pool, "INSERT INTO metadata_links (library_id, content_id, provider, state) VALUES ($1, $2, 'p', 'review')", r.lib, series)
	// A user row at the destination merges with the source's; neither was read, so the source's
	// state and notes win.
	exec(t, r.pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, notes) VALUES ('dst', 'u1', $1, 'comic/S_2019/ch2', 'destination')", r.lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch2", "S_2019", "/lib/S"))
	r.commit(false)

	want := []string{"comic/S_2019", "comic/S_2019/ch2", "comic/S_2019/ch9"}
	assertCatalog(t, r.pool, r.lib, want)

	assertRefs(t, r, map[string]string{"comic/S_2019": "comic/S", "comic/S_2019/ch2": "comic/S/ch1"})
	if fresh, err := db.SelectScalar[bool](context.Background(), r.pool,
		"SELECT bool_and(revision LIKE 'srv:%') FROM user_to_content"); err != nil || !fresh {
		t.Fatalf("moved rows without a fresh revision (%v)", err)
	}
	if id := contentIDByURI(t, r.pool, r.lib, "comic/S_2019/ch2"); id != leaf {
		t.Fatalf("leaf id = %s, want %s", id, leaf)
	}
	links, err := db.SelectScalars[string](context.Background(), r.pool,
		"SELECT c.uri || ':' || l.state FROM metadata_links l JOIN content c ON c.id = l.content_id")
	if must(t, err); !slices.Equal(links, []string{"comic/S_2019:review"}) {
		t.Fatalf("links = %v, want the series' link on its renamed row", links)
	}
	if got := readData(t, r.pool, r.lib, "comic/S_2019").Title.V; got != "comic/S" {
		t.Fatalf("series title = %q, want its override", got)
	}

	// A leaf's new match inputs make its series' pending link due. The leaf gets its metadata in
	// its own upsert, so it is written once.
	exec(t, r.pool, `UPDATE metadata_links SET retry_at = NULL;
		CREATE TABLE writes (id TEXT);
		CREATE FUNCTION log_write() RETURNS trigger LANGUAGE plpgsql AS $$
			BEGIN INSERT INTO writes VALUES (NEW.id); RETURN NULL; END $$;
		CREATE TRIGGER log_write AFTER UPDATE ON content FOR EACH ROW EXECUTE FUNCTION log_write()`)
	r.reload()
	r.place(withMeta(comicResult("/lib/S/ch1.cbz", "ch2", "S_2019", "/lib/S"), metadata.Fields{Volume: metadata.Val("7")}))
	r.commit(false)
	due, err := db.SelectScalars[string](context.Background(), r.pool,
		"SELECT content_id FROM metadata_links WHERE retry_at IS NOT NULL")
	if must(t, err); !slices.Equal(due, []string{series}) {
		t.Fatalf("due links = %v, want the series' link", due)
	}
	if n, err := db.SelectScalar[int](context.Background(), r.pool, "SELECT count(*) FROM writes WHERE id = $1", leaf); err != nil || n != 1 {
		t.Fatalf("leaf writes = %d (%v), want 1", n, err)
	}
	if got := readData(t, r.pool, r.lib, "comic/S_2019/ch2"); got.Title.V != "comic/S/ch1" || got.Volume.V != "7" {
		t.Fatalf("leaf data = %+v, want the file layer under its override", got)
	}
}

// A rename onto a URI where the user has a more recently read row keeps that row's reading state.
func TestFlushRenameMergesNewerUserRow(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)
	seedRefs(t, r, "comic/S/ch1")
	exec(t, r.pool, `UPDATE user_to_content SET status = 'reading', last_read_at = now() - interval '1 day',
		progress = '{"current_page": 3}' WHERE uri = 'comic/S/ch1'`)
	exec(t, r.pool, `INSERT INTO user_to_content (id, user_id, library_id, uri, status, last_read_at, progress, starred)
		VALUES ('dst', 'u1', $1, 'comic/S/ch2', 'completed', now(), '{"current_page": 9}', true)`, r.lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch2", "S", "/lib/S"))
	r.commit(false)

	var got string
	must(t, r.pool.QueryRow(context.Background(), `
		SELECT string_agg(concat_ws(' ', id, uri, status, progress::text, starred, notes, revision LIKE 'srv:%'), ',')
		FROM user_to_content WHERE library_id = $1`, r.lib).Scan(&got))
	if want := `dst comic/S/ch2 completed {"current_page": 9} t comic/S/ch1 t`; got != want {
		t.Fatalf("rows = %s, want %s", got, want)
	}
}

func TestFlushDeletesOrphansAndStampsScannedAt(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.commit(false)
	if at := libraryScannedAt(t, r.pool, r.lib); at != nil {
		t.Fatalf("scanned_at = %v, want nil before the final flush", at)
	}

	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/S"))
	counts := r.commit(true)

	if counts.Removed != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	assertCatalog(t, r.pool, r.lib, []string{"comic/T", "comic/T/ch1"})
	if at := libraryScannedAt(t, r.pool, r.lib); at == nil {
		t.Fatal("scanned_at was not stamped")
	}
}

// Deleted content takes its metadata and links with it, and its recent entry keeps its title.
// Re-added, it starts fresh.
func TestFlushDeletedContentTakesItsMetadata(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.commit(false)
	series := r.w.byURI["comic/S"]
	seedMetadata(t, r.pool, r.lib, "comic/S", metadata.Doc{Overrides: metadata.Fields{Title: metadata.Val("Mine")}})
	seedMetadata(t, r.pool, r.lib, "comic/S/ch1", metadata.Doc{Overrides: metadata.Fields{Title: metadata.Val("Chapter")}})
	exec(t, r.pool, "INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at) VALUES ('p', '1', '1', '{}', now(), now())")
	exec(t, r.pool, `INSERT INTO metadata_links (library_id, content_id, provider, state, external_id, origin)
		VALUES ($1, $2, 'p', 'linked', '1', 'manual')`, r.lib, series)

	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/S"))
	_, recent := r.commitRecent(true)

	assertCatalog(t, r.pool, r.lib, []string{"comic/T", "comic/T/ch1"})
	if n, err := db.SelectScalar[int](context.Background(), r.pool, "SELECT count(*) FROM metadata_links"); err != nil || n != 0 {
		t.Fatalf("links = %d (%v), want the series' link gone with it", n, err)
	}
	if got := recentByID(recent)[series]; got.Title != "Mine" || !got.Deleted || got.Removed != 1 {
		t.Fatalf("deleted series entry = %+v, want its title from the deleted row", got)
	}

	// Added again, it is new content without the old one's metadata.
	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(true)
	assertCatalog(t, r.pool, r.lib, []string{"comic/S", "comic/S/ch1", "comic/T", "comic/T/ch1"})
	if doc := readMeta(t, r.pool, r.lib, "comic/S"); doc.File.Title.V != "S" || !doc.Overrides.IsZero() ||
		readData(t, r.pool, r.lib, "comic/S").Title.V != "S" {
		t.Fatalf("metadata = %+v, want only a new file layer", doc)
	}
}

func TestFlushNoOpFinalStillEmits(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	notify := &recorder{}
	w := newWriter(ScanInput{LibraryID: lib}, nil, notify, newResolver(), nil, nil)
	rig := newRig(t, w)
	go commitLoop(rig.ctx, pool, testStore, &ComicsScanner{}, lib, rig.flushes, rig.done)
	rig.start()
	rig.walkOver()

	if err := rig.finish(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if w.prog.Phase != "done" || w.prog.CommitSeq != 1 || w.prog.Saved != (Counts{}) {
		t.Fatalf("progress = %+v", w.prog)
	}
	if seqs := notify.seqs(); !slices.Equal(seqs, []int{1}) {
		t.Fatalf("catalog events = %v, want one for the no-op final flush", seqs)
	}
	if at := libraryScannedAt(t, pool, lib); at == nil {
		t.Fatal("a no-op final flush must still stamp scanned_at")
	}
}

func TestFlushRenameHistory(t *testing.T) {
	for _, c := range []struct {
		name   string
		series []string
		final  string
	}{
		{"round trip", []string{"T", "S"}, "comic/S"},
		{"round trip then rename", []string{"T", "S", "U"}, "comic/U"},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newTestScan(t, "comics")
			parts := []string{"ch1", "ch2", "ch3"}
			for _, part := range parts {
				r.place(comicResult("/lib/S/"+part+".cbz", part, "S", "/lib/S"))
			}
			r.commit(false)

			layered := []string{"", "/ch1", "/ch3"}
			for _, suffix := range layered {
				seedMetadata(t, r.pool, r.lib, "comic/S"+suffix, metadata.Doc{
					Overrides: metadata.Fields{Title: metadata.Val("kept comic/S" + suffix)},
				})
			}
			exec(t, r.pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
			exec(t, r.pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a1', 'u1', $1, 'comic/S/ch3', true)", r.lib)

			r.reload()
			for i, series := range c.series {
				r.place(comicResult("/lib/S/"+parts[i]+".cbz", parts[i], series, "/lib/S"))
			}
			r.commit(false)

			assertCatalog(t, r.pool, r.lib, []string{c.final, c.final + "/ch1", c.final + "/ch2", c.final + "/ch3"})
			for _, suffix := range layered {
				if got := readMeta(t, r.pool, r.lib, c.final+suffix).Overrides; got.Title.V != "kept comic/S"+suffix {
					t.Fatalf("override at %s = %+v, want the one from comic/S%s", c.final+suffix, got, suffix)
				}
			}
			assertAnnotations(t, r.pool, r.lib, []string{c.final + "/ch3"})
		})
	}
}

func TestFlushIgnoresSelfRenameFromAnyProducer(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)
	for _, uri := range []string{"comic/S", "comic/S/ch1"} {
		seedMetadata(t, r.pool, r.lib, uri, metadata.Doc{Overrides: metadata.Fields{Title: metadata.Val("kept " + uri)}})
	}

	r.reload()
	set := r.w.set(r.w.byURI["comic/S"])
	set.OldURI = set.Ref.URI
	r.commit(false)

	for _, uri := range []string{"comic/S", "comic/S/ch1"} {
		if got := readMeta(t, r.pool, r.lib, uri).Overrides; got.Title.V != "kept "+uri {
			t.Fatalf("override at %s = %+v, want it preserved", uri, got)
		}
	}
}

func TestFlushRenameChainAcrossSeries(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "A", "/lib/A"))
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "B", "/lib/B"))
	r.commit(false)

	seedRefs(t, r, "comic/A", "comic/A/ch1", "comic/B", "comic/B/ch1")

	r.reload()
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "C", "/lib/B"))
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/A"))
	r.commit(false)

	want := []string{"comic/B", "comic/B/ch1", "comic/C", "comic/C/ch1"}
	assertCatalog(t, r.pool, r.lib, want)
	assertRefs(t, r, map[string]string{
		"comic/B": "comic/A", "comic/B/ch1": "comic/A/ch1",
		"comic/C": "comic/B", "comic/C/ch1": "comic/B/ch1",
	})
}

func TestFlushCustomListAnnotationCollision(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)

	exec(t, r.pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, r.pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ('cl1', 'list', 'private', 'u1')")
	exec(t, r.pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ('cl2', 'other', 'private', 'u1')")
	exec(t, r.pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('src', 'cl1', $1, 'comic/S/ch1', 'source')", r.lib)
	exec(t, r.pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('dst', 'cl1', $1, 'comic/S_2019/ch1', 'destination')", r.lib)
	exec(t, r.pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('free', 'cl2', $1, 'comic/S/ch1', 'free')", r.lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	r.commit(false)

	rows, err := db.Select[models.CustomListToContent](context.Background(), r.pool,
		"SELECT * FROM custom_list_to_content WHERE library_id = $1 ORDER BY id", r.lib)
	must(t, err)
	got := map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.URI
	}
	want := map[string]string{"free": "comic/S_2019/ch1", "src": "comic/S_2019/ch1"}
	if !maps.Equal(got, want) {
		t.Fatalf("list entries = %v, want %v", got, want)
	}
}

func TestFlushIntermediateReductionExcludesGone(t *testing.T) {
	r := newTestScan(t, "comics")
	beta := metadata.Fields{Publishers: metadata.Val([]string{"Beta"}), Language: metadata.Val("fr")}

	r.place(withOrder(withMeta(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"),
		metadata.Fields{Series: metadata.Val("S"), Publishers: metadata.Val([]string{"Alpha"})}), 0, 1))
	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"), beta), 0, 2))
	r.commit(false)
	if got := readMeta(t, r.pool, r.lib, "comic/S").File; !slices.Equal(got.Publishers.V, []string{"Alpha"}) {
		t.Fatalf("series metadata = %+v", got)
	}

	seriesID := r.w.byURI["comic/S"]
	goneID := r.w.keys[Key{seriesID, "ch1"}]

	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/S", "ch2.cbz"))
	if !r.w.gone[goneID] {
		t.Fatalf("gone = %v, want the missing leaf", r.w.gone)
	}
	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"), beta), 0, 2))
	r.commit(false)

	if got := readMeta(t, r.pool, r.lib, "comic/S").File; !slices.Equal(got.Publishers.V, []string{"Beta"}) {
		t.Fatalf("publishers = %v, want the proven-missing child excluded", got.Publishers.V)
	}
	if row := readContent(t, r.pool, goneID); row.URI != "comic/S/ch1" {
		t.Fatalf("proven-missing row = %+v, want it kept until the final flush", row)
	}
	kept := readContent(t, r.pool, r.w.keys[Key{seriesID, "ch2"}])
	if kept.Order == nil || *kept.Order != 0 {
		t.Fatalf("order = %v, want the remaining child first", kept.Order)
	}
}

func TestFlushFailedFinalRollsBackScannedAt(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	exec(t, pool, `
		CREATE FUNCTION fail_stamp() RETURNS trigger AS $fn$
		BEGIN
			RAISE EXCEPTION 'no stamp' USING ERRCODE = '22000';
		END $fn$ LANGUAGE plpgsql`)
	exec(t, pool, "CREATE TRIGGER fail_stamp AFTER UPDATE ON libraries FOR EACH ROW EXECUTE FUNCTION fail_stamp()")

	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	if _, _, _, err := r.recordCommit(true); err == nil {
		t.Fatal("expected the final flush to fail")
	}

	if at := libraryScannedAt(t, pool, lib); at != nil {
		t.Fatalf("scanned_at = %v, want the failed final flush rolled back", at)
	}
	assertCatalog(t, pool, lib, nil)
}

func TestFlushSharedSeriesAcrossDirectories(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S2/ch2.cbz", "ch2", "S", "/lib/S2"))
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)

	want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}
	assertCatalog(t, r.pool, r.lib, want)
	seriesID := r.w.byURI["comic/S"]
	if got := readContent(t, r.pool, seriesID); deref(got.FileURI) != "/lib/S" {
		t.Fatalf("series file_uri = %v, want the smallest member directory", deref(got.FileURI))
	}
	for _, part := range []string{"ch1", "ch2"} {
		if row := readContent(t, r.pool, r.w.keys[Key{seriesID, part}]); deref(row.ParentID) != seriesID {
			t.Fatalf("%s parent = %v, want the shared series", part, row.ParentID)
		}
	}

	exec(t, r.pool, "UPDATE content SET file_uri = $2 WHERE id = $1", seriesID, "/lib/Gone")
	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	counts := r.commit(false)
	if counts.Added != 0 || counts.Updated != 1 {
		t.Fatalf("counts = %+v, want the shared series reused", counts)
	}
	assertCatalog(t, r.pool, r.lib, want)
	if got := readContent(t, r.pool, seriesID); deref(got.FileURI) != "/lib/S" {
		t.Fatalf("series file_uri = %v, want a directory with no members patched to the smallest one", deref(got.FileURI))
	}
}

func TestFlushKeepsSeriesRenameWhenLeafConflicts(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)

	r.reload()
	r.place(comicResult("/lib/S/other.cbz", "ch1", "S_2019", "/lib/S"))
	if r.w.prog.Failed != 1 {
		t.Fatalf("failed = %d, want the leaf conflict", r.w.prog.Failed)
	}
	seriesID := r.w.byURI["comic/S_2019"]
	set := r.w.sets[seriesID]
	if set.OldURI != "comic/S" || len(set.Writes) != 0 {
		t.Fatalf("set = %+v, want the rename kept without writes", set)
	}
	r.commit(false)

	want := []string{"comic/S_2019", "comic/S_2019/ch1"}
	assertCatalog(t, r.pool, r.lib, want)
}

func bookResult(path string, meta epub.Metadata) Result {
	item := classifyBook(fsFile(path, baseTime, 10), meta, false, nil, false)
	return Result{File: item.File, Item: &item}
}

func TestSlashSeriesAppearsWhileTheLeafItShadowsDeparts(t *testing.T) {
	r := newTestScan(t, "books")

	r.place(bookResult("/lib/Foo/bar.epub", epub.Metadata{Title: "Bar", Series: "Foo"}))
	r.commit(false)
	assertCatalog(t, r.pool, r.lib, []string{"book/Foo", "book/Foo/bar"})

	r.reload()
	r.place(bookResult("/lib/Foo/bar.epub", epub.Metadata{Title: "Bar"}))
	r.place(bookResult("/lib/Foo bar/x.epub", epub.Metadata{Title: "X", Series: "Foo/bar"}))
	r.commit(true)

	want := []string{"book/Foo_bar", "book/Foo_bar/x", "book/bar"}
	got := contentURIs(t, r.pool, r.lib)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}

	series := readContent(t, r.pool, contentIDByURI(t, r.pool, r.lib, "book/Foo_bar"))
	if series.Type != "book_series" || series.URIPart != "Foo_bar" {
		t.Fatalf("series = %+v, want the sanitized series row", series)
	}
	if got := readMeta(t, r.pool, r.lib, "book/Foo_bar").File.Title.V; got != "Foo/bar" {
		t.Fatalf("series title = %q, want the name kept verbatim", got)
	}
}

func TestFlushInheritsTheRawFallbackSeriesTitle(t *testing.T) {
	r := newTestScan(t, "comics")

	item := classifyComic(fsFile("/lib/Foo\\bar/ch1.cbz", baseTime, 10), metadata.Fields{}, nil, testPages)
	r.place(Result{File: item.File, Item: item})
	r.commit(true)

	want := []string{"comic/Foo_bar", "comic/Foo_bar/ch1"}
	assertCatalog(t, r.pool, r.lib, want)
	if got := readMeta(t, r.pool, r.lib, "comic/Foo_bar").File.Title.V; got != "Foo\\bar" {
		t.Fatalf("series title = %q, want the raw directory name", got)
	}
}

func TestFlushSeriesTitleIgnoresChildOverrides(t *testing.T) {
	r := newTestScan(t, "comics")

	chapter := func(number string) Result { return comicItem("/lib/Series", number, metadata.Fields{}) }
	r.place(chapter("1"), chapter("2"))
	r.commit(false)

	doc := readMeta(t, r.pool, r.lib, "comic/Series/ch1")
	doc.Overrides = metadata.Fields{Series: metadata.Val("Curated")}
	seedMetadata(t, r.pool, r.lib, "comic/Series/ch1", doc)

	r.reload()
	r.place(chapter("1"))
	r.commit(true)
	if got := readMeta(t, r.pool, r.lib, "comic/Series").File.Title.V; got != "Series" {
		t.Fatalf("series title = %q, want the folder name, not a child override", got)
	}
}

func TestFlushSeriesTitleFallsBackToTheFolderName(t *testing.T) {
	r := newTestScan(t, "comics")

	r.place(comicItem("/lib/Foo (2019)", "1", metadata.Fields{}))
	r.commit(true)

	if got := readMeta(t, r.pool, r.lib, "comic/Foo_2019").File.Title.V; got != "Foo" {
		t.Fatalf("series title = %q, want the folder name without its year", got)
	}
}

func TestFallbackTitleHoldsWhateverFolderSharesASeriesFirst(t *testing.T) {
	pool := newTestPool(t)
	dirs := []string{"/lib/Foo (2019)", "/lib/Foo_2019"}

	for _, first := range []int{0, 1} {
		seen := []int{first, 1 - first}

		lib := newTestLibrary(t, pool, "comics")
		run := newScanRun(t, pool, lib, &ComicsScanner{})
		for n, i := range seen {
			run.place(comicItem(dirs[i], fmt.Sprint(n+1), metadata.Fields{}))
		}
		run.commit(true)
		if got := readMeta(t, pool, lib, "comic/Foo_2019").File.Title.V; got != "Foo" {
			t.Errorf("title = %q with %s first, want %q", got, dirs[first], "Foo")
		}
	}
}

func TestSharedSeriesFileURIPicksTheSmallestDirectory(t *testing.T) {
	pool := newTestPool(t)
	root := t.TempDir()
	dirs := []string{filepath.Join(root, "Foo\\bar"), filepath.Join(root, "Foo_bar")}
	for _, dir := range dirs {
		writeFile(t, filepath.Join(dir, "cover.jpg"), filepath.Base(dir))
	}

	for _, first := range []int{0, 1} {
		seen := []int{first, 1 - first}

		lib := newTestLibrary(t, pool, "comics")
		run := newScanRun(t, pool, lib, &ComicsScanner{})
		for n, i := range seen {
			run.place(comicItem(dirs[i], fmt.Sprint(n+1), metadata.Fields{}))
		}
		run.commit(true)
		assertSeriesLocation(t, pool, contentIDByURI(t, pool, lib, "comic/Foo_bar"), dirs[0])
	}
}

func TestSeriesKeepsItsDirectoryWhileANeighbourClaimsTheOther(t *testing.T) {
	pool := newTestPool(t)
	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")

	results := []Result{
		comicResult("/lib/A/ch1.cbz", "ch1", "S", "/lib/A"),
		comicResult("/lib/Z/ch2.cbz", "ch2", "S", "/lib/Z"),
		comicResult("/lib/Z/ch3.cbz", "ch3", "T", "/lib/Z"),
	}
	want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2", "comic/T", "comic/T/ch3"}

	for _, order := range [][]int{{0, 1, 2}, {1, 0, 2}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			run := newScanRun(t, pool, newTestLibrary(t, pool, "comics"), &ComicsScanner{})
			run.place(results[0])
			run.commit(false)
			series := run.w.byURI["comic/S"]
			exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ($1, 'u1', $2, $3, true)",
				models.MakeContentID(), run.lib, "comic/S/ch1")

			run.reload()
			for _, i := range order {
				run.place(results[i])
			}
			run.commit(true)
			if got := contentURIs(t, pool, run.lib); !slices.Equal(got, want) {
				t.Errorf("uris = %v, want %v", got, want)
			}
			if got := annotationURIs(t, pool, run.lib); !slices.Equal(got, []string{"comic/S/ch1"}) {
				t.Errorf("annotations = %v, want the annotation left where it was", got)
			}
			if got := deref(readContent(t, pool, series).FileURI); got != "/lib/A" {
				t.Errorf("series file_uri = %v, want the stored directory kept", got)
			}
		})
	}
}

func TestSeriesLayerReplacesWholeFileLayer(t *testing.T) {
	r := newTestScan(t, "comics")
	place := func() {
		r.place(withMeta(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"), metadata.Fields{
			Series: metadata.Val("New Series"), Genres: metadata.Val([]string{"Child Genre"}),
			Language: metadata.Val("en"), Description: metadata.Val("A volume"),
		}))
	}
	place()
	r.commit(false)
	seedMetadata(t, r.pool, r.lib, "comic/S", metadata.Doc{
		File: metadata.Fields{
			Title: metadata.Val("Stale"), Publishers: metadata.Val([]string{"Stale Press"}),
			Genres: metadata.Val([]string{"stale"}), Language: metadata.Val("jp"),
		},
		Overrides: metadata.Fields{
			Publishers: metadata.Val([]string{"Override Press"}), Description: metadata.Opt[string]{P: metadata.Null},
		},
	})

	r.reload()
	place()
	r.commit(true)

	got := readMeta(t, r.pool, r.lib, "comic/S")
	file := got.File
	if file.Title.V != "New Series" || !slices.Equal(file.Genres.V, []string{"child_genre"}) || file.Language.V != "en" {
		t.Fatalf("file layer = %+v", file)
	}
	if file.Publishers.P != metadata.Absent || file.Description.P != metadata.Absent {
		t.Fatalf("stale or volume fields in the series layer: %+v", file)
	}
	if !slices.Equal(got.Overrides.Publishers.V, []string{"Override Press"}) || got.Overrides.Description.P != metadata.Null {
		t.Fatalf("overrides = %+v", got.Overrides)
	}

	data := readData(t, r.pool, r.lib, "comic/S")
	if !slices.Equal(data.Publishers.V, []string{"Override Press"}) || data.Title.V != "New Series" || data.Language.V != "en" {
		t.Fatalf("data = %+v, want overrides over the new file layer", data)
	}
}

func TestFlushReducesSeriesOverInvalidChildren(t *testing.T) {
	for _, c := range []struct {
		libType, kind, dir, first, second string
	}{
		{"comics", "comic", "/lib/s", "/lib/s/ch1.cbz", "/lib/s/ch2.cbz"},
		{"books", "book", "", "/lib/s/one.epub", "/lib/s/two.epub"},
	} {
		t.Run(c.libType, func(t *testing.T) {
			r := newTestScan(t, c.libType)
			part := func(path string) string {
				return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			}
			member := func(path string, index float32, mtime time.Time, m metadata.Fields) Result {
				item := &ParsedItem{
					File:        fsFile(path, mtime, 10),
					URIPrefix:   c.kind,
					ContentType: c.kind,
					URIPart:     part(path),
					OrderParts:  []*float32{f32(index)},
					CoverSuffix: new("cover.jpg"),
					MetaRaw:     m,
					Series:      &ParsedSeries{URIPrefix: c.kind, URIPart: "s", ContentType: c.kind + "_series"},
				}
				if c.dir != "" {
					item.Series.FileURI = &c.dir
				}
				return Result{File: item.File, Item: item}
			}

			r.place(member(c.first, 1, baseTime, metadata.Fields{
				Series: metadata.Val("Retained Series"), Publishers: metadata.Val([]string{"Retained Press"}),
				Genres: metadata.Val([]string{"Retained Genre"})}))
			r.commit(false)

			r.reload()
			exec(t, r.pool, `UPDATE content SET "order" = 9 WHERE library_id = $1 AND uri_part = $2`,
				r.lib, part(c.first))
			exec(t, r.pool, "UPDATE content SET cover_uri = NULL, file_mtime = NULL WHERE id = $1",
				r.w.byURI[c.kind+"/s"])
			r.place(Result{File: fsFile(c.first, baseTime, 10)})
			r.place(member(c.second, 2, baseTime.Add(time.Hour), metadata.Fields{
				Series: metadata.Val("Other"), Publishers: metadata.Val([]string{"Other Press"}), Language: metadata.Val("en")}))
			r.commit(false)

			got := readMeta(t, r.pool, r.lib, c.kind+"/s").File
			if got.Title.V != "Retained Series" || !slices.Equal(got.Publishers.V, []string{"Retained Press"}) ||
				!slices.Equal(got.Genres.V, []string{"retained_genre"}) {
				t.Fatalf("series metadata = %+v, want the invalid first child still inherited from", got)
			}
			if got.Language.V != "en" {
				t.Fatalf("series metadata = %+v, want the valid child to contribute too", got)
			}

			seriesID := r.w.byURI[c.kind+"/s"]
			series := readContent(t, r.pool, seriesID)
			if deref(series.CoverURI) != c.first+"/cover.jpg" {
				t.Fatalf("series cover = %v, want the invalid first child's cover", series.CoverURI)
			}
			if series.FileMtime == nil || !series.FileMtime.Equal(baseTime) {
				t.Fatalf("series mtime = %v, want the invalid first child's mtime", series.FileMtime)
			}

			kids, err := db.Select[models.Content](context.Background(), r.pool,
				`SELECT `+models.ContentColumns("")+` FROM content WHERE parent_id = $1 ORDER BY "order"`, seriesID)
			must(t, err)
			if len(kids) != 2 || deref(kids[0].Order) != 0 || deref(kids[1].Order) != 1 {
				t.Fatalf("children = %+v, want the invalid child ranked first", kids)
			}
			first := kids[0]
			if first.Valid || first.URIPart != part(c.first) || deref(first.FileURI) != c.first ||
				deref(first.FileSize) != 10 || !first.FileMtime.Equal(baseTime) {
				t.Fatalf("invalid child = %+v", first)
			}
		})
	}
}

func TestSeriesLayerSkipsSeriesWithoutChildren(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "S", "/lib/A"))
	r.commit(false)
	seedMetadata(t, r.pool, r.lib, "comic/S", metadata.Doc{File: metadata.Fields{Title: metadata.Val("Existing")}})

	// A non-final flush keeps the gone chapter's row, which leaves the series without children.
	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/A"))
	r.commit(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/S", "comic/S/ch1"})
	if got := readMeta(t, r.pool, r.lib, "comic/S").File.Title.V; got != "Existing" {
		t.Fatalf("series title = %q, want a childless series left alone", got)
	}
}

func TestFlushMovesRefsWithRegroupedBooks(t *testing.T) {
	r := newTestScan(t, "books")
	r.place(bookResult("/lib/Foo v1.epub", epub.Metadata{Title: "Foo v1"}))
	r.commit(false)
	// The second set is left by removed content where the book is about to move.
	seedRefs(t, r, "book/Foo v1", "book/Foo/Foo v1")

	r.reload()
	r.place(inferredResult("/lib/Foo v1.epub", "Foo Vol. 1"))
	r.commit(true)
	assertCatalog(t, r.pool, r.lib, []string{"book/Foo", "book/Foo/Foo v1"})
	assertRefs(t, r, map[string]string{"book/Foo/Foo v1": "book/Foo v1"})

	r.reload()
	r.place(bookResult("/lib/Foo v1.epub", epub.Metadata{Title: "Foo v1"}))
	r.commit(true)
	assertCatalog(t, r.pool, r.lib, []string{"book/Foo v1"})
	assertRefs(t, r, map[string]string{"book/Foo v1": "book/Foo v1"})
}

func TestFlushBookJoinsTheBookSeriesHoldingItsName(t *testing.T) {
	series := map[string]func() Result{
		"inferred": func() Result { return inferredResult("/lib/Foo Vol. 2.epub", "Foo Vol. 2") },
		"metadata": func() Result { return bookResult("/lib/Foo Vol. 2.epub", epub.Metadata{Title: "Two", Series: "Foo"}) },
	}
	solo := func() Result { return bookResult("/lib/Foo.epub", epub.Metadata{Title: "Foo"}) }
	for name, vol := range series {
		t.Run(name+" series first", func(t *testing.T) {
			r := newTestScan(t, "books")
			r.place(vol())
			r.commit(false)
			r.place(solo())
			r.commit(true)
			if r.w.prog.Failed != 0 {
				t.Fatalf("failed = %d", r.w.prog.Failed)
			}
			assertCatalog(t, r.pool, r.lib, []string{"book/Foo", "book/Foo/Foo", "book/Foo/Foo Vol. 2"})
			if got := readContent(t, r.pool, contentIDByURI(t, r.pool, r.lib, "book/Foo/Foo")); len(got.OrderParts) != 1 || got.OrderParts[0] != nil {
				t.Fatalf("joined book order = %v, want unknown", got.OrderParts)
			}
		})
	}
	t.Run("blocked inference", func(t *testing.T) {
		r := newTestScan(t, "books")
		r.place(inferredResult("/lib/Foo v1.epub", "Foo Vol. 1"))
		r.place(bookResult("/lib/Bar.epub", epub.Metadata{Title: "Bar"}))
		r.commit(false)
		// Infers Bar volume 2, which the standalone Bar blocks, then joins the series Foo.
		r.place(inferredResult("/lib/Foo.epub", "Bar Vol. 2"))
		r.commit(true)
		assertCatalog(t, r.pool, r.lib, []string{"book/Bar", "book/Foo", "book/Foo/Foo", "book/Foo/Foo v1"})
		if got := readContent(t, r.pool, contentIDByURI(t, r.pool, r.lib, "book/Foo/Foo")); len(got.OrderParts) != 1 || got.OrderParts[0] != nil {
			t.Fatalf("joined book order = %v, want the inferred volume dropped", got.OrderParts)
		}
	})
	t.Run("book first", func(t *testing.T) {
		r := newTestScan(t, "books")
		r.place(solo())
		r.commit(false)
		r.place(series["inferred"]())
		r.commit(true)
		if r.w.prog.Failed != 0 {
			t.Fatalf("failed = %d", r.w.prog.Failed)
		}
		assertCatalog(t, r.pool, r.lib, []string{"book/Foo", "book/Foo Vol. 2"})
	})
}

// seedRefs puts a user row and a list entry at each uri, and an override on the content there if
// any, all labelled with that uri.
func seedRefs(t *testing.T, r *scanRun, uris ...string) {
	t.Helper()
	exec(t, r.pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, r.pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ('cl1', 'list', 'private', 'u1')")
	for i, uri := range uris {
		id := fmt.Sprint("r", i)
		exec(t, r.pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, notes) VALUES ($1, 'u1', $2, $3, $3)", id, r.lib, uri)
		exec(t, r.pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ($1, 'cl1', $2, $3, $3)", id, r.lib, uri)
		raw, _ := json.Marshal(metadata.Doc{V: 2, Overrides: metadata.Fields{Title: metadata.Val(uri)}})
		exec(t, r.pool, "UPDATE content SET data_raw = $3 WHERE library_id = $1 AND uri = $2", r.lib, uri, raw)
	}
}

// assertRefs checks that the user and list refs labelled with each source sit at its target, and
// that the content there is the source's, carrying its override.
func assertRefs(t *testing.T, r *scanRun, want map[string]string) {
	t.Helper()
	for _, table := range []string{"user_to_content", "custom_list_to_content"} {
		got := map[string]string{}
		var uri, label string
		rows, err := r.pool.Query(context.Background(), "SELECT uri, notes FROM "+table+" WHERE library_id = $1", r.lib)
		must(t, err)
		_, err = pgx.ForEachRow(rows, []any{&uri, &label}, func() error { got[uri] = label; return nil })
		must(t, err)
		if !maps.Equal(got, want) {
			t.Fatalf("%s = %v, want %v", table, got, want)
		}
	}
	for uri, label := range want {
		if got := readMeta(t, r.pool, r.lib, uri).Overrides; got.Title.V != label {
			t.Fatalf("override at %s = %+v, want %s's", uri, got, label)
		}
	}
}

func TestFlushNewSeriesAtAFreedURITakesNoRefs(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "A", "/lib/A"))
	r.commit(false)
	seedRefs(t, r, "comic/A", "comic/A/ch1")

	r.reload()
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/A"))
	r.place(comicResult("/lib/X/ch1.cbz", "ch1", "A", "/lib/X"))
	r.place(comicResult("/lib/X/ch2.cbz", "ch2", "C", "/lib/X"))
	r.commit(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/B", "comic/B/ch1", "comic/C", "comic/C/ch1", "comic/C/ch2"})
	assertRefs(t, r, map[string]string{"comic/B": "comic/A", "comic/B/ch1": "comic/A/ch1"})
}

func TestFlushLeavesSwappingURIsKeepTheirRefs(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "A", "/lib/A"))
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "B", "/lib/B"))
	r.place(comicResult("/lib/B/ch2.cbz", "ch2", "B", "/lib/B"))
	r.commit(false)
	seedRefs(t, r, "comic/A", "comic/A/ch1", "comic/B", "comic/B/ch1", "comic/B/ch2")

	// B→C, then A→B, then a new A takes B's former ch1: A/ch1 and B/ch1 swap.
	r.reload()
	r.place(comicResult("/lib/B/ch2.cbz", "ch2", "C", "/lib/B"))
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/A"))
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "A", "/lib/Y"))
	r.commit(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/A", "comic/A/ch1", "comic/B", "comic/B/ch1", "comic/C", "comic/C/ch2"})
	assertRefs(t, r, map[string]string{
		"comic/B": "comic/A", "comic/B/ch1": "comic/A/ch1",
		"comic/C": "comic/B", "comic/C/ch2": "comic/B/ch2",
		"comic/A/ch1": "comic/B/ch1",
	})
}

func withCover(r Result) Result {
	r.Item.CoverSuffix = new("001.jpg")
	return r
}

func recentByID(recent []RecentEntry) map[string]RecentEntry {
	byID := map[string]RecentEntry{}
	for _, e := range recent {
		byID[e.ID] = e
	}
	return byID
}

func TestFlushRecentCountsAddUpToTheTotals(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))
	r.place(comicResult("/lib/solo.cbz", "solo", "", ""))
	r.commit(false)
	seriesS, solo := r.w.byURI["comic/S"], r.w.keys[Key{"", "solo"}]

	r.reload()
	r.w.gone[r.w.keys[Key{seriesS, "ch2"}]] = true
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch3.cbz", "ch3", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.place(comicResult("/lib/other.cbz", "other", "", ""))
	r.w.gone[solo] = true
	counts, recent := r.commitRecent(true)

	if counts != (Counts{Added: 3, Updated: 1, Removed: 2}) {
		t.Fatalf("counts = %+v", counts)
	}
	var sum Counts
	for _, e := range recent {
		sum.add(e.Counts)
	}
	if sum != counts {
		t.Fatalf("entries sum to %+v, want %+v: %+v", sum, counts, recent)
	}
	byID := recentByID(recent)
	if got := byID[seriesS]; got.Counts != (Counts{Added: 1, Updated: 1, Removed: 1}) || got.Title != "S" || got.Deleted {
		t.Fatalf("series S = %+v", got)
	}
	// Deletes are stamped at take, after every placement.
	got := fp.Map(recent[2:], func(e RecentEntry) string { return e.ID })
	if want := []string{r.w.keys[Key{"", "other"}], r.w.byURI["comic/T"]}; !slices.Equal(got, want) {
		t.Fatalf("recent = %+v, want the deletes, then the newest placement first", recent)
	}
}

func TestFlushRecentCountsAReclaimedIDAsUpdated(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/Foo.cbz", "Foo", "", ""))
	r.commit(false)
	id := r.w.keys[Key{"", "Foo"}]

	r.reload()
	r.w.gone[id] = true
	r.place(comicResult("/lib/moved/Foo.cbz", "Foo", "", ""))
	counts, recent := r.commitRecent(false)

	if counts != (Counts{Updated: 1}) {
		t.Fatalf("counts = %+v, want the reclaimed row counted as updated", counts)
	}
	if len(recent) != 1 || recent[0].ID != id || recent[0].Counts != (Counts{Updated: 1}) {
		t.Fatalf("recent = %+v", recent)
	}
}

func TestFlushRecentStandaloneDeleteKeepsItsTitle(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(withCover(withMeta(comicResult("/lib/solo.cbz", "solo", "", ""), metadata.Fields{Title: metadata.Val("Solo Title")})))
	r.commit(false)
	solo := r.w.keys[Key{"", "solo"}]

	r.reload()
	r.w.gone[solo] = true
	_, recent := r.commitRecent(true)

	want := RecentEntry{ID: solo, Title: "Solo Title", Counts: Counts{Removed: 1}, Deleted: true}
	if len(recent) != 1 || recent[0] != want {
		t.Fatalf("recent = %+v, want %+v", recent, want)
	}
}

func TestFlushRecentDeleteKeepsItsTitleWhenALeafTakesItsURI(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(withMeta(comicResult("/lib/Foo.cbz", "Foo", "", ""), metadata.Fields{Title: metadata.Val("Original")}))
	r.place(withMeta(comicResult("/lib/S/Foo.cbz", "Foo", "S", "/lib/S"), metadata.Fields{Title: metadata.Val("Replacement")}))
	r.commit(false)
	original := r.w.keys[Key{"", "Foo"}]
	survivor := r.w.keys[Key{r.w.byURI["comic/S"], "Foo"}]

	r.reload()
	r.w.gone[original] = true
	r.place(withMeta(comicResult("/lib/S/Foo.cbz", "Foo", "", ""), metadata.Fields{Title: metadata.Val("Replacement")}))
	_, recent := r.commitRecent(false)

	if got := readMeta(t, r.pool, r.lib, "comic/Foo").File.Title.V; got != "Replacement" {
		t.Fatalf("comic/Foo title = %q, want the survivor's metadata there", got)
	}
	byID := recentByID(recent)
	if got := byID[original]; got.Title != "Original" || !got.Deleted || got.Removed != 1 {
		t.Fatalf("deleted entry = %+v, want its own title", got)
	}
	if got := byID[survivor]; got.Title != "Replacement" || got.Updated != 1 {
		t.Fatalf("survivor entry = %+v", got)
	}
}

func TestFlushRecentMarksADeletedSeries(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(withCover(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S")))
	r.place(withCover(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S")))
	r.commit(false)
	seriesS := r.w.byURI["comic/S"]
	if readContent(t, r.pool, seriesS).CoverURI == nil {
		t.Fatal("series must start with a cover")
	}

	r.reload()
	r.w.gone[r.w.keys[Key{seriesS, "ch1"}]] = true
	r.w.gone[r.w.keys[Key{seriesS, "ch2"}]] = true
	_, recent := r.commitRecent(true)

	want := RecentEntry{ID: seriesS, Title: "S", Counts: Counts{Removed: 2}, Deleted: true}
	if len(recent) != 1 || recent[0] != want {
		t.Fatalf("recent = %+v, want %+v", recent, want)
	}
}

func TestFlushRecentSkipsRenamesAndInvalidation(t *testing.T) {
	r := newTestScan(t, "comics")
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.commit(false)
	seriesS := r.w.byURI["comic/S"]

	r.reload()
	s := r.w.set(seriesS)
	s.OldURI, s.Ref.URI, s.Ref.URIPart = s.Ref.URI, "comic/S2", "S2"
	r.place(Result{File: fsFile("/lib/T/ch1.cbz", baseTime, 10)})
	counts, recent := r.commitRecent(false)

	assertCatalog(t, r.pool, r.lib, []string{"comic/S2", "comic/S2/ch1", "comic/T", "comic/T/ch1"})
	if counts != (Counts{}) || len(recent) != 0 {
		t.Fatalf("counts = %+v, recent = %+v, want neither a rename nor an invalidation counted", counts, recent)
	}
}

func TestFlushRecentKeepsTheNewestEntries(t *testing.T) {
	r := newTestScan(t, "comics")
	var want []string
	for i := range RecentCap + 2 {
		part := fmt.Sprintf("item%d", i)
		r.place(withCover(comicResult("/lib/"+part+".cbz", part, "", "")))
		want = append(want, r.w.keys[Key{"", part}])
	}
	slices.Reverse(want)
	counts, recent := r.commitRecent(false)

	if counts != (Counts{Added: RecentCap + 2}) {
		t.Fatalf("counts = %+v, want every entry summed, not only the kept ones", counts)
	}

	got := make([]string, len(recent))
	for i, e := range recent {
		got[i] = e.ID
		if e.CoverVersion == nil || e.Counts != (Counts{Added: 1}) {
			t.Fatalf("entry = %+v", e)
		}
	}
	if !slices.Equal(got, want[:RecentCap]) {
		t.Fatalf("recent = %v, want %v", got, want[:RecentCap])
	}
}
