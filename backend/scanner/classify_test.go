package scanner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
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

func TestSanitizeURIPartAtBookProducers(t *testing.T) {
	file := FSFile{Path: "/lib/Books/a\tb\\c.epub", Mtime: baseTime, Size: 10}
	item := classifyBook(file, epub.Metadata{Series: "Foo/bar\x01"}, false)

	if item.URIPart != "a_b_c" {
		t.Errorf("item part = %q, want the stem sanitized", item.URIPart)
	}
	if item.MetaRaw.Title != "a\tb\\c" {
		t.Errorf("title = %q, want the stem kept verbatim", item.MetaRaw.Title)
	}
	if item.Series.URIPart != "Foo_bar_" {
		t.Errorf("series part = %q, want the series name sanitized", item.Series.URIPart)
	}
	if item.Series.Title != "Foo/bar\x01" || item.MetaRaw.Series != "Foo/bar\x01" {
		t.Errorf("series title = %q and metadata series = %q, want both kept verbatim",
			item.Series.Title, item.MetaRaw.Series)
	}

	empty := classifyBook(FSFile{Path: "/lib/Books/.epub"}, epub.Metadata{Series: "/"}, false)
	if empty.URIPart != "_" || empty.Series.URIPart != "_" {
		t.Errorf("parts = %q and %q, want the empty fallback", empty.URIPart, empty.Series.URIPart)
	}
}

func TestSanitizeURIPartAtComicProducers(t *testing.T) {
	file := FSFile{Path: "/lib/S/S ch1.cbz", Mtime: baseTime, Size: 10}
	item := classifyComic(file, models.Metadata{Series: "Foo/bar", Title: "Ch. 1 / Special"}, 2019, testPages)

	if item.Series.URIPart != "Foo_bar_2019" {
		t.Errorf("series part = %q, want the separator replaced and the year suffix kept", item.Series.URIPart)
	}
	if item.Series.Title != "Foo/bar" || item.MetaRaw.Title != "Ch. 1 / Special" {
		t.Errorf("titles = %q and %q, want both kept verbatim", item.Series.Title, item.MetaRaw.Title)
	}
	if item.URIPart != "ch1" {
		t.Errorf("item part = %q, want an ordinary part unchanged", item.URIPart)
	}

	control := classifyComic(file, models.Metadata{Series: "Foo\x02bar"}, 0, testPages)
	if control.Series.URIPart != "Foo_bar" {
		t.Errorf("series part = %q, want the control character replaced", control.Series.URIPart)
	}
}

func TestClassifySanitizesEveryURIPartProducer(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "classify.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	sites, wrapped := 0, 0
	ast.Inspect(file, func(n ast.Node) bool {
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok || key.Name != "URIPart" {
			return true
		}
		sites++
		call, ok := kv.Value.(*ast.CallExpr)
		if !ok {
			t.Errorf("%s: URIPart is produced without sanitization", fset.Position(kv.Pos()))
			return true
		}
		fn, ok := call.Fun.(*ast.Ident)
		if !ok || fn.Name != "sanitizeURIPart" {
			t.Errorf("%s: URIPart is produced without sanitization", fset.Position(kv.Pos()))
			return true
		}
		wrapped++
		return true
	})
	if sites != 4 || wrapped != 4 {
		t.Fatalf("%d of %d URIPart producers sanitize, want 4 of 4", wrapped, sites)
	}
}

func TestClassifyCollisionFallsThroughToConflictHandling(t *testing.T) {
	first := FSFile{Path: "/lib/Books/a_b.epub", Mtime: baseTime, Size: 10}
	second := FSFile{Path: "/lib/Books/a\tb.epub", Mtime: baseTime, Size: 10}
	one := classifyBook(first, epub.Metadata{}, false)
	two := classifyBook(second, epub.Metadata{}, false)
	if one.URIPart != two.URIPart {
		t.Fatalf("parts = %q and %q, want sanitization to collide them", one.URIPart, two.URIPart)
	}

	r := newRepository(nil, "library")
	var counts scanCounts
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	applyParseResult(r, "library", first, &one, true, logf, &counts, &Counts{})
	applyParseResult(r, "library", second, &two, true, logf, &counts, &Counts{})

	if len(r.content) != 1 || deref(r.content[0].FileURI) != first.Path {
		t.Fatalf("content = %+v, want only the first file", r.content)
	}
	if counts.added.Load() != 1 || counts.failed.Load() != 1 || len(logs) != 1 {
		t.Fatalf("counts = %d added / %d failed, logs = %v", counts.added.Load(), counts.failed.Load(), logs)
	}

	w := testWriter(nil, nil)
	w.place(Result{File: first, Item: &one})
	w.place(Result{File: second, Item: &two})
	if w.prog.Failed != 1 {
		t.Fatalf("writer failed = %d, want the colliding placement rejected", w.prog.Failed)
	}
	if set := w.sets[""]; len(set.Writes) != 1 {
		t.Fatalf("standalone writes = %+v, want only the first file", set.Writes)
	}
}

func TestClassifySanitizedPartReachesBothScannerPaths(t *testing.T) {
	path := writeCBZ(t, filepath.Join(t.TempDir(), "Series", "Series ch1.cbz"),
		`<?xml version="1.0"?><ComicInfo><Series>Foo/bar</Series><Number>1</Number></ComicInfo>`)
	item := (&ComicsScanner{}).ParseFile("library", statFile(t, path))
	if item.Series.URIPart != "Foo_bar" || item.Series.Title != "Foo/bar" {
		t.Fatalf("parsed series = %+v", item.Series)
	}

	r := newRepository(nil, "library")
	var counts scanCounts
	applyParseResult(r, "library", item.File, item, true, func(string, ...any) {}, &counts, &Counts{})
	if len(r.content) != 2 || r.content[0].URI != "comic/Foo_bar" || r.content[1].URI != "comic/Foo_bar/ch1" {
		t.Fatalf("legacy content = %+v", r.content)
	}

	w := testWriter(nil, nil)
	w.place(Result{File: item.File, Item: item})
	id := w.byURI["comic/Foo_bar"]
	if id == "" {
		t.Fatalf("writer series uris = %v, want comic/Foo_bar", w.byURI)
	}
	set := w.sets[id]
	if set == nil || !set.New || set.Ref.URIPart != "Foo_bar" {
		t.Fatalf("writer series changes = %+v, want a new series at the sanitized part", set)
	}
	if len(set.Writes) != 1 || set.Writes[0].item.URIPart != "ch1" || w.prog.Failed != 0 {
		t.Fatalf("writes = %+v, failed = %d, want the leaf queued under the series",
			set.Writes, w.prog.Failed)
	}
}

func TestComicFallbackSeriesTitleKeepsTheRawName(t *testing.T) {
	file := fsFile("/lib/Foo\\bar/ch1.cbz", baseTime, 10)
	item := classifyComic(file, models.Metadata{}, 0, testPages)
	if item.Series.URIPart != "Foo_bar" || item.Series.Title != "Foo\\bar" {
		t.Fatalf("series = %+v, want a sanitized part and a verbatim title", item.Series)
	}
	if item.MetaRaw.Series != "" {
		t.Errorf("child series = %q, want an inferred name left off the child", item.MetaRaw.Series)
	}

	r := newRepository(nil, "library")
	var counts scanCounts
	parentID := applyParseResult(r, "library", file, item, true, func(string, ...any) {}, &counts, &Counts{})
	if parentID == nil {
		t.Fatalf("parentID = nil, counts failed = %d", counts.failed.Load())
	}
	seedSeriesTitle(r, "comic/Foo_bar", "Stale")
	updateGroupSeries(&ComicsScanner{}, r, map[string]bool{*parentID: true})

	if got := r.getMetadata("comic/Foo_bar").DataRaw.File.Raw.Title; got != "Foo\\bar" {
		t.Errorf("legacy series title = %q, want the raw directory name", got)
	}
}
