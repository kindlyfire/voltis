package comic

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"voltis/metadata"
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

func TestComicInfoToMetadata(t *testing.T) {
	for _, tc := range []struct {
		name, xml string
		want      metadata.Fields
	}{
		{"missing numbers are absent", ``, metadata.Fields{}},
		{"zero is a value", `<Count>0</Count><AlternateCount>0</AlternateCount>`,
			metadata.Fields{Count: metadata.Val(0), AlternateCount: metadata.Val(0)}},
		{"malformed numbers are absent", `<Title>T</Title><Count>abc</Count><Year>soon</Year><CommunityRating>x</CommunityRating>`,
			metadata.Fields{Title: metadata.Val("T")}},
		{"volume stays text", `<Volume>1.5</Volume>`, metadata.Fields{Volume: metadata.Val("1.5")}},
		{"rating on 0-100", `<CommunityRating>4</CommunityRating>`, metadata.Fields{Rating: metadata.Val(80.0)}},
		{"age rating", `<AgeRating>Teen</AgeRating>`, metadata.Fields{ContentRating: metadata.Val(metadata.Suggestive)}},
		{"unknown age rating", `<AgeRating>Rating Pending</AgeRating>`, metadata.Fields{}},
		{"full date", `<Year>2019</Year><Month>3</Month><Day>7</Day>`, metadata.Fields{PublicationDate: metadata.Val("2019-03-07")}},
		{"unset date parts", `<Year>2019</Year><Month>-1</Month><Day>7</Day>`, metadata.Fields{PublicationDate: metadata.Val("2019")}},
		{"unset year", `<Year>-1</Year><Month>3</Month>`, metadata.Fields{}},
		{"zero year", `<Year>0</Year>`, metadata.Fields{}},
		{"impossible month", `<Year>2019</Year><Month>13</Month><Day>1</Day>`, metadata.Fields{PublicationDate: metadata.Val("2019")}},
		{"impossible day", `<Year>2019</Year><Month>2</Month><Day>31</Day>`, metadata.Fields{PublicationDate: metadata.Val("2019-02")}},
		{"day without a month", `<Year>2019</Year><Day>7</Day>`, metadata.Fields{PublicationDate: metadata.Val("2019")}},
		{"unset numbers", `<Count>-1</Count><AlternateCount>-1</AlternateCount><Volume>-1</Volume>`, metadata.Fields{}},
		{"unknown text", `<Series>Unknown</Series><Publisher>Pub</Publisher><Genre>Action, Drama</Genre>`,
			metadata.Fields{Publishers: metadata.Val([]string{"Pub"}), Genres: metadata.Val([]string{"Action, Drama"})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ci, err := ParseComicInfo([]byte(`<?xml version="1.0"?><ComicInfo>` + tc.xml + `</ComicInfo>`))
			if err != nil {
				t.Fatal(err)
			}
			if got := ComicInfoToMetadata(ci); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// The scanner builds URIs from these, so a placeholder must not read as v-1 or year -1.
func TestParseComicInfoDropsPlaceholders(t *testing.T) {
	ci, err := ParseComicInfo([]byte(`<ComicInfo><Year>-1</Year><Volume>-1</Volume><Count>-1</Count></ComicInfo>`))
	if err != nil || ci.Year != nil || ci.Volume != "" || ci.Count != nil {
		t.Fatalf("parsed %+v (%v)", ci, err)
	}
}

func TestScanArchiveIsUnsized(t *testing.T) {
	img := jpegBytes(t)
	pages, ci := Scan(writeCBZ(t, map[string][]byte{
		"002.jpg":       img,
		"001.jpg":       img,
		"notes.txt":     []byte("x"),
		"ComicInfo.xml": []byte(`<ComicInfo><Title>T</Title></ComicInfo>`),
	}))
	want := []PageInfo{{Name: "001.jpg"}, {Name: "002.jpg"}}
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("pages = %+v, want %+v", pages, want)
	}
	if ci == nil || ci.Title != "T" {
		t.Errorf("comic info = %+v", ci)
	}
}

func TestScanPDFIsSized(t *testing.T) {
	if _, err := exec.LookPath("pdfinfo"); err != nil {
		t.Skip("pdfinfo not installed")
	}
	path := filepath.Join(t.TempDir(), "test.pdf")
	pdf := "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n" +
		"2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
		"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 72 144]>>endobj\n" +
		"trailer<</Root 1 0 R>>\n%%EOF\n"
	if err := os.WriteFile(path, []byte(pdf), 0o644); err != nil {
		t.Fatal(err)
	}
	pages, _ := Scan(path)
	want := []PageInfo{{Name: "p1", Width: 250, Height: 500, Sized: true}}
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("pages = %+v, want %+v", pages, want)
	}
}

func TestPageSizes(t *testing.T) {
	path := writeCBZ(t, map[string][]byte{"001.jpg": jpegBytes(t), "002.jpg": []byte("not an image")})
	pages, err := PageSizes(context.Background(), path, []string{"002.jpg", "001.jpg", "missing.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	want := []PageInfo{
		{Name: "002.jpg", Sized: true},
		{Name: "001.jpg", Width: 4, Height: 2, Sized: true},
		{Name: "missing.jpg", Sized: true},
	}
	if !reflect.DeepEqual(pages, want) {
		t.Errorf("pages = %+v, want %+v", pages, want)
	}

	notZip := filepath.Join(t.TempDir(), "bad.cbz")
	if err := os.WriteFile(notZip, []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{notZip, filepath.Join(t.TempDir(), "missing.cbz")} {
		if _, err := PageSizes(context.Background(), path, []string{"001.jpg"}); err == nil {
			t.Errorf("PageSizes(%s) succeeded", path)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := PageSizes(ctx, path, []string{"001.jpg"}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}
