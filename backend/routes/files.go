package routes

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/archive"
	"voltis/lib/epub"
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
		SELECT c.*, cm.data->'cover' AS cover
		FROM content c
		LEFT JOIN content_metadata cm ON cm.library_id = c.library_id AND cm.uri = c.uri
		WHERE c.id = $1
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
	return c.Blob(http.StatusOK, "image/jpeg", data)
}

func (fr *FileRoutes) getComicPage(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	var pageIndex int
	if _, err := fmt.Sscanf(c.Param("page_index"), "%d", &pageIndex); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid page index")
	}

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	if content.FileURI == nil || content.FileData == nil {
		return echo.NewHTTPError(http.StatusNotFound, "Content has no pages")
	}

	var fileData struct {
		Pages []json.RawMessage `json:"pages"`
	}
	if err := json.Unmarshal(content.FileData, &fileData); err != nil || len(fileData.Pages) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Content has no pages")
	}

	if pageIndex < 0 || pageIndex >= len(fileData.Pages) {
		return echo.NewHTTPError(http.StatusNotFound, "Page index out of range")
	}

	// Pages are stored as [name, width, height] tuples
	var pageTuple []json.RawMessage
	if err := json.Unmarshal(fileData.Pages[pageIndex], &pageTuple); err != nil || len(pageTuple) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Invalid page data")
	}
	var pageName string
	if err := json.Unmarshal(pageTuple[0], &pageName); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Invalid page data")
	}

	data, mediaType, err := readArchiveEntry(*content.FileURI, pageName)
	if err != nil {
		return err
	}

	if c.QueryParam("v") != "" {
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	return c.Blob(http.StatusOK, mediaType, data)
}

func (fr *FileRoutes) getBookChapters(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
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

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
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
	return blobUntrusted(c, "application/xhtml+xml", []byte(chapterContent))
}

func (fr *FileRoutes) getBookResource(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	contentID := c.Param("content_id")
	resourcePath := c.QueryParam("path")

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
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
	return blobUntrusted(c, mediaType, data)
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

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
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
		SELECT COUNT(*) AS file_count, SUM(file_size)::int AS total_size
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

	content, err := db.SelectOne[models.Content](ctx, fr.pool, "SELECT * FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "Content not found")
	}
	if err != nil {
		return err
	}

	if content.Type == "comic" || content.Type == "book" {
		if content.FileURI == nil {
			return echo.NewHTTPError(http.StatusNotFound, "Content has no file")
		}
		return c.File(*content.FileURI)
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

func readPDFPage(pdfPath, innerPath string) ([]byte, string, error) {
	m := pdfPagePattern.FindStringSubmatch(innerPath)
	if m == nil {
		return nil, "", echo.NewHTTPError(http.StatusBadRequest, "Invalid PDF page identifier")
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
		return nil, "", echo.NewHTTPError(http.StatusInternalServerError, "PDF rendering failed")
	}
	return data, "image/jpeg", nil
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
