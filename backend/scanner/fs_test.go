package scanner

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(content), 0o644))
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
