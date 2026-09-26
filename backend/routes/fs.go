package routes

import (
	"cmp"
	"context"
	"errors"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
)

// FsRoutes lets admins browse the server's filesystem to pick library sources.
type FsRoutes struct{}

func (fr *FsRoutes) Register(g *echo.Group) {
	g.GET("/roots", adminOnly(fr.roots))
	g.GET("/list", adminOnly(fr.list))
	g.POST("/resolve", adminOnly(fr.resolve))
}

var (
	fsSlots   = make(chan struct{}, 4)
	fsTimeout = 10 * time.Second
)

// fsWork runs fn in a goroutine, because a stat on a dead network mount can block forever and
// can't be cancelled. The slot is only released when fn returns, so stuck workers can't pile up.
func fsWork[T any](ctx context.Context, fn func(context.Context) (T, error)) (T, error) {
	var zero T
	ctx, cancel := context.WithTimeout(ctx, fsTimeout)
	defer cancel()
	select {
	case fsSlots <- struct{}{}:
	case <-ctx.Done():
		return zero, fsTimeoutErr(ctx)
	}

	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-fsSlots }()
		v, err := fn(ctx)
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		return zero, fsTimeoutErr(ctx)
	}
}

var errFsTimeout error = echo.NewHTTPError(http.StatusGatewayTimeout, "Timed out reading folder")

func fsTimeoutErr(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errFsTimeout
	}
	return ctx.Err()
}

type mountDTO struct {
	Path   string `json:"path"`
	FSType string `json:"fstype,omitempty"`
	// The mount point didn't answer a stat in time; it may be a dead network mount.
	TimedOut bool `json:"timed_out,omitempty"`
}

type rootsDTO struct {
	Mounts      []mountDTO `json:"mounts"`
	MountsError string     `json:"mounts_error,omitempty"`
}

var hiddenFSTypes = map[string]bool{
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true, "devpts": true, "devtmpfs": true,
	"securityfs": true, "debugfs": true, "tracefs": true, "configfs": true, "pstore": true,
	"mqueue": true, "hugetlbfs": true, "fusectl": true, "bpf": true, "nsfs": true,
	"binfmt_misc": true, "autofs": true,
}

// System, boot and scratch locations nobody keeps a library in.
var hiddenMountDirs = []string{"/proc", "/sys", "/dev", "/run", "/tmp", "/var", "/boot", "/efi", "/swap"}

// parseMountInfo parses /proc/self/mountinfo (see proc_pid_mountinfo(5)) and returns the mount
// points worth browsing, deduplicated and sorted.
func parseMountInfo(s string) []mountDTO {
	byPath := map[string]mountDTO{}
	for line := range strings.Lines(s) {
		if m, ok := parseMountLine(strings.TrimSuffix(line, "\n")); ok && !hiddenMount(m) {
			byPath[m.Path] = m
		}
	}
	mounts := slices.Collect(maps.Values(byPath))
	slices.SortFunc(mounts, func(a, b mountDTO) int { return strings.Compare(a.Path, b.Path) })
	return mounts
}

// Optional fields come between the mount options and the " - " separator, so fstype has no fixed
// index.
func parseMountLine(line string) (mountDTO, bool) {
	fields := strings.Split(line, " ")
	sep := slices.Index(fields, "-")
	if sep < 6 || sep+1 >= len(fields) {
		return mountDTO{}, false
	}
	return mountDTO{Path: unescapeMountField(fields[4]), FSType: fields[sep+1]}, true
}

func hiddenMount(m mountDTO) bool {
	return hiddenFSTypes[m.FSType] || slices.ContainsFunc(hiddenMountDirs, func(dir string) bool {
		return m.Path == dir || strings.HasPrefix(m.Path, dir+"/")
	})
}

// The kernel escapes space, tab, newline and backslash as octal (\040 \011 \012 \134).
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

func (fr *FsRoutes) roots(c echo.Context) error {
	res := rootsDTO{}
	var mounts []mountDTO
	if data, err := os.ReadFile("/proc/self/mountinfo"); err != nil {
		res.MountsError = err.Error()
	} else {
		mounts = parseMountInfo(string(data))
	}
	if !slices.ContainsFunc(mounts, func(m mountDTO) bool { return m.Path == "/" }) {
		mounts = append([]mountDTO{{Path: "/"}}, mounts...)
	}

	// Docker bind-mounts files such as /etc/hosts; only folders are useful here.
	keep := make([]bool, len(mounts))
	var wg sync.WaitGroup
	for i, m := range mounts {
		wg.Go(func() {
			_, err := fsWork(reqCtx(c), func(context.Context) (string, error) { return resolveDir(m.Path) })
			mounts[i].TimedOut = err == errFsTimeout
			keep[i] = err == nil || mounts[i].TimedOut
		})
	}
	wg.Wait()
	for i, m := range mounts {
		if keep[i] {
			res.Mounts = append(res.Mounts, m)
		}
	}
	return c.JSON(http.StatusOK, res)
}

type folderEntryDTO struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Symlink bool   `json:"symlink"`
}

type folderListingDTO struct {
	Path      string           `json:"path"`
	Parent    *string          `json:"parent"`
	Entries   []folderEntryDTO `json:"entries"`
	Truncated bool             `json:"truncated"`
}

var (
	listMaxEntries = 20000
	listMaxFolders = 2000
)

func (fr *FsRoutes) list(c echo.Context) error {
	q, err := BindQuery[struct {
		Path   string `query:"path" validate:"required"`
		Hidden bool   `query:"hidden"`
	}](c)
	if err != nil {
		return err
	}
	if !filepath.IsAbs(q.Path) {
		return echo.NewHTTPError(http.StatusBadRequest, "Path must be absolute")
	}
	listing, err := fsWork(reqCtx(c), func(ctx context.Context) (folderListingDTO, error) {
		dir, err := resolveDir(q.Path)
		if err != nil {
			return folderListingDTO{}, err
		}
		return listDir(ctx, dir, q.Hidden)
	})
	if err != nil {
		if status, msg := fsError(err); status != http.StatusInternalServerError {
			return echo.NewHTTPError(status, msg+": "+q.Path)
		}
		return err
	}
	return c.JSON(http.StatusOK, listing)
}

func listDir(ctx context.Context, dir string, hidden bool) (folderListingDTO, error) {
	f, err := os.Open(dir)
	if err != nil {
		return folderListingDTO{}, err
	}
	defer func() { _ = f.Close() }()

	res := folderListingDTO{Path: dir, Entries: []folderEntryDTO{}}
	if parent := filepath.Dir(dir); parent != dir {
		res.Parent = &parent
	}
	seen := 0
	for !res.Truncated {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		batch, err := f.ReadDir(256)
		for _, e := range batch {
			if seen++; seen > listMaxEntries {
				res.Truncated = true
				break
			}
			if !hidden && strings.HasPrefix(e.Name(), ".") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			symlink := e.Type()&fs.ModeSymlink != 0
			if symlink {
				if info, err := os.Stat(path); err != nil || !info.IsDir() {
					continue
				}
			} else if !e.IsDir() {
				continue
			}
			if len(res.Entries) == listMaxFolders {
				res.Truncated = true
				break
			}
			res.Entries = append(res.Entries, folderEntryDTO{Name: e.Name(), Path: path, Symlink: symlink})
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return res, err
		}
	}
	slices.SortFunc(res.Entries, func(a, b folderEntryDTO) int {
		return cmp.Or(strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), strings.Compare(a.Name, b.Name))
	})
	return res, nil
}

type resolveRequest struct {
	Paths           []string `json:"paths" validate:"max=100"`
	NearestExisting bool     `json:"nearest_existing"`
}

type resolveResultDTO struct {
	Input        string  `json:"input"`
	Path         *string `json:"path"`
	FallbackFrom string  `json:"fallback_from,omitempty"`
	Error        string  `json:"error,omitempty"`
}

func (fr *FsRoutes) resolve(c echo.Context) error {
	var req resolveRequest
	if err := c.Bind(&req); err != nil {
		return err
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}
	results, err := fsWork(reqCtx(c), func(context.Context) ([]resolveResultDTO, error) {
		cwd, err := os.Getwd()
		if err == nil {
			cwd, err = filepath.EvalSymlinks(cwd)
		}
		if err != nil {
			return nil, err
		}
		results := make([]resolveResultDTO, len(req.Paths))
		for i, input := range req.Paths {
			results[i] = resolveOne(cwd, input, req.NearestExisting)
		}
		return results, nil
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{"results": results})
}

// Relative inputs are legacy sources, which the scanner resolves against the working directory.
func resolveOne(cwd, input string, nearest bool) resolveResultDTO {
	res := resolveResultDTO{Input: input}
	if input == "" {
		res.Error = "Path is empty"
		return res
	}
	path := input
	if !filepath.IsAbs(path) {
		// Not filepath.Join, which would clean "link/.." lexically.
		path = cwd + string(filepath.Separator) + path
	}
	for {
		dir, err := resolveDir(path)
		if err == nil {
			res.Path = &dir
			return res
		}
		parent := filepath.Dir(path)
		if !nearest || !errors.Is(err, fs.ErrNotExist) || parent == path {
			_, res.Error = fsError(err)
			return res
		}
		res.FallbackFrom = cmp.Or(res.FallbackFrom, filepath.Clean(path))
		path = parent
	}
}

// The stat comes first: unlike filepath.EvalSymlinks, the kernel reports symlink loops as ELOOP.
// Both resolve ".." physically, so "/link/.." is the link target's parent.
func resolveDir(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", syscall.ENOTDIR
	}
	return filepath.EvalSymlinks(path)
}

// fsError maps a filesystem error to a status and message. Unknown errors map to a 500.
func fsError(err error) (int, string) {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return http.StatusNotFound, "Folder does not exist"
	case errors.Is(err, fs.ErrPermission):
		return http.StatusForbidden, "Permission denied"
	case errors.Is(err, syscall.ENOTDIR):
		return http.StatusBadRequest, "Not a folder"
	case errors.Is(err, syscall.ELOOP):
		return http.StatusBadRequest, "Symlink loop"
	}
	return http.StatusInternalServerError, err.Error()
}
