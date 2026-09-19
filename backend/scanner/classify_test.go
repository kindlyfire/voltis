package scanner

import (
	"reflect"
	"testing"

	"voltis/lib/comic"
	"voltis/lib/epub"
	"voltis/models"
)

var testPages = []comic.PageInfo{
	{Name: "001.jpg", Width: 4, Height: 2},
	{Name: "002.jpg", Width: 4, Height: 2},
}

func TestClassifyComicTuples(t *testing.T) {
	cases := []struct {
		name string
		path string
		meta models.Metadata
		year int
		want string
	}{
		{
			"chapter only",
			"/lib/Other Series/Other Series ch7.cbz", models.Metadata{}, 0,
			"prefix=comic type=comic part=ch7 order=[nil,7] cover=001.jpg title=Ch. 7 series=comic|comic_series|Other Series|Other Series index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"fallback chapter strips directory prefix",
			"/lib/Series 1000/Series 1000 002.cbz", models.Metadata{}, 0,
			"prefix=comic type=comic part=ch2 order=[nil,2] cover=001.jpg title=Ch. 2 series=comic|comic_series|Series 1000|Series 1000 index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
		{
			"metadata year without comicinfo year",
			"/lib/Plain/Plain ch1.cbz", models.Metadata{Series: "Plain Series"}, 0,
			"prefix=comic type=comic part=ch1 order=[nil,1] cover=001.jpg title=Ch. 1 series=comic|comic_series|Plain Series|Plain Series index=0 data={\"pages\":[[\"001.jpg\",4,2],[\"002.jpg\",4,2]]}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := FSFile{Path: c.path, Mtime: baseTime, Size: 10}
			got := summarize(classifyComic(file, c.meta, c.year, testPages))
			if got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestClassifyBookTuples(t *testing.T) {
	cases := []struct {
		name       string
		path       string
		meta       epub.Metadata
		coverValid bool
		want       string
	}{
		{
			"title falls back to stem",
			"/lib/Books/untitled-file.epub", epub.Metadata{}, false,
			"prefix=book type=book part=untitled-file order=[0] cover=nil title=untitled-file series=nil index=0 data=",
		},
		{
			"invalid cover is dropped",
			"/lib/Books/broken.epub", epub.Metadata{Title: "Broken", CoverPath: "missing.jpg"}, false,
			"prefix=book type=book part=broken order=[0] cover=nil title=Broken series=nil index=0 data=",
		},
		{
			"series without index",
			"/lib/Books/no-index.epub", epub.Metadata{Title: "No Index", Series: "Book Series"}, false,
			"prefix=book type=book part=no-index order=[0] cover=nil title=No Index series=book|book_series|Book Series|Book Series index=0 data=",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := FSFile{Path: c.path, Mtime: baseTime, Size: 10}
			item := classifyBook(file, c.meta, c.coverValid)
			if got := summarize(&item); got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}
}

func TestClassifyBookMetadata(t *testing.T) {
	file := FSFile{Path: "/lib/Books/story.epub", Mtime: baseTime, Size: 10}
	meta := epub.Metadata{
		Title:           "Story",
		Authors:         []string{"Ann Author", "Ben Writer"},
		Description:     "A story",
		Publisher:       "Pub",
		Language:        "en",
		PublicationDate: "2020-01-02",
		Series:          "Book Series",
	}
	got := classifyBook(file, meta, false).MetaRaw
	want := models.Metadata{
		Title:       "Story",
		Description: "A story",
		Staff: []models.StaffEntry{
			{Name: "Ann Author", Role: "author"},
			{Name: "Ben Writer", Role: "author"},
		},
		Publisher:       "Pub",
		Language:        "en",
		PublicationDate: "2020-01-02",
		Series:          "Book Series",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("meta = %+v, want %+v", got, want)
	}
}

func TestSanitizeURIPart(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Foo/bar", "Foo_bar"},
		{`Foo\bar`, "Foo_bar"},
		{"a/b\\c", "a_b_c"},
		{"tab\there", "tab_here"},
		{"nl\nhere", "nl_here"},
		{"null\x00byte", "null_byte"},
		{"ordinary name", "ordinary name"},
		{"Series_2019", "Series_2019"},
		{"v1_ch2.5", "v1_ch2.5"},
		{"Ünïcødé 漫画", "Ünïcødé 漫画"},
		{"", "_"},
		{"/", "_"},
		{"//", "__"},
	}
	for _, c := range cases {
		got := sanitizeURIPart(c.in)
		if got != c.want {
			t.Errorf("sanitizeURIPart(%q) = %q, want %q", c.in, got, c.want)
		}
		if again := sanitizeURIPart(got); again != got {
			t.Errorf("sanitizeURIPart(%q) not idempotent: %q", got, again)
		}
	}
}

func TestSanitizeURIPartNotActivated(t *testing.T) {
	file := FSFile{Path: "/lib/Series/ch1.cbz", Mtime: baseTime, Size: 10}
	item := classifyComic(file, models.Metadata{Series: "Foo/bar"}, 0, testPages)
	if item.Series.URIPart != "Foo/bar" || item.Series.Title != "Foo/bar" {
		t.Fatalf("series = %+v, want unsanitized producers", item.Series)
	}

	book := classifyBook(FSFile{Path: "/lib/Books/a.epub"}, epub.Metadata{Series: "Foo/bar"}, false)
	if book.Series.URIPart != "Foo/bar" {
		t.Fatalf("book series part = %q, want unsanitized", book.Series.URIPart)
	}
}
