package scanner

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"

	"voltis/lib/archive"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/models/contentmeta"
	"voltis/models/contentmetamerge"
)

// ComicsScanner implements FileScanner for comic archives.
type ComicsScanner struct{}

var imageExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
}

var coverNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp"}

type pageInfo struct {
	Name   string `json:"name"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

func (cs *ComicsScanner) FileEligible(path string) bool {
	return isComicFile(path)
}

func scanArchivePages(path string) ([]pageInfo, *contentmetamerge.ComicInfo) {
	a, err := archive.Open(path)
	if err != nil {
		slog_scan("failed to open archive", "path", path, "err", err)
		return nil, nil
	}
	defer func() { _ = a.Close() }()

	entries, err := a.List()
	if err != nil {
		slog_scan("failed to list archive", "path", path, "err", err)
		return nil, nil
	}

	var pages []pageInfo
	var comicInfo *contentmetamerge.ComicInfo

	for _, entry := range entries {
		if entry.Name == "ComicInfo.xml" {
			data, err := a.ReadFile(entry.Name)
			if err == nil {
				comicInfo, _ = contentmetamerge.ParseComicInfo(data)
			}
			continue
		}

		ext := strings.ToLower(filepath.Ext(entry.Name))
		if !imageExtensions[ext] {
			continue
		}

		rc, err := a.OpenFile(entry.Name)
		if err != nil {
			pages = append(pages, pageInfo{Name: entry.Name})
			continue
		}

		cfg, _, err := image.DecodeConfig(rc)
		_ = rc.Close()
		if err != nil {
			pages = append(pages, pageInfo{Name: entry.Name})
			continue
		}
		pages = append(pages, pageInfo{Name: entry.Name, Width: cfg.Width, Height: cfg.Height})
	}

	sort.Slice(pages, func(i, j int) bool {
		return pages[i].Name < pages[j].Name
	})

	return pages, comicInfo
}

var pdfPagesRe = regexp.MustCompile(`^Pages:\s+(\d+)`)
var pdfSizeRe = regexp.MustCompile(`([\d.]+)\s*x\s*([\d.]+)`)

func scanPDFPages(path string) []pageInfo {
	result, err := exec.Command("pdfinfo", path).Output()
	if err != nil {
		slog_scan("pdfinfo failed", "path", path, "err", err)
		return nil
	}

	var pageCount int
	var pageWidth, pageHeight int

	for _, line := range strings.Split(string(result), "\n") {
		if m := pdfPagesRe.FindStringSubmatch(line); m != nil {
			pageCount, _ = strconv.Atoi(m[1])
		} else if strings.HasPrefix(line, "Page size:") {
			if m := pdfSizeRe.FindStringSubmatch(line); m != nil {
				w, _ := strconv.ParseFloat(m[1], 64)
				h, _ := strconv.ParseFloat(m[2], 64)
				// Convert points to pixels at 250 DPI
				pageWidth = int(math.Round(w * 250 / 72))
				pageHeight = int(math.Round(h * 250 / 72))
			}
		}
	}

	if pageCount <= 0 {
		return nil
	}

	pages := make([]pageInfo, pageCount)
	for i := range pageCount {
		pages[i] = pageInfo{
			Name:   fmt.Sprintf("p%d", i+1),
			Width:  pageWidth,
			Height: pageHeight,
		}
	}
	return pages
}

func (cs *ComicsScanner) ParseFile(libraryID string, file FSFile) *ParsedItem {
	path := file.Path

	var pages []pageInfo
	var comicInfo *contentmetamerge.ComicInfo

	if strings.ToLower(filepath.Ext(path)) == ".pdf" {
		pages = scanPDFPages(path)
	} else {
		pages, comicInfo = scanArchivePages(path)
	}

	if len(pages) == 0 {
		return nil
	}

	// Extract metadata from ComicInfo
	var meta contentmeta.Metadata
	if comicInfo != nil {
		meta = contentmetamerge.ComicInfoToMetadata(comicInfo)
	}

	// Determine series
	dir := filepath.Dir(path)
	dirName := filepath.Base(dir)
	fallbackName, fallbackYear := parseSeriesName(dirName)

	seriesName := fallbackName
	if meta.Series != "" {
		seriesName = meta.Series
	}
	var seriesYear *int
	if comicInfo != nil && comicInfo.Year != 0 {
		seriesYear = &comicInfo.Year
	} else {
		seriesYear = fallbackYear
	}

	seriesURIPart := seriesName
	if seriesYear != nil {
		seriesURIPart = fmt.Sprintf("%s_%d", seriesName, *seriesYear)
	}

	// Parse volume/chapter from filename
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	filename := cleanSeriesName(stem)

	var volNum *float64
	var chNum *float64

	if meta.Volume != 0 {
		f := float64(meta.Volume)
		volNum = &f
	} else {
		volNum = parseVolume(filename)
	}

	if meta.Number != "" {
		f, err := parseFloatStr(meta.Number)
		if err == nil {
			chNum = &f
		} else {
			chNum = parseChapter(meta.Number)
		}
	} else {
		chNum = parseChapter(filename)
	}

	yearNum := parseSeriesYear(stem)
	if volNum == nil && chNum == nil {
		stripped, _ := removeCommonPrefix(filename, dirName)
		chNum = parseFallbackChapter(stripped)
	}

	// Build URI parts
	var uriParts []string
	if volNum != nil {
		uriParts = append(uriParts, fmt.Sprintf("v%s", formatNum(*volNum)))
	}
	if chNum != nil {
		uriParts = append(uriParts, fmt.Sprintf("ch%s", formatNum(*chNum)))
	}
	if volNum == nil && chNum == nil && yearNum != nil {
		uriParts = append(uriParts, fmt.Sprintf("y%d", *yearNum))
	}
	if len(uriParts) == 0 {
		return nil
	}
	uriPart := strings.Join(uriParts, "_")

	// Build title
	var titleParts []string
	if volNum != nil {
		titleParts = append(titleParts, fmt.Sprintf("Vol. %s", formatNum(*volNum)))
	}
	if chNum != nil {
		titleParts = append(titleParts, fmt.Sprintf("Ch. %s", formatNum(*chNum)))
	}
	if volNum == nil && chNum == nil && yearNum != nil {
		titleParts = append(titleParts, fmt.Sprintf("%s (%d)", seriesName, *yearNum))
	}
	title := strings.Join(titleParts, " ")
	if title == "" {
		title = filename
	}
	if meta.Title == "" {
		meta.Title = title
	}

	// Build order parts
	var orderParts []*float32
	if volNum != nil {
		f := float32(*volNum)
		orderParts = append(orderParts, &f)
	} else {
		orderParts = append(orderParts, nil)
	}
	if chNum != nil {
		f := float32(*chNum)
		orderParts = append(orderParts, &f)
	} else {
		orderParts = append(orderParts, nil)
	}

	// Build file data (pages)
	pageTuples := fp.Map(pages, func(p pageInfo) any {
		return []any{p.Name, p.Width, p.Height}
	})
	fd, _ := json.Marshal(map[string]any{"pages": pageTuples})

	return &ParsedItem{
		File:        file,
		URIPrefix:   "comic",
		ContentType: "comic",
		URIPart:     uriPart,
		OrderParts:  orderParts,
		CoverSuffix: new(pages[0].Name),
		FileData:    fd,
		MetaRaw:     meta,
		Series: &ParsedSeries{
			URIPrefix:   "comic",
			URIPart:     seriesURIPart,
			ContentType: "comic_series",
			Title:       seriesName,
			FileURI:     new(dir),
		},
	}
}

func (cs *ComicsScanner) UpdateSeries(r *repository, series *models.Content, items []*models.Content) {
	inheritChildMetadata(r, series, items)

	// Find series cover
	series.CoverURI = nil
	series.FileMtime = nil
	cs.scanSeriesCover(series)
	if series.CoverURI == nil && len(items) > 0 {
		series.CoverURI = items[0].CoverURI
		series.FileMtime = items[0].FileMtime
	}
	r.markDirty(series)
}

func (cs *ComicsScanner) scanSeriesCover(series *models.Content) {
	if series.FileURI == nil {
		return
	}
	for _, name := range coverNames {
		coverPath := filepath.Join(*series.FileURI, name)
		if info, err := os.Stat(coverPath); err == nil && !info.IsDir() {
			series.CoverURI = new(coverPath)
			series.FileMtime = new(info.ModTime().UTC())
			return
		}
	}
}

func formatNum(f float64) string {
	if f == float64(int(f)) {
		return fmt.Sprintf("%d", int(f))
	}
	return fmt.Sprintf("%g", f)
}

func parseFloatStr(s string) (float64, error) {
	f := 0.0
	_, err := fmt.Sscanf(s, "%f", &f)
	return f, err
}
