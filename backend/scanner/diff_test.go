package scanner

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func fingerprint(id, path string, mtime *time.Time, size *int, valid bool) Fingerprint {
	return Fingerprint{ID: id, Path: path, Mtime: mtime, Size: size, Valid: valid}
}

func TestChanged(t *testing.T) {
	mtime := baseTime
	size := 10
	stored := fingerprint("a", "/lib/a.cbz", &mtime, &size, true)

	cases := []struct {
		name   string
		file   FSFile
		fp     Fingerprint
		exists bool
		force  bool
		want   bool
	}{
		{"missing row", fsFile("/lib/a.cbz", baseTime, 10), Fingerprint{}, false, false, true},
		{"unchanged", fsFile("/lib/a.cbz", baseTime, 10), stored, true, false, false},
		{"force", fsFile("/lib/a.cbz", baseTime, 10), stored, true, true, true},
		{"invalid retry", fsFile("/lib/a.cbz", baseTime, 10), fingerprint("a", "/lib/a.cbz", &mtime, &size, false), true, false, true},
		{"size differs", fsFile("/lib/a.cbz", baseTime, 11), stored, true, false, true},
		{"sub-millisecond mtime", fsFile("/lib/a.cbz", baseTime.Add(400*time.Microsecond), 10), stored, true, false, false},
		{"millisecond mtime", fsFile("/lib/a.cbz", baseTime.Add(time.Millisecond), 10), stored, true, false, true},
		{"nil columns match zero file", FSFile{Path: "/lib/a.cbz"}, fingerprint("a", "/lib/a.cbz", nil, nil, true), true, false, false},
		{"nil columns differ", fsFile("/lib/a.cbz", baseTime, 10), fingerprint("a", "/lib/a.cbz", nil, nil, true), true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := changed(c.file, c.fp, c.exists, c.force); got != c.want {
				t.Fatalf("changed = %v, want %v", got, c.want)
			}
		})
	}
}

func mustResolveFile(t *testing.T, res *resolver, path string) string {
	t.Helper()
	key, ok := res.file(path)
	if !ok {
		t.Fatalf("resolve %s: unresolvable", path)
	}
	return key
}

func mustResolveDir(t *testing.T, res *resolver, path string) string {
	t.Helper()
	key, ok := res.dir(path)
	if !ok {
		t.Fatalf("resolve %s: unresolvable", path)
	}
	return key
}

type coverageFixture struct {
	*coverage
	t   *testing.T
	res *resolver
}

func (f coverageFixture) list(dir string, names ...string) []string {
	f.t.Helper()
	return f.listed(mustResolveDir(f.t, f.res, dir), names)
}

func coverageOf(t *testing.T, paths ...string) coverageFixture {
	t.Helper()
	res := newResolver()
	fps := map[string]Fingerprint{}
	for i, p := range paths {
		fps[mustResolveFile(t, res, p)] = Fingerprint{ID: string(rune('a' + i)), Path: p}
	}
	return coverageFixture{coverage: newCoverage(fps), t: t, res: res}
}

func TestCoverageProvesAbsence(t *testing.T) {
	c := coverageOf(t, "/lib/Foo/ch1.cbz", "/lib/Foo/ch2.cbz", "/lib/Foo/Sub/ch3.cbz", "/lib/Bar/ch4.cbz")

	if got := c.list("/lib", "Foo", "Bar"); got != nil {
		t.Fatalf("listed(/lib) = %v, want nil", got)
	}
	if got := c.list("/lib/Foo", "ch1.cbz", "Sub", "extra.txt"); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("missing leaf = %v, want [b]", got)
	}
	if got := c.list("/lib/Foo", "ch1.cbz", "Sub"); got != nil {
		t.Fatalf("second listing = %v, want nil", got)
	}
	if got := c.list("/lib/Foo", "ch1.cbz"); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("missing directory = %v, want [c]", got)
	}
	if got := c.list("/lib/Bar/Missing"); got != nil {
		t.Fatalf("unknown dir = %v, want nil", got)
	}
	if got := c.list("/lib/Bar"); !slices.Equal(got, []string{"d"}) {
		t.Fatalf("empty listing = %v, want [d]", got)
	}
}

func TestCoverageRetainsUnprovenPaths(t *testing.T) {
	c := coverageOf(t, "/lib/Foo/ch1.cbz", "/lib/Unreadable/ch2.cbz", "/lib/Foo Extra/ch3.cbz")

	if got := c.list("/lib", "Foo", "Unreadable", "Foo Extra"); got != nil {
		t.Fatalf("listed(/lib) = %v, want nil", got)
	}
	if got := c.list("/lib/Foo", "ch1.cbz"); got != nil {
		t.Fatalf("stat-failed name present = %v, want nil", got)
	}
	if got := c.list("/lib/Foo Extra", "ch3.cbz", "ch1.cbz"); got != nil {
		t.Fatalf("sibling directory = %v, want nil", got)
	}

	all := c.root.leaves(nil)
	slices.Sort(all)
	if !slices.Equal(all, []string{"a", "b", "c"}) {
		t.Fatalf("retained = %v, want all three", all)
	}
}

func TestCoverageProvesNothingWithoutAVerifiedIdentity(t *testing.T) {
	c := coverageOf(t, "/lib/Foo/ch1.cbz")

	if got := c.listed("", nil); got != nil {
		t.Fatalf("unverified listing = %v, want nil", got)
	}
	if left := c.root.leaves(nil); !slices.Equal(left, []string{"a"}) {
		t.Fatalf("retained = %v, want the leaf kept", left)
	}
}

func TestCoverageFilterBoundaries(t *testing.T) {
	c := coverageOf(t, "/lib/Foo/ch1.cbz", "/lib/Foo Extra/ch2.cbz")

	if got := c.list("/lib/Foo"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("filtered dir = %v, want [a]", got)
	}
	if left := c.root.leaves(nil); !slices.Equal(left, []string{"b"}) {
		t.Fatalf("outside filter = %v, want [b]", left)
	}
}

func TestCoveragePathParts(t *testing.T) {
	res := newResolver()
	cases := [][2]any{
		{"/voltis-absent/Foo", []string{"", "voltis-absent", "Foo"}},
		{"/voltis-absent//Foo/", []string{"", "voltis-absent", "Foo"}},
		{"/", []string{""}},
	}
	for _, c := range cases {
		if got := pathParts(mustResolveDir(t, res, c[0].(string))); !slices.Equal(got, c[1].([]string)) {
			t.Fatalf("pathParts(%q) = %v, want %v", c[0], got, c[1])
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"lib/Foo", ".", "./lib/Foo/", "..", "../Other/book.cbz"} {
		want := pathParts(mustResolveFile(t, res, filepath.Join(cwd, rel)))
		got := pathParts(mustResolveFile(t, res, rel))
		if !slices.Equal(got, want) {
			t.Fatalf("pathParts(%q) = %v, want %v", rel, got, want)
		}
		if slices.Contains(got, "..") {
			t.Fatalf("pathParts(%q) = %v, want no parent edge in the trie", rel, got)
		}
	}
}

func TestCoverageOverlappingRoots(t *testing.T) {
	root := filepath.Join("/lib", "Foo")
	c := coverageOf(t, filepath.Join(root, "ch1.cbz"), filepath.Join(root, "Sub", "ch2.cbz"))

	if got := c.list(root, "Sub"); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("listed root = %v, want [a]", got)
	}
	if got := c.list(filepath.Join(root, "Sub"), "ch2.cbz"); got != nil {
		t.Fatalf("nested listing = %v, want nil", got)
	}
}

func TestCoverageProvesAbsenceUnderRelativeRoot(t *testing.T) {
	c := coverageOf(t, "keep.cbz", "missing.cbz", filepath.Join("Sub", "ch1.cbz"), filepath.Join("Kept", "ch2.cbz"))

	if got := c.list(".", "keep.cbz", "Kept"); !slices.Equal(got, []string{"b", "c"}) {
		t.Fatalf("listed(.) = %v, want the removed file and subtree", got)
	}
	if got := c.list("Kept"); !slices.Equal(got, []string{"d"}) {
		t.Fatalf("listed(Kept) = %v, want [d]", got)
	}
	if left := c.root.leaves(nil); !slices.Equal(left, []string{"a"}) {
		t.Fatalf("retained = %v, want only the listed file", left)
	}
}
