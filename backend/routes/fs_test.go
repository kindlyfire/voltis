package routes

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func realTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func mkdirs(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := os.MkdirAll(filepath.Join(root, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func touch(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func listPath(c *testClient, path string, hidden bool) *response {
	q := url.Values{"path": {path}}
	if hidden {
		q.Set("hidden", "true")
	}
	return c.Get("/api/fs/list?" + q.Encode())
}

func entryNames(listing map[string]any) []string {
	var names []string
	for _, e := range listing["entries"].([]any) {
		names = append(names, s(e.(map[string]any)["name"]))
	}
	return names
}

func resolvePaths(t *testing.T, c *testClient, nearest bool, paths ...string) []resolveResultDTO {
	t.Helper()
	res := c.Post("/api/fs/resolve", map[string]any{"paths": paths, "nearest_existing": nearest}).Assert(t, 200)
	var body struct{ Results []resolveResultDTO }
	if err := json.Unmarshal(res.Body, &body); err != nil {
		t.Fatal(err)
	}
	return body.Results
}

func TestFsRequiresAdmin(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	member, _ := newMemberClient(t, admin)
	anonymous := admin.newSession(t)

	for c, code := range map[*testClient]int{admin: 200, member: 403, anonymous: 401} {
		c.Get("/api/fs/roots").Assert(t, code)
		listPath(c, "/", false).Assert(t, code)
		c.Post("/api/fs/resolve", map[string]any{"paths": []string{"/"}}).Assert(t, code)
	}
}

func TestFsRoots(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	res := c.Get("/api/fs/roots").Assert(t, 200).JSON()
	if res["mounts_error"] != nil {
		t.Fatalf("mounts_error = %v", res["mounts_error"])
	}
	var paths []string
	for _, m := range res["mounts"].([]any) {
		paths = append(paths, s(m.(map[string]any)["path"]))
	}
	if !slices.Contains(paths, "/") || slices.Contains(paths, "/proc") {
		t.Fatalf("mounts = %v, want / and no /proc", paths)
	}
}

func TestFsList(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := realTempDir(t)
	other := realTempDir(t)
	mkdirs(t, dir, "Books", "books", ".hidden", "with space", "ünïcode", "a+b#c")
	mkdirs(t, other, "Target")
	touch(t, filepath.Join(dir, "notes.txt"))
	mustSymlink(t, filepath.Join(other, "Target"), filepath.Join(dir, "link"))
	mustSymlink(t, filepath.Join(dir, "missing"), filepath.Join(dir, "broken"))
	mustSymlink(t, filepath.Join(dir, "notes.txt"), filepath.Join(dir, "filelink"))
	mustSymlink(t, filepath.Join(dir, "loop"), filepath.Join(dir, "loop"))

	listing := listPath(c, dir, false).Assert(t, 200).JSON()
	assertEq(t, s(listing["path"]), dir)
	assertEq(t, s(listing["parent"]), filepath.Dir(dir))
	assertEq(t, listing["truncated"].(bool), false)
	want := []string{"a+b#c", "Books", "books", "link", "with space", "ünïcode"}
	if got := entryNames(listing); !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	link := listing["entries"].([]any)[3].(map[string]any)
	assertEq(t, s(link["path"]), filepath.Join(dir, "link"))
	assertEq(t, link["symlink"].(bool), true)

	hidden := entryNames(listPath(c, dir, true).Assert(t, 200).JSON())
	assertEq(t, hidden[0], ".hidden")

	assertEq(t, s(listPath(c, filepath.Join(dir, "a+b#c"), false).Assert(t, 200).JSON()["path"]),
		filepath.Join(dir, "a+b#c"))
	assertEq(t, s(listPath(c, filepath.Join(dir, "link"), false).Assert(t, 200).JSON()["path"]),
		filepath.Join(other, "Target"))
	// ".." applies to the link target, not to the path as written.
	assertEq(t, s(listPath(c, filepath.Join(dir, "link")+"/..", false).Assert(t, 200).JSON()["path"]), other)
	assertEq(t, listPath(c, "/", false).Assert(t, 200).JSON()["parent"], nil)

	listPath(c, filepath.Join(dir, "missing"), false).Assert(t, 404)
	listPath(c, filepath.Join(dir, "broken"), false).Assert(t, 404)
	listPath(c, filepath.Join(dir, "notes.txt"), false).Assert(t, 400)
	listPath(c, filepath.Join(dir, "notes.txt", "x"), false).Assert(t, 400)
	listPath(c, filepath.Join(dir, "loop"), false).Assert(t, 400)
	listPath(c, "relative/path", false).Assert(t, 400)
	c.Get("/api/fs/list").Assert(t, 400)
}

func TestFsListPermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("permissions are not enforced for root")
	}
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := realTempDir(t)
	blocked := filepath.Join(dir, "Blocked")
	mkdirs(t, dir, "Blocked")
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(blocked, 0o755) })

	listPath(c, blocked, false).Assert(t, 403)
}

func TestFsListTruncates(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := realTempDir(t)
	mkdirs(t, dir, "a", "b", "c")
	touch(t, filepath.Join(dir, "f1"), filepath.Join(dir, "f2"), filepath.Join(dir, "f3"))

	oldFolders, oldEntries := listMaxFolders, listMaxEntries
	t.Cleanup(func() { listMaxFolders, listMaxEntries = oldFolders, oldEntries })
	cases := []struct {
		folders, entries int
		truncated        bool
		count            int
	}{
		// Files don't count against the folder cap.
		{folders: 3, entries: 6, count: 3},
		{folders: 2, entries: 6, truncated: true, count: 2},
		{folders: 3, entries: 5, truncated: true},
	}
	for _, tc := range cases {
		listMaxFolders, listMaxEntries = tc.folders, tc.entries
		listing := listPath(c, dir, false).Assert(t, 200).JSON()
		assertEq(t, listing["truncated"].(bool), tc.truncated)
		if tc.count > 0 {
			assertEq(t, len(entryNames(listing)), tc.count)
		}
	}
}

func TestFsResolve(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := realTempDir(t)
	other := realTempDir(t)
	mkdirs(t, dir, "Books")
	mkdirs(t, other, "Target")
	mustSymlink(t, filepath.Join(other, "Target"), filepath.Join(dir, "link"))
	touch(t, filepath.Join(dir, "notes.txt"))
	t.Chdir(dir)

	gone := filepath.Join(dir, "Books", "gone", "deeper")
	results := resolvePaths(t, c, false,
		filepath.Join(dir, "link"), "Books", "link/..", gone, filepath.Join(dir, "notes.txt"), "")
	paths := make([]string, len(results))
	for i, r := range results {
		if r.Path != nil {
			paths[i] = *r.Path
		}
	}
	if want := []string{filepath.Join(other, "Target"), filepath.Join(dir, "Books"), other, "", "", ""}; !slices.Equal(paths, want) {
		t.Fatalf("paths = %v, want %v", paths, want)
	}
	assertEq(t, results[1].Input, "Books")
	assertEq(t, results[3].Error, "Folder does not exist")
	assertEq(t, results[4].Error, "Not a folder")

	fallback := resolvePaths(t, c, true, gone, filepath.Join(dir, "link"))
	assertEq(t, *fallback[0].Path, filepath.Join(dir, "Books"))
	assertEq(t, fallback[0].FallbackFrom, gone)
	assertEq(t, fallback[1].FallbackFrom, "")

	c.Post("/api/fs/resolve", map[string]any{"paths": make([]string, 101)}).Assert(t, 400)
}

// The scanner skips symlinked source roots, so the picker hands back the resolved path.
func TestFsResolvedSymlinkScans(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir := realTempDir(t)
	real := filepath.Join(dir, "Real")
	mkdirs(t, real, "Series")
	writeCBZ(t, filepath.Join(real, "Series", "ch1.cbz"), "")
	link := filepath.Join(realTempDir(t), "Link")
	mustSymlink(t, real, link)

	picked := resolvePaths(t, c, false, link)[0].Path
	assertEq(t, *picked, real)
	libID := libraryAt(t, c, "comics", *picked)
	runScans(t, pool, c, map[string]any{"ids": []string{libID}})
	assertContentURIs(t, pool, libID, []string{"comic/Series", "comic/Series/ch1"})
}

func TestParseMountInfo(t *testing.T) {
	long := "/long/" + strings.Repeat("x", 100_000)
	input := strings.Join([]string{
		`22 1 8:1 / / rw,relatime shared:1 - ext4 /dev/sda1 rw`,
		`23 22 0:5 / /proc rw,nosuid shared:2 - proc proc rw`,
		`24 22 0:6 / /sys/fs/cgroup rw - cgroup2 cgroup2 rw`,
		`25 22 0:7 / /run/user/1000 rw - tmpfs tmpfs rw`,
		`26 22 0:8 / /mnt/my\040books rw master:3 propagate_from:2 - fuse.sshfs host:/b rw`,
		`27 22 0:9 / /media rw - nfs4 srv:/x rw`,
		`28 22 0:10 / /tmp rw - tmpfs tmpfs rw`,
		`29 22 0:11 / /back\134slash\011tab\134040 rw - xfs /dev/x rw`,
		`not a mount line`,
		`30 22 0:12 / /missing-fstype rw -`,
		`31 22 0:13 / /media rw - overlay overlay rw`,
		`32 22 8:1 /etc/hosts /etc/hosts rw - ext4 /dev/sda1 rw`,
		`33 22 0:14 / ` + long + ` rw - ext4 /dev/sdb rw`,
		`34 23 0:15 / /proc/sys/fs/binfmt_misc rw - binfmt_misc binfmt_misc rw`,
		`35 22 0:16 / /srv rw - autofs systemd-1 rw`,
		`36 22 8:2 / /efi rw - vfat /dev/sda2 rw`,
		`37 22 8:1 /@swap /swap rw - btrfs /dev/sda1 rw`,
		`38 22 8:1 /@var /var/lib/docker rw - btrfs /dev/sda1 rw`,
	}, "\n")

	mounts := parseMountInfo(input)
	want := []mountDTO{
		{Path: "/", FSType: "ext4"},
		{Path: "/back\\slash\ttab\\040", FSType: "xfs"},
		{Path: "/etc/hosts", FSType: "ext4"},
		{Path: long, FSType: "ext4"},
		{Path: "/media", FSType: "overlay"},
		{Path: "/mnt/my books", FSType: "fuse.sshfs"},
	}
	if !slices.Equal(mounts, want) {
		t.Fatalf("mounts = %v\nwant %v", mounts, want)
	}
}

func TestFsWorkKeepsSlotsOfStuckWorkers(t *testing.T) {
	old := fsTimeout
	fsTimeout = 20 * time.Millisecond
	t.Cleanup(func() { fsTimeout = old })

	release := make(chan struct{})
	stuck := func(context.Context) (int, error) {
		<-release
		return 0, nil
	}
	for range cap(fsSlots) {
		_, err := fsWork(context.Background(), stuck)
		assertEq(t, err, errFsTimeout)
	}
	assertEq(t, len(fsSlots), cap(fsSlots))

	ran := false
	_, err := fsWork(context.Background(), func(context.Context) (int, error) {
		ran = true
		return 0, nil
	})
	assertEq(t, err, errFsTimeout)
	assertEq(t, ran, false)

	close(release)
	waitUntil(t, "stuck workers to release their slots", func() bool { return len(fsSlots) == 0 })
	v, err := fsWork(context.Background(), func(context.Context) (int, error) { return 42, nil })
	assertEq(t, v, 42)
	assertEq(t, err, nil)
}
