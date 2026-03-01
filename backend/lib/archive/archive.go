package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

var (
	ErrUnsupportedFormat = errors.New("unsupported archive format")
	ErrFileNotFound      = errors.New("file not found in archive")
)

type Entry struct {
	Name string
	Size int64
}

type Archive interface {
	List() ([]Entry, error)
	ReadFile(name string) ([]byte, error)
	OpenFile(name string) (io.ReadCloser, error)
	Close() error
}

func Open(path string) (Archive, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip", ".cbz", ".epub":
		return openZip(path)
	case ".rar", ".cbr":
		return openRar(path)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, ext)
	}
}

// Zip

type zipArchive struct {
	r *zip.ReadCloser
}

func openZip(path string) (*zipArchive, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	return &zipArchive{r: r}, nil
}

func (z *zipArchive) List() ([]Entry, error) {
	var entries []Entry
	for _, f := range z.r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		entries = append(entries, Entry{
			Name: f.Name,
			Size: int64(f.UncompressedSize64),
		})
	}
	return entries, nil
}

func (z *zipArchive) ReadFile(name string) ([]byte, error) {
	rc, err := z.OpenFile(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

func (z *zipArchive) OpenFile(name string) (io.ReadCloser, error) {
	for _, f := range z.r.File {
		if f.Name == name {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrFileNotFound, name)
}

func (z *zipArchive) Close() error {
	return z.r.Close()
}

// Rar

type rarArchive struct {
	path string
}

func openRar(path string) (*rarArchive, error) {
	// Validate we can open it
	files, err := rardecode.List(path)
	if err != nil {
		return nil, err
	}
	_ = files
	return &rarArchive{path: path}, nil
}

func (r *rarArchive) List() ([]Entry, error) {
	files, err := rardecode.List(r.path)
	if err != nil {
		return nil, err
	}
	var entries []Entry
	for _, f := range files {
		if f.IsDir {
			continue
		}
		entries = append(entries, Entry{
			Name: f.Name,
			Size: f.UnPackedSize,
		})
	}
	return entries, nil
}

func (r *rarArchive) ReadFile(name string) ([]byte, error) {
	rc, err := r.OpenFile(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return io.ReadAll(rc)
}

// rarFileReader wraps a rardecode.ReadCloser positioned at a specific entry.
type rarFileReader struct {
	archive *rardecode.ReadCloser
}

func (r *rarFileReader) Read(p []byte) (int, error) { return r.archive.Read(p) }
func (r *rarFileReader) Close() error               { return r.archive.Close() }

func (r *rarArchive) OpenFile(name string) (io.ReadCloser, error) {
	rc, err := rardecode.OpenReader(r.path)
	if err != nil {
		return nil, err
	}

	for {
		header, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = rc.Close()
			return nil, err
		}
		if header.Name == name {
			return &rarFileReader{archive: rc}, nil
		}
	}
	_ = rc.Close()
	return nil, fmt.Errorf("%w: %s", ErrFileNotFound, name)
}

func (r *rarArchive) Close() error {
	return nil
}
