package routes

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"net/http"
	"os"
	"path/filepath"
	"testing"

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
