package scanner

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"voltis/lib/comic"
	"voltis/lib/epub"
	"voltis/metadata"
)

var testPages = []comic.PageInfo{{Name: "001.jpg"}, {Name: "002.jpg"}}

func TestClassifyComicTuples(t *testing.T) {
	cases := []struct {
		name string
		path string
		meta metadata.Fields
		want string
	}{
		{
			"chapter only",
			"/lib/Other Series/Other Series ch7.cbz", metadata.Fields{},
			"prefix=comic type=comic part=ch7 order=[nil,7] cover=001.jpg title=Ch. 7 series=comic|comic_series|Other Series index=0 data={\"pages\":[[\"001.jpg\"],[\"002.jpg\"]]}",
		},
		{
			"fallback chapter strips directory prefix and tags",
			"/lib/Series 1000/Series 1000 002 (2019).cbz", metadata.Fields{},
			"prefix=comic type=comic part=ch2 order=[nil,2] cover=001.jpg title=Ch. 2 series=comic|comic_series|Series 1000 index=0 data={\"pages\":[[\"001.jpg\"],[\"002.jpg\"]]}",
		},
		{
			"metadata year without comicinfo year",
			"/lib/Plain/Plain ch1.cbz", metadata.Fields{Series: metadata.Val("Plain Series")},
			"prefix=comic type=comic part=ch1 order=[nil,1] cover=001.jpg title=Ch. 1 series=comic|comic_series|Plain Series index=0 data={\"pages\":[[\"001.jpg\"],[\"002.jpg\"]]}",
		},
		{
			"comicinfo number ignores filename issue marker",
			"/lib/Tide Atlas/0074 - Spare Extra #1.cbz", metadata.Fields{Number: metadata.Val("74")},
			"prefix=comic type=comic part=ch74 order=[nil,74] cover=001.jpg title=Ch. 74 series=comic|comic_series|Tide Atlas index=0 data={\"pages\":[[\"001.jpg\"],[\"002.jpg\"]]}",
		},
		{
			"series name is not a chapter marker",
			"/lib/C3 Unit/C3 Unit v02.cbz", metadata.Fields{},
			"prefix=comic type=comic part=v2 order=[2,nil] cover=001.jpg title=Vol. 2 series=comic|comic_series|C3 Unit index=0 data={\"pages\":[[\"001.jpg\"],[\"002.jpg\"]]}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file := FSFile{Path: c.path, Mtime: baseTime, Size: 10}
			got := summarize(classifyComic(file, c.meta, nil, testPages))
			if got != c.want {
				t.Errorf("got  %s\nwant %s", got, c.want)
			}
		})
	}

	t.Run("sized pages", func(t *testing.T) {
		pages := []comic.PageInfo{{Name: "p1", Width: 4, Height: 2, Sized: true}, {Name: "p2", Sized: true}}
		item := classifyComic(FSFile{Path: "/lib/S/S ch1.pdf", Mtime: baseTime, Size: 10}, metadata.Fields{}, nil, pages)
		if got, want := string(item.FileData), `{"pages":[["p1",4,2],["p2",0,0]]}`; got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	})
}

func TestClassifyBookTuples(t *testing.T) {
	check := func(t *testing.T, path string, meta epub.Metadata, want string) {
		t.Helper()
		item := classifyBook(FSFile{Path: path, Mtime: baseTime, Size: 10}, meta, false, nil, false)
		if got := summarize(&item); got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
	}
	t.Run("invalid cover is dropped", func(t *testing.T) {
		check(t, "/lib/Books/broken.epub", epub.Metadata{Title: "Broken", CoverPath: "missing.jpg"},
			"prefix=book type=book part=broken order=[nil] cover=nil title=Broken series=nil index=0 data=")
	})
	t.Run("series without index", func(t *testing.T) {
		check(t, "/lib/Books/no-index.epub", epub.Metadata{Title: "No Index", Series: "Book Series"},
			"prefix=book type=book part=no-index order=[nil] cover=nil title=No Index series=book|book_series|Book Series index=0 data=")
	})
}

func TestClassifyBookInference(t *testing.T) {
	const ser = "Ironbound - From Nothing to Legend's End"
	cases := []struct {
		name, stem string
		meta       epub.Metadata
		infer      bool
		series     string // empty for a standalone book
		order      *float32
	}{
		{
			"title marker", ser + " v02 [Pub] {x}",
			epub.Metadata{Title: "Ironbound: From Nothing to Legend’s End Vol. 02"}, true,
			"Ironbound: From Nothing to Legend’s End", f32(2),
		},
		{
			"filename fallback", ser + " v03 [Pub] {x}",
			epub.Metadata{Title: "Ironbound"}, true, ser, f32(3),
		},
		{
			"title and filename volumes differ", ser + " v03",
			epub.Metadata{Title: "Ironbound Vol. 4"}, true, "", nil,
		},
		{
			"special title", ser + " SP02 - Volume 10 [STORE☆FRONT Exclusive Short Story]",
			epub.Metadata{Title: "Ironbound Volume 10 - STORE☆FRONT Exclusive Popularity Poll Short Story"}, true, "", nil,
		},
		{
			"metadata series wins", ser + " SP03",
			epub.Metadata{Title: "Other Vol. 3", Series: "Meta Series", SeriesIndex: 100000, HasSeriesIndex: true}, true,
			"Meta Series", f32(100000),
		},
		{
			"explicit index wins over the inferred volume", "Zero v01",
			epub.Metadata{Title: "Ironbound Zero: Volume 1", HasSeriesIndex: true}, true, "Ironbound Zero", f32(0),
		},
		{
			"inference off", ser + " v01",
			epub.Metadata{Title: "Ironbound: From Nothing to Legend's End Vol. 01"}, false, "", nil,
		},
		{"no marker", "Plenty - Jane Author", epub.Metadata{Title: "Plenty"}, true, "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			item := classifyBook(FSFile{Path: "/lib/" + c.stem + ".epub"}, c.meta, false, nil, c.infer)
			series := ""
			if item.Series != nil {
				series = item.Series.URIPart
				if item.Series.Inferred != (c.meta.Series == "") {
					t.Errorf("inferred = %v, want %v", item.Series.Inferred, c.meta.Series == "")
				}
			}
			if series != c.series {
				t.Errorf("series = %q, want %q", series, c.series)
			}
			if len(item.OrderParts) != 1 || !reflect.DeepEqual(item.OrderParts[0], c.order) {
				t.Errorf("order = %v, want [%v]", item.OrderParts, c.order)
			}
			if item.MetaRaw.Series.V != c.meta.Series || item.MetaRaw.SeriesIndex.V != c.meta.SeriesIndex {
				t.Errorf("child series = %q/%g, want the metadata values %q/%g",
					item.MetaRaw.Series.V, item.MetaRaw.SeriesIndex.V, c.meta.Series, c.meta.SeriesIndex)
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
	got := classifyBook(file, meta, false, nil, false).MetaRaw
	want := metadata.Fields{
		Title:       metadata.Val("Story"),
		Description: metadata.Val("A story"),
		Staff: metadata.Val([]metadata.Staff{
			{Name: "Ann Author", Role: "author"},
			{Name: "Ben Writer", Role: "author"},
		}),
		Publishers:      metadata.Val([]string{"Pub"}),
		Language:        metadata.Val("en"),
		PublicationDate: metadata.Val("2020-01-02"),
		Series:          metadata.Val("Book Series"),
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
	item := classifyBook(file, epub.Metadata{Series: "Foo/bar\x01"}, false, nil, false)

	if item.URIPart != "a_b_c" {
		t.Errorf("item part = %q, want the stem sanitized", item.URIPart)
	}
	if item.MetaRaw.Title.V != "a\tb\\c" {
		t.Errorf("title = %q, want the stem kept verbatim", item.MetaRaw.Title.V)
	}
	if item.Series.URIPart != "Foo_bar_" {
		t.Errorf("series part = %q, want the series name sanitized", item.Series.URIPart)
	}
	if item.MetaRaw.Series.V != "Foo/bar\x01" {
		t.Errorf("metadata series = %q, want it kept verbatim", item.MetaRaw.Series.V)
	}

	empty := classifyBook(FSFile{Path: "/lib/Books/.epub"}, epub.Metadata{Series: "/"}, false, nil, false)
	if empty.URIPart != "_" || empty.Series.URIPart != "_" {
		t.Errorf("parts = %q and %q, want the empty fallback", empty.URIPart, empty.Series.URIPart)
	}
}

func TestSanitizeURIPartAtComicProducers(t *testing.T) {
	file := FSFile{Path: "/lib/S/S ch1.cbz", Mtime: baseTime, Size: 10}
	item := classifyComic(file, metadata.Fields{Series: metadata.Val("Foo/bar"), Title: metadata.Val("Ch. 1 / Special")}, new(2019), testPages)

	if item.Series.URIPart != "Foo_bar_2019" {
		t.Errorf("series part = %q, want the separator replaced and the year suffix kept", item.Series.URIPart)
	}
	if item.MetaRaw.Title.V != "Ch. 1 / Special" {
		t.Errorf("title = %q, want it kept verbatim", item.MetaRaw.Title.V)
	}
	if item.MetaRaw.Series.V != "Foo/bar" {
		t.Errorf("metadata series = %q, want it kept verbatim", item.MetaRaw.Series.V)
	}
	if item.URIPart != "ch1" {
		t.Errorf("item part = %q, want an ordinary part unchanged", item.URIPart)
	}

	control := classifyComic(file, metadata.Fields{Series: metadata.Val("Foo\x02bar")}, nil, testPages)
	if control.Series.URIPart != "Foo_bar" {
		t.Errorf("series part = %q, want the control character replaced", control.Series.URIPart)
	}
}

func TestClassifySanitizesEveryURIPartProducer(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "classify.go", nil, 0)
	must(t, err)

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
	one := classifyBook(first, epub.Metadata{}, false, nil, false)
	two := classifyBook(second, epub.Metadata{}, false, nil, false)
	if one.URIPart != two.URIPart {
		t.Fatalf("parts = %q and %q, want sanitization to collide them", one.URIPart, two.URIPart)
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

func TestClassifySanitizedPartReachesTheWriter(t *testing.T) {
	path := writeCBZ(t, filepath.Join(t.TempDir(), "Series", "Series ch1.cbz"),
		`<?xml version="1.0"?><ComicInfo><Series>Foo/bar</Series><Number>1</Number></ComicInfo>`)
	item := (&ComicsScanner{}).ParseFile(statFile(t, path))
	if item.Series.URIPart != "Foo_bar" {
		t.Fatalf("parsed series = %+v", item.Series)
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

func TestComicFallbackSeriesLeavesTheInferredNameOffTheChild(t *testing.T) {
	file := fsFile("/lib/Foo\\bar/ch1.cbz", baseTime, 10)
	item := classifyComic(file, metadata.Fields{}, nil, testPages)
	if item.Series.URIPart != "Foo_bar" {
		t.Fatalf("series = %+v, want the folder name sanitized into the part", item.Series)
	}
	if item.MetaRaw.Series.P != metadata.Absent {
		t.Errorf("child series = %+v, want an inferred name left off the child", item.MetaRaw.Series)
	}
}

func TestClassifyBookWords(t *testing.T) {
	item := classifyBook(FSFile{Path: "/lib/Books/x.epub", Mtime: baseTime, Size: 10},
		epub.Metadata{Title: "X"}, false, &epub.WordCounts{Docs: map[string]int{"OEBPS/b.xhtml": 7, "OEBPS/a.xhtml": 120}}, false)

	if got := string(item.FileData); got != `{"words":{"OEBPS/a.xhtml":120,"OEBPS/b.xhtml":7}}` {
		t.Errorf("file data = %s", got)
	}
	if empty := classifyBook(FSFile{Path: "/lib/Books/x.epub"}, epub.Metadata{}, false, nil, false); empty.FileData != nil {
		t.Errorf("file data = %s, want none without counts", empty.FileData)
	}
}

func TestParseBookCountsWords(t *testing.T) {
	text := "This paragraph is comfortably longer than the textless floor."
	path := writeZip(t, filepath.Join(t.TempDir(), "book.epub"), map[string][]byte{
		"META-INF/container.xml": []byte(`<?xml version="1.0"?><container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`),
		"OEBPS/content.opf": []byte(`<?xml version="1.0"?><package><metadata><dc:title xmlns:dc="http://purl.org/dc/elements/1.1/">B</dc:title></metadata><manifest>
			<item id="c1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
		</manifest><spine><itemref idref="c1"/></spine></package>`),
		"OEBPS/text/ch1.xhtml": []byte(`<!DOCTYPE html><html><body><p>` + text + `</p></body></html>`),
	})

	item := (&BooksScanner{}).ParseFile(statFile(t, path))
	want := fmt.Sprintf(`{"words":{"OEBPS/text/ch1.xhtml":%d}}`, len(strings.Fields(text)))
	if got := string(item.FileData); got != want {
		t.Errorf("file data = %s, want %s", got, want)
	}
}
