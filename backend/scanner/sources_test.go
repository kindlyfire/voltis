package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func sameDevice(a, b os.FileInfo) bool {
	sa, oka := a.Sys().(*syscall.Stat_t)
	sb, okb := b.Sys().(*syscall.Stat_t)
	return !oka || !okb || sa.Dev == sb.Dev
}

func sourceTree(t *testing.T) string {
	t.Helper()
	root := realTempDir(t)
	writeFile(t, filepath.Join(root, "A", "a1.cbz"), "a1")
	writeFile(t, filepath.Join(root, "B", "b1.cbz"), "b1")
	writeFile(t, filepath.Join(root, "B", "inner", "i1.cbz"), "i1")
	writeFile(t, filepath.Join(root, "B", "inner", "Sub", "s1.cbz"), "s1")
	writeFile(t, filepath.Join(root, "Lib", "Series", "ch1.cbz"), "ch1")
	symlink(t, filepath.Join(root, "B", "inner"), filepath.Join(root, "A", "link"))
	symlink(t, filepath.Join(root, "Lib"), filepath.Join(root, "Alias"))
	return root
}

func spell(parts ...string) string {
	return strings.Join(parts, string(filepath.Separator))
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func crossDeviceDir(t *testing.T, sameAs string) string {
	t.Helper()
	here, err := os.Stat(sameAs)
	must(t, err)
	for _, base := range []string{"/dev/shm", os.Getenv("XDG_RUNTIME_DIR")} {
		if base == "" {
			continue
		}
		info, err := os.Stat(base)
		if err != nil || sameDevice(here, info) {
			continue
		}
		dir, err := os.MkdirTemp(base, "voltis-mount")
		if err != nil {
			continue
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		return dir
	}
	t.Skip("no writable directory on another filesystem")
	return ""
}

func inventory(t *testing.T, sources []string) []string {
	t.Helper()
	var paths []string
	collect := func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil || d.IsDir() || !isComicFile(path) {
			return nil
		}
		paths = append(paths, path)
		return nil
	}
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			t.Fatalf("inventory %v: %v", sources, err)
		}
		if !info.IsDir() {
			if isComicFile(source) {
				paths = append(paths, source)
			}
			continue
		}
		if err := filepath.WalkDir(source, collect); err != nil {
			t.Fatalf("inventory %v: %v", sources, err)
		}
	}
	slices.Sort(paths)
	return paths
}

func inventoryAbsent(t *testing.T, sources []string, rows map[string]string) []string {
	t.Helper()
	walked := map[string]bool{}
	for _, p := range inventory(t, sources) {
		walked[p] = true
	}
	var out []string
	for path, id := range rows {
		if !walked[path] {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

type walkOutcome struct {
	seen      []string
	keys      []string
	gone      []string
	unchanged int
}

func indexThenWalk(t *testing.T, sources []string, rows map[string]string, mutate func()) walkOutcome {
	t.Helper()
	res := newResolver()
	fps := make([]Fingerprint, 0, len(rows))
	for path, id := range rows {
		f := Fingerprint{ID: id, Path: path, Valid: true}
		if info, err := os.Lstat(path); err == nil {
			mtime, size := info.ModTime(), int(info.Size())
			f.Mtime, f.Size = &mtime, &size
		}
		fps = append(fps, f)
	}
	w := newWriter(ScanInput{LibraryID: "library"}, nil, nil, res, fps, nil)

	if mutate != nil {
		mutate()
	}

	var o walkOutcome
	for _, ev := range mustWalk(t, sources, isComicFile) {
		w.event(ev)
		if ev.Kind == Seen {
			o.seen = append(o.seen, ev.Path)
			o.keys = append(o.keys, mustResolveFile(t, res, ev.Path))
		}
	}
	slices.Sort(o.seen)
	slices.Sort(o.keys)
	o.gone = slices.Sorted(maps.Keys(w.gone))
	o.unchanged = w.prog.Unchanged
	return o
}

type sourceCase struct {
	name                 string
	chdir                string
	sources              []string
	setup                func(t *testing.T, root string)
	mutate               func(t *testing.T, root string)
	extra                []string
	absent               []string
	wantErr              bool
	lexicalRows          bool
	want                 []string
	remove               string
	removalProvesNothing bool
}

type sourceRows struct {
	res   *resolver
	byID  map[string]string
	byKey map[string]string
}

func (r sourceRows) add(path string) {
	if key, ok := r.res.file(path); ok {
		if _, taken := r.byKey[key]; taken {
			return
		}
		r.byKey[key] = fmt.Sprintf("c%d", len(r.byID)+1)
		r.byID[path] = r.byKey[key]
		return
	}
	r.byID[path] = fmt.Sprintf("c%d", len(r.byID)+1)
}

func (r sourceRows) ids(t *testing.T, paths []string) []string {
	t.Helper()
	out := make([]string, len(paths))
	for i, p := range paths {
		id, ok := r.byID[p]
		if !ok {
			t.Fatalf("%s is not a stored row; rows = %v", p, r.byID)
		}
		out[i] = id
	}
	slices.Sort(out)
	return out
}

func (r sourceRows) under(t *testing.T, path string) []string {
	t.Helper()
	target := mustResolveFile(t, r.res, path)
	var out []string
	for key, id := range r.byKey {
		if pathWithin(key, target) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func (r sourceRows) matched(keys []string) int {
	n := 0
	for key := range r.byKey {
		if slices.Contains(keys, key) {
			n++
		}
	}
	return n
}

func runSourceCase(t *testing.T, c sourceCase) {
	root := sourceTree(t)
	if c.setup != nil {
		c.setup(t, root)
	}
	cwd := root
	if c.chdir != "" {
		cwd = filepath.Join(root, c.chdir)
	}
	t.Chdir(cwd)

	expand := func(s string) string { return strings.ReplaceAll(s, "{root}", root) }
	sources := make([]string, len(c.sources))
	for i, s := range c.sources {
		sources[i] = expand(s)
	}

	if c.wantErr {
		out := make(chan Event, 16)
		if err := walk(context.Background(), sources, isComicFile, out); err == nil {
			t.Fatalf("walk accepted %v", sources)
		}
		if len(out) != 0 {
			t.Fatalf("walk emitted %d events for rejected sources", len(out))
		}
		return
	}

	listing := inventory(t, sources)
	rows := sourceRows{res: newResolver(), byID: map[string]string{}, byKey: map[string]string{}}
	for _, p := range listing {
		rows.add(p)
	}
	for _, p := range c.extra {
		rows.add(expand(p))
	}

	var mutate func()
	if c.mutate != nil {
		mutate = func() { c.mutate(t, root) }
	}
	run := indexThenWalk(t, sources, rows.byID, mutate)

	if c.mutate == nil {
		if c.lexicalRows {
			if resolvable(listing) {
				t.Fatalf("inventory %v resolves; drop lexicalRows", listing)
			}
			want := make([]string, len(c.want))
			for i, p := range c.want {
				want[i] = filepath.Join(root, p)
			}
			slices.Sort(want)
			if !slices.Equal(run.keys, want) {
				t.Fatalf("found = %v, want %v", run.keys, want)
			}
		} else {
			for _, p := range run.seen {
				if !slices.Contains(listing, p) {
					t.Fatalf("emitted %q, a spelling a plain walk never stores; inventory = %v", p, listing)
				}
			}
			want := make([]string, len(listing))
			for i, p := range listing {
				want[i] = mustResolveFile(t, rows.res, p)
			}
			slices.Sort(want)
			if !slices.Equal(run.keys, slices.Compact(want)) {
				t.Fatalf("found = %v, want the inventory %v", run.keys, want)
			}
		}
		if got, want := run.unchanged, rows.matched(run.keys); got != want {
			t.Fatalf("unchanged = %d, want %d of %v matched", got, want, rows.byID)
		}
	}

	absent := rows.ids(t, c.absent)
	if !slices.Equal(run.gone, absent) {
		t.Fatalf("rescan proved %v absent, want %v", run.gone, absent)
	}
	parity := !c.lexicalRows && c.mutate == nil && len(c.extra) == 0
	if parity {
		if got := inventoryAbsent(t, sources, rows.byID); !slices.Equal(got, absent) {
			t.Fatalf("a plain rescan proved %v absent, want %v", got, absent)
		}
	}

	if c.remove == "" {
		return
	}
	target := filepath.Join(root, c.remove)
	want := absent
	if !c.removalProvesNothing {
		want = slices.Sorted(slices.Values(append(rows.under(t, target), absent...)))
		if len(want) == 0 {
			t.Fatalf("%s covers no stored row; rows = %v", c.remove, rows.byID)
		}
	} else if len(rows.under(t, target)) == 0 {
		t.Fatalf("%s covers no stored row; rows = %v", c.remove, rows.byID)
	}
	must(t, os.RemoveAll(target))

	run = indexThenWalk(t, sources, rows.byID, nil)
	if !slices.Equal(run.gone, want) {
		t.Fatalf("after removing %s gone = %v, want %v", c.remove, run.gone, want)
	}
	if parity {
		if got := inventoryAbsent(t, sources, rows.byID); !slices.Equal(got, want) {
			t.Fatalf("after removing %s a plain walk proved %v absent, want %v", c.remove, got, want)
		}
	}
}

func resolvable(paths []string) bool {
	for _, p := range paths {
		if _, err := filepath.EvalSymlinks(p); err != nil {
			return false
		}
	}
	return true
}

func TestWalkSourceMatrix(t *testing.T) {
	sep := string(filepath.Separator)
	book := func(t *testing.T, root string) { writeFile(t, filepath.Join(root, "B", "Book ch1.cbz"), "book") }
	cases := []sourceCase{
		{name: "relative", sources: []string{"A"}, remove: "A/a1.cbz"},
		{name: "absolute", sources: []string{"{root}/A"}, remove: "A/a1.cbz"},
		{name: "mixed", sources: []string{"A", "{root}/B"}, remove: "B/inner/i1.cbz"},
		{name: "empty", sources: []string{""}, wantErr: true},
		{name: "missing root", sources: []string{"{root}/B/absent.cbz"}, wantErr: true},
		{name: "dot", sources: []string{"."}, remove: "Lib/Series/ch1.cbz"},
		{name: "parent", chdir: "Lib", sources: []string{".."}, remove: "A/a1.cbz"},
		{name: "trailing separator", sources: []string{"A" + sep, "{root}/B" + sep}, remove: "B/b1.cbz"},
		{name: "duplicate spellings", sources: []string{"A", "A" + sep, "./A", "{root}/A"}, remove: "A/a1.cbz"},
		{name: "bare symlink", sources: []string{"Alias"}, extra: []string{spell("Lib", "Series", "ch1.cbz")},
			remove: "Lib/Series/ch1.cbz", removalProvesNothing: true},
		{name: "symlinked ancestor and its descendant", sources: []string{"Alias", "Alias/Series"},
			remove: "Lib/Series/ch1.cbz"},
		{name: "bare symlink and a traversable spelling", sources: []string{"Alias", "Alias" + sep},
			remove: "Lib/Series/ch1.cbz"},
		{name: "symlink inside an ordinary ancestor", sources: []string{"A", "B"}, remove: "B/inner/i1.cbz"},
		{name: "descendant reachable only through a symlink", sources: []string{"A", "A/link/Sub"},
			remove: "B/inner/Sub/s1.cbz"},
		{name: "parent segment crossing a symlink", sources: []string{"A", "A/link/../b1.cbz"}, remove: "A/a1.cbz"},
		{name: "parent segment crossing a symlink to a directory", lexicalRows: true,
			sources: []string{"A", "A/link/.."}, extra: []string{"{root}/B/b1.cbz"},
			absent: []string{spell("A", "b1.cbz")}, remove: "B/b1.cbz",
			want: []string{"A/a1.cbz", "B/b1.cbz", "B/inner/i1.cbz", "B/inner/Sub/s1.cbz"}},
		{name: "file root", sources: []string{"{root}/B/b1.cbz", "{root}/B/inner"}, remove: "B/inner/i1.cbz"},
		{name: "file root below a directory root", sources: []string{"B", "B/b1.cbz"}, remove: "B/inner/i1.cbz"},
		{name: "deleted directory ancestors", sources: []string{"B"}, remove: "B/inner"},
		{name: "dangling ancestor", sources: []string{"A"}, remove: "A/a1.cbz",
			extra: []string{spell("A", "link", "..", "Book ch1.cbz")},
			setup: func(t *testing.T, root string) {
				book(t, root)
				must(t, os.RemoveAll(filepath.Join(root, "B", "inner")))
			}},
		{name: "looping ancestor", sources: []string{"A"}, remove: "A/a1.cbz",
			extra: []string{spell("A", "loop", "..", "Book ch1.cbz")},
			setup: func(t *testing.T, root string) {
				book(t, root)
				symlink(t, filepath.Join(root, "A", "loop"), filepath.Join(root, "A", "loop"))
			}},
		{name: "final component file symlinks", sources: []string{"A"}, remove: "A/alias.cbz",
			setup: func(t *testing.T, root string) {
				symlink(t, filepath.Join(root, "A", "a1.cbz"), filepath.Join(root, "A", "alias.cbz"))
			}},
		{name: "directory replaced by a symlink between indexing and listing",
			sources: []string{"Alias/Series"},
			setup: func(t *testing.T, root string) {
				must(t, os.MkdirAll(filepath.Join(root, "Other", "Series"), 0o755))
			},
			mutate: func(t *testing.T, root string) {
				must(t, os.Remove(filepath.Join(root, "Alias")))
				symlink(t, filepath.Join(root, "Other"), filepath.Join(root, "Alias"))
			}},
		{name: "symlink replaced by a directory between indexing and listing",
			sources: []string{"Alias/Series"},
			mutate: func(t *testing.T, root string) {
				must(t, os.Remove(filepath.Join(root, "Alias")))
				must(t, os.MkdirAll(filepath.Join(root, "Alias", "Series"), 0o755))
			}},
		{name: "mount boundary crossed between indexing and listing",
			sources: []string{"Alias/Series"},
			setup: func(t *testing.T, root string) {
				other := crossDeviceDir(t, root)
				must(t, os.MkdirAll(filepath.Join(other, "Series"), 0o755))
				t.Setenv("VOLTIS_TEST_OTHER", other)
			},
			mutate: func(t *testing.T, root string) {
				must(t, os.Remove(filepath.Join(root, "Alias")))
				symlink(t, os.Getenv("VOLTIS_TEST_OTHER"), filepath.Join(root, "Alias"))
			}},
		{name: "directory replaced between indexing and listing", sources: []string{"Lib"},
			absent: []string{spell("Lib", "Series", "ch1.cbz")},
			mutate: func(t *testing.T, root string) {
				series := filepath.Join(root, "Lib", "Series")
				must(t, os.RemoveAll(series))
				must(t, os.MkdirAll(series, 0o755))
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { runSourceCase(t, c) })
	}
}
