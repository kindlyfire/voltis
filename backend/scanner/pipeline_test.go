package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/tasks"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type recorder struct {
	mu     sync.Mutex
	events []CatalogChanged
}

func (r *recorder) CatalogChanged(ev CatalogChanged) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *recorder) seqs() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]int, len(r.events))
	for i, ev := range r.events {
		out[i] = ev.CommitSeq
	}
	return out
}

type pipeline struct {
	t       *testing.T
	pool    *pgxpool.Pool
	lib     string
	root    string
	manager *tasks.Manager
	def     *tasks.TaskDef
	notify  *recorder
}

func newPipeline(t *testing.T, libType string) *pipeline {
	t.Helper()
	fastFlushes(t)

	pool := newTestPool(t)
	root := t.TempDir()
	lib := newTestLibrary(t, pool, libType)

	notify := &recorder{}
	manager := tasks.NewManager(pool, func(tasks.Snapshot) {})
	def := NewScanTask(notify)
	manager.Register(def)
	t.Cleanup(manager.Close)

	return &pipeline{t: t, pool: pool, lib: lib, root: root, manager: manager, def: def, notify: notify}
}

func (p *pipeline) push(def *tasks.TaskDef, in ScanInput) (ScanResult, error) {
	p.t.Helper()
	in.LibraryID = p.lib
	if in.Sources == nil {
		in.Sources = []string{p.root}
	}
	if in.LibraryType == "" {
		in.LibraryType = "comics"
	}
	handle, err := p.manager.Push(def, in)
	if err != nil {
		p.t.Fatalf("push: %v", err)
	}
	result, err := handle.Wait()
	if err != nil {
		return ScanResult{}, err
	}
	return result.(ScanResult), nil
}

func (p *pipeline) scan(in ScanInput) (ScanResult, error) {
	p.t.Helper()
	return p.push(p.def, in)
}

func (p *pipeline) mustLegacyScan(in ScanInput) ScanResult {
	p.t.Helper()
	result, err := p.push(ScanTask, in)
	if err != nil {
		p.t.Fatalf("legacy scan: %v", err)
	}
	return result
}

func (p *pipeline) mustScan(in ScanInput) ScanResult {
	p.t.Helper()
	result, err := p.scan(in)
	if err != nil {
		p.t.Fatalf("scan: %v", err)
	}
	return result
}

func TestScanPipelineLeavesPathsLegacyScansStillRecognise(t *testing.T) {
	p := newPipeline(t, "comics")
	t.Chdir(p.root)

	ch1 := filepath.Join("S", "ch1.cbz")
	writeCBZFixture(t, ch1, "S", "1")
	writeCBZFixture(t, filepath.Join("S", "ch2.cbz"), "S", "2")

	if r := p.mustLegacyScan(ScanInput{Sources: []string{"."}}); r.Added != 2 {
		t.Fatalf("legacy seed = %+v", r)
	}

	if r := p.mustScan(ScanInput{Sources: []string{"."}, FilterPaths: []string{ch1}, Force: true}); r.Updated != 1 {
		t.Fatalf("filtered rescan = %+v", r)
	}

	id := contentIDByURI(t, p.pool, p.lib, "comic/S/ch1")
	writeFile(t, ch1, "not a zip")

	r := p.mustLegacyScan(ScanInput{Sources: []string{"."}})
	if r.Removed != 0 || r.Failed != 1 {
		t.Fatalf("legacy rescan = %+v, want the corrupt file invalidated, not removed", r)
	}
	if c := readContent(t, p.pool, id); c.Valid || deref(c.FileURI) != ch1 {
		t.Fatalf("content = %+v, want the original row invalidated in place", c)
	}
}

func TestScanPipelineMatchesRowsStoredWithAnotherSpelling(t *testing.T) {
	p := newPipeline(t, "comics")
	t.Chdir(p.root)

	ch1 := filepath.Join("S", "ch1.cbz")
	writeCBZFixture(t, ch1, "S", "1")
	file := statFile(t, ch1)
	abs, err := filepath.Abs(ch1)
	if err != nil {
		t.Fatal(err)
	}
	mtime := file.Mtime.UTC()
	seedContent(t, p.pool,
		models.Content{ID: "p1", LibraryID: p.lib, Type: "comic_series", URI: "comic/S", URIPart: "S", Valid: true,
			FileURI: new(filepath.Join(p.root, "S"))},
		models.Content{ID: "l1", LibraryID: p.lib, Type: "comic", URI: "comic/S/ch1", URIPart: "ch1", Valid: true,
			FileURI: new(abs), FileMtime: &mtime, FileSize: new(int(file.Size)), ParentID: new("p1")},
	)

	r := p.mustScan(ScanInput{Sources: []string{"."}})
	if r.Unchanged != 1 || r.Added != 0 || r.Updated != 0 || r.Removed != 0 {
		t.Fatalf("scan = %+v, want the absolutely stored row recognised through the relative walk", r)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/S", "comic/S/ch1"})
	if c := readContent(t, p.pool, "l1"); deref(c.FileURI) != abs {
		t.Fatalf("file_uri = %v, want the stored spelling left alone", c.FileURI)
	}
}

func TestScanPipelineComics(t *testing.T) {
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	writeCBZFixture(t, filepath.Join(series, "Foo ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(series, "Foo ch2.cbz"), "Foo", "2")
	writeFile(t, filepath.Join(series, "notes.txt"), "ignored")

	result := p.mustScan(ScanInput{Concurrency: 2})
	if result.Added != 2 || result.Updated != 0 || result.Removed != 0 || result.Failed != 0 {
		t.Fatalf("first scan = %+v", result)
	}
	want := []string{"comic/Foo_2019", "comic/Foo_2019/ch1", "comic/Foo_2019/ch2"}
	assertCatalog(t, p.pool, p.lib, want)
	if meta := readMeta(t, p.pool, p.lib, "comic/Foo_2019").File.Raw; meta.Title != "Foo" {
		t.Fatalf("series metadata = %+v", meta)
	}
	if seqs := p.notify.seqs(); len(seqs) == 0 || seqs[len(seqs)-1] != len(seqs) {
		t.Fatalf("catalog events = %v", seqs)
	}

	unchanged := p.mustScan(ScanInput{})
	if unchanged.Unchanged != 2 || unchanged.Added != 0 || unchanged.Updated != 0 || unchanged.Removed != 0 {
		t.Fatalf("unchanged scan = %+v", unchanged)
	}

	forced := p.mustScan(ScanInput{Force: true})
	if forced.Updated != 2 || forced.Added != 0 || forced.Unchanged != 0 {
		t.Fatalf("forced scan = %+v", forced)
	}

	if err := os.Remove(filepath.Join(series, "Foo ch2.cbz")); err != nil {
		t.Fatal(err)
	}
	removed := p.mustScan(ScanInput{})
	if removed.Removed != 1 || removed.Unchanged != 1 {
		t.Fatalf("removal scan = %+v", removed)
	}
	assertCatalog(t, p.pool, p.lib, want[:2])
}

func TestScanPipelineBooks(t *testing.T) {
	p := newPipeline(t, "books")
	writeEPUBFixture(t, filepath.Join(p.root, "Bar v1.epub"), "Bar Volume 1", "Bar", "1")
	writeEPUBFixture(t, filepath.Join(p.root, "Solo.epub"), "Solo", "", "")

	result := p.mustScan(ScanInput{LibraryType: "books"})
	if result.Added != 2 || result.Failed != 0 {
		t.Fatalf("scan = %+v", result)
	}
	want := []string{"book/Bar", "book/Bar/Bar v1", "book/Solo"}
	assertCatalog(t, p.pool, p.lib, want)
}

func TestScanPipelineRetainsRowsUnderUnreadableDirectories(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	writeCBZFixture(t, filepath.Join(series, "Foo ch1.cbz"), "Foo", "1")

	p.mustScan(ScanInput{})
	before := contentURIs(t, p.pool, p.lib)

	if err := os.Chmod(series, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(series, 0o755) })

	result := p.mustScan(ScanInput{})
	if result.Removed != 0 {
		t.Fatalf("scan = %+v, want nothing removed", result)
	}
	assertCatalog(t, p.pool, p.lib, before)
}

func TestScanPipelineFilterPaths(t *testing.T) {
	p := newPipeline(t, "comics")
	foo := filepath.Join(p.root, "Foo (2019)")
	bar := filepath.Join(p.root, "Bar (2020)")
	writeCBZFixture(t, filepath.Join(foo, "Foo ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(bar, "Bar ch1.cbz"), "Bar", "1")

	result := p.mustScan(ScanInput{FilterPaths: []string{foo}})
	if result.Added != 1 {
		t.Fatalf("filtered scan = %+v", result)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Foo_2019", "comic/Foo_2019/ch1"})

	full := []string{"comic/Bar_2020", "comic/Bar_2020/ch1", "comic/Foo_2019", "comic/Foo_2019/ch1"}
	if got := p.mustScan(ScanInput{}); got.Added != 1 {
		t.Fatalf("full scan = %+v", got)
	}
	assertCatalog(t, p.pool, p.lib, full)

	if err := os.RemoveAll(bar); err != nil {
		t.Fatal(err)
	}
	if result := p.mustScan(ScanInput{FilterPaths: []string{foo}}); result.Removed != 0 {
		t.Fatalf("scan outside the filter removed rows: %+v", result)
	}
	assertCatalog(t, p.pool, p.lib, full)
}

func TestScanPipelineFailedParsesInvalidateAndRetry(t *testing.T) {
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	path := filepath.Join(series, "Foo ch1.cbz")
	writeCBZFixture(t, path, "Foo", "1")
	p.mustScan(ScanInput{})

	id, err := db.SelectScalar[string](context.Background(), p.pool,
		"SELECT id FROM content WHERE library_id = $1 AND file_uri = $2", p.lib, path)
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, path, "corrupt")
	result := p.mustScan(ScanInput{})
	if result.Failed != 1 || result.Added != 0 || result.Updated != 0 {
		t.Fatalf("corrupt scan = %+v", result)
	}
	if row := readContent(t, p.pool, id); row.Valid {
		t.Fatal("row must be invalidated")
	}

	writeCBZFixture(t, path, "Foo", "1")
	if result := p.mustScan(ScanInput{}); result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("repair scan = %+v", result)
	}
	if row := readContent(t, p.pool, id); !row.Valid {
		t.Fatal("repaired row must be valid again")
	}
}

func TestScanConcurrencyProducesStableCatalog(t *testing.T) {
	p := newPipeline(t, "comics")
	for _, name := range []string{"A (2019)", "B (2020)", "C (2021)"} {
		dir := filepath.Join(p.root, name)
		for _, n := range []string{"1", "2", "3", "4", "5"} {
			writeCBZFixture(t, filepath.Join(dir, name+" ch"+n+".cbz"), name[:1], n)
		}
	}

	result := p.mustScan(ScanInput{Concurrency: 8})
	if result.Added != 15 || result.Failed != 0 {
		t.Fatalf("scan = %+v", result)
	}

	rows, err := db.Select[models.Content](context.Background(), p.pool,
		"SELECT * FROM content WHERE library_id = $1", p.lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 18 {
		t.Fatalf("rows = %d, want 18", len(rows))
	}
	for _, c := range rows {
		if c.Type == "comic_series" {
			continue
		}
		if c.Order == nil || c.ParentID == nil {
			t.Fatalf("leaf = %+v", c)
		}
	}

	again := p.mustScan(ScanInput{Concurrency: 8})
	if again.Unchanged != 15 || again.Added != 0 || again.Removed != 0 {
		t.Fatalf("second scan = %+v", again)
	}
}

func TestScanConcurrencyCancellation(t *testing.T) {
	p := newPipeline(t, "comics")
	fixture := filepath.Join(p.root, "Foo (2019)", "Foo ch1.cbr")
	if err := os.MkdirAll(filepath.Dir(fixture), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fixture, 0o600); err != nil {
		t.Fatal(err)
	}

	var stuck *os.File
	t.Cleanup(func() {
		if stuck == nil {
			stuck, _ = os.OpenFile(fixture, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		}
		if stuck != nil {
			_ = stuck.Close()
		}
	})

	returned := make(chan struct{})
	process := p.def.Process
	p.def.Process = func(input any, tc *tasks.TaskContext) (any, error) {
		defer close(returned)
		return process(input, tc)
	}

	handle, err := p.manager.Push(p.def, ScanInput{LibraryID: p.lib, LibraryType: "comics",
		Sources: []string{p.root}, Concurrency: 1})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for stuck == nil && time.Now().Before(deadline) {
		f, err := os.OpenFile(fixture, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			stuck = f
			break
		}
		if !errors.Is(err, syscall.ENXIO) {
			t.Fatalf("open fixture for writing: %v", err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if stuck == nil {
		t.Fatal("timed out waiting for a parser to open the fixture")
	}

	if err := p.manager.Cancel(handle.ID()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-returned:
		t.Fatal("the scan returned while a parser was still reading")
	case <-time.After(250 * time.Millisecond):
	}

	if err := stuck.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-returned:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the cancelled scan to join its parsers")
	}
	if _, err := handle.Wait(); err == nil {
		t.Fatal("cancelled scan must report an error")
	}
	assertCatalog(t, p.pool, p.lib, nil)
}

func TestScanPipelineRelativeRootProvesAbsence(t *testing.T) {
	p := newPipeline(t, "comics")
	t.Chdir(p.root)
	series := filepath.Join("Foo (2019)")
	writeCBZFixture(t, filepath.Join(series, "Foo ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(series, "Foo ch2.cbz"), "Foo", "2")
	writeCBZFixture(t, "Solo.cbz", "Solo", "1")

	if got := p.mustScan(ScanInput{Sources: []string{"."}}); got.Added != 3 {
		t.Fatalf("first scan = %+v", got)
	}

	if err := os.Remove(filepath.Join(series, "Foo ch2.cbz")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove("Solo.cbz"); err != nil {
		t.Fatal(err)
	}
	got := p.mustScan(ScanInput{Sources: []string{"."}})
	if got.Removed != 2 {
		t.Fatalf("second scan = %+v, want both removals proven", got)
	}
	want := []string{"comic/Foo_2019", "comic/Foo_2019/ch1"}
	assertCatalog(t, p.pool, p.lib, want)
}

func TestScanPipelineSourcesOutsideTheWorkingDirectory(t *testing.T) {
	p := newPipeline(t, "comics")
	here := filepath.Join(p.root, "Here")
	other := filepath.Join(p.root, "Other")
	if err := os.MkdirAll(here, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(here)

	writeCBZFixture(t, filepath.Join("Foo (2019)", "Foo ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join("..", "Other", "Bar (2020)", "Bar ch1.cbz"), "Bar", "1")

	sources := []string{".", filepath.Join("..", "Other")}
	if got := p.mustScan(ScanInput{Sources: sources}); got.Added != 2 {
		t.Fatalf("first scan = %+v, want both roots catalogued", got)
	}
	want := []string{"comic/Bar_2020", "comic/Bar_2020/ch1", "comic/Foo_2019", "comic/Foo_2019/ch1"}
	assertCatalog(t, p.pool, p.lib, want)

	again := p.mustScan(ScanInput{Sources: sources})
	if again.Removed != 0 || again.Unchanged != 2 {
		t.Fatalf("second scan = %+v, want nothing removed while both files exist", again)
	}
	assertCatalog(t, p.pool, p.lib, want)

	mixed := p.mustScan(ScanInput{Sources: []string{here, filepath.Join("..", "Other")}})
	if mixed.Removed != 0 || mixed.Added != 0 || mixed.Unchanged != 2 {
		t.Fatalf("mixed absolute and relative sources = %+v, want the same rows matched", mixed)
	}

	filtered := p.mustScan(ScanInput{Sources: sources, FilterPaths: []string{"."}})
	if filtered.Removed != 0 {
		t.Fatalf("filtered scan = %+v, want rows outside the filter retained", filtered)
	}
	assertCatalog(t, p.pool, p.lib, want)
}

func TestScanPipelineRetainsRowsWhoseAncestryCannotBeResolved(t *testing.T) {
	p := newPipeline(t, "comics")
	t.Chdir(p.root)
	if err := os.MkdirAll(filepath.Join(p.root, "A"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(p.root, "B", "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	book := filepath.Join(p.root, "B", "Book ch1.cbz")
	writeCBZFixture(t, book, "Book", "1")
	symlink(t, filepath.Join(p.root, "B", "inner"), filepath.Join(p.root, "A", "link"))

	stored := spell("A", "link", "..", "Book ch1.cbz")
	file := statFile(t, stored)
	mtime := file.Mtime.UTC()
	seedContent(t, p.pool,
		models.Content{ID: "p1", LibraryID: p.lib, Type: "comic_series", URI: "comic/Book", URIPart: "Book",
			Valid: true, FileURI: new(spell("A", "link", ".."))},
		models.Content{ID: "l1", LibraryID: p.lib, Type: "comic", URI: "comic/Book/ch1", URIPart: "ch1", Valid: true,
			FileURI: new(stored), FileMtime: &mtime, FileSize: new(int(file.Size)), ParentID: new("p1")},
	)

	if err := os.RemoveAll(filepath.Join(p.root, "B", "inner")); err != nil {
		t.Fatal(err)
	}

	result := p.mustScan(ScanInput{Sources: []string{"A"}})
	if result.Removed != 0 {
		t.Fatalf("scan = %+v, want nothing removed while the archive is still on disk", result)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Book", "comic/Book/ch1"})
	if _, err := os.Stat(book); err != nil {
		t.Fatalf("archive missing from disk: %v", err)
	}
}
