package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/lib/epub"
	"voltis/models"
	"voltis/models/metaraw"
)

var baseTime = time.Unix(1700000000, 0).UTC()

func testLeaf(id, typ, path string, mtime time.Time, size int, valid bool) models.Content {
	return models.Content{
		ID:        id,
		LibraryID: "library",
		Type:      typ,
		URIPart:   id,
		URI:       "comic/series/" + id,
		Valid:     valid,
		FileURI:   new(path),
		FileMtime: new(mtime),
		FileSize:  new(size),
		CreatedAt: baseTime,
		UpdatedAt: baseTime,
	}
}

func fsFile(path string, mtime time.Time, size int64) FSFile {
	return FSFile{Path: path, Mtime: mtime, Size: size}
}

func rawMeta(m models.Metadata) metaraw.MetadataRaw {
	return metaraw.MetadataRaw{File: &metaraw.RawContainer[models.Metadata]{Raw: m}}
}

func parsedComic(file FSFile, uriPart string) *ParsedItem {
	return &ParsedItem{
		File:        file,
		URIPrefix:   "comic",
		ContentType: "comic",
		URIPart:     uriPart,
		OrderParts:  []*float32{new(float32(1))},
		Series: &ParsedSeries{
			URIPrefix:   "comic",
			URIPart:     "Series",
			ContentType: "comic_series",
			Title:       "Series",
			FileURI:     new("/lib/s"),
		},
	}
}

func assertUntouched(t *testing.T, r *repository, before []models.Content) {
	t.Helper()
	if !reflect.DeepEqual(r.content, before) {
		t.Fatalf("content = %+v, want %+v", r.content, before)
	}
	if len(r.deletedContent) != 0 {
		t.Fatalf("deletedContent = %d rows, want 0", len(r.deletedContent))
	}
	if len(r.dirtyIDs) != 0 {
		t.Fatalf("dirtyIDs = %v, want empty", r.dirtyIDs)
	}
}

type diffPaths struct{ added, updated, unchanged, removed []string }

func repoOf(build func() []models.Content) func() *repository {
	return func() *repository {
		r := newRepository(nil, "library")
		r.content = build()
		return r
	}
}

func TestMatchFiles(t *testing.T) {
	paths := func(p ...string) []string { return p }
	boundary := repoOf(func() []models.Content {
		return []models.Content{
			testLeaf("a", "comic", "/lib/Foo/ch1.cbz", baseTime, 10, true),
			testLeaf("b", "comic", "/lib/Foo/ch2.cbz", baseTime, 10, true),
			testLeaf("c", "comic", "/lib/Foo Extra/ch3.cbz", baseTime, 10, true),
			testLeaf("d", "comic", "/lib/Foo/Sub/ch4.cbz", baseTime, 10, true),
		}
	})
	failed := repoOf(func() []models.Content {
		return []models.Content{
			testLeaf("a", "comic", "/lib/Broken/ch1.cbz", baseTime, 10, true),
			testLeaf("b", "comic", "/lib/Broken/Sub/ch2.cbz", baseTime, 10, true),
			testLeaf("c", "comic", "/lib/Odd/ch9.cbz", baseTime, 10, true),
			testLeaf("d", "comic", "/lib/Broken Extra/ch3.cbz", baseTime, 10, true),
			testLeaf("e", "comic", "/lib/Ok/ch4.cbz", baseTime, 10, true),
			testLeaf("f", "comic", "/lib/Broken/ch5.cbz", baseTime, 10, true),
		}
	})
	invalid := repoOf(func() []models.Content {
		return []models.Content{
			testLeaf("valid", "comic", "/lib/s/a.cbz", baseTime, 10, true),
			testLeaf("invalid", "comic", "/lib/s/b.cbz", baseTime, 10, false),
			testLeaf("changed", "comic", "/lib/s/c.cbz", baseTime, 10, true),
			testLeaf("gone", "comic", "/lib/s/d.cbz", baseTime, 10, false),
		}
	})
	exact := repoOf(func() []models.Content {
		return []models.Content{
			testLeaf("a", "comic", "/lib/Foo/ch1.cbz", baseTime, 10, true),
			testLeaf("b", "comic", "/lib/Foo/ch1.cbz.bak", baseTime, 10, true),
		}
	})
	boundaryFiles := []FSFile{
		fsFile("/lib/Foo/ch1.cbz", baseTime, 10),
		fsFile("/lib/Foo/new.cbz", baseTime, 10),
		fsFile("/lib/Foo Extra/ch5.cbz", baseTime, 10),
	}
	invalidFiles := []FSFile{
		fsFile("/lib/s/a.cbz", baseTime, 10),
		fsFile("/lib/s/b.cbz", baseTime, 10),
		fsFile("/lib/s/c.cbz", baseTime, 20),
	}
	brokenChanged := []FSFile{fsFile("/lib/Broken/ch5.cbz", baseTime.Add(time.Hour), 20)}
	brokenSame := []FSFile{fsFile("/lib/Broken/ch5.cbz", baseTime, 10)}

	cases := []struct {
		name            string
		build           func() *repository
		files           []FSFile
		filters, failed []string
		force           bool
		want            diffPaths
	}{
		{"directory filter", boundary, boundaryFiles, paths("/lib/Foo"), nil, false, diffPaths{
			added: paths("/lib/Foo/new.cbz"), unchanged: paths("/lib/Foo/ch1.cbz"),
			removed: paths("/lib/Foo/ch2.cbz", "/lib/Foo/Sub/ch4.cbz")}},
		{"overlapping filters", boundary, boundaryFiles, paths("/lib/Foo", "/lib/Foo/Sub"), nil, false, diffPaths{
			added: paths("/lib/Foo/new.cbz"), unchanged: paths("/lib/Foo/ch1.cbz"),
			removed: paths("/lib/Foo/ch2.cbz", "/lib/Foo/Sub/ch4.cbz")}},
		{"sibling boundary", boundary, boundaryFiles, paths("/lib/Foo Extra"), nil, false, diffPaths{
			added: paths("/lib/Foo Extra/ch5.cbz"), removed: paths("/lib/Foo Extra/ch3.cbz")}},
		{"exact file boundary", exact, nil, paths("/lib/Foo/ch1.cbz"), nil, false, diffPaths{
			removed: paths("/lib/Foo/ch1.cbz")}},
		{"failed directory and failed file", failed, brokenChanged, nil, paths("/lib/Broken", "/lib/Odd/ch9.cbz"), false, diffPaths{
			updated: paths("/lib/Broken/ch5.cbz"),
			removed: paths("/lib/Broken Extra/ch3.cbz", "/lib/Ok/ch4.cbz")}},
		{"filtered failure", failed, brokenSame, paths("/lib/Broken", "/lib/Broken Extra"), paths("/lib/Broken"), false, diffPaths{
			unchanged: paths("/lib/Broken/ch5.cbz"), removed: paths("/lib/Broken Extra/ch3.cbz")}},
		{"invalid retry", invalid, invalidFiles, nil, nil, false, diffPaths{
			updated: paths("/lib/s/b.cbz", "/lib/s/c.cbz"), unchanged: paths("/lib/s/a.cbz"),
			removed: paths("/lib/s/d.cbz")}},
		{"forced retry", invalid, invalidFiles, nil, nil, true, diffPaths{
			updated: paths("/lib/s/a.cbz", "/lib/s/b.cbz", "/lib/s/c.cbz"),
			removed: paths("/lib/s/d.cbz")}},
		{"protected missing invalid file", invalid, invalidFiles, nil, paths("/lib/s/d.cbz"), false, diffPaths{
			updated: paths("/lib/s/b.cbz", "/lib/s/c.cbz"), unchanged: paths("/lib/s/a.cbz")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := c.build()
			toAdd, toUpdate, unchanged, toRemove := matchFiles(r, c.files, c.filters, c.failed, c.force)
			assertPaths(t, "toAdd", toAdd, c.want.added...)
			assertPaths(t, "toUpdate", toUpdate, c.want.updated...)
			assertPaths(t, "unchanged", unchanged, c.want.unchanged...)
			assertPaths(t, "toRemove", toRemove, c.want.removed...)
			assertUntouched(t, r, c.build().content)
		})
	}
}

func TestApplyParseResult(t *testing.T) {
	r := newRepository(nil, "library")
	var counts scanCounts
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	assertCounts := func(added, updated, failed int64) {
		t.Helper()
		if counts.added.Load() != added || counts.updated.Load() != updated || counts.failed.Load() != failed {
			t.Fatalf("counts = %d/%d/%d, want %d/%d/%d",
				counts.added.Load(), counts.updated.Load(), counts.failed.Load(), added, updated, failed)
		}
	}

	if parentID := applyParseResult(r, "library", fsFile("/lib/s/new.cbz", baseTime, 5), nil, true, logf, &counts, &Counts{}); parentID != nil {
		t.Fatalf("parentID = %v, want nil", *parentID)
	}
	if len(r.content) != 0 || len(r.deletedContent) != 0 || len(r.dirtyIDs) != 0 {
		t.Fatalf("content = %d, deletedContent = %d, dirtyIDs = %v", len(r.content), len(r.deletedContent), r.dirtyIDs)
	}
	assertCounts(0, 0, 1)

	file := fsFile("/lib/s/ch1.cbz", baseTime, 10)
	parentID := applyParseResult(r, "library", file, parsedComic(file, "ch1"), true, logf, &counts, &Counts{})
	if len(r.content) != 2 || r.content[0].Type != "comic_series" {
		t.Fatalf("content = %+v", r.content)
	}
	if parentID == nil || *parentID != r.content[0].ID {
		t.Fatalf("parentID = %v, want %s", parentID, r.content[0].ID)
	}
	if child := r.content[1]; !child.Valid || *child.FileURI != file.Path || child.URI != "comic/Series/ch1" {
		t.Fatalf("child = %+v", child)
	}
	assertCounts(1, 0, 1)
	if len(logs) != 0 {
		t.Fatalf("logs = %v", logs)
	}

	dupe := fsFile("/lib/s/ch1 (v2).cbz", baseTime, 10)
	parentID = applyParseResult(r, "library", dupe, parsedComic(dupe, "ch1"), true, logf, &counts, &Counts{})
	if parentID == nil || *parentID != r.content[0].ID {
		t.Fatalf("parentID = %v, want %s", parentID, r.content[0].ID)
	}
	if len(r.content) != 2 || len(r.deletedContent) != 0 || *r.content[1].FileURI != file.Path {
		t.Fatalf("content = %+v, deletedContent = %d", r.content, len(r.deletedContent))
	}
	assertCounts(1, 0, 2)
	if len(logs) != 1 || !strings.Contains(logs[0], dupe.Path) || !strings.Contains(logs[0], "comic/Series/ch1") {
		t.Fatalf("logs = %v", logs)
	}
}

func TestInvalidFileRecovers(t *testing.T) {
	path := "/lib/books/story.epub"
	r := newRepository(nil, "library")
	r.content = []models.Content{testLeaf("book1", "book", path, baseTime, 10, true)}

	if parentID := r.invalidateFile(path); parentID != nil {
		t.Fatalf("parentID = %v, want nil", *parentID)
	}
	if r.content[0].Valid || !r.dirtyIDs["book1"] {
		t.Fatalf("row = %+v, dirtyIDs = %v", r.content[0], r.dirtyIDs)
	}

	files := []FSFile{fsFile(path, baseTime, 10)}
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, nil, nil, false)
	assertPaths(t, "toAdd", toAdd)
	assertPaths(t, "toUpdate", toUpdate, path)
	assertPaths(t, "unchanged", unchanged)
	assertPaths(t, "toRemove", toRemove)

	parsed := &ParsedItem{
		File:        files[0],
		URIPrefix:   "book",
		ContentType: "book",
		URIPart:     "story",
		OrderParts:  []*float32{new(float32(1))},
		FileData:    json.RawMessage(`{"ok":true}`),
		MetaRaw:     models.Metadata{Title: "Story"},
	}
	var counts scanCounts
	if parentID := applyParseResult(r, "library", files[0], parsed, false, func(string, ...any) {}, &counts, &Counts{}); parentID != nil {
		t.Fatalf("parentID = %v, want nil", *parentID)
	}
	if counts.updated.Load() != 1 || counts.added.Load() != 0 || counts.failed.Load() != 0 {
		t.Fatalf("counts = %d/%d/%d, want 0/1/0", counts.added.Load(), counts.updated.Load(), counts.failed.Load())
	}
	if len(r.content) != 1 || len(r.deletedContent) != 0 {
		t.Fatalf("content = %d, deletedContent = %d", len(r.content), len(r.deletedContent))
	}
	if c := r.content[0]; c.ID != "book1" || !c.Valid || string(c.FileData) != `{"ok":true}` {
		t.Fatalf("row = %+v", c)
	}
}

func TestUpdateGroupSeriesRetainsInvalidChildren(t *testing.T) {
	cases := []struct {
		kind       string
		scanner    FileScanner
		firstPath  string
		secondPath string
		seriesDir  bool
	}{
		{"comic", &ComicsScanner{}, "/lib/s/ch1.cbz", "/lib/s/ch2.cbz", true},
		{"book", &BooksScanner{}, "/lib/s/one.epub", "/lib/s/two.epub", false},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			invalid := testLeaf("first", c.kind, c.firstPath, baseTime, 10, false)
			invalid.URI = c.kind + "/s/first"
			invalid.ParentID = new("p")
			invalid.OrderParts = []*float32{new(float32(1))}
			invalid.CoverURI = new(c.firstPath + "/p1.jpg")
			invalid.FileData = json.RawMessage(`{"pages":1}`)

			valid := testLeaf("second", c.kind, c.secondPath, baseTime.Add(time.Hour), 20, true)
			valid.URI = c.kind + "/s/second"
			valid.ParentID = new("p")
			valid.OrderParts = []*float32{new(float32(2))}
			valid.CoverURI = new(c.secondPath + "/p1.jpg")

			var seriesFileURI *string
			if c.seriesDir {
				seriesFileURI = new(t.TempDir())
			}
			r := newRepository(nil, "library")
			r.content = []models.Content{
				{ID: "p", LibraryID: "library", Type: c.kind + "_series", URIPart: "s", URI: c.kind + "/s", Valid: true, FileURI: seriesFileURI},
				valid,
				invalid,
			}
			r.metadata = []*metadataRow{
				{URI: invalid.URI, LibraryID: "library", DataRaw: rawMeta(models.Metadata{
					Publisher: "Retained Press", Genre: "Retained Genre", Series: "Retained Series",
				})},
				{URI: valid.URI, LibraryID: "library", DataRaw: rawMeta(models.Metadata{
					Publisher: "Other Press", Genre: "Other Genre", Series: "Other Series",
				})},
			}

			updateGroupSeries(c.scanner, r, map[string]bool{"p": true})

			if len(r.content) != 3 || len(r.deletedContent) != 0 {
				t.Fatalf("content = %d, deletedContent = %d", len(r.content), len(r.deletedContent))
			}
			items := r.childrenOf("p")
			byID := map[string]*models.Content{}
			for _, item := range items {
				byID[item.ID] = item
			}
			first, second := byID["first"], byID["second"]
			if len(items) != 2 || first == nil || second == nil {
				t.Fatalf("children = %+v", items)
			}
			if first.Order == nil || *first.Order != 0 || second.Order == nil || *second.Order != 1 {
				t.Fatalf("order = %v, %v", first.Order, second.Order)
			}
			if first.Valid || *first.FileURI != c.firstPath || *first.FileSize != 10 ||
				!first.FileMtime.Equal(baseTime) || string(first.FileData) != `{"pages":1}` {
				t.Fatalf("invalid child = %+v", first)
			}

			meta := r.getMetadata(c.kind + "/s").DataRaw.Merge()
			if meta.Publisher != "Retained Press" || meta.Genre != "Retained Genre" || meta.Title != "Retained Series" {
				t.Fatalf("series metadata = %+v", meta)
			}
			series := &r.content[0]
			if series.CoverURI == nil || *series.CoverURI != c.firstPath+"/p1.jpg" {
				t.Fatalf("series cover = %v", series.CoverURI)
			}
			if c.seriesDir && (series.FileMtime == nil || !series.FileMtime.Equal(baseTime)) {
				t.Fatalf("series mtime = %v", series.FileMtime)
			}
		})
	}
}

func TestApplyParseResultRejectsSeriesKeyHeldByLeaf(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "books")
	standalone := "/lib/Books/Foo_bar.epub"
	seedContent(t, pool, models.Content{
		ID: "b1", LibraryID: lib, Type: "book", URI: "book/Foo_bar", URIPart: "Foo_bar",
		Valid: true, FileURI: new(standalone), FileMtime: &baseTime, FileSize: new(10),
	})

	r := newRepository(pool, lib)
	if err := r.load(context.Background()); err != nil {
		t.Fatalf("load: %v", err)
	}

	member := fsFile("/lib/Foo bar/x.epub", baseTime, 10)
	item := classifyBook(member, epub.Metadata{Title: "X", Series: "Foo/bar"}, false)
	if item.Series.URIPart != "Foo_bar" {
		t.Fatalf("series part = %q, want the sanitized collision", item.Series.URIPart)
	}

	var counts scanCounts
	var logs []string
	logf := func(format string, args ...any) { logs = append(logs, fmt.Sprintf(format, args...)) }
	applyParseResult(r, lib, member, &item, true, logf, &counts, &Counts{})
	if err := r.commitGroup(context.Background()); err != nil {
		t.Fatalf("commit: %v", err)
	}

	book := readContent(t, pool, "b1")
	if deref(book.FileURI) != standalone || book.Type != "book" || !book.Valid {
		t.Fatalf("book = %+v, want the existing row untouched", book)
	}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, []string{"book/Foo_bar"}) {
		t.Fatalf("uris = %v, want nothing placed beneath the book", got)
	}
	if counts.added.Load() != 0 || counts.failed.Load() != 1 || len(logs) != 1 {
		t.Fatalf("counts = %d added / %d failed, logs = %v",
			counts.added.Load(), counts.failed.Load(), logs)
	}
}

func seedSeriesTitle(r *repository, uri, title string) {
	r.getMetadata(uri).DataRaw.File = &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: title}}
}

func comicItem(dir, number string, meta models.Metadata) (FSFile, *ParsedItem) {
	meta.Number = number
	file := fsFile(dir+"/ch"+number+".cbz", baseTime, 10)
	return file, classifyComic(file, meta, 0, testPages)
}

func TestInheritedTitlePrefersExplicitChildSeriesOverFolderFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		child int
		layer string
	}{
		{"comicinfo on the first chapter", 0, "file"},
		{"comicinfo on the second chapter", 1, "file"},
		{"override on the first chapter", 0, "overrides"},
		{"override on the second chapter", 1, "overrides"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRepository(nil, "library")
			var counts scanCounts
			var parentID *string
			paths := make([]string, 2)
			for i := range 2 {
				var meta models.Metadata
				if tc.layer == "file" && tc.child == i {
					meta.Series = "Curated"
				}
				file, item := comicItem("/lib/Series", fmt.Sprint(i+1), meta)
				paths[i] = file.Path
				parentID = applyParseResult(r, "library", file, item, true, func(string, ...any) {}, &counts, &Counts{})
			}
			if parentID == nil {
				t.Fatalf("no series, failed = %d", counts.failed.Load())
			}
			if tc.layer == "overrides" {
				child := r.findContentByFileURI(paths[tc.child])
				r.getMetadata(child.URI).DataRaw.Overrides = &metaraw.RawContainer[models.Metadata]{
					Raw: models.Metadata{Series: "Curated"},
				}
			}

			i := slices.IndexFunc(r.content, func(c models.Content) bool { return c.ID == *parentID })
			seedSeriesTitle(r, r.content[i].URI, "Stale")
			updateGroupSeries(&ComicsScanner{}, r, map[string]bool{*parentID: true})

			if got := r.getMetadata(r.content[i].URI).DataRaw.File.Raw.Title; got != "Curated" {
				t.Fatalf("series title = %q, want the explicit child series to beat the folder fallback", got)
			}
		})
	}
}

func TestInheritedTitleFallsBackToTheSeriesKeyWhenTheFolderDiffers(t *testing.T) {
	r := newRepository(nil, "library")
	var counts scanCounts
	file, item := comicItem("/lib/Foo (2019)", "1", models.Metadata{})
	parentID := applyParseResult(r, "library", file, item, true, func(string, ...any) {}, &counts, &Counts{})
	if parentID == nil {
		t.Fatalf("no series, failed = %d", counts.failed.Load())
	}
	seedSeriesTitle(r, "comic/Foo_2019", "Stale")
	updateGroupSeries(&ComicsScanner{}, r, map[string]bool{*parentID: true})

	if got := r.getMetadata("comic/Foo_2019").DataRaw.File.Raw.Title; got != "Foo_2019" {
		t.Fatalf("series title = %q, want the series key", got)
	}
}
