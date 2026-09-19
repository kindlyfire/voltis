package scanner

import (
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5/pgxpool"
)

func withMeta(r Result, m models.Metadata) Result {
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

func withCover(r Result, suffix string) Result {
	r.Item.CoverSuffix = &suffix
	return r
}

func withFileData(r Result, data string) Result {
	r.Item.FileData = json.RawMessage(data)
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
	if err != nil {
		t.Fatal(err)
	}
	res := newResolver()
	fps, err := loadFingerprints(context.Background(), pool, lib)
	if err != nil {
		t.Fatal(err)
	}
	wantKey := filepath.Join(cwd, "S", "ch1.cbz")
	if len(fps) != 1 || fps[0].Path != leaf {
		t.Fatalf("fingerprints = %v, want the stored spelling %s", fps, leaf)
	}

	refs, err := loadSeries(context.Background(), pool, lib)
	if err != nil {
		t.Fatal(err)
	}
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
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

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
	rec, counts, err := r.recordCommit(false)
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
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	manifest := `{"pages": [["001.jpg", 4, 2]]}`
	r.place(withCover(withFileData(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"), manifest), "001.jpg"))
	if counts := r.commit(false); counts.Added != 1 {
		t.Fatalf("counts = %+v", counts)
	}

	seriesID := r.w.byURI["comic/S"]
	leafID := r.w.keys[Key{seriesID, "ch1"}]
	seriesManifest := `{"pages": [["cover.jpg", 2, 3]]}`
	exec(t, pool, "UPDATE content SET file_data = $2 WHERE id = $1", seriesID, seriesManifest)

	leafBefore, seriesBefore := readContent(t, pool, leafID), readContent(t, pool, seriesID)
	var stored map[string]any
	if err := json.Unmarshal(leafBefore.FileData, &stored); err != nil || stored["pages"] == nil {
		t.Fatalf("file_data = %s", leafBefore.FileData)
	}

	r.reload()
	r.place(Result{File: fsFile("/lib/S/ch1.cbz", baseTime, 10)})
	r.place(comicResult("/lib/S2/ch2.cbz", "ch2", "S", "/lib/S2"))
	r.commit(false)

	leafAfter, seriesAfter := readContent(t, pool, leafID), readContent(t, pool, seriesID)
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
	if deref(seriesAfter.FileURI) != "/lib/S2" {
		t.Fatalf("series file_uri = %v, want the patch to land", seriesAfter.FileURI)
	}
	if string(seriesAfter.FileData) != string(seriesBefore.FileData) {
		t.Fatalf("series file_data = %s, want %s", seriesAfter.FileData, seriesBefore.FileData)
	}
	if seriesAfter.URI != "comic/S" || seriesAfter.URIPart != "S" {
		t.Fatalf("series = %+v, want no rename", seriesAfter)
	}
}

func TestFlushReReducesSeriesAfterLaterMember(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"),
		models.Metadata{Series: "S", Publisher: "Beta", Language: "fr"}), 0, 2))
	r.commit(false)

	if got := readMeta(t, pool, lib, "comic/S").File.Raw; got.Publisher != "Beta" || got.Title != "S" {
		t.Fatalf("series metadata = %+v", got)
	}

	r.reload()
	r.place(withOrder(withMeta(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"),
		models.Metadata{Series: "S", Publisher: "Alpha"}), 0, 1))
	r.commit(false)

	got := readMeta(t, pool, lib, "comic/S").File.Raw
	if got.Publisher != "Alpha" {
		t.Fatalf("publisher = %q, want the earlier member to win", got.Publisher)
	}
	if got.Language != "fr" {
		t.Fatalf("language = %q, want the later member to still contribute", got.Language)
	}

	seriesID := r.w.byURI["comic/S"]
	kids, err := db.SelectScalars[string](context.Background(), pool,
		`SELECT uri_part FROM content WHERE parent_id = $1 ORDER BY "order"`, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(kids, []string{"ch1", "ch2"}) {
		t.Fatalf("ordering = %v", kids)
	}
}

func TestFlushCrossSeriesKeyReuse(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

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
	if got := contentURIs(t, pool, lib); !slices.Equal(got, []string{"comic/B", "comic/B/ch1"}) {
		t.Fatalf("uris = %v", got)
	}
	moved := readContent(t, pool, r.w.keys[Key{seriesB, "ch1"}])
	if deref(moved.FileURI) != "/lib/A/ch1.cbz" || deref(moved.ParentID) != seriesB {
		t.Fatalf("moved row = %+v", moved)
	}
}

func TestFlushStandaloneKeyReleasedBeforeSeriesInsert(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/X/ch1.cbz", "ch1", "X", "/lib/X"))
	r.place(comicResult("/lib/Foo.cbz", "Foo", "", ""))
	r.commit(false)

	if got := contentURIs(t, pool, lib); !slices.Equal(got, []string{"comic/Foo", "comic/X", "comic/X/ch1"}) {
		t.Fatalf("uris = %v", got)
	}

	r.reload()
	r.place(comicResult("/lib/Foo.cbz", "Foo", "X", "/lib/X"))
	r.place(comicResult("/lib/Foo/ch1.cbz", "ch1", "Foo", "/lib/Foo"))
	r.commit(false)

	want := []string{"comic/Foo", "comic/Foo/ch1", "comic/X", "comic/X/ch1", "comic/X/Foo"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	newSeries, err := db.SelectOne[models.Content](context.Background(), pool,
		"SELECT * FROM content WHERE library_id = $1 AND uri = 'comic/Foo'", lib)
	if err != nil {
		t.Fatal(err)
	}
	if newSeries.Type != "comic_series" {
		t.Fatalf("comic/Foo = %+v, want the new series", newSeries)
	}
}

func TestFlushRenameMovesAnnotationsWithChildKeyChange(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch9.cbz", "ch9", "S", "/lib/S"))
	r.commit(false)

	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a1', 'u1', $1, 'comic/S/ch1', true)", lib)
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a2', 'u1', $1, 'comic/S', true)", lib)
	seedMetadata(t, pool, lib, "comic/S/ch1", metaraw.MetadataRaw{
		Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "kept"}},
	})

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch2", "S_2019", "/lib/S"))
	r.commit(false)

	want := []string{"comic/S_2019", "comic/S_2019/ch2", "comic/S_2019/ch9"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}

	annotations, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM user_to_content WHERE library_id = $1 ORDER BY uri", lib)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(annotations, []string{"comic/S_2019", "comic/S_2019/ch1"}) {
		t.Fatalf("annotations = %v, want the rename pair at the old child part", annotations)
	}
	if got := readMeta(t, pool, lib, "comic/S_2019/ch1").Overrides; got == nil || got.Raw.Title != "kept" {
		t.Fatalf("override = %+v, want it to follow the rename", got)
	}
}

func TestFlushDeletesOrphansAndStampsScannedAt(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/T/ch1.cbz", "ch1", "T", "/lib/T"))
	r.commit(false)
	if at := libraryScannedAt(t, pool, lib); at != nil {
		t.Fatalf("scanned_at = %v, want nil before the final flush", at)
	}

	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/S"))
	counts := r.commit(true)

	if counts.Removed != 1 {
		t.Fatalf("counts = %+v", counts)
	}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, []string{"comic/T", "comic/T/ch1"}) {
		t.Fatalf("uris = %v, want the emptied series gone", got)
	}
	if at := libraryScannedAt(t, pool, lib); at == nil {
		t.Fatal("scanned_at was not stamped")
	}
}

func TestFlushNoOpFinalStillEmits(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	notify := &recorder{}
	w := newWriter(ScanInput{LibraryID: lib}, nil, notify, newResolver(), nil, nil)
	rig := newRig(t, w)
	go commitLoop(rig.ctx, pool, &ComicsScanner{}, lib, rig.flushes, rig.done)
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

func TestFlushRoundTripRenameKeepsMetadata(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch3.cbz", "ch3", "S", "/lib/S"))
	r.commit(false)

	layered := []string{"comic/S", "comic/S/ch1", "comic/S/ch3"}
	for _, uri := range layered {
		seedMetadata(t, pool, lib, uri, metaraw.MetadataRaw{
			Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "kept " + uri}},
		})
	}
	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a1', 'u1', $1, 'comic/S/ch3', true)", lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "T", "/lib/S"))
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))
	r.commit(false)

	want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2", "comic/S/ch3"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	for _, uri := range layered {
		if got := readMeta(t, pool, lib, uri).Overrides; got == nil || got.Raw.Title != "kept "+uri {
			t.Fatalf("override at %s = %+v, want it preserved", uri, got)
		}
	}
	annotations, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM user_to_content WHERE library_id = $1 ORDER BY uri", lib)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(annotations, []string{"comic/S/ch3"}) {
		t.Fatalf("annotations = %v", annotations)
	}
}

func TestFlushRoundTripThenRenameStillMoves(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	for _, part := range []string{"ch1", "ch2", "ch3"} {
		r.place(comicResult("/lib/S/"+part+".cbz", part, "S", "/lib/S"))
	}
	r.commit(false)

	for _, uri := range []string{"comic/S", "comic/S/ch1", "comic/S/ch3"} {
		seedMetadata(t, pool, lib, uri, metaraw.MetadataRaw{
			Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "kept " + uri}},
		})
	}
	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a1', 'u1', $1, 'comic/S/ch3', true)", lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "T", "/lib/S"))
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch3.cbz", "ch3", "U", "/lib/S"))
	r.commit(false)

	want := []string{"comic/U", "comic/U/ch1", "comic/U/ch2", "comic/U/ch3"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	for _, pair := range [][2]string{{"comic/U", "comic/S"}, {"comic/U/ch1", "comic/S/ch1"}, {"comic/U/ch3", "comic/S/ch3"}} {
		if got := readMeta(t, pool, lib, pair[0]).Overrides; got == nil || got.Raw.Title != "kept "+pair[1] {
			t.Fatalf("override at %s = %+v, want the one from %s", pair[0], got, pair[1])
		}
	}
	annotations, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM user_to_content WHERE library_id = $1 ORDER BY uri", lib)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(annotations, []string{"comic/U/ch3"}) {
		t.Fatalf("annotations = %v, want the chain to land at the final name", annotations)
	}
}

func TestFlushIgnoresSelfRenameFromAnyProducer(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)
	for _, uri := range []string{"comic/S", "comic/S/ch1"} {
		seedMetadata(t, pool, lib, uri, metaraw.MetadataRaw{
			Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "kept " + uri}},
		})
	}

	r.reload()
	set := r.w.set(r.w.byURI["comic/S"])
	set.OldURI = set.Ref.URI
	r.commit(false)

	for _, uri := range []string{"comic/S", "comic/S/ch1"} {
		if got := readMeta(t, pool, lib, uri).Overrides; got == nil || got.Raw.Title != "kept "+uri {
			t.Fatalf("override at %s = %+v, want it preserved", uri, got)
		}
	}
}

func TestFlushRenameChainAcrossSeries(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "A", "/lib/A"))
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "B", "/lib/B"))
	r.commit(false)

	seeded := []string{"comic/A", "comic/A/ch1", "comic/B", "comic/B/ch1"}
	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	for i, uri := range seeded {
		seedMetadata(t, pool, lib, uri, metaraw.MetadataRaw{
			Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: uri}},
		})
		exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ($1, 'u1', $2, $3, true)",
			"a"+string(rune('1'+i)), lib, uri)
	}

	r.reload()
	r.place(comicResult("/lib/B/ch1.cbz", "ch1", "C", "/lib/B"))
	r.place(comicResult("/lib/A/ch1.cbz", "ch1", "B", "/lib/A"))
	r.commit(false)

	want := []string{"comic/B", "comic/B/ch1", "comic/C", "comic/C/ch1"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	moved := map[string]string{
		"comic/B": "comic/A", "comic/B/ch1": "comic/A/ch1",
		"comic/C": "comic/B", "comic/C/ch1": "comic/B/ch1",
	}
	for uri, from := range moved {
		if got := readMeta(t, pool, lib, uri).Overrides; got == nil || got.Raw.Title != from {
			t.Fatalf("metadata at %s = %+v, want the layer from %s", uri, got, from)
		}
	}
	annotations, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM user_to_content WHERE library_id = $1 ORDER BY uri", lib)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(annotations, want) {
		t.Fatalf("annotations = %v, want %v", annotations, want)
	}
}

func TestFlushCustomListAnnotationCollision(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)

	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ('cl1', 'list', 'private', 'u1')")
	exec(t, pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ('cl2', 'other', 'private', 'u1')")
	exec(t, pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('src', 'cl1', $1, 'comic/S/ch1', 'source')", lib)
	exec(t, pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('dst', 'cl1', $1, 'comic/S_2019/ch1', 'destination')", lib)
	exec(t, pool, "INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes) VALUES ('free', 'cl2', $1, 'comic/S/ch1', 'free')", lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	r.commit(false)

	rows, err := db.Select[models.CustomListToContent](context.Background(), pool,
		"SELECT * FROM custom_list_to_content WHERE library_id = $1 ORDER BY id", lib)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, row := range rows {
		got[row.ID] = row.URI
	}
	want := map[string]string{"dst": "comic/S_2019/ch1", "free": "comic/S_2019/ch1", "src": "comic/S/ch1"}
	if !maps.Equal(got, want) {
		t.Fatalf("list entries = %v, want %v", got, want)
	}
}

func TestFlushIntermediateReductionExcludesGone(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(withOrder(withMeta(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"),
		models.Metadata{Series: "S", Publisher: "Alpha"}), 0, 1))
	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"),
		models.Metadata{Publisher: "Beta", Language: "fr"}), 0, 2))
	r.commit(false)
	if got := readMeta(t, pool, lib, "comic/S").File.Raw; got.Publisher != "Alpha" {
		t.Fatalf("series metadata = %+v", got)
	}

	seriesID := r.w.byURI["comic/S"]
	goneID := r.w.keys[Key{seriesID, "ch1"}]

	r.reload()
	r.w.event(listedEvent(t, r.w, "/lib/S", "ch2.cbz"))
	if !r.w.gone[goneID] {
		t.Fatalf("gone = %v, want the missing leaf", r.w.gone)
	}
	r.place(withOrder(withMeta(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"),
		models.Metadata{Publisher: "Beta", Language: "fr"}), 0, 2))
	r.commit(false)

	if got := readMeta(t, pool, lib, "comic/S").File.Raw; got.Publisher != "Beta" {
		t.Fatalf("publisher = %q, want the proven-missing child excluded", got.Publisher)
	}
	if row := readContent(t, pool, goneID); row.URI != "comic/S/ch1" {
		t.Fatalf("proven-missing row = %+v, want it kept until the final flush", row)
	}
	kept := readContent(t, pool, r.w.keys[Key{seriesID, "ch2"}])
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
	if _, err := r.tryCommit(true); err == nil {
		t.Fatal("expected the final flush to fail")
	}

	if at := libraryScannedAt(t, pool, lib); at != nil {
		t.Fatalf("scanned_at = %v, want the failed final flush rolled back", at)
	}
	if got := contentURIs(t, pool, lib); len(got) != 0 {
		t.Fatalf("uris = %v, want the whole flush rolled back", got)
	}
}

func TestFlushSharedSeriesAcrossDirectories(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S2/ch2.cbz", "ch2", "S", "/lib/S2"))
	r.commit(false)

	want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	seriesID := r.w.byURI["comic/S"]
	for _, part := range []string{"ch1", "ch2"} {
		if row := readContent(t, pool, r.w.keys[Key{seriesID, part}]); deref(row.ParentID) != seriesID {
			t.Fatalf("%s parent = %v, want the shared series", part, row.ParentID)
		}
	}

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	counts := r.commit(false)
	if counts.Added != 0 || counts.Updated != 1 {
		t.Fatalf("counts = %+v, want the shared series reused", counts)
	}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	if deref(readContent(t, pool, seriesID).FileURI) != "/lib/S" {
		t.Fatalf("series file_uri = %v, want the latest directory", readContent(t, pool, seriesID).FileURI)
	}
}

func TestFlushKeepsSeriesRenameWhenLeafConflicts(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r := newScanRun(t, pool, lib, &ComicsScanner{})

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
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want the rename to survive the leaf conflict", got)
	}
}
