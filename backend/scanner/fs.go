package scanner

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FSFile struct {
	Path  string
	Mtime time.Time
	Size  int64
}

func (f FSFile) HasChanged(other FSFile) bool {
	return !f.Mtime.Truncate(time.Millisecond).Equal(other.Mtime.Truncate(time.Millisecond)) || f.Size != other.Size
}

type walkFailure struct {
	Path string
	Err  error
}

type walkResult struct {
	Files    []FSFile
	Failures []walkFailure
}

var comicExtensions = map[string]bool{
	".cbz": true, ".zip": true, ".cbr": true, ".rar": true, ".pdf": true,
}

func (w *walkResult) visitor(eligible func(string) bool) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			w.Failures = append(w.Failures, walkFailure{Path: path, Err: err})
			return nil
		}
		if d == nil || d.IsDir() || !eligible(path) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			w.Failures = append(w.Failures, walkFailure{Path: path, Err: err})
			return nil
		}
		w.Files = append(w.Files, FSFile{
			Path:  path,
			Mtime: info.ModTime(),
			Size:  info.Size(),
		})
		return nil
	}
}

func walkSources(sources []string, eligible func(string) bool) (walkResult, error) {
	var result walkResult
	visit := result.visitor(eligible)

	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			return walkResult{}, err
		}

		if !info.IsDir() {
			if eligible(source) {
				result.Files = append(result.Files, FSFile{
					Path:  source,
					Mtime: info.ModTime(),
					Size:  info.Size(),
				})
			}
			continue
		}

		if err := filepath.WalkDir(source, visit); err != nil {
			return walkResult{}, err
		}
	}
	return result, nil
}

func pathWithin(path, root string) bool {
	if path == "" || root == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func withinAny(path string, roots []string) bool {
	for _, root := range roots {
		if pathWithin(path, root) {
			return true
		}
	}
	return false
}

func isComicFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return comicExtensions[ext]
}
