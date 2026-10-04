package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"slices"
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
	// Each calls fn for every named entry, in names order except for a solid RAR (archive order).
	// Its own failures are *EntryError; fn's errors are returned unchanged.
	Each(names []string, fn func(i int, r io.Reader) error) error
	Close() error
}

// EntryError is a failure of one named entry.
type EntryError struct {
	Index int // in names
	Name  string
	Err   error
}

func (e *EntryError) Error() string { return e.Name + ": " + e.Err.Error() }
func (e *EntryError) Unwrap() error { return e.Err }

// eachOpen calls fn for every name in order, with the entry open returns.
func eachOpen(names []string, open func(name string) (io.ReadCloser, error), fn func(int, io.Reader) error) error {
	for i, name := range names {
		rc, err := open(name)
		if err != nil {
			return &EntryError{Index: i, Name: name, Err: err}
		}
		err = fn(i, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
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

func (z *zipArchive) Each(names []string, fn func(int, io.Reader) error) error {
	return eachOpen(names, z.OpenFile, fn)
}

func (z *zipArchive) Close() error {
	return z.r.Close()
}

// Rar

type rarArchive struct {
	path   string
	files  []*rardecode.File
	byName map[string]*rardecode.File
}

func openRar(path string) (*rarArchive, error) {
	files, err := rardecode.List(path)
	if err != nil {
		return nil, err
	}
	byName := make(map[string]*rardecode.File, len(files))
	for _, f := range files {
		if _, ok := byName[f.Name]; !ok && !f.IsDir {
			byName[f.Name] = f
		}
	}
	return &rarArchive{path: path, files: files, byName: byName}, nil
}

func (r *rarArchive) List() ([]Entry, error) {
	var entries []Entry
	for _, f := range r.files {
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

// OpenFile opens a non-solid entry directly; a solid one is only reachable by decoding the
// entries before it.
func (r *rarArchive) OpenFile(name string) (io.ReadCloser, error) {
	f, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, name)
	}
	if !f.Solid {
		return f.Open()
	}

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

func (r *rarArchive) Each(names []string, fn func(int, io.Reader) error) error {
	solid := slices.ContainsFunc(names, func(name string) bool {
		f, ok := r.byName[name]
		return ok && f.Solid
	})
	if !solid {
		return eachOpen(names, r.OpenFile, fn)
	}

	want := make(map[string][]int, len(names))
	for i, name := range names {
		want[name] = append(want[name], i)
	}
	seen := make([]bool, len(names))
	firstUnseen := func(err error) error {
		i := slices.Index(seen, false)
		return &EntryError{Index: i, Name: names[i], Err: err}
	}

	rc, err := rardecode.OpenReader(r.path)
	if err != nil {
		return firstUnseen(err)
	}
	defer func() { _ = rc.Close() }()
	for len(want) > 0 {
		header, err := rc.Next()
		if err == io.EOF {
			return firstUnseen(ErrFileNotFound)
		}
		if err != nil {
			return firstUnseen(err)
		}
		idx := want[header.Name]
		if header.IsDir || len(idx) == 0 {
			continue
		}
		delete(want, header.Name)
		// A name wanted more than once is buffered, as the entry can be decoded only once.
		var data []byte
		if len(idx) > 1 {
			if data, err = io.ReadAll(rc); err != nil {
				return firstUnseen(err)
			}
		}
		for _, i := range idx {
			seen[i] = true
			var entry io.Reader = rc
			if len(idx) > 1 {
				entry = bytes.NewReader(data)
			}
			if err := fn(i, entry); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *rarArchive) Close() error {
	return nil
}
