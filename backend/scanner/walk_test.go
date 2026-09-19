package scanner

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func collectWalk(t *testing.T, ctx context.Context, roots []string, eligible func(string) bool) ([]Event, error) {
	t.Helper()
	out := make(chan Event, 1024)
	err := walk(ctx, roots, eligible, out)
	close(out)
	var events []Event
	for ev := range out {
		events = append(events, ev)
	}
	return events, err
}

func eventPaths(events []Event, kind EventKind) []string {
	var out []string
	for _, ev := range events {
		if ev.Kind == kind {
			out = append(out, ev.Path)
		}
	}
	slices.Sort(out)
	return out
}

func TestWalkEmitsListingsBeforeFiles(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Series", "ch1.cbz"), "one")
	writeFile(t, filepath.Join(dir, "Series", "Sub", "ch2.cbz"), "twotwo")
	writeFile(t, filepath.Join(dir, "Series", "notes.txt"), "ignored")

	events, err := collectWalk(t, context.Background(), []string{dir}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	want := []string{dir, filepath.Join(dir, "Series"), filepath.Join(dir, "Series", "Sub")}
	if got := eventPaths(events, Listed); !slices.Equal(got, want) {
		t.Fatalf("listed = %v, want %v", got, want)
	}

	seen := map[string]int{}
	listed := map[string]int{}
	for i, ev := range events {
		switch ev.Kind {
		case Listed:
			listed[ev.Path] = i
		case Seen:
			seen[ev.Path] = i
		}
	}
	for path, at := range seen {
		if listed[filepath.Dir(path)] > at {
			t.Fatalf("%s seen before its directory listing", path)
		}
	}

	for _, ev := range events {
		if ev.Kind != Seen {
			continue
		}
		info, err := os.Stat(ev.Path)
		if err != nil {
			t.Fatal(err)
		}
		if ev.File.Size != info.Size() || !ev.File.Mtime.Equal(info.ModTime()) || ev.File.Path != ev.Path {
			t.Fatalf("%s: file = %+v", ev.Path, ev.File)
		}
	}

	if got := eventPaths(events, Seen); !slices.Equal(got, []string{
		filepath.Join(dir, "Series", "Sub", "ch2.cbz"), filepath.Join(dir, "Series", "ch1.cbz"),
	}) {
		t.Fatalf("seen = %v", got)
	}

	names := events[slices.IndexFunc(events, func(ev Event) bool {
		return ev.Kind == Listed && ev.Path == filepath.Join(dir, "Series")
	})].Names
	slices.Sort(names)
	if !slices.Equal(names, []string{"Sub", "ch1.cbz", "notes.txt"}) {
		t.Fatalf("names = %v", names)
	}
}

func TestWalkRootPreflight(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ch1.cbz"), "one")

	events, err := collectWalk(t, context.Background(), []string{dir, filepath.Join(dir, "Missing")}, isComicFile)
	if err == nil {
		t.Fatal("expected an error for the missing root")
	}
	if len(events) != 0 {
		t.Fatalf("events = %v, want none", events)
	}
}

func TestWalkRootFile(t *testing.T) {
	dir := t.TempDir()
	comic := filepath.Join(dir, "ch1.cbz")
	notes := filepath.Join(dir, "notes.txt")
	writeFile(t, comic, "one")
	writeFile(t, notes, "two")

	events, err := collectWalk(t, context.Background(), []string{comic, notes}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(events) != 1 || events[0].Kind != Seen || events[0].File.Path != comic {
		t.Fatalf("events = %+v", events)
	}
}

func TestWalkOverlappingRoots(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "Series")
	writeFile(t, filepath.Join(sub, "ch1.cbz"), "one")

	events, err := collectWalk(t, context.Background(), []string{sub, dir, dir + string(filepath.Separator), sub}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Listed); !slices.Equal(got, []string{dir, sub}) {
		t.Fatalf("listed = %v, want each directory once", got)
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{filepath.Join(sub, "ch1.cbz")}) {
		t.Fatalf("seen = %v, want one file", got)
	}
}

func TestWalkListingFailure(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	dir := t.TempDir()
	blocked := filepath.Join(dir, "Blocked")
	writeFile(t, filepath.Join(blocked, "ch1.cbz"), "one")
	writeFile(t, filepath.Join(dir, "ch2.cbz"), "two")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	events, err := collectWalk(t, context.Background(), []string{dir}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Failed); !slices.Equal(got, []string{blocked}) {
		t.Fatalf("failed = %v, want [%s]", got, blocked)
	}
	if got := eventPaths(events, Listed); !slices.Equal(got, []string{dir}) {
		t.Fatalf("listed = %v, want only the root", got)
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{filepath.Join(dir, "ch2.cbz")}) {
		t.Fatalf("seen = %v", got)
	}
}

func TestWalkStatFailure(t *testing.T) {
	dir := t.TempDir()
	vanishing := filepath.Join(dir, "gone.cbz")
	writeFile(t, vanishing, "one")
	writeFile(t, filepath.Join(dir, "ch1.cbz"), "two")

	eligible := func(path string) bool {
		if path == vanishing {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}
		return isComicFile(path)
	}

	events, err := collectWalk(t, context.Background(), []string{dir}, eligible)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Failed); !slices.Equal(got, []string{vanishing}) {
		t.Fatalf("failed = %v, want [%s]", got, vanishing)
	}
	for _, ev := range events {
		if ev.Kind == Failed && !errors.Is(ev.Err, fs.ErrNotExist) {
			t.Fatalf("err = %v, want not-exist", ev.Err)
		}
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{filepath.Join(dir, "ch1.cbz")}) {
		t.Fatalf("seen = %v", got)
	}
}

func TestWalkSymlinks(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "Real")
	writeFile(t, filepath.Join(real, "ch1.cbz"), "one")
	tree := filepath.Join(dir, "Tree")
	if err := os.MkdirAll(tree, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(tree, "Link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	events, err := collectWalk(t, context.Background(), []string{tree}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Listed); !slices.Equal(got, []string{tree}) {
		t.Fatalf("listed = %v, want only %s", got, tree)
	}
	if got := eventPaths(events, Seen); got != nil {
		t.Fatalf("seen = %v, want none", got)
	}

	events, err = collectWalk(t, context.Background(), []string{filepath.Join(tree, "Link")}, isComicFile)
	if err != nil {
		t.Fatalf("walk symlinked root: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("symlinked root events = %+v, want none", events)
	}
}

func TestWalkKeepsTheSpellingOfEachRoot(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "Lib", "Series", "ch1.cbz"), "one")
	t.Chdir(filepath.Join(dir, "Lib"))

	cases := []struct {
		label string
		roots []string
		want  []string
	}{
		{"relative", []string{"Series"}, []string{filepath.Join("Series", "ch1.cbz")}},
		{"absolute", []string{dir}, []string{filepath.Join(dir, "Lib", "Series", "ch1.cbz")}},
		{"dot", []string{"."}, []string{filepath.Join("Series", "ch1.cbz")}},
		{"parent", []string{filepath.Join("..", "Lib")}, []string{filepath.Join("..", "Lib", "Series", "ch1.cbz")}},
		{"mixed keeps the widest root", []string{"Series", dir}, []string{filepath.Join(dir, "Lib", "Series", "ch1.cbz")}},
	}
	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			events, err := collectWalk(t, context.Background(), c.roots, isComicFile)
			if err != nil {
				t.Fatalf("walk: %v", err)
			}
			if got := eventPaths(events, Seen); !slices.Equal(got, c.want) {
				t.Fatalf("seen = %v, want %v", got, c.want)
			}
			res := newResolver()
			for _, ev := range events {
				if ev.Kind != Seen {
					continue
				}
				if key := mustResolveFile(t, res, ev.Path); key != filepath.Join(dir, "Lib", "Series", "ch1.cbz") {
					t.Fatalf("%s normalises to %s", ev.Path, key)
				}
			}
		})
	}
}

func TestWalkRelativeRootUnderSymlinkedWorkingDirectory(t *testing.T) {
	real, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stored := filepath.Join(real, "Series", "ch1.cbz")
	writeFile(t, stored, "one")
	alias := filepath.Join(t.TempDir(), "Alias")
	if err := os.Symlink(real, alias); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(real)
	t.Setenv("PWD", alias)

	if cwd, err := os.Getwd(); err != nil || cwd != alias {
		t.Skipf("working directory = %q, %v, want the symlinked spelling %q", cwd, err, alias)
	}

	events, err := collectWalk(t, context.Background(), []string{"."}, isComicFile)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{filepath.Join("Series", "ch1.cbz")}) {
		t.Fatalf("seen = %v, want the file below the relative root", got)
	}
	if got := eventPaths(events, Listed); !slices.Equal(got, []string{".", "Series"}) {
		t.Fatalf("listed = %v", got)
	}
	for _, ev := range events {
		if ev.Kind == Listed && ev.Dir != filepath.Join(real, filepath.Clean(ev.Path)) {
			t.Fatalf("%s was listed as %q, want the real directory below %s", ev.Path, ev.Dir, real)
		}
	}

	rows := map[string]string{stored: "c1"}
	run := indexThenWalk(t, []string{"."}, rows, nil)
	if run.unchanged != 1 {
		t.Fatalf("unchanged = %d, want the row stored under its real path matched through the symlinked cwd", run.unchanged)
	}
	if len(run.gone) != 0 {
		t.Fatalf("gone = %v, want the row retained", run.gone)
	}

	if err := os.Remove(stored); err != nil {
		t.Fatal(err)
	}
	if run = indexThenWalk(t, []string{"."}, rows, nil); !slices.Equal(run.gone, []string{"c1"}) {
		t.Fatalf("gone = %v, want the removal proven through the symlinked cwd", run.gone)
	}
}

func TestWalkCancellation(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ch1.cbz"), "one")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := make(chan Event)
	if err := walk(ctx, []string{dir}, isComicFile, out); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func writeCBZFixture(t *testing.T, path, series, number string) {
	t.Helper()
	writeCBZ(t, path, `<?xml version="1.0"?><ComicInfo><Series>`+series+`</Series><Number>`+
		number+`</Number><Writer>Ada</Writer></ComicInfo>`)
}

func writeEPUBFixture(t *testing.T, path, title, series, index string) {
	t.Helper()
	seriesMeta := ""
	if series != "" {
		seriesMeta = `<meta name="calibre:series" content="` + series + `"/>` +
			`<meta name="calibre:series_index" content="` + index + `"/>`
	}
	writeEPUB(t, path, `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>`+title+`</dc:title>
    <dc:creator>Ada</dc:creator>
    <dc:language>en</dc:language>
    `+seriesMeta+`
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest><item id="cover-img" href="cover.jpg" media-type="image/jpeg"/></manifest>
</package>`)
}

func TestWalkAdapterFixtures(t *testing.T) {
	dir := t.TempDir()
	comicPath := filepath.Join(dir, "Foo (2019)", "Foo ch1.cbz")
	writeCBZFixture(t, comicPath, "Foo", "1")
	bookPath := filepath.Join(dir, "Bar v1.epub")
	writeEPUBFixture(t, bookPath, "Bar Volume 1", "Bar", "1")
	broken := filepath.Join(dir, "Foo (2019)", "broken.cbz")
	writeFile(t, broken, "not a zip")

	comics := &ComicsScanner{}
	events, err := collectWalk(t, context.Background(), []string{dir}, comics.FileEligible)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{comicPath, broken}) {
		t.Fatalf("comic files = %v", got)
	}

	item := comics.ParseFile("library", FSFile{Path: comicPath})
	if item == nil || item.URIPart != "ch1" || item.Series == nil || item.Series.URIPart != "Foo_2019" || item.Series.Title != "Foo" {
		t.Fatalf("comic item = %+v", item)
	}
	if item.Series.FileURI == nil || *item.Series.FileURI != filepath.Dir(comicPath) {
		t.Fatalf("series file uri = %+v", item.Series)
	}
	if item.CoverSuffix == nil || *item.CoverSuffix != "001.jpg" {
		t.Fatalf("cover = %+v", item.CoverSuffix)
	}
	if comics.ParseFile("library", FSFile{Path: broken}) != nil {
		t.Fatal("broken archive should not parse")
	}

	books := &BooksScanner{}
	events, err = collectWalk(t, context.Background(), []string{dir}, books.FileEligible)
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if got := eventPaths(events, Seen); !slices.Equal(got, []string{bookPath}) {
		t.Fatalf("book files = %v", got)
	}
	book := books.ParseFile("library", FSFile{Path: bookPath})
	if book == nil || book.URIPart != "Bar v1" || book.Series == nil || book.Series.URIPart != "Bar" {
		t.Fatalf("book item = %+v", book)
	}
	if book.CoverSuffix == nil || *book.CoverSuffix != "cover.jpg" {
		t.Fatalf("book cover = %+v", book.CoverSuffix)
	}
}
