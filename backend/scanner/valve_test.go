package scanner

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"voltis/models"
)

func realTempDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	must(t, err)
	return root
}

func walkInto(t *testing.T, w *writer, roots ...string) {
	t.Helper()
	for _, ev := range mustWalk(t, roots, isComicFile) {
		w.event(ev)
	}
}

func TestWriterValveStopsRemovalsAfterAnUnverifiedListing(t *testing.T) {
	w := testWriter([]Fingerprint{
		leafFP("l1", "/lib/S/ch1.cbz", "ch1", "p1"),
		leafFP("l2", "/lib/T/ch1.cbz", "ch1", "p2"),
	}, nil)

	w.event(listedEvent(t, w, "/lib/S"))
	if !slices.Equal(slices.Sorted(maps.Keys(w.gone)), []string{"l1"}) {
		t.Fatalf("gone = %v, want the verified listing to prove l1 absent", w.gone)
	}

	w.event(Event{Kind: Listed, Path: "/lib/T"})
	if w.trust {
		t.Fatal("an unverified listing must stop the scan from removing anything")
	}
	if len(w.gone) != 0 {
		t.Fatalf("gone = %v, want the earlier proof dropped with the valve open", w.gone)
	}

	w.event(listedEvent(t, w, "/lib/S"))
	if len(w.gone) != 0 {
		t.Fatalf("gone = %v, want no further proof accepted", w.gone)
	}

	f := w.take(true)
	if !f.keep {
		t.Fatal("the final flush must carry the valve")
	}
	if len(f.sets) != 0 {
		t.Fatalf("final flush = %v, want nothing to delete", f.sets)
	}
}

func TestWriterValveDetectsADirectoryRetargetedAfterIndexing(t *testing.T) {
	for _, c := range []struct {
		name   string
		walked func(series string) string
	}{
		{"stored spelling", func(series string) string { return series }},
		{"different spelling", func(series string) string { return series + string(filepath.Separator) + "." }},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, series, w := retargetFixture(t)

			walkInto(t, w, c.walked(series))

			if w.trust {
				t.Fatal("a directory that resolves elsewhere than it was indexed must open the valve")
			}
			if len(w.gone) != 0 {
				t.Fatalf("gone = %v, want nothing proven absent", w.gone)
			}
			assertStoredRowUntouched(t, w, series)
		})
	}
}

func TestScanValveKeepsRowsWhoseFileMovedDuringTheScan(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	root := realTempDir(t)

	writeFile(t, filepath.Join(root, "A", "ch1.cbz"), "one")
	must(t, os.MkdirAll(filepath.Join(root, "B"), 0o755))
	alias := filepath.Join(root, "Alias")
	symlink(t, filepath.Join(root, "A"), alias)

	stored := filepath.Join(alias, "ch1.cbz")
	file := statFile(t, stored)
	mtime := file.Mtime.UTC()
	seedContent(t, pool,
		models.Content{ID: "p1", LibraryID: lib, Type: "comic_series", URI: "comic/A", URIPart: "A",
			Valid: true, FileURI: new(alias)},
		models.Content{ID: "p2", LibraryID: lib, Type: "comic_series", URI: "comic/Empty", URIPart: "Empty",
			Valid: true},
		models.Content{ID: "l1", LibraryID: lib, Type: "comic", URI: "comic/A/ch1", URIPart: "ch1", Valid: true,
			FileURI: new(stored), FileMtime: &mtime, FileSize: new(int(file.Size)), ParentID: new("p1")},
	)

	r := newScanRun(t, pool, lib, &ComicsScanner{})

	must(t, os.Rename(filepath.Join(root, "A", "ch1.cbz"), filepath.Join(root, "B", "ch1.cbz")))
	must(t, os.Remove(alias))
	symlink(t, filepath.Join(root, "B"), alias)

	walkInto(t, r.w, filepath.Join(root, "A"))

	if r.w.trust {
		t.Fatal("a row whose stored path now resolves elsewhere must open the valve")
	}
	counts := r.commit(true)
	if counts.Removed != 0 {
		t.Fatalf("counts = %+v, want a successful scan with no removals", counts)
	}
	want := []string{"comic/A", "comic/A/ch1", "comic/Empty"}
	assertCatalog(t, pool, lib, want)
	if at := libraryScannedAt(t, pool, lib); at == nil {
		t.Fatal("scanned_at must still advance")
	}
}

func TestScanPipelineRemovesDescendantsOfADirectoryReplacedByAFile(t *testing.T) {
	for _, rel := range []string{"ch1.cbz", "Sub/ch1.cbz"} {
		t.Run(rel, func(t *testing.T) {
			p := newPipeline(t, "comics")
			dir := filepath.Join(p.root, "S")
			writeCBZFixture(t, filepath.Join(dir, filepath.FromSlash(rel)), "S", "1")
			if r := p.mustScan(ScanInput{}); r.Added != 1 {
				t.Fatalf("seed scan = %+v", r)
			}

			must(t, os.RemoveAll(dir))
			writeFile(t, dir, "no longer a directory")

			if r := p.mustScan(ScanInput{}); r.Removed != 1 {
				t.Fatalf("rescan = %+v, want the orphaned chapter removed", r)
			}
			assertCatalog(t, p.pool, p.lib, nil)
		})
	}
}

func retargetFixture(t *testing.T) (string, string, *writer) {
	t.Helper()
	root := realTempDir(t)
	writeFile(t, filepath.Join(root, "Lib", "Series", "ch1.cbz"), "one")
	writeFile(t, filepath.Join(root, "Other", "Series", "ch1.cbz"), "another one")
	alias := filepath.Join(root, "Alias")
	symlink(t, filepath.Join(root, "Lib"), alias)

	series := filepath.Join(alias, "Series")
	w := testWriter([]Fingerprint{leafFP("l1", filepath.Join(root, "Lib", "Series", "ch1.cbz"), "ch1", "p1")},
		[]SeriesRef{seriesRefOf("p1", "Series", series)})

	must(t, os.Remove(alias))
	symlink(t, filepath.Join(root, "Other"), alias)
	return root, series, w
}

func assertStoredRowUntouched(t *testing.T, w *writer, series string) {
	t.Helper()
	file := filepath.Join(series, "ch1.cbz")
	if _, ok := w.at(file); ok {
		t.Fatal("a file behind the retargeted alias was matched to the stored row")
	}
	if id, ok := w.dirSeries(series); ok {
		t.Fatalf("the retargeted directory still resolves to series %s", id)
	}
	w.place(comicResult(file, "ch1", "Series", series))
	for _, s := range w.sets {
		if slices.Contains(writeIDs(s), "l1") {
			t.Fatal("placement scheduled a write over the stored row behind the retargeted alias")
		}
	}
}

func TestWriterValveRejectsAnIdentityItCouldNotVerify(t *testing.T) {
	_, series, w := retargetFixture(t)

	w.event(Event{Kind: Listed, Path: series, Names: []string{"ch1.cbz"}, Files: []string{"ch1.cbz"}})

	if w.trust {
		t.Fatal("an unverified listing must open the valve")
	}
	assertStoredRowUntouched(t, w, series)
}

func TestWalkerIdentityRejectsAHandleWhosePathWasRetargeted(t *testing.T) {
	root := realTempDir(t)
	writeFile(t, filepath.Join(root, "Lib", "Series", "ch1.cbz"), "one")
	writeFile(t, filepath.Join(root, "Other", "Series", "ch1.cbz"), "another one")
	alias := filepath.Join(root, "Alias")
	symlink(t, filepath.Join(root, "Lib"), alias)

	series := filepath.Join(alias, "Series")
	f, err := os.Open(series)
	must(t, err)
	defer f.Close()

	w := walker{res: newResolver()}
	if id := w.identity(f, series); id != filepath.Join(root, "Lib", "Series") {
		t.Fatalf("identity = %q, want the directory the handle was opened on", id)
	}

	must(t, os.Remove(alias))
	symlink(t, filepath.Join(root, "Other"), alias)

	if id := w.identity(f, series); id != "" {
		t.Fatalf("identity = %q, want a retained handle whose path now names another directory rejected", id)
	}
}

func TestWriterValveRejectsAFileProofOvertakenByADirectory(t *testing.T) {
	root := realTempDir(t)
	blocker := filepath.Join(root, "S")
	writeFile(t, blocker, "not a directory")

	w := testWriter([]Fingerprint{leafFP("l1", filepath.Join(blocker, "Sub", "ch1.cbz"), "ch1", "p1")},
		[]SeriesRef{seriesRefOf("p1", "S", "")})
	if !slices.Equal(w.behind[blocker], []string{"l1"}) {
		t.Fatalf("behind = %v, want l1 indexed behind the file that replaced its directory", w.behind)
	}

	ev := Event{Kind: Listed, Path: root, Dir: mustResolveDir(t, w.res, root),
		Names: []string{"S"}, Files: []string{"S"}}

	must(t, os.Remove(blocker))
	writeFile(t, filepath.Join(blocker, "Sub", "ch1.cbz"), "one")
	must(t, os.Chmod(blocker, 0o000))
	t.Cleanup(func() { os.Chmod(blocker, 0o755) })
	if f, err := os.Open(filepath.Join(blocker, "Sub")); err == nil {
		f.Close()
		t.Skip("the unreadable directory is still traversable")
	}

	w.event(ev)

	if len(w.gone) != 0 {
		t.Fatalf("gone = %v, want a proof overtaken by a directory rejected", w.gone)
	}
	if w.trust {
		t.Fatal("a file proof overtaken by a directory must open the valve")
	}
}
