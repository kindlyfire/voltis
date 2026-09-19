package scanner

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"voltis/models"
	"voltis/models/metaraw"
)

var baseTime = time.Unix(1700000000, 0).UTC()

func leafRow(id, typ, path string, mtime time.Time, size int, valid bool) models.Content {
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
	if len(r.contentD) != 0 {
		t.Fatalf("contentD = %d rows, want 0", len(r.contentD))
	}
	if len(r.dirtyIDs) != 0 {
		t.Fatalf("dirtyIDs = %v, want empty", r.dirtyIDs)
	}
}

func TestMatchFilesFilterBoundaries(t *testing.T) {
	newRepo := func() *repository {
		r := newRepository(nil, "library")
		r.content = []models.Content{
			leafRow("a", "comic", "/lib/Foo/ch1.cbz", baseTime, 10, true),
			leafRow("b", "comic", "/lib/Foo/ch2.cbz", baseTime, 10, true),
			leafRow("c", "comic", "/lib/Foo Extra/ch3.cbz", baseTime, 10, true),
			leafRow("d", "comic", "/lib/Foo/Sub/ch4.cbz", baseTime, 10, true),
		}
		return r
	}
	files := []FSFile{
		fsFile("/lib/Foo/ch1.cbz", baseTime, 10),
		fsFile("/lib/Foo/new.cbz", baseTime, 10),
		fsFile("/lib/Foo Extra/ch5.cbz", baseTime, 10),
	}
	r := newRepo()

	for _, filters := range [][]string{{"/lib/Foo"}, {"/lib/Foo", "/lib/Foo/Sub"}} {
		label := fmt.Sprint(filters)
		toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, filters, nil, false)
		assertPaths(t, label+" toAdd", toAdd, "/lib/Foo/new.cbz")
		assertPaths(t, label+" toUpdate", toUpdate)
		assertPaths(t, label+" unchanged", unchanged, "/lib/Foo/ch1.cbz")
		assertPaths(t, label+" toRemove", toRemove, "/lib/Foo/ch2.cbz", "/lib/Foo/Sub/ch4.cbz")
		assertUntouched(t, r, newRepo().content)
	}

	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, []string{"/lib/Foo Extra"}, nil, false)
	assertPaths(t, "sibling toAdd", toAdd, "/lib/Foo Extra/ch5.cbz")
	assertPaths(t, "sibling toUpdate", toUpdate)
	assertPaths(t, "sibling unchanged", unchanged)
	assertPaths(t, "sibling toRemove", toRemove, "/lib/Foo Extra/ch3.cbz")
	assertUntouched(t, r, newRepo().content)

	exact := newRepository(nil, "library")
	exact.content = []models.Content{
		leafRow("a", "comic", "/lib/Foo/ch1.cbz", baseTime, 10, true),
		leafRow("b", "comic", "/lib/Foo/ch1.cbz.bak", baseTime, 10, true),
	}
	toAdd, toUpdate, unchanged, toRemove = matchFiles(exact, nil, []string{"/lib/Foo/ch1.cbz"}, nil, false)
	assertPaths(t, "exact toAdd", toAdd)
	assertPaths(t, "exact toUpdate", toUpdate)
	assertPaths(t, "exact unchanged", unchanged)
	assertPaths(t, "exact toRemove", toRemove, "/lib/Foo/ch1.cbz")
}

func TestMatchFilesFailedScopes(t *testing.T) {
	newRepo := func() *repository {
		r := newRepository(nil, "library")
		r.content = []models.Content{
			leafRow("a", "comic", "/lib/Broken/ch1.cbz", baseTime, 10, true),
			leafRow("b", "comic", "/lib/Broken/Sub/ch2.cbz", baseTime, 10, true),
			leafRow("c", "comic", "/lib/Odd/ch9.cbz", baseTime, 10, true),
			leafRow("d", "comic", "/lib/Broken Extra/ch3.cbz", baseTime, 10, true),
			leafRow("e", "comic", "/lib/Ok/ch4.cbz", baseTime, 10, true),
			leafRow("f", "comic", "/lib/Broken/ch5.cbz", baseTime, 10, true),
		}
		return r
	}
	r := newRepo()

	changed := []FSFile{fsFile("/lib/Broken/ch5.cbz", baseTime.Add(time.Hour), 20)}
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, changed, nil, []string{"/lib/Broken", "/lib/Odd/ch9.cbz"}, false)
	assertPaths(t, "toAdd", toAdd)
	assertPaths(t, "toUpdate", toUpdate, "/lib/Broken/ch5.cbz")
	assertPaths(t, "unchanged", unchanged)
	assertPaths(t, "toRemove", toRemove, "/lib/Broken Extra/ch3.cbz", "/lib/Ok/ch4.cbz")
	assertUntouched(t, r, newRepo().content)

	same := []FSFile{fsFile("/lib/Broken/ch5.cbz", baseTime, 10)}
	toAdd, toUpdate, unchanged, toRemove = matchFiles(r, same, []string{"/lib/Broken", "/lib/Broken Extra"}, []string{"/lib/Broken"}, false)
	assertPaths(t, "filtered toAdd", toAdd)
	assertPaths(t, "filtered toUpdate", toUpdate)
	assertPaths(t, "filtered unchanged", unchanged, "/lib/Broken/ch5.cbz")
	assertPaths(t, "filtered toRemove", toRemove, "/lib/Broken Extra/ch3.cbz")
	assertUntouched(t, r, newRepo().content)
}

func TestMatchFilesInvalidRetries(t *testing.T) {
	newRepo := func() *repository {
		r := newRepository(nil, "library")
		r.content = []models.Content{
			leafRow("valid", "comic", "/lib/s/a.cbz", baseTime, 10, true),
			leafRow("invalid", "comic", "/lib/s/b.cbz", baseTime, 10, false),
			leafRow("changed", "comic", "/lib/s/c.cbz", baseTime, 10, true),
			leafRow("gone", "comic", "/lib/s/d.cbz", baseTime, 10, false),
		}
		return r
	}
	files := []FSFile{
		fsFile("/lib/s/a.cbz", baseTime, 10),
		fsFile("/lib/s/b.cbz", baseTime, 10),
		fsFile("/lib/s/c.cbz", baseTime, 20),
	}
	r := newRepo()

	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, nil, nil, false)
	assertPaths(t, "toAdd", toAdd)
	assertPaths(t, "toUpdate", toUpdate, "/lib/s/b.cbz", "/lib/s/c.cbz")
	assertPaths(t, "unchanged", unchanged, "/lib/s/a.cbz")
	assertPaths(t, "toRemove", toRemove, "/lib/s/d.cbz")
	assertUntouched(t, r, newRepo().content)

	toAdd, toUpdate, unchanged, toRemove = matchFiles(r, files, nil, nil, true)
	assertPaths(t, "forced toAdd", toAdd)
	assertPaths(t, "forced toUpdate", toUpdate, "/lib/s/a.cbz", "/lib/s/b.cbz", "/lib/s/c.cbz")
	assertPaths(t, "forced unchanged", unchanged)
	assertPaths(t, "forced toRemove", toRemove, "/lib/s/d.cbz")
	assertUntouched(t, r, newRepo().content)

	toAdd, toUpdate, unchanged, toRemove = matchFiles(r, files, nil, []string{"/lib/s/d.cbz"}, false)
	assertPaths(t, "protected toAdd", toAdd)
	assertPaths(t, "protected toUpdate", toUpdate, "/lib/s/b.cbz", "/lib/s/c.cbz")
	assertPaths(t, "protected unchanged", unchanged, "/lib/s/a.cbz")
	assertPaths(t, "protected toRemove", toRemove)
	assertUntouched(t, r, newRepo().content)
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

	if parentID := applyParseResult(r, "library", fsFile("/lib/s/new.cbz", baseTime, 5), nil, true, logf, &counts); parentID != nil {
		t.Fatalf("parentID = %v, want nil", *parentID)
	}
	if len(r.content) != 0 || len(r.contentD) != 0 || len(r.dirtyIDs) != 0 {
		t.Fatalf("content = %d, contentD = %d, dirtyIDs = %v", len(r.content), len(r.contentD), r.dirtyIDs)
	}
	assertCounts(0, 0, 1)

	file := fsFile("/lib/s/ch1.cbz", baseTime, 10)
	parentID := applyParseResult(r, "library", file, parsedComic(file, "ch1"), true, logf, &counts)
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
	parentID = applyParseResult(r, "library", dupe, parsedComic(dupe, "ch1"), true, logf, &counts)
	if parentID == nil || *parentID != r.content[0].ID {
		t.Fatalf("parentID = %v, want %s", parentID, r.content[0].ID)
	}
	if len(r.content) != 2 || len(r.contentD) != 0 || *r.content[1].FileURI != file.Path {
		t.Fatalf("content = %+v, contentD = %d", r.content, len(r.contentD))
	}
	assertCounts(1, 0, 2)
	if len(logs) != 1 || !strings.Contains(logs[0], dupe.Path) || !strings.Contains(logs[0], "comic/Series/ch1") {
		t.Fatalf("logs = %v", logs)
	}
}

func TestInvalidFileRecovers(t *testing.T) {
	path := "/lib/books/story.epub"
	r := newRepository(nil, "library")
	r.content = []models.Content{leafRow("book1", "book", path, baseTime, 10, true)}

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
	if parentID := applyParseResult(r, "library", files[0], parsed, false, func(string, ...any) {}, &counts); parentID != nil {
		t.Fatalf("parentID = %v, want nil", *parentID)
	}
	if counts.updated.Load() != 1 || counts.added.Load() != 0 || counts.failed.Load() != 0 {
		t.Fatalf("counts = %d/%d/%d, want 0/1/0", counts.added.Load(), counts.updated.Load(), counts.failed.Load())
	}
	if len(r.content) != 1 || len(r.contentD) != 0 {
		t.Fatalf("content = %d, contentD = %d", len(r.content), len(r.contentD))
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
			invalid := leafRow("first", c.kind, c.firstPath, baseTime, 10, false)
			invalid.URI = c.kind + "/s/first"
			invalid.ParentID = new("p")
			invalid.OrderParts = []*float32{new(float32(1))}
			invalid.CoverURI = new(c.firstPath + "/p1.jpg")
			invalid.FileData = json.RawMessage(`{"pages":1}`)

			valid := leafRow("second", c.kind, c.secondPath, baseTime.Add(time.Hour), 20, true)
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

			if len(r.content) != 3 || len(r.contentD) != 0 {
				t.Fatalf("content = %d, contentD = %d", len(r.content), len(r.contentD))
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
