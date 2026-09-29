// Package covers serves cover images: local ones resized from content files, and provider ones
// downloaded on first use.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"voltis/config"
	"voltis/db"
	"voltis/metadata"

	"github.com/cshum/vipsgen/vips"
	"golang.org/x/sync/singleflight"
)

const (
	maxWidth       = 750
	maxDownload    = 20 << 20
	maxDownloads   = 4
	maxRedirects   = 3
	requestTimeout = 10 * time.Second
	// A request that finds every download slot taken falls back rather than queue behind them.
	slotWait = time.Second
	// A failed cover, or any cover of a host that failed to answer, is not downloaded again sooner,
	// so an outage costs one timeout per host rather than one per cover.
	retryFailed = time.Minute
	// Older temporary files were left by a crash: downloads time out far sooner.
	staleTemp = time.Hour
)

type Cache struct {
	dir    string
	client *http.Client
	slots  chan struct{}
	group  singleflight.Group

	mu     sync.Mutex
	failed map[string]time.Time // cache path or host -> earliest retry
}

func New(dir string) *Cache {
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > maxRedirects || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
			return errors.New("cover redirect refused")
		}
		return nil
	}}
	return &Cache{dir: dir, client: client, slots: make(chan struct{}, maxDownloads), failed: map[string]time.Time{}}
}

// Version identifies the cover a content serves, for cache busting: a provider cover, else the
// local one, else nil when there is none.
func Version(ref *metadata.CoverRef, local bool, mtime *time.Time) *string {
	switch {
	case ref != nil:
		return new(hash(ref.URL)[:12])
	case local && mtime != nil:
		return new(strconv.FormatInt(mtime.UnixNano(), 36))
	case local:
		return new("0")
	}
	return nil
}

func hash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Local returns a content's resized local cover, cached until its file changes.
func (c *Cache) Local(contentID string, mtime *time.Time, read func() ([]byte, error)) ([]byte, error) {
	path := filepath.Join(c.dir, "covers", contentID+".jpg")
	if info, err := os.Stat(path); err == nil && (mtime == nil || !info.ModTime().Before(*mtime)) {
		if data, err := os.ReadFile(path); err == nil {
			return data, nil
		}
	}
	data, err := read()
	if err != nil {
		return nil, err
	}
	if data, err = resize(data); err != nil {
		return nil, err
	}
	_ = write(path, data)
	return data, nil
}

// Provider returns a provider cover, downloading it on first use.
func (c *Cache) Provider(ref metadata.CoverRef) ([]byte, error) {
	path := c.providerPath(ref)
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	data, err, _ := c.group.Do(path, func() (any, error) { return c.download(ref.URL, path) })
	if err != nil {
		return nil, err
	}
	return data.([]byte), nil
}

func (c *Cache) providerDir() string { return filepath.Join(c.dir, "provider-covers") }

func (c *Cache) providerPath(ref metadata.CoverRef) string {
	return filepath.Join(c.providerDir(), hash(ref.URL)+".jpg")
}

func (c *Cache) download(rawURL, path string) ([]byte, error) {
	// A download that finished between the caller's check and this call.
	if data, err := os.ReadFile(path); err == nil {
		return data, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if c.failedRecently(path) || c.failedRecently(u.Host) {
		return nil, errors.New("cover download failed recently")
	}

	select {
	case c.slots <- struct{}{}:
	case <-time.After(slotWait):
		return nil, errors.New("too many cover downloads")
	}
	defer func() { <-c.slots }()
	data, hostDown, err := c.fetch(rawURL)
	if err == nil {
		err = write(path, data)
	}
	if err != nil {
		c.fail(path)
		if hostDown {
			c.fail(u.Host)
		}
		return nil, err
	}
	return data, nil
}

func (c *Cache) failedRecently(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Now().Before(c.failed[key])
}

func (c *Cache) fail(key string) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	maps.DeleteFunc(c.failed, func(_ string, retry time.Time) bool { return !now.Before(retry) })
	c.failed[key] = now.Add(retryFailed)
}

// fetch downloads an image and re-encodes it, so only decodable images are cached. hostDown
// reports a failure of the host rather than of this cover. It does not use the caller's context:
// other requests may be waiting on the same download.
func (c *Cache) fetch(url string) (data []byte, hostDown bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "Voltis/"+config.AppVersion)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500, fmt.Errorf("download cover: HTTP %d", resp.StatusCode)
	}
	if data, err = io.ReadAll(io.LimitReader(resp.Body, maxDownload+1)); err != nil {
		return nil, true, err
	}
	if len(data) > maxDownload {
		return nil, false, errors.New("download cover: too large")
	}
	if !isImage(data) {
		return nil, false, errors.New("download cover: not an image")
	}
	data, err = resize(data)
	return data, false, err
}

// isImage reports whether data starts like an accepted image format. Without this check, vips
// would try every loader, including ones unfit for untrusted input (svg, pdf, magick).
func isImage(data []byte) bool {
	for _, sig := range [][]byte{[]byte("\xff\xd8\xff"), []byte("\x89PNG\r\n\x1a\n"), []byte("GIF87a"), []byte("GIF89a")} {
		if bytes.HasPrefix(data, sig) {
			return true
		}
	}
	if len(data) < 12 {
		return false
	}
	webp := string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP"
	avif := string(data[4:8]) == "ftyp" && (string(data[8:12]) == "avif" || string(data[8:12]) == "avis")
	return webp || avif
}

// GC removes provider covers that no metadata references.
func (c *Cache) GC(ctx context.Context, q db.Querier) error {
	refs, err := db.SelectScalars[metadata.CoverRef](ctx, q,
		"SELECT DISTINCT data->'cover' FROM content_metadata WHERE data ? 'cover'")
	if err != nil {
		return err
	}
	keep := map[string]bool{}
	for _, ref := range refs {
		keep[c.providerPath(ref)] = true
	}
	entries, err := os.ReadDir(c.providerDir())
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		path := filepath.Join(c.providerDir(), e.Name())
		if keep[path] || isDownloading(e) {
			continue
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// isDownloading reports a temporary file that a download may still be writing.
func isDownloading(e fs.DirEntry) bool {
	info, err := e.Info()
	return err == nil && filepath.Ext(e.Name()) == ".tmp" && time.Since(info.ModTime()) < staleTemp
}

func resize(data []byte) ([]byte, error) {
	opts := vips.DefaultThumbnailBufferOptions()
	opts.Height = 10000
	opts.Size = vips.SizeDown
	img, err := vips.NewThumbnailBuffer(data, maxWidth, opts)
	if err != nil {
		return nil, fmt.Errorf("resize cover: %w", err)
	}
	defer img.Close()

	jpegOpts := vips.DefaultJpegsaveBufferOptions()
	jpegOpts.Q = 85
	return img.JpegsaveBuffer(jpegOpts)
}

// EncodeJPEG re-encodes an untrusted image as JPEG at its own size.
func EncodeJPEG(data []byte) ([]byte, error) {
	if !isImage(data) {
		return nil, errors.New("encode jpeg: not an image")
	}
	img, err := vips.NewImageFromBuffer(data, nil)
	if err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	defer img.Close()
	// 8-bit sRGB first, so that 16-bit, grey and CMYK images flatten with MaxAlpha 255.
	if err := img.Colourspace(vips.InterpretationSrgb, nil); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	if err := img.Cast(vips.BandFormatUchar, nil); err != nil {
		return nil, fmt.Errorf("encode jpeg: %w", err)
	}
	if img.HasAlpha() { // JPEG has no alpha
		err := img.Flatten(&vips.FlattenOptions{Background: []float64{255, 255, 255}, MaxAlpha: 255})
		if err != nil {
			return nil, fmt.Errorf("encode jpeg: %w", err)
		}
	}
	opts := vips.DefaultJpegsaveBufferOptions()
	opts.Q = 90
	return img.JpegsaveBuffer(opts)
}

// write replaces path atomically, so readers never see a partial file.
func write(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), "*.tmp")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
