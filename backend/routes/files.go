package routes

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/archive"
	"voltis/lib/comic"
	"voltis/lib/epub"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type FileRoutes struct {
	pool   *pgxpool.Pool
	covers *covers.Cache
}

func (fr *FileRoutes) Register(g *echo.Group) {
	g.GET("/cover/:content_id", fr.getCover)
	g.GET("/comic-page/:content_id/:page_index", fr.getComicPage)
	g.GET("/book-chapters/:content_id", fr.getBookChapters)
	g.GET("/book-chapter/:content_id", fr.getBookChapter)
	g.GET("/book-resource/:content_id", fr.getBookResource)
	g.GET("/download-info/:content_id", fr.getDownloadInfo)
	g.GET("/download/:content_id", fr.download)
	g.GET("/offline/:content_id", fr.offline)
}

func (fr *FileRoutes) getCover(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	type coverRow struct {
		models.Content
		Cover *metadata.CoverRef `db:"cover"`
	}
	r, err := db.SelectOne[coverRow](reqCtx(c), fr.pool, `
		SELECT `+models.ContentColumns("")+`, data->'cover' AS cover FROM content WHERE id = $1
	`, c.Param("content_id"))
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	version := covers.Version(r.Cover, r.CoverURI != nil, r.FileMtime)
	if r.Cover != nil {
		data, err := fr.covers.Provider(*r.Cover)
		if err == nil {
			return blobCover(c, data, version)
		}
		slog.Warn("provider cover unavailable", "content_id", r.ID, "err", err)
		// Neither the fallback nor a 404 may stay cached once the provider cover loads.
		c.Response().Header().Set("Cache-Control", "no-store")
		version = nil
	}
	if r.CoverURI == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content has no cover")
	}
	data, err := fr.covers.Local(r.ID, r.FileMtime, func() ([]byte, error) {
		data, _, err := readContentFile(*r.CoverURI)
		return data, err
	})
	if err != nil {
		return err
	}
	return blobCover(c, data, version)
}

// coverMediaType is the type of every cover, since covers.resize re-encodes them all to JPEG.
const coverMediaType = "image/jpeg"

// blobCover serves a cover, cached for good only under its own version so that a stale URL cannot
// pin the cover that replaced it. A nil version marks a fallback, which is not cached at all.
func blobCover(c echo.Context, data []byte, version *string) error {
	cache := "no-store"
	if version != nil {
		cache = "no-cache"
		if c.QueryParam("v") == *version {
			cache = "public, max-age=31536000, immutable"
		}
	}
	c.Response().Header().Set("Cache-Control", cache)
	return c.Blob(http.StatusOK, coverMediaType, data)
}

func (fr *FileRoutes) getComicPage(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	var pageIndex int
	if _, err := fmt.Sscanf(c.Param("page_index"), "%d", &pageIndex); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid page index")
	}

	_, _, data, mediaType, err := comicPage(reqCtx(c), fr.pool, c.Param("content_id"), pageIndex)
	if err != nil {
		return err
	}

	if c.QueryParam("v") != "" {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	return c.Blob(http.StatusOK, mediaType, data)
}

// comicPage reads page index of a comic, and returns the names of all its pages.
func comicPage(ctx context.Context, pool *pgxpool.Pool, contentID string, index int) (
	content models.Content, names []string, data []byte, mediaType string, err error,
) {
	content, pages, err := contentPages(ctx, pool, contentID)
	if err != nil {
		return
	}
	if index < 0 || index >= len(pages) {
		err = echo.NewHTTPError(http.StatusNotFound, "Page index out of range")
		return
	}
	names = pageNames(pages)
	data, mediaType, err = readArchiveEntry(*content.FileURI, names[index])
	return
}

// contentPages loads a content row and its pages; 404 when it has none.
func contentPages(ctx context.Context, pool *pgxpool.Pool, id string) (models.Content, []comic.PageInfo, error) {
	content, err := getContent(ctx, pool, id)
	if err != nil {
		return models.Content{}, nil, err
	}
	var pages []comic.PageInfo
	if content.FileURI != nil {
		pages, err = comicPages(content.FileData)
	}
	if err != nil || len(pages) == 0 {
		return models.Content{}, nil, echo.NewHTTPError(http.StatusNotFound, "Content has no pages")
	}
	return content, pages, nil
}

// comicPages reads file_data's [name] or [name, width, height] tuples.
func comicPages(fileData []byte) ([]comic.PageInfo, error) {
	var fd struct {
		Pages [][]json.RawMessage `json:"pages"`
	}
	if err := json.Unmarshal(fileData, &fd); err != nil {
		return nil, err
	}
	pages := make([]comic.PageInfo, len(fd.Pages))
	for i, p := range fd.Pages {
		if len(p) == 0 {
			return nil, errors.New("invalid page data")
		}
		if err := json.Unmarshal(p[0], &pages[i].Name); err != nil {
			return nil, err
		}
		if len(p) >= 3 {
			// An unparsable size stays 0, like an undecodable page.
			_ = json.Unmarshal(p[1], &pages[i].Width)
			_ = json.Unmarshal(p[2], &pages[i].Height)
		}
	}
	return pages, nil
}

func comicPageNames(fileData []byte) ([]string, error) {
	pages, err := comicPages(fileData)
	if err != nil {
		return nil, err
	}
	return pageNames(pages), nil
}

func pageNames(pages []comic.PageInfo) []string {
	return fp.Map(pages, func(p comic.PageInfo) string { return p.Name })
}

var errPageTooLarge = errors.New("page too large")

const (
	offlineMediaType   = "application/vnd.voltis.pages"
	maxOfflineManifest = 4 << 20
	maxOfflinePage     = 256 << 20
	offlineEndIndex    = math.MaxUint32
	offlineErrorIndex  = math.MaxUint32 - 1
)

type offlinePage struct {
	Name      string `json:"name"`
	MediaType string `json:"media_type"`
	Width     *int   `json:"width"`
	Height    *int   `json:"height"`
}

type offlineManifest struct {
	Format    int           `json:"format"`
	ContentID string        `json:"content_id"`
	Version   string        `json:"version"`
	FileSize  *int          `json:"file_size"`
	FileMtime *time.Time    `json:"file_mtime"`
	PageCount int           `json:"page_count"`
	From      int           `json:"from"`
	Pages     []offlinePage `json:"pages"`
}

// offline streams a comic's pages from index from on as frames (manifest, pages, then an end or
// error frame), so a client can download them for offline reading and resume an aborted download.
func (fr *FileRoutes) offline(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}
	ctx := reqCtx(c)

	content, pages, err := contentPages(ctx, fr.pool, c.Param("content_id"))
	if err != nil {
		return err
	}
	if content.Type != "comic" {
		return echo.NewHTTPError(http.StatusBadRequest, "Content is not a comic")
	}
	from := 0
	if q := c.QueryParam("from"); q != "" {
		if from, err = strconv.Atoi(q); err != nil || from < 0 || from > len(pages) {
			return echo.NewHTTPError(http.StatusBadRequest, "Invalid from")
		}
	}
	path := *content.FileURI
	if err := checkScannedFile(path, content.FileMtime, content.FileSize); err != nil {
		return err
	}

	names := pageNames(pages)
	wanted := names[from:]
	isPDF := strings.ToLower(filepath.Ext(path)) == ".pdf"
	var a archive.Archive
	if !isPDF {
		if a, err = archive.Open(path); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		defer func() { _ = a.Close() }()
	}

	m := offlineManifest{
		Format: 1, ContentID: content.ID, Version: offlineVersion(content, names),
		FileSize: content.FileSize, PageCount: len(pages), From: from,
		Pages: make([]offlinePage, len(pages)),
	}
	if content.FileMtime != nil {
		t := content.FileMtime.UTC()
		m.FileMtime = &t
	}
	for i, p := range pages {
		m.Pages[i] = offlinePage{Name: p.Name, MediaType: "image/jpeg", Width: nonZero(p.Width), Height: nonZero(p.Height)}
		if !isPDF {
			m.Pages[i].MediaType = guessMediaType(p.Name)
		}
	}
	manifest, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if len(manifest) > maxOfflineManifest {
		return echo.NewHTTPError(http.StatusUnprocessableEntity, "Too many pages")
	}

	w := c.Response()
	w.Header().Set(echo.HeaderContentType, offlineMediaType)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	// An error returned after the header would end the chunked body cleanly, which a client could
	// not tell from a complete stream, so transport errors abort the connection instead.
	if err := writeFrame(w, manifest); err != nil {
		panic(http.ErrAbortHandler)
	}

	// send writes page i of wanted, once the context and the page's size are checked.
	send := func(i int, read func() ([]byte, error)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		data, err := read()
		if err == nil && len(data) > maxOfflinePage {
			err = errPageTooLarge
		}
		if err != nil {
			return &archive.EntryError{Index: i, Name: wanted[i], Err: err}
		}
		return writeFrame(w, data, uint32(from+i))
	}
	if isPDF {
		for i, name := range wanted {
			if err = send(i, func() ([]byte, error) { return renderPDFPage(path, name) }); err != nil {
				break
			}
		}
	} else {
		var buf bytes.Buffer
		err = a.Each(wanted, func(i int, r io.Reader) error {
			return send(i, func() ([]byte, error) {
				buf.Reset()
				_, err := buf.ReadFrom(io.LimitReader(r, maxOfflinePage+1))
				return buf.Bytes(), err
			})
		})
	}
	if ctx.Err() != nil {
		return nil
	}
	if entryErr, ok := errors.AsType[*archive.EntryError](err); ok {
		page := from + entryErr.Index
		slog.Warn("offline page unavailable", "content_id", content.ID, "page", page, "name", entryErr.Name, "err", entryErr.Err)
		// Other errors may carry server paths.
		reason := "unreadable"
		switch {
		case errors.Is(entryErr.Err, archive.ErrFileNotFound):
			reason = archive.ErrFileNotFound.Error()
		case errors.Is(entryErr.Err, errPageTooLarge):
			reason = errPageTooLarge.Error()
		}
		if writeFrame(w, fmt.Appendf(nil, "page %d: %s", page, reason), offlineErrorIndex) != nil {
			panic(http.ErrAbortHandler)
		}
		return nil
	}
	if err != nil || writeFrame(w, nil, offlineEndIndex) != nil {
		panic(http.ErrAbortHandler)
	}
	return nil
}

// checkScannedFile answers 404 when path is not a file, and 409 when it no longer matches the
// last scan, whose page list would then not fit it. The scanner records os.Stat for a file given
// as a source root but lstat-like info for files inside directories, so either may match.
func checkScannedFile(path string, mtime *time.Time, size *int) error {
	fi, err := os.Stat(path)
	if err != nil || fi.IsDir() {
		return echo.NewHTTPError(http.StatusNotFound, "File not found")
	}
	matches := func(fi os.FileInfo) bool {
		return (size == nil || int64(*size) == fi.Size()) &&
			(mtime == nil || fi.ModTime().Truncate(time.Millisecond).Equal(mtime.Truncate(time.Millisecond)))
	}
	if matches(fi) {
		return nil
	}
	if li, err := os.Lstat(path); err == nil && matches(li) {
		return nil
	}
	return echo.NewHTTPError(http.StatusConflict, "File changed since the last scan")
}

// offlineVersion identifies the last scan of a comic's file; a null mtime or size hashes as "".
func offlineVersion(content models.Content, names []string) string {
	var mtime, size string
	if content.FileMtime != nil {
		mtime = content.FileMtime.UTC().Format(time.RFC3339Nano)
	}
	if content.FileSize != nil {
		size = strconv.Itoa(*content.FileSize)
	}
	parts := append([]string{*content.FileURI, mtime, size}, names...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])[:32]
}

// writeFrame writes the header fields and data's length as big-endian u32s, then data, and
// flushes them.
func writeFrame(w http.ResponseWriter, data []byte, header ...uint32) error {
	buf := make([]byte, 0, 4*len(header)+4)
	for _, h := range append(header, uint32(len(data))) {
		buf = binary.BigEndian.AppendUint32(buf, h)
	}
	if _, err := w.Write(buf); err != nil {
		return err
	}
	if _, err := w.Write(data); err != nil {
		return err
	}
	return http.NewResponseController(w).Flush()
}

func nonZero(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

func (fr *FileRoutes) getBookChapters(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, fr.pool, contentID)
	if err != nil {
		return err
	}

	if content.FileURI == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if !strings.HasSuffix(strings.ToLower(*content.FileURI), ".epub") {
		return echo.NewHTTPError(http.StatusBadRequest, "Content is not an EPUB")
	}

	structure, err := epub.BuildStructure(*content.FileURI)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var fileData struct {
		Words map[string]int `json:"words"`
	}
	if json.Unmarshal(content.FileData, &fileData) == nil {
		for i, item := range structure.Spine {
			structure.Spine[i].Words = fileData.Words[item.Href]
		}
	}

	return c.JSON(http.StatusOK, structure)
}

func (fr *FileRoutes) getBookChapter(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")
	href := c.QueryParam("href")

	content, err := getContent(ctx, fr.pool, contentID)
	if err != nil {
		return err
	}

	if content.FileURI == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if !strings.HasSuffix(strings.ToLower(*content.FileURI), ".epub") {
		return echo.NewHTTPError(http.StatusBadRequest, "Content is not an EPUB")
	}

	chapterContent, err := epub.ReadChapter(*content.FileURI, href)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Chapter not found")
	}

	c.Response().Header().Set("Content-Disposition", "attachment")
	cacheIfVersioned(c)
	return blobUntrusted(c, "application/xhtml+xml", []byte(chapterContent))
}

func (fr *FileRoutes) getBookResource(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")
	resourcePath := c.QueryParam("path")

	content, err := getContent(ctx, fr.pool, contentID)
	if err != nil {
		return err
	}

	if content.FileURI == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}

	if !strings.HasSuffix(strings.ToLower(*content.FileURI), ".epub") {
		return echo.NewHTTPError(http.StatusBadRequest, "Content is not an EPUB")
	}

	entry, err := epub.NormalizeArchivePath(resourcePath)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "Invalid resource path")
	}

	data, mediaType, err := readArchiveEntry(*content.FileURI, entry)
	if err != nil {
		return err
	}
	cacheIfVersioned(c)
	return blobUntrusted(c, mediaType, data)
}

// cacheIfVersioned lets the browser keep a response whose URL carries the
// file's version (`v`), which changes whenever the file does.
func cacheIfVersioned(c echo.Context) {
	if c.QueryParam("v") != "" {
		c.Response().Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	}
}

func blobUntrusted(c echo.Context, mediaType string, data []byte) error {
	h := c.Response().Header()
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, mediaType, data)
}

type downloadInfoResponse struct {
	FileCount int  `json:"file_count"`
	TotalSize *int `json:"total_size"`
}

func (fr *FileRoutes) getDownloadInfo(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, fr.pool, contentID)
	if err != nil {
		return err
	}

	if content.Type == "comic" || content.Type == "book" {
		if content.FileURI == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Content has no file")
		}
		return c.JSON(http.StatusOK, downloadInfoResponse{
			FileCount: 1,
			TotalSize: content.FileSize,
		})
	}

	// Series: aggregate children
	var stats struct {
		FileCount int  `db:"file_count"`
		TotalSize *int `db:"total_size"`
	}
	err = fr.pool.QueryRow(ctx, `
		SELECT COUNT(*) AS file_count, SUM(file_size)::bigint AS total_size
		FROM content WHERE parent_id = $1 AND file_uri IS NOT NULL
	`, contentID).Scan(&stats.FileCount, &stats.TotalSize)
	if err != nil {
		return err
	}
	if stats.FileCount == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "No downloadable files")
	}

	return c.JSON(http.StatusOK, downloadInfoResponse{
		FileCount: stats.FileCount,
		TotalSize: stats.TotalSize,
	})
}

func (fr *FileRoutes) download(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := getContent(ctx, fr.pool, contentID)
	if err != nil {
		return err
	}

	if content.Type == "comic" || content.Type == "book" {
		if content.FileURI == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Content has no file")
		}
		return serveContentFile(c, *content.FileURI)
	}

	// Series: stream a ZIP of all children's files
	type childFile struct {
		FileURI string `db:"file_uri"`
		URIPart string `db:"uri_part"`
	}
	children, err := db.Select[childFile](ctx, fr.pool, `
		SELECT file_uri, uri_part FROM content
		WHERE parent_id = $1 AND file_uri IS NOT NULL
		ORDER BY "order"
	`, contentID)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "No downloadable files")
	}

	zipFilename := content.URIPart + ".zip"
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, zipFilename))
	c.Response().WriteHeader(http.StatusOK)

	zw := zip.NewWriter(c.Response())
	defer func() { _ = zw.Close() }()

	for _, child := range children {
		name := filepath.Base(child.FileURI)
		w, err := zw.CreateHeader(&zip.FileHeader{
			Name:   name,
			Method: zip.Store,
		})
		if err != nil {
			return err
		}

		f, err := os.Open(child.FileURI)
		if err != nil {
			return err
		}
		_, err = io.Copy(w, f)
		_ = f.Close()
		if err != nil {
			return err
		}
	}

	return nil
}

// File reading helpers

var archiveExtensions = map[string]bool{
	".zip": true, ".cbz": true, ".cbr": true, ".rar": true, ".epub": true, ".pdf": true,
}

var pdfPagePattern = regexp.MustCompile(`^p(\d+)$`)

func readContentFile(uri string) ([]byte, string, error) {
	// Try as a regular file first
	if data, err := os.ReadFile(uri); err == nil {
		mediaType := guessMediaType(uri)
		return data, mediaType, nil
	}

	// Walk up the path to find an archive
	archivePath, innerPath := findArchiveAndInnerPath(uri)
	if archivePath == "" {
		return nil, "", echo.NewHTTPError(http.StatusNotFound, "File not found")
	}

	return readArchiveEntry(archivePath, innerPath)
}

func readArchiveEntry(archivePath, innerPath string) ([]byte, string, error) {
	// PDF page rendering via pdftoppm
	if strings.ToLower(filepath.Ext(archivePath)) == ".pdf" {
		return readPDFPage(archivePath, innerPath)
	}

	a, err := archive.Open(archivePath)
	if err != nil {
		return nil, "", echo.NewHTTPError(http.StatusNotFound, "File not found")
	}
	defer func() { _ = a.Close() }()

	data, err := a.ReadFile(innerPath)
	if err != nil {
		return nil, "", echo.NewHTTPError(http.StatusNotFound, "File not found")
	}

	mediaType := guessMediaType(innerPath)
	return data, mediaType, nil
}

var errInvalidPDFPage = errors.New("invalid PDF page identifier")

func readPDFPage(pdfPath, innerPath string) ([]byte, string, error) {
	data, err := renderPDFPage(pdfPath, innerPath)
	if errors.Is(err, errInvalidPDFPage) {
		return nil, "", echo.NewHTTPError(http.StatusBadRequest, "Invalid PDF page identifier")
	}
	if err != nil {
		return nil, "", echo.NewHTTPError(http.StatusInternalServerError, "PDF rendering failed")
	}
	return data, "image/jpeg", nil
}

// renderPDFPage renders page pN of a PDF as a JPEG.
func renderPDFPage(pdfPath, name string) ([]byte, error) {
	m := pdfPagePattern.FindStringSubmatch(name)
	if m == nil {
		return nil, errInvalidPDFPage
	}
	page := m[1]

	cmd := exec.Command("pdftoppm",
		"-r", "250",
		"-jpeg", "-jpegopt", "quality=90",
		"-singlefile",
		"-f", page, "-l", page,
		pdfPath,
	)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("PDF rendering failed: %w", err)
	}
	return data, nil
}

func findArchiveAndInnerPath(uri string) (string, string) {
	parts := strings.Split(filepath.ToSlash(uri), "/")
	for i := len(parts) - 1; i > 0; i-- {
		candidate := strings.Join(parts[:i], "/")
		ext := strings.ToLower(filepath.Ext(candidate))
		if !archiveExtensions[ext] {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			innerPath := strings.Join(parts[i:], "/")
			return candidate, innerPath
		}
	}
	return "", ""
}

// opdsMediaType is the type of a content file, with the comic archive types that sniffing and
// mime.TypeByExtension do not know.
func opdsMediaType(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".epub":
		return "application/epub+zip"
	case ".cbz", ".zip":
		return "application/vnd.comicbook+zip"
	case ".cbr", ".rar":
		return "application/vnd.comicbook-rar"
	case ".pdf":
		return "application/pdf"
	}
	return guessMediaType(path)
}

// pseMediaType is the one type PSE serves a comic's pages as: PNG or GIF when every page is, else
// JPEG, which other pages are converted to. PDF pages (p1, p2…) have no extension, so they get
// JPEG, which readPDFPage renders.
func pseMediaType(pageNames []string) string {
	common := ""
	for i, n := range pageNames {
		ext := strings.ToLower(filepath.Ext(n))
		if i > 0 && ext != common {
			return "image/jpeg"
		}
		common = ext
	}
	switch common {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	}
	return "image/jpeg"
}

// serveContentFile sends a comic or book file as a named attachment; c.File supports Range.
func serveContentFile(c echo.Context, path string) error {
	name := filepath.Base(path)
	h := c.Response().Header()
	h.Set(echo.HeaderContentType, opdsMediaType(name))
	h.Set(echo.HeaderContentDisposition, contentDisposition(name))
	return c.File(path)
}

// contentDisposition names an attachment, with an ASCII fallback for clients that ignore filename*.
func contentDisposition(name string) string {
	ascii := []byte(name)
	for i, b := range ascii {
		if b < 0x20 || b > 0x7e || b == '"' || b == '\\' {
			ascii[i] = '_'
		}
	}
	enc := strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
	return `attachment; filename="` + string(ascii) + `"; filename*=UTF-8''` + enc
}

func guessMediaType(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return "application/octet-stream"
	}
	mt := mime.TypeByExtension(ext)
	if mt == "" {
		return "application/octet-stream"
	}
	return mt
}
