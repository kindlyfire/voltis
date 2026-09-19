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

	"voltis/models"
	"voltis/scanner"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestLibraryCRUD(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Create
	lib := c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": dir1}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(lib["name"]), "test")
	assertEq(t, s(lib["type"]), "comics")
	id := s(lib["id"])

	// List
	libs := c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 1)
	assertEq(t, s(libs[0]["id"]), id)

	// Update
	updated := c.Post("/api/libraries/"+id, map[string]any{
		"name": "test2", "type": "books",
		"sources": []map[string]any{{"path_uri": dir2}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(updated["name"]), "test2")
	assertEq(t, s(updated["type"]), "comics") // type can't change

	// Delete
	c.Delete("/api/libraries/"+id).Assert(t, 200)

	// Verify gone
	libs = c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 0)
}

func TestLibraryValidation(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	// Invalid source path
	c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": "/nonexistent/path"}},
	}).Assert(t, 400)

	// Delete nonexistent
	c.Delete("/api/libraries/nonexistent").Assert(t, 404)
}

func writeEPUB(t *testing.T, path, title, series string) {
	t.Helper()
	seriesMeta := ""
	if series != "" {
		seriesMeta = `<meta name="calibre:series" content="` + series + `"/>` +
			`<meta name="calibre:series_index" content="1"/>`
	}
	buf := &bytes.Buffer{}
	zw := zip.NewWriter(buf)
	entries := map[string]string{
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
	}
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
		t.Fatalf("write epub: %v", err)
	}
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

	lib := c.Post("/api/libraries/new", map[string]any{
		"name": "books", "type": "books",
		"sources": []map[string]any{{"path_uri": dir}},
	}).Assert(t, 200).JSON()
	libID := s(lib["id"])

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
	if (out != scanner.ScanResult{Updated: 1, Duration: out.Duration}) {
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
