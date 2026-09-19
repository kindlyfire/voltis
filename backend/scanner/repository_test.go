package scanner

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"voltis/models"
)

func populatedLeaf() models.Content {
	c := leafRow("leaf", "comic", "/lib/s/ch1.cbz", baseTime, 42, true)
	c.URI = "comic/s/ch1"
	c.URIPart = "ch1"
	c.ParentID = new("parent")
	c.CoverURI = new("/lib/s/ch1.cbz/p1.jpg")
	c.Order = new(3)
	c.OrderParts = []*float32{new(float32(1)), new(float32(1.5))}
	c.FileData = json.RawMessage(`{"pages":[["p1.jpg",100,200]]}`)
	return c
}

func TestInvalidatedRowLifecycle(t *testing.T) {
	newRepo := func() *repository {
		r := newRepository(nil, "library")
		r.content = []models.Content{
			{ID: "parent", LibraryID: "library", Type: "comic_series", URIPart: "s", URI: "comic/s", Valid: true},
			populatedLeaf(),
			leafRow("sibling", "comic", "/lib/s/ch2.cbz", baseTime, 10, true),
		}
		r.metadata = []*metadataRow{{
			URI:       "comic/s/ch1",
			LibraryID: "library",
			DataRaw:   rawMeta(models.Metadata{Title: "Ch. 1"}),
		}}
		return r
	}

	r := newRepo()
	var counts scanCounts

	if parentID := r.invalidateFile("/lib/s/missing.cbz"); parentID != nil {
		t.Fatalf("unknown path: parentID = %v, want nil", *parentID)
	}
	assertUntouched(t, r, newRepo().content)

	before := populatedLeaf()
	parentID := applyParseResult(r, "library", fsFile("/lib/s/ch1.cbz", baseTime, 42), nil, false, func(string, ...any) {}, &counts)
	if parentID == nil || *parentID != "parent" {
		t.Fatalf("parentID = %v, want parent", parentID)
	}
	if counts.failed.Load() != 1 || counts.added.Load() != 0 || counts.updated.Load() != 0 {
		t.Fatalf("counts = %d/%d/%d, want 0/0/1", counts.added.Load(), counts.updated.Load(), counts.failed.Load())
	}
	if len(r.content) != 3 || len(r.contentD) != 0 {
		t.Fatalf("content = %d, contentD = %d", len(r.content), len(r.contentD))
	}

	got := r.content[1]
	want := before
	want.Valid = false
	want.UpdatedAt = got.UpdatedAt
	if !reflect.DeepEqual(got, want) || !got.UpdatedAt.After(before.UpdatedAt) {
		t.Fatalf("row = %+v, want %+v", got, want)
	}
	if !reflect.DeepEqual(r.content[2], newRepo().content[2]) {
		t.Fatalf("sibling = %+v", r.content[2])
	}
	if !r.dirtyIDs["leaf"] || len(r.dirtyIDs) != 1 {
		t.Fatalf("dirtyIDs = %v", r.dirtyIDs)
	}
	if len(r.metadata) != 1 || r.metadata[0].dirty || r.metadata[0].DataRaw.File.Raw.Title != "Ch. 1" {
		t.Fatalf("metadata = %+v", r.metadata[0])
	}

	conflicting := &ParsedItem{File: fsFile("/lib/s/ch1 (v2).cbz", baseTime, 10), URIPart: "ch1"}
	if r.checkURIAvailable(conflicting, new("parent")) {
		t.Fatal("invalid row should still reserve its uri_part")
	}
	same := &ParsedItem{File: fsFile("/lib/s/ch1.cbz", baseTime, 10), URIPart: "ch1"}
	if !r.checkURIAvailable(same, new("parent")) {
		t.Fatal("original file path should remain permitted")
	}

	time.Sleep(time.Millisecond)
	parentID = r.invalidateFile("/lib/s/ch1.cbz")
	if parentID == nil || *parentID != "parent" {
		t.Fatalf("parentID = %v, want parent", parentID)
	}
	if len(r.content) != 3 || len(r.contentD) != 0 || r.content[1].Valid || !r.content[1].UpdatedAt.After(got.UpdatedAt) {
		t.Fatalf("content = %+v", r.content)
	}
}
