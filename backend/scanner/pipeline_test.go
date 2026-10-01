package scanner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/tasks"
	"voltis/metadata"
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
	def := NewScanTask(notify, testStore)
	manager.Register(def)
	t.Cleanup(manager.Close)

	return &pipeline{t: t, pool: pool, lib: lib, root: root, manager: manager, def: def, notify: notify}
}

func (p *pipeline) scan(in ScanInput) (ScanResult, error) {
	p.t.Helper()
	in.LibraryID = p.lib
	if in.Sources == nil {
		in.Sources = []string{p.root}
	}
	if in.LibraryType == "" {
		in.LibraryType = "comics"
	}
	handle, err := p.manager.Push(p.def, in)
	if err != nil {
		p.t.Fatalf("push: %v", err)
	}
	result, err := handle.Wait()
	if err != nil {
		return ScanResult{}, err
	}
	return result.(ScanResult), nil
}

func (p *pipeline) mustScan(in ScanInput) ScanResult {
	p.t.Helper()
	result, err := p.scan(in)
	if err != nil {
		p.t.Fatalf("scan: %v", err)
	}
	return result
}

func TestScanPipelineMatchesRowsStoredWithAnotherSpelling(t *testing.T) {
	p := newPipeline(t, "comics")
	t.Chdir(p.root)

	ch1 := filepath.Join("S", "ch1.cbz")
	writeCBZFixture(t, ch1, "S", "1")
	file := statFile(t, ch1)
	abs, err := filepath.Abs(ch1)
	must(t, err)
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
	if meta := readMeta(t, p.pool, p.lib, "comic/Foo_2019").File; meta.Title.V != "Foo" {
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

	must(t, os.Remove(filepath.Join(series, "Foo ch2.cbz")))
	removed := p.mustScan(ScanInput{})
	if removed.Removed != 1 || removed.Unchanged != 1 {
		t.Fatalf("removal scan = %+v", removed)
	}
	assertCatalog(t, p.pool, p.lib, want[:2])
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

	must(t, os.Chmod(series, 0))
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

	must(t, os.RemoveAll(bar))
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
	must(t, err)
	manifest := string(readContent(t, p.pool, id).FileData)
	exec(t, p.pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, p.pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('a1', 'u1', $1, $2, true)",
		p.lib, "comic/Foo_2019/ch1")

	writeFile(t, path, "corrupt")
	result := p.mustScan(ScanInput{})
	if result.Failed != 1 || result.Added != 0 || result.Updated != 0 || result.Removed != 0 {
		t.Fatalf("corrupt scan = %+v", result)
	}
	row := readContent(t, p.pool, id)
	if row.Valid {
		t.Fatal("row must be invalidated")
	}
	if string(row.FileData) != manifest {
		t.Fatalf("file_data = %s, want the manifest kept through invalidation", row.FileData)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Foo_2019", "comic/Foo_2019/ch1"})
	assertAnnotations(t, p.pool, p.lib, []string{"comic/Foo_2019/ch1"})

	writeCBZFixture(t, path, "Foo", "1")
	if result := p.mustScan(ScanInput{}); result.Updated != 1 || result.Failed != 0 {
		t.Fatalf("repair scan = %+v", result)
	}
	if row := readContent(t, p.pool, id); !row.Valid {
		t.Fatal("repaired row must be valid again")
	}
	assertAnnotations(t, p.pool, p.lib, []string{"comic/Foo_2019/ch1"})
}

func comicInfoXML(series, number, publisher, language string) string {
	return `<?xml version="1.0"?><ComicInfo><Series>` + series + `</Series><Number>` + number +
		`</Number><Publisher>` + publisher + `</Publisher><LanguageISO>` + language + `</LanguageISO></ComicInfo>`
}

func TestScanPipelineEarlierMemberCorrectsSeriesWithoutLosingOverrides(t *testing.T) {
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	writeCBZ(t, filepath.Join(series, "Foo ch2.cbz"), comicInfoXML("Foo", "2", "Beta", "fr"))

	p.mustScan(ScanInput{})
	if got := readMeta(t, p.pool, p.lib, "comic/Foo_2019").File; !slices.Equal(got.Publishers.V, []string{"Beta"}) {
		t.Fatalf("series metadata = %+v, want the only member", got)
	}

	doc := readMeta(t, p.pool, p.lib, "comic/Foo_2019")
	doc.Overrides = metadata.Fields{Publishers: metadata.Val([]string{"Override Press"})}
	seedMetadata(t, p.pool, p.lib, "comic/Foo_2019", doc)

	writeCBZ(t, filepath.Join(series, "Foo ch1.cbz"), comicInfoXML("Foo", "1", "Alpha", ""))
	if result := p.mustScan(ScanInput{}); result.Added != 1 || result.Unchanged != 1 {
		t.Fatalf("second scan = %+v", result)
	}

	got := readMeta(t, p.pool, p.lib, "comic/Foo_2019")
	if !slices.Equal(got.File.Publishers.V, []string{"Alpha"}) {
		t.Fatalf("file layer publishers = %v, want the earlier member to win", got.File.Publishers.V)
	}
	if got.File.Language.V != "fr" {
		t.Fatalf("file layer language = %q, want the later member to still contribute", got.File.Language.V)
	}
	if !slices.Equal(got.Overrides.Publishers.V, []string{"Override Press"}) {
		t.Fatalf("overrides = %+v, want them untouched", got.Overrides)
	}
	if data := readData(t, p.pool, p.lib, "comic/Foo_2019"); !slices.Equal(data.Publishers.V, []string{"Override Press"}) {
		t.Fatalf("data publishers = %v, want the override to win", data.Publishers.V)
	}
}

func TestScanPipelineStampsScannedAtOnlyOnASuccessfulFinalFlush(t *testing.T) {
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	writeCBZFixture(t, filepath.Join(series, "Foo ch1.cbz"), "Foo", "1")

	p.mustScan(ScanInput{})
	first := libraryScannedAt(t, p.pool, p.lib)
	if first == nil {
		t.Fatal("scanned_at = nil, want the successful scan stamped")
	}

	exec(t, p.pool, `
		CREATE FUNCTION fail_stamp() RETURNS trigger AS $fn$
		BEGIN
			RAISE EXCEPTION 'no stamp' USING ERRCODE = '22000';
		END $fn$ LANGUAGE plpgsql`)
	exec(t, p.pool, "CREATE TRIGGER fail_stamp AFTER UPDATE ON libraries FOR EACH ROW EXECUTE FUNCTION fail_stamp()")

	writeCBZFixture(t, filepath.Join(series, "Foo ch2.cbz"), "Foo", "2")
	if _, err := p.scan(ScanInput{}); err == nil {
		t.Fatal("the final flush must fail while the stamp is rejected")
	}

	at := libraryScannedAt(t, p.pool, p.lib)
	if at == nil || !at.Equal(*first) {
		t.Fatalf("scanned_at = %v, want it left at %v by the failed final flush", at, first)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Foo_2019", "comic/Foo_2019/ch1", "comic/Foo_2019/ch2"})
}

func TestScanPipelineRerunAfterAnInterruptedScanConverges(t *testing.T) {
	p := newPipeline(t, "comics")
	series := filepath.Join(p.root, "Foo (2019)")
	for _, n := range []string{"1", "2", "3"} {
		writeCBZFixture(t, filepath.Join(series, "Foo ch"+n+".cbz"), "Foo", n)
	}

	saved := statFile(t, filepath.Join(series, "Foo ch1.cbz"))
	run := newScanRun(t, p.pool, p.lib, &ComicsScanner{})
	run.place(Result{File: saved, Item: (&ComicsScanner{}).ParseFile(saved)})
	run.commit(false)
	batch := contentIDByURI(t, p.pool, p.lib, "comic/Foo_2019/ch1")

	result := p.mustScan(ScanInput{})
	if result.Added != 2 || result.Unchanged != 1 || result.Removed != 0 || result.Failed != 0 {
		t.Fatalf("rerun = %+v, want the saved batch reused and the rest added", result)
	}
	want := []string{"comic/Foo_2019", "comic/Foo_2019/ch1", "comic/Foo_2019/ch2", "comic/Foo_2019/ch3"}
	assertCatalog(t, p.pool, p.lib, want)
	if got := contentIDByURI(t, p.pool, p.lib, "comic/Foo_2019/ch1"); got != batch {
		t.Fatalf("ch1 id = %s, want the row the interrupted scan saved (%s)", got, batch)
	}

	if again := p.mustScan(ScanInput{}); again.Unchanged != 3 || again.Added != 0 || again.Removed != 0 {
		t.Fatalf("settled scan = %+v", again)
	}
	assertCatalog(t, p.pool, p.lib, want)
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
		"SELECT "+models.ContentColumns("")+" FROM content WHERE library_id = $1", p.lib)
	must(t, err)
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
	must(t, os.MkdirAll(filepath.Dir(fixture), 0o755))
	must(t, syscall.Mkfifo(fixture, 0o600))

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
	must(t, err)

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

	must(t, p.manager.Cancel(handle.ID()))
	select {
	case <-returned:
		t.Fatal("the scan returned while a parser was still reading")
	case <-time.After(250 * time.Millisecond):
	}

	must(t, stuck.Close())
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

	must(t, os.Remove(filepath.Join(series, "Foo ch2.cbz")))
	must(t, os.Remove("Solo.cbz"))
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
	must(t, os.MkdirAll(here, 0o755))
	must(t, os.MkdirAll(other, 0o755))
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
	must(t, os.MkdirAll(filepath.Join(p.root, "A"), 0o755))
	must(t, os.MkdirAll(filepath.Join(p.root, "B", "inner"), 0o755))
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

	must(t, os.RemoveAll(filepath.Join(p.root, "B", "inner")))

	result := p.mustScan(ScanInput{Sources: []string{"A"}})
	if result.Removed != 0 {
		t.Fatalf("scan = %+v, want nothing removed while the archive is still on disk", result)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Book", "comic/Book/ch1"})
	if _, err := os.Stat(book); err != nil {
		t.Fatalf("archive missing from disk: %v", err)
	}
}

func TestScanKeepsASeriesInItsStoredDirectory(t *testing.T) {
	p := newPipeline(t, "comics")
	a, z := filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(z, "ch2.cbz"), "Foo", "2")
	writeFile(t, filepath.Join(a, "cover.jpg"), "a")
	writeFile(t, filepath.Join(z, "cover.jpg"), "z")

	if got := p.mustScan(ScanInput{}); got.Added != 2 {
		t.Fatalf("first scan = %+v", got)
	}
	series := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	if got := readContent(t, p.pool, series); deref(got.FileURI) != a {
		t.Fatalf("series file_uri = %v, want the smallest member directory %s", deref(got.FileURI), a)
	}

	writeCBZFixture(t, filepath.Join(z, "ch3.cbz"), "Foo", "3")
	if got := p.mustScan(ScanInput{}); got.Added != 1 || got.Unchanged != 2 {
		t.Fatalf("second scan = %+v", got)
	}
	assertSeriesLocation(t, p.pool, series, a)

	first := filepath.Join(p.root, "0")
	writeCBZFixture(t, filepath.Join(first, "ch0.cbz"), "Foo", "0")
	writeFile(t, filepath.Join(first, "cover.jpg"), "0")
	if got := p.mustScan(ScanInput{}); got.Added != 1 || got.Unchanged != 3 {
		t.Fatalf("third scan = %+v", got)
	}
	assertSeriesLocation(t, p.pool, series, a)
}

func TestScanMovesASeriesOffADeletedDirectory(t *testing.T) {
	p := newPipeline(t, "comics")
	a, z := filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(z, "ch2.cbz"), "Foo", "2")
	writeFile(t, filepath.Join(z, "cover.jpg"), "z")

	if got := p.mustScan(ScanInput{}); got.Added != 2 {
		t.Fatalf("first scan = %+v", got)
	}
	series := contentIDByURI(t, p.pool, p.lib, "comic/Foo")

	must(t, os.RemoveAll(a))
	if got := p.mustScan(ScanInput{}); got.Removed != 1 || got.Unchanged != 1 {
		t.Fatalf("second scan = %+v", got)
	}
	assertSeriesLocation(t, p.pool, series, z)
	assertCatalog(t, p.pool, p.lib, []string{"comic/Foo", "comic/Foo/ch2"})
}

func TestScanNeverMovesASettledSeries(t *testing.T) {
	p := newPipeline(t, "comics")
	a, z := filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(z, "ch2.cbz"), "Foo", "2")
	p.mustScan(ScanInput{})

	series := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	if got := deref(readContent(t, p.pool, series).FileURI); got != a {
		t.Fatalf("series file_uri = %v, want the smallest member directory %s", got, a)
	}

	for _, in := range []ScanInput{{}, {Force: true}, {FilterPaths: []string{z}}, {}} {
		p.mustScan(in)
		if got := deref(readContent(t, p.pool, series).FileURI); got != a {
			t.Fatalf("series file_uri = %v after %+v, want %s left untouched", got, in, a)
		}
	}
}

func TestScanFollowsASeriesWhoseMemberJoinedAnother(t *testing.T) {
	p := newPipeline(t, "comics")
	zero, a, z := filepath.Join(p.root, "0"), filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(z, "ch2.cbz"), "Foo", "2")
	writeCBZFixture(t, filepath.Join(zero, "ch3.cbz"), "Bar", "3")
	for _, dir := range []string{zero, a, z} {
		writeFile(t, filepath.Join(dir, "cover.jpg"), filepath.Base(dir))
	}

	if got := p.mustScan(ScanInput{}); got.Added != 3 {
		t.Fatalf("first scan = %+v", got)
	}
	foo := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	bar := contentIDByURI(t, p.pool, p.lib, "comic/Bar")
	if got := deref(readContent(t, p.pool, foo).FileURI); got != a {
		t.Fatalf("series file_uri = %v, want the smallest member directory %s", got, a)
	}

	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Bar", "1")
	if got := p.mustScan(ScanInput{Force: true}); got.Updated != 3 {
		t.Fatalf("reparenting scan = %+v", got)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Bar", "comic/Bar/ch1", "comic/Bar/ch3", "comic/Foo", "comic/Foo/ch2"})

	assertSeriesLocation(t, p.pool, foo, z)
	if got := deref(readContent(t, p.pool, bar).FileURI); got != zero {
		t.Fatalf("joined series file_uri = %v, want %s kept while it still has members", got, zero)
	}

	if again := p.mustScan(ScanInput{}); again.Unchanged != 3 || again.Updated != 0 {
		t.Fatalf("settled scan = %+v", again)
	}
	if got := deref(readContent(t, p.pool, foo).FileURI); got != z {
		t.Fatalf("former series file_uri = %v on reload, want %s persisted by the reparenting scan", got, z)
	}
}

func TestScanPersistsADirectoryCorrectionWithNothingElseToCommit(t *testing.T) {
	p := newPipeline(t, "comics")
	a := filepath.Join(p.root, "A")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeFile(t, filepath.Join(a, "cover.jpg"), "a")

	if got := p.mustScan(ScanInput{}); got.Added != 1 {
		t.Fatalf("first scan = %+v", got)
	}
	series := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	exec(t, p.pool, "UPDATE content SET file_uri = $2, cover_uri = NULL WHERE id = $1",
		series, filepath.Join(p.root, "Gone"))

	if got := p.mustScan(ScanInput{}); got.Unchanged != 1 || got.Added != 0 || got.Updated != 0 {
		t.Fatalf("settled rescan = %+v, want nothing to parse", got)
	}
	assertSeriesLocation(t, p.pool, series, a)
}

func TestScanLeavesBookSeriesDirectoryless(t *testing.T) {
	p := newPipeline(t, "books")
	volume := filepath.Join(p.root, "Bar v1.epub")
	writeEPUBFixture(t, volume, "Bar Volume 1", "Bar", "1")
	writeEPUBFixture(t, filepath.Join(p.root, "Solo.epub"), "Solo", "", "")

	want := []string{"book/Bar", "book/Bar/Bar v1", "book/Solo"}
	if got := p.mustScan(ScanInput{LibraryType: "books"}); got.Added != 2 || got.Failed != 0 {
		t.Fatalf("first scan = %+v", got)
	}
	assertCatalog(t, p.pool, p.lib, want)
	series := contentIDByURI(t, p.pool, p.lib, "book/Bar")
	check := func(what string) {
		t.Helper()
		if got := readContent(t, p.pool, series); got.FileURI != nil {
			t.Fatalf("book series file_uri = %q after %s, want books to stay directoryless", *got.FileURI, what)
		}
	}
	check("the first scan")

	for _, in := range []ScanInput{
		{LibraryType: "books"},
		{LibraryType: "books", Force: true},
		{LibraryType: "books", FilterPaths: []string{volume}},
	} {
		p.mustScan(in)
		check(fmt.Sprintf("%+v", in))
	}
	assertCatalog(t, p.pool, p.lib, want)
}

func TestScanRejectedPlacementContributesNoDirectory(t *testing.T) {
	p := newPipeline(t, "comics")
	a, m, z := filepath.Join(p.root, "A"), filepath.Join(p.root, "M"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "b1.cbz"), "Bar", "1")
	writeCBZFixture(t, filepath.Join(z, "b2.cbz"), "Bar", "2")
	writeCBZFixture(t, filepath.Join(m, "f1.cbz"), "Foo", "9")
	for _, dir := range []string{a, m, z} {
		writeFile(t, filepath.Join(dir, "cover.jpg"), filepath.Base(dir))
	}

	if got := p.mustScan(ScanInput{}); got.Added != 3 {
		t.Fatalf("first scan = %+v", got)
	}
	bar := contentIDByURI(t, p.pool, p.lib, "comic/Bar")
	foo := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	if got := deref(readContent(t, p.pool, bar).FileURI); got != a {
		t.Fatalf("Bar file_uri = %v, want the smallest member directory %s", got, a)
	}

	must(t, os.Remove(filepath.Join(a, "b1.cbz")))
	writeCBZFixture(t, filepath.Join(m, "f1.cbz"), "Bar", "2")

	for i := range 3 {
		if got := p.mustScan(ScanInput{}); got.Failed != 1 {
			t.Fatalf("rescan %d = %+v, want the colliding member rejected", i, got)
		}
		assertSeriesLocation(t, p.pool, bar, z)
		if got := deref(readContent(t, p.pool, foo).FileURI); got != m {
			t.Fatalf("rescan %d: Foo file_uri = %v, want %s left untouched", i, got, m)
		}
		assertCatalog(t, p.pool, p.lib, []string{"comic/Bar", "comic/Bar/ch2", "comic/Foo", "comic/Foo/ch9"})
	}

	writeCBZFixture(t, filepath.Join(m, "f1.cbz"), "Bar", "9")
	if got := p.mustScan(ScanInput{}); got.Failed != 0 || got.Updated != 1 {
		t.Fatalf("resolving scan = %+v", got)
	}
	assertCatalog(t, p.pool, p.lib, []string{"comic/Bar", "comic/Bar/ch2", "comic/Bar/ch9"})
	assertSeriesLocation(t, p.pool, bar, z)
}

func TestScanKeepsADirectoryPinnedByAnotherMember(t *testing.T) {
	p := newPipeline(t, "comics")
	zero, a, z := filepath.Join(p.root, "0"), filepath.Join(p.root, "A"), filepath.Join(p.root, "Z")
	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Foo", "1")
	writeCBZFixture(t, filepath.Join(a, "ch2.cbz"), "Foo", "2")
	writeCBZFixture(t, filepath.Join(z, "ch3.cbz"), "Foo", "3")
	writeCBZFixture(t, filepath.Join(zero, "ch9.cbz"), "Bar", "9")
	for _, dir := range []string{zero, a, z} {
		writeFile(t, filepath.Join(dir, "cover.jpg"), filepath.Base(dir))
	}

	if got := p.mustScan(ScanInput{}); got.Added != 4 {
		t.Fatalf("first scan = %+v", got)
	}
	foo := contentIDByURI(t, p.pool, p.lib, "comic/Foo")
	if got := deref(readContent(t, p.pool, foo).FileURI); got != a {
		t.Fatalf("series file_uri = %v, want the smallest member directory %s", got, a)
	}

	writeCBZFixture(t, filepath.Join(a, "ch1.cbz"), "Bar", "1")
	if got := p.mustScan(ScanInput{}); got.Updated != 1 || got.Unchanged != 3 {
		t.Fatalf("reparenting scan = %+v", got)
	}
	assertCatalog(t, p.pool, p.lib,
		[]string{"comic/Bar", "comic/Bar/ch1", "comic/Bar/ch9", "comic/Foo", "comic/Foo/ch2", "comic/Foo/ch3"})

	assertSeriesLocation(t, p.pool, foo, a)
}

func TestScanInfersBookSeriesAndRescansWithoutChurn(t *testing.T) {
	p := newPipeline(t, "books")
	dir := filepath.Join(p.root, "Foo")
	writeEPUBFixture(t, filepath.Join(dir, "Foo v01 [Tag].epub"), "Foo’s Tale Vol. 01", "", "")
	writeEPUBFixture(t, filepath.Join(dir, "Foo v02 [Tag].epub"), "Foo's Tale Vol. 02", "", "")
	writeEPUBFixture(t, filepath.Join(dir, "Foo SP01.epub"), "Foo's Tale Vol. 2 Short Stories", "Foo's Tale", "100000")
	writeEPUBFixture(t, filepath.Join(dir, "Solo.epub"), "Solo", "", "")

	children := func() []string {
		t.Helper()
		kids, err := db.SelectScalars[string](context.Background(), p.pool, `
			SELECT c.uri_part FROM content c JOIN content s ON s.id = c.parent_id
			WHERE c.library_id = $1 AND s.type = 'book_series' ORDER BY c."order"`, p.lib)
		must(t, err)
		return kids
	}
	inferred := func() {
		t.Helper()
		if got, want := children(), []string{"Foo v01 [Tag]", "Foo v02 [Tag]", "Foo SP01"}; !slices.Equal(got, want) {
			t.Fatalf("children = %v, want %v", got, want)
		}
	}

	p.mustScan(ScanInput{LibraryType: "books"})
	inferred()
	before := contentURIs(t, p.pool, p.lib)
	if len(before) != 5 {
		t.Fatalf("uris = %v, want one series, three children and Solo", before)
	}

	p.mustScan(ScanInput{LibraryType: "books", Force: true})
	inferred()
	assertCatalog(t, p.pool, p.lib, before)

	// A user's row follows the regrouped book both ways.
	followsV01 := func() {
		t.Helper()
		var content, ref string
		must(t, p.pool.QueryRow(context.Background(), `
			SELECT c.uri, u.uri FROM content c, user_to_content u
			WHERE c.library_id = $1 AND c.uri_part = 'Foo v01 [Tag]' AND u.id = 'probe'`, p.lib).Scan(&content, &ref))
		if ref != content {
			t.Fatalf("ref = %s, want it at the book's uri %s", ref, content)
		}
	}
	exec(t, p.pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, p.pool, `INSERT INTO user_to_content (id, user_id, library_id, uri, starred)
		SELECT 'probe', 'u1', $1, uri, true FROM content WHERE library_id = $1 AND uri_part = 'Foo v01 [Tag]'`, p.lib)

	off := models.LibrarySettings{BookSeriesInference: models.BookSeriesInferenceOff}
	p.mustScan(ScanInput{LibraryType: "books", Force: true, Settings: off})
	uris := contentURIs(t, p.pool, p.lib)
	if got := children(); !slices.Equal(got, []string{"Foo SP01"}) || len(uris) != 5 ||
		!slices.Contains(uris, "book/Foo v01 [Tag]") || !slices.Contains(uris, "book/Foo v02 [Tag]") {
		t.Fatalf("uris = %v, want only the metadata series grouped", uris)
	}
	followsV01()

	p.mustScan(ScanInput{LibraryType: "books", Force: true})
	inferred()
	followsV01()
}
