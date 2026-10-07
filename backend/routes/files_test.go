package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"voltis/metadata"
	"voltis/models"
	"voltis/providers/providertest"

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

func TestProviderCover(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestSeries(t, c)
	base := "/api/metadata/content/" + id
	img := testJPEG(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing.jpg" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(img)
	}))
	t.Cleanup(upstream.Close)
	serve := func(path string) {
		c.fake.Put("1", providertest.Payload{Fields: metadata.Fields{Title: metadata.Val("Remote"),
			Kind: metadata.Val(metadata.Manga)}, CoverURL: upstream.URL + path})
	}
	version := func() string {
		t.Helper()
		v := c.Get("/api/content/"+id).Assert(t, 200).JSON()["cover_version"]
		if v == nil {
			t.Fatal("no cover version")
		}
		return s(v)
	}
	getCoverAt := func(v string, want int) (cacheControl string) {
		t.Helper()
		return c.Get("/api/files/cover/"+id+"?v="+v).Assert(t, want).Headers.Get("Cache-Control")
	}
	getCover := func(want int) string {
		t.Helper()
		return getCoverAt(version(), want)
	}
	immutable := "public, max-age=31536000, immutable"

	// Without a local cover, only a provider one.
	serve("/cover.jpg")
	c.Post(base+"/link", map[string]any{"provider": "fake", "external_id": "1"}).Assert(t, 200)
	assertEq(t, getCover(200), immutable)
	assertEq(t, c.Get("/api/files/cover/"+id).Assert(t, 200).Headers.Get("Cache-Control"), "no-cache")
	providerVersion := version()

	list := s(c.Post("/api/custom-lists", map[string]any{"name": "l", "visibility": "private"}).Assert(t, 200).JSON()["id"])
	mustExec(t, pool, `INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri)
		SELECT 'e1', $1, library_id, uri FROM content WHERE id = $2`, list, id)
	covers := c.Get("/api/custom-lists?user=me").Assert(t, 200).JSONArray()[0]["covers"].([]any)
	if len(covers) != 1 || covers[0].(map[string]any)["cover_version"] == nil {
		t.Fatalf("list covers = %v", covers)
	}

	// A failed download falls back, uncached, to the local cover or a 404.
	serve("/missing.jpg")
	c.Post(base+"/refresh", map[string]any{"provider": "fake"}).Assert(t, 200)
	assertEq(t, getCover(404), "no-store")
	cbz := testCBZ(t, t.TempDir(), map[string][]byte{"01.jpg": img})
	mustExec(t, pool, "UPDATE content SET cover_uri = $2, file_mtime = now() WHERE id = $1", id, cbz+"/01.jpg")
	assertEq(t, getCover(200), "no-store")

	// Clearing the cover restores the local one, which the provider cover's URL must not pin.
	c.Post(base+"/overrides", map[string]any{"rev": 0, "fields": map[string]any{"cover": nil}}).Assert(t, 200)
	assertEq(t, getCover(200), immutable)
	assertEq(t, getCoverAt(providerVersion, 200), "no-cache")
}

func TestBookFilesCacheOnlyWhenVersioned(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestBook(t, pool, testEPUB(t, t.TempDir()), "{}")

	for _, path := range []string{
		"/api/files/book-chapter/" + id + "?href=OEBPS/text/ch1.xhtml",
		"/api/files/book-resource/" + id + "?path=" + url.QueryEscape("OEBPS/images/pic a.jpg"),
	} {
		assertEq(t, c.Get(path).Assert(t, 200).Headers.Get("Cache-Control"), "")
		assertEq(t, c.Get(path+"&v=1").Assert(t, 200).Headers.Get("Cache-Control"),
			"private, max-age=31536000, immutable")
	}
}

func TestComicPages(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	img := testJPEG(t)
	id := newTestContent(t, pool)
	mustExec(t, pool, `UPDATE content SET file_uri = $2, file_data = '{"pages": [["01.jpg"], ["02.jpg"]]}' WHERE id = $1`,
		id, testCBZ(t, t.TempDir(), map[string][]byte{"01.jpg": []byte("other"), "02.jpg": img}))

	if body := c.Get("/api/files/comic-page/"+id+"/1").Assert(t, 200).Body; !bytes.Equal(body, img) {
		t.Errorf("page 1 returned %d bytes, want the second entry", len(body))
	}
	c.Get("/api/files/comic-page/"+id+"/2").Assert(t, 404)

	offline := "/api/files/offline/" + id
	assertFrames := func(got []offlineFrame, want ...offlineFrame) {
		t.Helper()
		if !slices.EqualFunc(got, want, func(a, b offlineFrame) bool { return a.index == b.index && bytes.Equal(a.data, b.data) }) {
			t.Fatalf("frames = %v, want %v", got, want)
		}
	}
	end := offlineFrame{offlineEndIndex, []byte{}}

	manifest, frames := readFrames(t, c.Get(offline).Assert(t, 200).Body)
	assertEq(t, manifest["page_count"], any(2.0))
	assertEq(t, manifest["pages"].([]any)[0].(map[string]any)["width"], nil)
	assertFrames(frames, offlineFrame{0, []byte("other")}, offlineFrame{1, img}, end)

	_, frames = readFrames(t, c.Get(offline+"?from=1").Assert(t, 200).Body)
	assertFrames(frames, offlineFrame{1, img}, end)
	c.Get(offline+"?from=3").Assert(t, 400)

	// A page missing from the archive ends the stream with an error frame after the pages before it.
	mustExec(t, pool, `UPDATE content SET file_data = '{"pages": [["01.jpg"], ["02.jpg"], ["03.jpg"]]}' WHERE id = $1`, id)
	_, frames = readFrames(t, c.Get(offline).Assert(t, 200).Body)
	assertFrames(frames, offlineFrame{0, []byte("other")}, offlineFrame{1, img},
		offlineFrame{offlineErrorIndex, []byte("page 2: file not found in archive")})
}

type offlineFrame struct {
	index uint32
	data  []byte
}

// readFrames parses an offline stream into its manifest and the frames after it.
func readFrames(t *testing.T, body []byte) (map[string]any, []offlineFrame) {
	t.Helper()
	r := bytes.NewReader(body)
	u32 := func() uint32 {
		var v uint32
		if err := binary.Read(r, binary.BigEndian, &v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	bytesOf := func(n uint32) []byte {
		b := make([]byte, n)
		if _, err := io.ReadFull(r, b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	var manifest map[string]any
	if err := json.Unmarshal(bytesOf(u32()), &manifest); err != nil {
		t.Fatal(err)
	}
	var frames []offlineFrame
	for r.Len() > 0 {
		index := u32()
		frames = append(frames, offlineFrame{index, bytesOf(u32())})
	}
	return manifest, frames
}

func TestDownloadInfoAbove2GiB(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	seriesID := newTestContent(t, pool)
	mustExec(t, pool, `UPDATE content SET type = 'comic_series' WHERE id = $1`, seriesID)

	var childID string
	for i, size := range []int64{3_000_000_000, 1} {
		id := models.MakeContentID()
		mustExec(t, pool, `
			INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, file_uri, file_size)
			SELECT $1, $1, 'file:///lib/' || $1, 'comic', library_id, id, '/lib/' || $1 || '.cbz', $3
			FROM content WHERE id = $2
		`, id, seriesID, size)
		if i == 0 {
			childID = id
		}
	}

	info := c.Get("/api/files/download-info/"+seriesID).Assert(t, 200).JSON()
	assertEq(t, info["file_count"], any(2.0))
	assertEq(t, info["total_size"], any(3_000_000_001.0))
	assertEq(t, c.Get("/api/files/download-info/"+childID).Assert(t, 200).JSON()["total_size"], any(3_000_000_000.0))
}
