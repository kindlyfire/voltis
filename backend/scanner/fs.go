package scanner

import (
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

var comicExtensions = map[string]bool{
	".cbz": true, ".zip": true, ".cbr": true, ".rar": true, ".pdf": true,
}

func walkSources(sources []string, eligible func(string) bool) ([]FSFile, error) {
	var files []FSFile
	for _, source := range sources {
		info, err := os.Stat(source)
		if err != nil {
			return nil, err
		}

		if !info.IsDir() {
			if eligible(source) {
				files = append(files, FSFile{
					Path:  source,
					Mtime: info.ModTime(),
					Size:  info.Size(),
				})
			}
			continue
		}

		err = filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // skip errors
			}
			if d.IsDir() {
				return nil
			}
			if !eligible(path) {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return nil
			}
			files = append(files, FSFile{
				Path:  path,
				Mtime: info.ModTime(),
				Size:  info.Size(),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}

func isComicFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return comicExtensions[ext]
}
