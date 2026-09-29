package covers

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"voltis/db/dbtest"
	"voltis/metadata"
)

func testJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 2)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// upstream serves body and counts requests.
func upstream(t *testing.T, body []byte) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func files(t *testing.T, c *Cache) []string {
	t.Helper()
	entries, _ := os.ReadDir(c.providerDir())
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestNewURLDownloadsANewFile(t *testing.T) {
	srv, hits := upstream(t, testJPEG(t))
	c := New(t.TempDir())
	for _, path := range []string{"/a", "/a", "/b"} {
		if _, err := c.Provider(metadata.CoverRef{URL: srv.URL + path}); err != nil {
			t.Fatal(err)
		}
	}
	if n, got := hits.Load(), files(t, c); n != 2 || len(got) != 2 {
		t.Fatalf("downloads = %d, files = %v", n, got)
	}
	a, b := metadata.CoverRef{URL: srv.URL + "/a"}, metadata.CoverRef{URL: srv.URL + "/b"}
	if *Version(&a, false, nil) == *Version(&b, false, nil) {
		t.Fatal("a new URL kept the cover version")
	}
}

func TestFailedDownloadIsNotCachedNorRetriedAtOnce(t *testing.T) {
	c := New(t.TempDir())
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="2"/>`)
	for _, body := range [][]byte{svg, []byte("<html>not an image</html>"), bytes.Repeat([]byte{0}, maxDownload+1)} {
		srv, hits := upstream(t, body)
		ref := metadata.CoverRef{URL: srv.URL}
		for range 2 {
			if _, err := c.Provider(ref); err == nil {
				t.Fatal("Provider succeeded")
			}
		}
		if n := hits.Load(); n != 1 {
			t.Fatalf("downloads = %d, want 1", n)
		}
	}
	if got := files(t, c); len(got) != 0 {
		t.Fatalf("files = %v", got)
	}
}

func TestAcceptedFormats(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 2))); err != nil {
		t.Fatal(err)
	}
	for _, body := range [][]byte{testJPEG(t), buf.Bytes()} {
		srv, _ := upstream(t, body)
		if _, err := New(t.TempDir()).Provider(metadata.CoverRef{URL: srv.URL}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpiredFailuresAreDropped(t *testing.T) {
	c := New(t.TempDir())
	c.failed["old"] = time.Now().Add(-time.Second)
	c.fail("new")
	if _, ok := c.failed["old"]; ok || !c.failedRecently("new") {
		t.Fatalf("failed = %v", c.failed)
	}
}

func TestGCKeepsReferencedCovers(t *testing.T) {
	srv, _ := upstream(t, testJPEG(t))
	pool := dbtest.Pool(t)
	ctx := context.Background()
	c := New(t.TempDir())
	kept, dropped := metadata.CoverRef{URL: srv.URL + "/a"}, metadata.CoverRef{URL: srv.URL + "/b"}
	for _, ref := range []metadata.CoverRef{kept, dropped} {
		if _, err := c.Provider(ref); err != nil {
			t.Fatal(err)
		}
	}
	// Left by a crash, and being written.
	crashed, writing := filepath.Join(c.providerDir(), "crashed.tmp"), filepath.Join(c.providerDir(), "writing.tmp")
	for _, path := range []string{crashed, writing} {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(crashed, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO libraries (id, name, type) VALUES ('l_test', 'lib', 'comics')"); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO content_metadata (library_id, uri, data_raw, data) VALUES
			('l_test', 'a', '{}', jsonb_build_object('cover', jsonb_build_object('url', $1::text))),
			('l_test', 'b', '{}', '{"title": "No cover"}')
	`, kept.URL); err != nil {
		t.Fatal(err)
	}

	if err := c.GC(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if got, want := files(t, c), []string{filepath.Base(c.providerPath(kept)), "writing.tmp"}; !slices.Equal(got, want) {
		t.Fatalf("files = %v, want %v", got, want)
	}
}

func TestHostFailureBacksOffTheWholeHost(t *testing.T) {
	img := testJPEG(t)
	var hits atomic.Int32
	var down atomic.Bool
	down.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch {
		case r.URL.Path == "/missing":
			http.NotFound(w, r)
		case down.Load():
			http.Error(w, "down", http.StatusServiceUnavailable)
		default:
			_, _ = w.Write(img)
		}
	}))
	t.Cleanup(srv.Close)
	get := func(c *Cache, path string) error {
		_, err := c.Provider(metadata.CoverRef{URL: srv.URL + path})
		return err
	}

	c := New(t.TempDir())
	if get(c, "/a") == nil || get(c, "/b") == nil || hits.Load() != 1 {
		t.Fatalf("hits = %d, want the host skipped after a 503", hits.Load())
	}

	// A missing cover is the cover's failure, not the host's.
	down.Store(false)
	c = New(t.TempDir())
	if get(c, "/missing") == nil || get(c, "/a") != nil {
		t.Fatal("a 404 blocked the host")
	}
}

func TestRedirectsAreLimited(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/again", http.StatusFound)
	}))
	t.Cleanup(srv.Close)
	if _, err := New(t.TempDir()).Provider(metadata.CoverRef{URL: srv.URL}); err == nil {
		t.Fatal("Provider succeeded")
	}
	if n := hits.Load(); n != maxRedirects+1 {
		t.Fatalf("requests = %d, want %d", n, maxRedirects+1)
	}
}
