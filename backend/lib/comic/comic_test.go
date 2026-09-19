package comic

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.White)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeCBZ(t *testing.T, entries map[string][]byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.cbz")
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

func assertPages(t *testing.T, path string, want ...string) {
	t.Helper()
	pages, _ := Scan(path)
	got := make([]string, len(pages))
	for i, p := range pages {
		got[i] = p.Name
	}
	if len(got) != len(want) {
		t.Fatalf("expected pages %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected pages %v, got %v", want, got)
		}
	}
}

func TestScanSkipsEscapingEntries(t *testing.T) {
	img := jpegBytes(t)
	assertPages(t, writeCBZ(t, map[string][]byte{
		"01.jpg":                    img,
		"chapter1/01.jpg":           img,
		"../../escape.jpg":          img,
		"chapter1/../../escape.jpg": img,
		`..\..\escape.jpg`:          img,
		"/abs.jpg":                  img,
		"../../junk.jpg":            []byte("root:x:0:0:root:/root:/bin/sh\n"),
	}), "01.jpg", "chapter1/01.jpg")
}

func TestIsArchiveLocal(t *testing.T) {
	cases := map[string]bool{
		"":                false,
		".":               false,
		"..":              false,
		"01.jpg":          true,
		"chapter1/01.jpg": true,
		"../x.jpg":        false,
		"a/../../b.jpg":   false,
		"/abs.jpg":        false,
		"a//b.jpg":        false,
		"dir/":            false,
		`..\x.jpg`:        false,
		`C:\x.jpg`:        false,
	}
	for name, want := range cases {
		if got := isArchiveLocal(name); got != want {
			t.Errorf("isArchiveLocal(%q) = %v, want %v", name, got, want)
		}
	}
}
