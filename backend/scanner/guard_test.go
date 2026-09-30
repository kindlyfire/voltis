package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"
)

// writeSeries writes n chapters of the series named after dir, numbered from 1.
func writeSeries(t *testing.T, dir string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		writeCBZFixture(t, chapter(dir, i), filepath.Base(dir), fmt.Sprint(i))
	}
}

func chapter(dir string, i int) string {
	return filepath.Join(dir, fmt.Sprintf("c%03d.cbz", i))
}

func unreadable(t *testing.T, dir string) {
	t.Helper()
	must(t, os.Chmod(dir, 0))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func leafIDs(t *testing.T, p *pipeline) []string {
	t.Helper()
	ids, err := db.SelectScalars[string](context.Background(), p.pool,
		"SELECT id FROM content WHERE library_id = $1 AND type = 'comic'", p.lib)
	must(t, err)
	return ids
}

func TestScanRemovalGuard(t *testing.T) {
	always := models.LibrarySettings{AlwaysRemoveMissing: true}
	p := newPipeline(t, "comics")
	for _, c := range []struct {
		name    string
		seed    map[string]int // directory under the root → chapters
		sources []string       // under the root; the root itself when empty
		change  func(t *testing.T, root string) ScanInput
		added   int
		removed int
		reason  string // how the suppression reason starts, after the root
	}{
		{
			name: "emptied root",
			seed: map[string]int{"S": 3},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "S")))
				return ScanInput{}
			},
			reason: " lists no files (3 of 3 items missing)",
		},
		{
			name: "60 of 100",
			seed: map[string]int{"A": 60, "B": 40},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "A")))
				return ScanInput{}
			},
			reason: " is missing most of its items (60 of 100 items missing)",
		},
		{
			name: "60 of 100 with the switch on",
			seed: map[string]int{"A": 60, "B": 40},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "A")))
				return ScanInput{Settings: always}
			},
			removed: 60,
		},
		{
			name: "2 of 100",
			seed: map[string]int{"S": 100},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.Remove(chapter(filepath.Join(root, "S"), 1)))
				must(t, os.Remove(chapter(filepath.Join(root, "S"), 2)))
				return ScanInput{}
			},
			removed: 2,
		},
		{
			name: "3 of 4",
			seed: map[string]int{"S": 4},
			change: func(t *testing.T, root string) ScanInput {
				for i := 1; i <= 3; i++ {
					must(t, os.Remove(chapter(filepath.Join(root, "S"), i)))
				}
				return ScanInput{}
			},
			removed: 3,
		},
		{
			name: "40 of 60 is under the floor",
			seed: map[string]int{"A": 40, "B": 20},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "A")))
				return ScanInput{}
			},
			removed: 40,
		},
		{
			name: "60 moved into subfolders",
			seed: map[string]int{"S": 100},
			change: func(t *testing.T, root string) ScanInput {
				dir := filepath.Join(root, "S")
				for i := 1; i <= 60; i++ {
					moved := chapter(filepath.Join(dir, fmt.Sprint("sub", i%3)), i)
					must(t, os.MkdirAll(filepath.Dir(moved), 0o755))
					must(t, os.Rename(chapter(dir, i), moved))
				}
				return ScanInput{}
			},
		},
		{
			name: "60 gone while 60 unrelated files appear",
			seed: map[string]int{"A": 60, "B": 40},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "A")))
				fresh := filepath.Join(root, "N")
				writeSeries(t, fresh, 60)
				for i := 1; i <= 60; i++ {
					at := baseTime.Add(time.Duration(i) * time.Hour)
					must(t, os.Chtimes(chapter(fresh, i), at, at))
				}
				return ScanInput{}
			},
			added:  60,
			reason: " is missing most of its items (60 of 100 items missing)",
		},
		{
			name:    "a second root loses 2 of 100 while the first loses 60 of 100",
			seed:    map[string]int{"L/A": 60, "L/B": 40, "M/C": 100},
			sources: []string{"L", "M"},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "L", "A")))
				must(t, os.Remove(chapter(filepath.Join(root, "M", "C"), 1)))
				must(t, os.Remove(chapter(filepath.Join(root, "M", "C"), 2)))
				return ScanInput{}
			},
			reason: string(filepath.Separator) + "L is missing most of its items (60 of 100 items missing)",
		},
		{
			name:    "an unreadable root among intact ones",
			seed:    map[string]int{"L/A": 5, "M/B": 5},
			sources: []string{"L", "M"},
			change: func(t *testing.T, root string) ScanInput {
				unreadable(t, filepath.Join(root, "L"))
				must(t, os.Remove(chapter(filepath.Join(root, "M", "B"), 1)))
				return ScanInput{}
			},
			removed: 1,
		},
		{
			name: "an unreadable subdirectory beside removed items",
			seed: map[string]int{"A": 3, "B": 3},
			change: func(t *testing.T, root string) ScanInput {
				must(t, os.RemoveAll(filepath.Join(root, "A")))
				unreadable(t, filepath.Join(root, "B"))
				return ScanInput{}
			},
			removed: 3,
		},
		{
			name: "a content scan of the series and its present chapters",
			seed: map[string]int{"S": 3},
			change: func(t *testing.T, root string) ScanInput {
				dir := filepath.Join(root, "S")
				must(t, os.Remove(chapter(dir, 1)))
				return ScanInput{FilterPaths: []string{dir, chapter(dir, 2), chapter(dir, 3)}}
			},
			removed: 1,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if strings.Contains(c.name, "unreadable") && os.Getuid() == 0 {
				t.Skip("permissions are not enforced for root")
			}
			p.t, p.root, p.lib = t, t.TempDir(), newTestLibrary(t, p.pool, "comics")
			stored := 0
			for dir, n := range c.seed {
				writeSeries(t, filepath.Join(p.root, dir), n)
				stored += n
			}
			var sources []string
			for _, s := range c.sources {
				sources = append(sources, filepath.Join(p.root, s))
			}
			if got := p.mustScan(ScanInput{Sources: sources}); got.Added != stored {
				t.Fatalf("seed scan = %+v, want %d added", got, stored)
			}
			before := leafIDs(t, p)

			in := c.change(t, p.root)
			in.Sources = sources
			got := p.mustScan(in)
			if got.Added != c.added || got.Removed != c.removed || got.Failed != 0 {
				t.Fatalf("scan = %+v, want %d added, %d removed and none failed", got, c.added, c.removed)
			}
			kept := 0
			for _, id := range leafIDs(t, p) {
				if slices.Contains(before, id) {
					kept++
				}
			}
			if kept != stored-c.removed {
				t.Fatalf("kept %d of %d leaf IDs, want %d", kept, stored, stored-c.removed)
			}

			logs, err := db.SelectScalar[string](context.Background(), p.pool,
				"SELECT coalesce(string_agg(logs, ''), '') FROM tasks WHERE input->>'library_id' = $1", p.lib)
			must(t, err)
			if c.reason == "" {
				if got.RemovalsSuppressed != "" || strings.Contains(logs, "Removals suppressed") {
					t.Fatalf("removals suppressed: %q, log:\n%s", got.RemovalsSuppressed, logs)
				}
				return
			}
			if !strings.HasSuffix(got.RemovalsSuppressed, c.reason) {
				t.Fatalf("removals suppressed = %q, want it to end with %q", got.RemovalsSuppressed, c.reason)
			}
			if !strings.Contains(logs, "Removals suppressed: "+got.RemovalsSuppressed+". Nothing was removed.") {
				t.Fatalf("log does not say why nothing was removed:\n%s", logs)
			}
		})
	}
}

// A suppressed scan leaves a series as it was, even when the directory it is stored in is gone:
// its directory, cover, child order and metadata don't move to the remaining children.
func TestScanSuppressedRemovalsLeaveSeriesUntouched(t *testing.T) {
	p := newPipeline(t, "comics")
	a, z := filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(a, "ch2.cbz"), "Foo", "2")
	writeCBZFixture(t, filepath.Join(z, "ch3.cbz"), "Foo", "3")
	writeFile(t, filepath.Join(z, "cover.jpg"), "z")
	writeSeries(t, filepath.Join(p.root, "B"), 60)
	p.mustScan(ScanInput{})

	series := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	state := func() string {
		t.Helper()
		rows, err := db.SelectScalars[string](context.Background(), p.pool, `
			SELECT c.uri || ' ' || coalesce(c.file_uri, '') || ' ' || coalesce(c.cover_uri, '') || ' ' ||
			       coalesce(c."order"::text, '') || ' ' ||
			       CASE WHEN c.id = $1 THEN c.updated_at::text || ' ' || c.meta_updated_at::text || ' ' ||
			                                 c.data_raw::text || ' ' || c.data::text ELSE '' END
			FROM content c
			WHERE c.id = $1 OR c.parent_id = $1 ORDER BY c.uri`, series)
		must(t, err)
		return strings.Join(rows, "\n")
	}
	want := state()
	if !strings.Contains(want, "comic/Foo "+a+" ") {
		t.Fatalf("series is not stored in %s:\n%s", a, want)
	}

	must(t, os.RemoveAll(a))
	must(t, os.RemoveAll(filepath.Join(p.root, "B")))
	got := p.mustScan(ScanInput{Force: true})
	if got.RemovalsSuppressed == "" || got.Removed != 0 || got.Updated != 1 {
		t.Fatalf("scan = %+v, want the remaining chapter rewritten and nothing removed", got)
	}
	if now := state(); now != want {
		t.Fatalf("series after the suppressed scan:\n%s\nwant:\n%s", now, want)
	}
}
