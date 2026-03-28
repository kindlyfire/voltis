package scanner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"voltis/lib/comic"
	"voltis/lib/fp"
	"voltis/models"
)

// ComicsScanner implements FileScanner for comic archives.
type ComicsScanner struct{}

var coverNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp"}

func (cs *ComicsScanner) FileEligible(path string) bool {
	return isComicFile(path)
}

func (cs *ComicsScanner) ParseFile(libraryID string, file FSFile) *ParsedItem {
	path := file.Path

	pages, comicInfo := comic.Scan(path)

	if len(pages) == 0 {
		return nil
	}

	// Extract metadata from ComicInfo
	var meta models.Metadata
	if comicInfo != nil {
		meta = comic.ComicInfoToMetadata(comicInfo)
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
	pageTuples := fp.Map(pages, func(p comic.PageInfo) any {
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
