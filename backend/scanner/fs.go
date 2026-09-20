package scanner

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type FSFile struct {
	Path  string
	Mtime time.Time
	Size  int64
}

var comicExtensions = map[string]bool{
	".cbz": true, ".zip": true, ".cbr": true, ".rar": true, ".pdf": true,
}

type resolved struct {
	path string
	ok   bool
}

type resolver struct {
	cwd  string
	dirs map[string]resolved
}

func newResolver() *resolver {
	cwd, _ := os.Getwd()
	if real, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = real
	}
	return &resolver{cwd: cwd, dirs: map[string]resolved{}}
}

func (r *resolver) file(path string) (string, bool) {
	dir, base := filepath.Split(path)
	switch base {
	case "", ".", "..":
		return r.dir(path)
	}
	parent, ok := r.dir(cmp.Or(dir, "."))
	if !ok {
		return "", false
	}
	return filepath.Join(parent, base), true
}

func (r *resolver) verified(path, real string) {
	r.dirs[trimSeparators(path)] = resolved{path: real, ok: true}
}

func (r *resolver) reject(path string) {
	r.dirs[trimSeparators(path)] = resolved{}
}

func (r *resolver) blocker(path string) (string, bool) {
	for dir := filepath.Dir(path); ; {
		if at, ok := r.dir(dir); ok {
			return at, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func (r *resolver) dir(path string) (string, bool) {
	path = trimSeparators(path)
	if hit, ok := r.dirs[path]; ok {
		return hit.path, hit.ok
	}
	out, ok := r.evalDir(path)
	r.dirs[path] = resolved{path: out, ok: ok}
	return out, ok
}

func (r *resolver) evalDir(path string) (string, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return r.abs(real), true
	}
	_, base := filepath.Split(path)
	if !errors.Is(err, fs.ErrNotExist) || base == "" || base == "." || base == ".." {
		return "", false
	}
	return r.file(path)
}

func (r *resolver) abs(path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(r.cwd, path)
}

func trimSeparators(path string) string {
	sep := string(filepath.Separator)
	if trimmed := strings.TrimRight(path, sep); trimmed != "" {
		return trimmed
	}
	if strings.HasPrefix(path, sep) {
		return sep
	}
	return path
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func withinAny(path string, roots []string) bool {
	return slices.ContainsFunc(roots, func(root string) bool { return pathWithin(path, root) })
}

func plainFile(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&(fs.ModeDir|fs.ModeSymlink) == 0
}

func isComicFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return comicExtensions[ext]
}

type EventKind uint8

const (
	Listed EventKind = iota
	Seen
	Failed
)

type Event struct {
	Kind  EventKind
	Path  string
	Dir   string
	Names []string
	Files []string
	File  FSFile
	Err   error
}

type walker struct {
	ctx      context.Context
	res      *resolver
	eligible func(string) bool
	out      chan<- Event
}

type walkRoot struct {
	path     string
	key      string
	resolved bool
	info     os.FileInfo
}

func walk(ctx context.Context, roots []string, eligible func(string) bool, out chan<- Event) error {
	res := newResolver()
	var all []walkRoot
	for _, root := range roots {
		info, err := os.Stat(root)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			key, ok := res.file(root)
			all = append(all, walkRoot{path: root, key: cmp.Or(key, root), resolved: ok, info: info})
			continue
		}
		if link, err := os.Lstat(root); err == nil && link.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		key, ok := res.dir(root)
		path := root
		if clean, _ := res.dir(filepath.Clean(root)); ok && clean != key {
			path = key
		}
		all = append(all, walkRoot{path: path, key: cmp.Or(key, root), resolved: ok, info: info})
	}

	slices.SortStableFunc(all, func(a, b walkRoot) int { return strings.Compare(a.key, b.key) })

	w := walker{ctx: ctx, res: res, eligible: eligible, out: out}
	var covered []string
	for _, root := range all {
		if root.resolved && withinAny(root.key, covered) {
			continue
		}
		covered = append(covered, root.key)
		if !root.info.IsDir() {
			if eligible(root.path) {
				file := FSFile{Path: root.path, Mtime: root.info.ModTime(), Size: root.info.Size()}
				if err := w.emit(Event{Kind: Seen, Path: root.path, File: file}); err != nil {
					return err
				}
			}
			continue
		}
		if err := w.dir(root.path); err != nil {
			return err
		}
	}
	return nil
}

func (w walker) emit(ev Event) error {
	select {
	case w.out <- ev:
		return nil
	case <-w.ctx.Done():
		return w.ctx.Err()
	}
}

func (w walker) list(path string) ([]os.DirEntry, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		return nil, "", err
	}
	slices.SortFunc(entries, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	return entries, w.identity(f, path), nil
}

func (w walker) identity(f *os.File, path string) string {
	listed, err := f.Stat()
	if err != nil {
		return ""
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	named, err := os.Stat(real)
	if err != nil || !os.SameFile(listed, named) {
		return ""
	}
	return w.res.abs(real)
}

func (w walker) dir(path string) error {
	entries, id, err := w.list(path)
	if err != nil {
		return w.emit(Event{Kind: Failed, Path: path, Err: err})
	}

	names := make([]string, len(entries))
	var files []string
	for i, e := range entries {
		names[i] = e.Name()
		if e.Type()&(fs.ModeDir|fs.ModeSymlink) == 0 {
			files = append(files, e.Name())
		}
	}
	if err := w.emit(Event{Kind: Listed, Path: path, Dir: id, Names: names, Files: files}); err != nil {
		return err
	}

	for _, e := range entries {
		child := filepath.Join(path, e.Name())
		if e.IsDir() {
			if err := w.dir(child); err != nil {
				return err
			}
			continue
		}
		if !w.eligible(child) {
			continue
		}
		info, infoErr := e.Info()
		if infoErr != nil {
			if err := w.emit(Event{Kind: Failed, Path: child, Err: infoErr}); err != nil {
				return err
			}
			continue
		}
		file := FSFile{Path: child, Mtime: info.ModTime(), Size: info.Size()}
		if err := w.emit(Event{Kind: Seen, Path: child, File: file}); err != nil {
			return err
		}
	}
	return nil
}
