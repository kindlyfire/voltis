package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testCBZ(t *testing.T, dir string, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(dir, "comic.cbz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadArchiveEntry(t *testing.T) {
	root := t.TempDir()
	sentinel := []byte("SECRET-HOST-FILE")
	if err := os.WriteFile(filepath.Join(root, "secret.jpg"), sentinel, 0o644); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(root, "lib", "series")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	page := testJPEG(t)
	archivePath := testCBZ(t, dir, map[string][]byte{"01.jpg": page})

	escape := "../../../secret.jpg"
	if filepath.Join(archivePath, escape) != filepath.Join(root, "secret.jpg") {
		t.Fatal("escape name does not resolve onto the sentinel")
	}

	data, mediaType, err := readArchiveEntry(archivePath, "01.jpg")
	if err != nil {
		t.Fatalf("legitimate page: %v", err)
	}
	if !bytes.Equal(data, page) {
		t.Fatal("legitimate page returned wrong bytes")
	}
	if mediaType != "image/jpeg" {
		t.Fatalf("media type = %q, want image/jpeg", mediaType)
	}

	for _, name := range []string{escape, "01/../" + escape, filepath.Join(root, "secret.jpg")} {
		data, _, err := readArchiveEntry(archivePath, name)
		if bytes.Equal(data, sentinel) {
			t.Fatalf("%q leaked host file", name)
		}
		he, ok := err.(*echo.HTTPError)
		if !ok || he.Code != http.StatusNotFound {
			t.Fatalf("%q: expected 404, got %v", name, err)
		}
	}
}

func testEPUB(t *testing.T, dir string) string {
	t.Helper()
	opf := `<?xml version="1.0"?><package><metadata/><manifest>
		<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
		<item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
	</manifest><spine><itemref idref="c1"/></spine></package>`
	nav := `<!DOCTYPE html><html xmlns:epub="http://www.idpf.org/2007/ops"><body>
		<nav epub:type="toc"><ol><li><a href="text/ch1.xhtml#start">One</a></li></ol></nav></body></html>`
	path := filepath.Join(dir, "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	entries := map[string][]byte{
		"META-INF/container.xml": []byte(`<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`),
		"OEBPS/content.opf":      []byte(opf),
		"OEBPS/nav.xhtml":        []byte(nav),
		"OEBPS/text/ch1.xhtml":   []byte(`<!DOCTYPE html><html><body><p id="start">One</p></body></html>`),
	}
	for name, body := range resourceEntries {
		entries[name] = body
	}
	for name, data := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

var resourceEntries = map[string][]byte{
	"OEBPS/images/pic a.jpg":   []byte("SPACE"),
	"OEBPS/images/pic%20a.jpg": []byte("PERCENT"),
	"OEBPS/images/pic#b.jpg":   []byte("HASH"),
	"OEBPS/images/pic?c.jpg":   []byte("QUERY"),
}

func newTestBook(t *testing.T, pool *pgxpool.Pool, fileURI, fileData string) string {
	t.Helper()
	ctx := context.Background()

	libID := models.MakeLibraryID()
	if _, err := pool.Exec(ctx,
		"INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'books')", libID); err != nil {
		t.Fatal(err)
	}
	contentID := models.MakeContentID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO content (id, uri_part, uri, type, library_id, file_uri, file_data)
		VALUES ($1, $1, 'file:///lib/' || $1, 'book', $2, $3, $4)
	`, contentID, libID, fileURI, fileData); err != nil {
		t.Fatal(err)
	}
	return contentID
}

func TestGetBookChapters(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	path := testEPUB(t, t.TempDir())
	id := newTestBook(t, pool, path, `{"words":{"OEBPS/text/ch1.xhtml":42}}`)

	body := c.Get("/api/files/book-chapters/"+id).Assert(t, 200).Body
	want := `{"spine":[{"href":"OEBPS/text/ch1.xhtml","title":"One","linear":true,"words":42}],` +
		`"toc":[{"id":"0","title":"One","depth":0,"href":"OEBPS/text/ch1.xhtml","fragment":"start"}]}` + "\n"
	if string(body) != want {
		t.Errorf("got  %s\nwant %s", body, want)
	}
}

func TestGetBookResource(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "secret.jpg"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := newTestBook(t, pool, testEPUB(t, dir), "{}")

	for name, want := range resourceEntries {
		resp := c.Get("/api/files/book-resource/"+id+"?path="+url.QueryEscape(name)).Assert(t, 200)
		if !bytes.Equal(resp.Body, want) {
			t.Errorf("%q returned %q, want %q", name, resp.Body, want)
		}
	}

	for _, p := range []string{"../secret.jpg", "OEBPS/../../secret.jpg", "/etc/passwd", "", "OEBPS/images/", "OEBPS/images/.."} {
		c.Get("/api/files/book-resource/"+id+"?path="+url.QueryEscape(p)).Assert(t, 400)
	}
	c.Get("/api/files/book-resource/"+id+"?path=OEBPS/missing.jpg").Assert(t, 404)
}
