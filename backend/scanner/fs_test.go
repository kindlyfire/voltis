package scanner

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"
)

type stubInfo struct {
	name  string
	size  int64
	mtime time.Time
}

func (s stubInfo) Name() string       { return s.name }
func (s stubInfo) Size() int64        { return s.size }
func (s stubInfo) Mode() fs.FileMode  { return 0 }
func (s stubInfo) ModTime() time.Time { return s.mtime }
func (s stubInfo) IsDir() bool        { return false }
func (s stubInfo) Sys() any           { return nil }

type stubEntry struct {
	name string
	dir  bool
	info fs.FileInfo
	err  error
}

func (s stubEntry) Name() string { return s.name }
func (s stubEntry) IsDir() bool  { return s.dir }
func (s stubEntry) Type() fs.FileMode {
	if s.dir {
		return fs.ModeDir
	}
	return 0
}
func (s stubEntry) Info() (fs.FileInfo, error) { return s.info, s.err }

func assertPaths(t *testing.T, label string, got []FSFile, want ...string) {
	t.Helper()
	paths := make([]string, len(got))
	for i, f := range got {
		paths[i] = f.Path
	}
	slices.Sort(paths)
	want = slices.Clone(want)
	slices.Sort(want)
	if !slices.Equal(paths, want) {
		t.Fatalf("%s: got %v, want %v", label, paths, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPathWithin(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct {
		name string
		path string
		root string
		want bool
	}{
		{"exact directory", "/lib/Foo", "/lib/Foo", true},
		{"exact file", "/lib/Foo/ch1.cbz", "/lib/Foo/ch1.cbz", true},
		{"nested child", "/lib/Foo/Sub/ch1.cbz", "/lib/Foo", true},
		{"prefix sibling dir", "/lib/Foo Extra/ch1.cbz", "/lib/Foo", false},
		{"prefix sibling exact", "/lib/Foo Extra", "/lib/Foo", false},
		{"filename suffix", "/lib/Foo/ch1.cbz.bak", "/lib/Foo/ch1.cbz", false},
		{"trailing separator on root", "/lib/Foo/ch1.cbz", "/lib/Foo" + sep, true},
		{"trailing separator on path", "/lib/Foo" + sep, "/lib/Foo", true},
		{"repeated separators", "/lib//Foo///ch1.cbz", "/lib/Foo", true},
		{"dot component in path", "/lib/./Foo/ch1.cbz", "/lib/Foo", true},
		{"dot component in root", "/lib/Foo/ch1.cbz", "/lib/./Foo", true},
		{"dotdot escapes root", "/lib/Foo/../Bar/ch1.cbz", "/lib/Foo", false},
		{"filesystem root", "/lib/Foo/ch1.cbz", "/", true},
		{"root within itself", "/", "/", true},
		{"parent of root", "/lib", "/lib/Foo", false},
		{"relative paths", "Foo/Sub/ch1.cbz", "Foo", true},
		{"relative sibling", "Foo Extra/ch1.cbz", "Foo", false},
		{"relative path absolute root", "Foo/ch1.cbz", "/lib/Foo", false},
		{"absolute path relative root", "/lib/Foo/ch1.cbz", "Foo", false},
		{"empty path", "", "/lib/Foo", false},
		{"empty root", "/lib/Foo/ch1.cbz", "", false},
		{"both empty", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pathWithin(c.path, c.root); got != c.want {
				t.Fatalf("pathWithin(%q, %q) = %v, want %v", c.path, c.root, got, c.want)
			}
		})
	}
}

func TestWalkSources(t *testing.T) {
	dir := t.TempDir()
	series := filepath.Join(dir, "Series")
	nested := filepath.Join(series, "Sub", "ch2.cbz")
	writeFile(t, filepath.Join(series, "ch1.cbz"), "one")
	writeFile(t, nested, "twotwo")
	writeFile(t, filepath.Join(series, "notes.txt"), "ignored")

	extra := filepath.Join(dir, "Extra")
	explicit := filepath.Join(extra, "ch3.cbz")
	writeFile(t, explicit, "three!")
	writeFile(t, filepath.Join(extra, "readme.txt"), "ignored")

	result, err := walkSources([]string{series, explicit, filepath.Join(extra, "readme.txt")}, isComicFile)
	if err != nil {
		t.Fatalf("walkSources: %v", err)
	}
	if len(result.Failures) != 0 {
		t.Fatalf("unexpected failures: %v", result.Failures)
	}
	assertPaths(t, "files", result.Files, filepath.Join(series, "ch1.cbz"), nested, explicit)

	for _, f := range result.Files {
		info, err := os.Stat(f.Path)
		if err != nil {
			t.Fatal(err)
		}
		if f.Size != info.Size() || !f.Mtime.Equal(info.ModTime()) {
			t.Fatalf("%s: %d/%v, want %d/%v", f.Path, f.Size, f.Mtime, info.Size(), info.ModTime())
		}
	}

	result, err = walkSources([]string{series, filepath.Join(dir, "Missing")}, isComicFile)
	if err == nil {
		t.Fatal("expected error for missing source")
	}
	if len(result.Files) != 0 || len(result.Failures) != 0 {
		t.Fatalf("expected empty result, got %+v", result)
	}
}

func TestWalkVisitorFailures(t *testing.T) {
	var w walkResult
	visit := w.visitor(isComicFile)
	call := func(path string, d fs.DirEntry, err error) {
		t.Helper()
		if got := visit(path, d, err); got != nil {
			t.Fatalf("visitor(%q) = %v, want nil", path, got)
		}
	}
	readErr := errors.New("permission denied")
	infoErr := errors.New("stat failed")
	mtime := time.Unix(1700000000, 0)

	call("/lib/Broken", stubEntry{name: "Broken", dir: true}, readErr)
	call("/lib/Vanished", nil, readErr)
	call("/lib/Broken/ch1.cbz", stubEntry{name: "ch1.cbz", info: stubInfo{name: "ch1.cbz", size: 12, mtime: mtime}}, nil)
	call("/lib/Good/ch2.cbz", stubEntry{name: "ch2.cbz", info: stubInfo{name: "ch2.cbz", size: 3, mtime: mtime}}, nil)
	call("/lib/Good/ch3.cbz", stubEntry{name: "ch3.cbz", err: infoErr}, nil)
	call("/lib/Good/notes.txt", stubEntry{name: "notes.txt", err: infoErr}, nil)
	call("/lib/Good/Sub", stubEntry{name: "Sub", dir: true}, nil)
	call("/lib/Good/ch4.cbz", stubEntry{name: "ch4.cbz", info: stubInfo{name: "ch4.cbz", size: 5, mtime: mtime}}, nil)

	want := []walkFailure{
		{Path: "/lib/Broken", Err: readErr},
		{Path: "/lib/Vanished", Err: readErr},
		{Path: "/lib/Good/ch3.cbz", Err: infoErr},
	}
	if !reflect.DeepEqual(w.Failures, want) {
		t.Fatalf("failures = %+v, want %+v", w.Failures, want)
	}
	assertPaths(t, "files", w.Files, "/lib/Broken/ch1.cbz", "/lib/Good/ch2.cbz", "/lib/Good/ch4.cbz")
	if w.Files[0].Size != 12 || !w.Files[0].Mtime.Equal(mtime) {
		t.Fatalf("file = %+v", w.Files[0])
	}
}
