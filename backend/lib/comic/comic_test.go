package comic

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"os"
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
