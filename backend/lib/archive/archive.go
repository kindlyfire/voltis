package archive

import (
	"archive/zip"
	"bytes"
	"context"
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

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// Reader fails reads with the context's error once it ends.
func Reader(ctx context.Context, r io.Reader) io.Reader {
	if ctx.Done() == nil {
		return r
	}
	return ctxReader{ctx, r}
}

func readCloser(ctx context.Context, rc io.ReadCloser) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{Reader(ctx, rc), rc}
}

func Open(path string) (Archive, error) { return OpenContext(context.Background(), path) }

// OpenContext opens an archive whose reads and traversals stop when ctx ends.
func OpenContext(ctx context.Context, path string) (Archive, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".zip", ".cbz", ".epub":
		return openZip(ctx, path)
	case ".rar", ".cbr":
		return openRar(ctx, path)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, ext)
	}
}

// Zip

type zipArchive struct {
	ctx context.Context
	r   *zip.ReadCloser
}

func openZip(ctx context.Context, path string) (*zipArchive, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	return &zipArchive{ctx: ctx, r: r}, nil
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
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			return readCloser(z.ctx, rc), nil
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
	ctx    context.Context
	path   string
	files  []*rardecode.File
	byName map[string]*rardecode.File
}

func openRar(ctx context.Context, path string) (*rarArchive, error) {
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
	return &rarArchive{ctx: ctx, path: path, files: files, byName: byName}, nil
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

// OpenFile opens a non-solid entry directly; a solid one is only reachable by decoding the
// entries before it.
func (r *rarArchive) OpenFile(name string) (io.ReadCloser, error) {
	f, ok := r.byName[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrFileNotFound, name)
	}
	if !f.Solid {
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		return readCloser(r.ctx, rc), nil
	}

	rc, err := rardecode.OpenReader(r.path)
	if err != nil {
		return nil, err
	}
	for {
		// Next decodes the rest of a skipped solid entry unchecked, so drain it through the context.
		if _, err := io.Copy(io.Discard, Reader(r.ctx, rc)); err != nil {
			_ = rc.Close()
			return nil, err
		}
		header, err := rc.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			_ = rc.Close()
			return nil, err
		}
		if header.Name == name {
			return readCloser(r.ctx, rc), nil
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
		// Next decodes the rest of a skipped solid entry unchecked, so drain it through the context.
		if _, err := io.Copy(io.Discard, Reader(r.ctx, rc)); err != nil {
			return firstUnseen(err)
		}
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
			if data, err = io.ReadAll(Reader(r.ctx, rc)); err != nil {
				return firstUnseen(err)
			}
		}
		for _, i := range idx {
			seen[i] = true
			entry := Reader(r.ctx, rc)
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
