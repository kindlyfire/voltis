package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"voltis/db"
	"voltis/metadata"
	"voltis/models"
	"voltis/scanner"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLibraryCRUD(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	lib := c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": dir1}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(lib["name"]), "test")
	assertEq(t, s(lib["type"]), "comics")
	id := s(lib["id"])

	libs := c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 1)
	assertEq(t, s(libs[0]["id"]), id)

	updated := c.Post("/api/libraries/"+id, map[string]any{
		"name": "test2", "type": "books",
		"sources": []map[string]any{{"path_uri": dir2}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(updated["name"]), "test2")
	assertEq(t, s(updated["type"]), "comics")

	c.Delete("/api/libraries/"+id).Assert(t, 200)

	libs = c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 0)
}

func TestLibraryValidation(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": "/nonexistent/path"}},
	}).Assert(t, 400)

	c.Delete("/api/libraries/nonexistent").Assert(t, 404)
}

func TestLibrarySettings(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := t.TempDir()
	body := func(settings any) map[string]any {
		b := map[string]any{"name": "books", "type": "books", "sources": []map[string]any{{"path_uri": dir}}}
		if settings != nil {
			b["settings"] = settings
		}
		return b
	}
	settings := func(lib map[string]any) string { return asJSON(t, lib["settings"]) }

	lib := c.Post("/api/libraries/new", body(nil)).Assert(t, 200).JSON()
	assertEq(t, settings(lib), `{"auto_match":{},"book_series_inference":"conservative"}`)
	assertEq(t, asJSON(t, lib["sources"]), `[{"path_uri":"`+dir+`","settings":{}}]`)
	id := s(lib["id"])

	// Keys of providers not registered are kept.
	off := map[string]any{"book_series_inference": "off", "auto_match": map[string]any{"fake": true, "gone": false}}
	want := `{"auto_match":{"fake":true,"gone":false},"book_series_inference":"off"}`
	assertEq(t, settings(c.Post("/api/libraries/"+id, body(off)).Assert(t, 200).JSON()), want)
	assertEq(t, settings(c.Post("/api/libraries/"+id, body(nil)).Assert(t, 200).JSON()), want)
	assertEq(t, settings(c.Get("/api/libraries").Assert(t, 200).JSONArray()[0]), want)

	c.Post("/api/libraries/"+id, body(map[string]any{"book_series_inference": "aggressive"})).Assert(t, 400)
	for _, bad := range []any{"yes", true, map[string]any{"x": 1}, map[string]any{"x": nil}} {
		c.Post("/api/libraries/"+id, body(map[string]any{"auto_match": bad})).Assert(t, 400)
		c.Post("/api/libraries/"+id, map[string]any{"name": "books", "sources": []map[string]any{
			{"path_uri": dir, "settings": map[string]any{"auto_match": bad}}}}).Assert(t, 400)
	}
	c.Post("/api/libraries/"+id, map[string]any{"name": "books", "sources": []map[string]any{
		{"path_uri": dir}, {"path_uri": dir + "/"}}}).Assert(t, 400)
	// Omitted keys take their defaults: matching is off unless asked for.
	partial := c.Post("/api/libraries/"+id, body(map[string]any{"book_series_inference": "off"})).Assert(t, 200).JSON()
	assertEq(t, settings(partial), `{"auto_match":{},"book_series_inference":"off"}`)
	for _, sources := range []any{nil, []any{}} {
		lib := c.Post("/api/libraries/"+id, map[string]any{"name": "books", "sources": sources}).Assert(t, 200).JSON()
		assertEq(t, asJSON(t, lib["sources"]), `[]`)
	}
	stored, err := db.SelectScalar[string](context.Background(), pool, "SELECT sources::text FROM libraries WHERE id = $1", id)
	if err != nil || stored != "[]" {
		t.Fatalf("stored sources = %s (%v)", stored, err)
	}
	src := map[string]any{"path_uri": dir, "settings": map[string]any{"auto_match": map[string]any{"fake": false}}}
	lib = c.Post("/api/libraries/"+id, map[string]any{"name": "books", "sources": []any{src}}).Assert(t, 200).JSON()
	assertEq(t, asJSON(t, lib["sources"]), `[{"path_uri":"`+dir+`","settings":{"auto_match":{"fake":false}}}]`)
}

// Coverage that grows makes the series it covers due; coverage that shrinks leaves them alone.
func TestLibraryAutoMatchChanges(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	if err := c.st.Set(context.Background(), settings.MetadataMatchingPaused, true); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a")
	if err := os.Mkdir(a, 0o755); err != nil {
		t.Fatal(err)
	}
	id := s(c.Post("/api/libraries/new", map[string]any{"name": "c", "type": "comics"}).Assert(t, 200).JSON()["id"])
	// s is under the source, v under the narrower one, u under neither.
	for name, file := range map[string]string{"s": filepath.Join(dir, "b", "s.cbz"), "u": "/elsewhere/u.cbz", "v": filepath.Join(a, "v.cbz")} {
		insertSeries(t, pool, id, name, file)
		mustExec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
			VALUES ($1, 'comic/' || $2, 'fake', 'unmatched', now() + interval '1 day')`, id, name)
	}
	due := func() string {
		t.Helper()
		autoMatching.Wait()
		due, err := db.SelectScalar[string](context.Background(), pool, `SELECT coalesce(string_agg(uri, ' ' ORDER BY uri), '')
			FROM metadata_links WHERE retry_at <= now()`)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, pool, "UPDATE metadata_links SET retry_at = now() + interval '1 day'")
		return due
	}
	save := func(library bool, sources ...any) {
		t.Helper()
		c.Post("/api/libraries/"+id, map[string]any{"name": "c", "sources": sources,
			"settings": map[string]any{"auto_match": map[string]any{"fake": library}}}).Assert(t, 200)
	}
	source := func(path string, on ...bool) map[string]any {
		out := map[string]any{"path_uri": path}
		if len(on) > 0 {
			out["settings"] = map[string]any{"auto_match": map[string]any{"fake": on[0]}}
		}
		return out
	}

	for _, step := range []struct {
		name    string
		library bool
		sources []any
		due     string
	}{
		{"a source on", false, []any{source(dir, true)}, "comic/s comic/v"},
		{"the library on too", true, []any{source(dir)}, "comic/s comic/u comic/v"},
		{"the source off", true, []any{source(dir, false)}, ""},
		{"a narrower source off", true, []any{source(a, false)}, "comic/s comic/u"},
		{"widening the off source", true, []any{source(dir, false)}, ""},
		{"a source on in a library off", false, []any{source(dir, true)}, "comic/s comic/v"},
		{"narrowing the on source", false, []any{source(a, true)}, ""},
		{"everything off", false, nil, ""},
	} {
		save(step.library, step.sources...)
		if got := due(); got != step.due {
			t.Fatalf("%s: due %q, want %q", step.name, got, step.due)
		}
	}
}

// Coverage that grows wakes the worker, for series never matched, and in libraries just created.
func TestLibraryAutoMatchWakesTheWorker(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := t.TempDir()
	idle := func() {
		waitUntil(t, "the worker to idle", func() bool {
			return c.Get("/api/metadata/summary").Assert(t, 200).JSON()["worker"].(map[string]any)["activity"] == "idle"
		})
	}
	matched := func(id, uri string) {
		waitUntil(t, "a match of "+uri, func() bool {
			n, err := db.SelectScalar[int](context.Background(), pool,
				"SELECT count(*) FROM metadata_links WHERE library_id = $1 AND uri = $2", id, uri)
			return err == nil && n == 1
		})
	}
	idle()
	id := s(c.Post("/api/libraries/new", map[string]any{"name": "c", "type": "comics"}).Assert(t, 200).JSON()["id"])
	insertSeries(t, pool, id, "s", filepath.Join(dir, "s", "1.cbz"))
	c.Post("/api/libraries/"+id, map[string]any{"name": "c", "sources": []any{map[string]any{"path_uri": dir,
		"settings": map[string]any{"auto_match": map[string]any{"fake": true}}}}}).Assert(t, 200)
	matched(id, "comic/s")

	idle()
	insertSeries(t, pool, id, "t", filepath.Join(dir, "t", "1.cbz"))
	c.Post("/api/libraries/new", map[string]any{"name": "d", "type": "comics",
		"settings": map[string]any{"auto_match": map[string]any{"fake": true}}}).Assert(t, 200)
	matched(id, "comic/t")
}

func insertSeries(t *testing.T, pool *pgxpool.Pool, lib, id, fileURI string) {
	t.Helper()
	mustExec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id)
		VALUES ($1, $1, 'comic/' || $1, 'comic_series', $2)`, id, lib)
	mustExec(t, pool, `INSERT INTO content (id, uri_part, uri, file_uri, type, parent_id, library_id)
		VALUES ($1 || '1', '1', 'comic/' || $1 || '/1', $3, 'comic', $1, $2)`, id, lib, fileURI)
	mustExec(t, pool, `INSERT INTO content_metadata (uri, library_id, data_raw, data, data_version)
		VALUES ('comic/' || $1, $2, '{"v": 2, "file": {"title": "Local"}}', '{"title": "Local"}', $3)`,
		id, lib, metadata.DataVersion)
}

func asJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestScanFollowsTheBookSeriesInferenceSetting(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dir := t.TempDir()
	writeEPUB(t, filepath.Join(dir, "Foo v1.epub"), "Foo Vol. 1", "")
	writeEPUB(t, filepath.Join(dir, "Foo v2.epub"), "Foo Vol. 2", "")
	libID := libraryAt(t, c, "books", dir)

	runScans(t, pool, c, map[string]any{"ids": []string{libID}})
	assertContentURIs(t, pool, libID, []string{"book/Foo", "book/Foo/Foo v1", "book/Foo/Foo v2"})

	c.Post("/api/libraries/"+libID, map[string]any{
		"name": "books", "type": "books", "sources": []map[string]any{{"path_uri": dir}},
		"settings": map[string]any{"book_series_inference": "off"},
	}).Assert(t, 200)
	runScans(t, pool, c, map[string]any{"ids": []string{libID}})
	assertContentURIs(t, pool, libID, []string{"book/Foo", "book/Foo/Foo v1", "book/Foo/Foo v2"})
	runScans(t, pool, c, map[string]any{"ids": []string{libID}, "force": true})
	assertContentURIs(t, pool, libID, []string{"book/Foo v1", "book/Foo v2"})
}

func writeZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	for _, name := range slices.Sorted(maps.Keys(entries)) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create: %v", err)
		}
		if _, err := w.Write([]byte(entries[name])); err != nil {
			t.Fatalf("zip write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}
}

func writeEPUB(t *testing.T, path, title, series string) {
	t.Helper()
	seriesMeta := ""
	if series != "" {
		seriesMeta = `<meta name="calibre:series" content="` + series + `"/>` +
			`<meta name="calibre:series_index" content="1"/>`
	}
	writeZip(t, path, map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?><container><rootfiles>` +
			`<rootfile full-path="content.opf"/></rootfiles></container>`,
		"content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:language>en</dc:language>
    ` + seriesMeta + `
  </metadata>
  <manifest/>
</package>`,
	})
}

func runScans(t *testing.T, pool *pgxpool.Pool, c *testClient, body map[string]any) []string {
	t.Helper()
	res := c.Post("/api/libraries/scan", body).Assert(t, 200).JSON()
	raw := res["task_ids"].([]any)
	ids := make([]string, len(raw))
	for i, v := range raw {
		ids[i] = s(v)
		waitUntil(t, "scan "+ids[i]+" to finish", func() bool {
			var status int
			err := pool.QueryRow(context.Background(), "SELECT status FROM tasks WHERE id = $1", ids[i]).Scan(&status)
			return err == nil && status >= models.TaskStatusCompleted
		})
	}
	return ids
}

func scanOutcome(t *testing.T, pool *pgxpool.Pool, id string) (int, scanner.ScanResult) {
	t.Helper()
	var status int
	var output []byte
	if err := pool.QueryRow(context.Background(),
		"SELECT status, output FROM tasks WHERE id = $1", id).Scan(&status, &output); err != nil {
		t.Fatalf("read task %s: %v", id, err)
	}
	var result scanner.ScanResult
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode task output %s: %v", output, err)
	}
	return status, result
}

func TestScanningABookSeriesLeavesUnrelatedBooksAlone(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dir := t.TempDir()
	solo := filepath.Join(dir, "Solo.epub")
	writeEPUB(t, filepath.Join(dir, "Bar v1.epub"), "Bar Volume 1", "Bar")
	writeEPUB(t, solo, "Solo", "")

	libID := libraryAt(t, c, "books", dir)

	runScans(t, pool, c, map[string]any{"ids": []string{libID}})
	runScans(t, pool, c, map[string]any{"ids": []string{libID}, "force": true})

	ctx := context.Background()
	var seriesID string
	var seriesFileURI *string
	err := pool.QueryRow(ctx, "SELECT id, file_uri FROM content WHERE library_id = $1 AND uri = 'book/Bar'",
		libID).Scan(&seriesID, &seriesFileURI)
	if err != nil {
		t.Fatalf("read series: %v", err)
	}
	if seriesFileURI != nil {
		t.Errorf("book series file_uri = %q, want books to stay directoryless", *seriesFileURI)
	}

	if err := os.WriteFile(solo, []byte("not an epub"), 0o644); err != nil {
		t.Fatalf("corrupt solo: %v", err)
	}
	ids := runScans(t, pool, c, map[string]any{"content_ids": []string{seriesID}})
	if len(ids) != 1 {
		t.Fatalf("task_ids = %v, want one scan queued for the selected series", ids)
	}
	status, out := scanOutcome(t, pool, ids[0])
	if status != models.TaskStatusCompleted {
		t.Fatalf("selected scan status = %d, want it completed", status)
	}
	if (out != scanner.ScanResult{Counts: scanner.Counts{Updated: 1}, Duration: out.Duration}) {
		t.Fatalf("selected scan = %+v, want exactly the selected member reparsed", out)
	}

	var valid bool
	if err := pool.QueryRow(ctx, "SELECT valid FROM content WHERE library_id = $1 AND uri = 'book/Solo'",
		libID).Scan(&valid); err != nil {
		t.Fatalf("read solo: %v", err)
	}
	if !valid {
		t.Fatal("scanning the series reparsed an unrelated book in the same directory")
	}
}
