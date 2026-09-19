package scanner

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"voltis/lib/comic"
	"voltis/lib/epub"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/scanner/keys"
)

func classifyBook(file FSFile, meta epub.Metadata, coverValid bool) ParsedItem {
	path := file.Path

	// Title from metadata, falling back to filename stem
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title := meta.Title
	if title == "" {
		title = stem
	}

	// Order parts
	var orderParts []*float32
	if meta.HasSeriesIndex {
		f := float32(meta.SeriesIndex)
		orderParts = append(orderParts, &f)
	} else {
		f := float32(0)
		orderParts = append(orderParts, &f)
	}

	// Cover suffix
	var coverSuffix *string
	if coverValid {
		coverSuffix = new(meta.CoverPath)
	}

	// Metadata
	fileMeta := models.Metadata{Title: title}
	for _, a := range meta.Authors {
		fileMeta.Staff = append(fileMeta.Staff, models.StaffEntry{Name: a, Role: "author"})
	}
	fileMeta.Description = meta.Description
	fileMeta.Publisher = meta.Publisher
	fileMeta.Language = meta.Language
	fileMeta.PublicationDate = meta.PublicationDate
	fileMeta.Series = meta.Series
	if meta.HasSeriesIndex {
		fileMeta.SeriesIndex = meta.SeriesIndex
	}

	// Series
	var series *ParsedSeries
	if meta.Series != "" {
		series = &ParsedSeries{
			URIPrefix:   "book",
			URIPart:     meta.Series,
			ContentType: "book_series",
			Title:       meta.Series,
		}
	}

	return ParsedItem{
		File:        file,
		Series:      series,
		URIPrefix:   "book",
		ContentType: "book",
		URIPart:     stem,
		OrderParts:  orderParts,
		CoverSuffix: coverSuffix,
		MetaRaw:     fileMeta,
	}
}

func classifyComic(file FSFile, meta models.Metadata, year int, pages []comic.PageInfo) *ParsedItem {
	path := file.Path

	// Determine series
	dir := filepath.Dir(path)
	dirName := filepath.Base(dir)
	fallbackName, fallbackYear := keys.ParseSeriesName(dirName)

	seriesName := fallbackName
	if meta.Series != "" {
		seriesName = meta.Series
	}
	var seriesYear *int
	if year != 0 {
		seriesYear = &year
	} else {
		seriesYear = fallbackYear
	}

	seriesURIPart := seriesName
	if seriesYear != nil {
		seriesURIPart = fmt.Sprintf("%s_%d", seriesName, *seriesYear)
	}

	// Parse volume/chapter from filename
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	filename := keys.CleanSeriesName(stem)

	var volNum *float64
	var chNum *float64

	if meta.Volume != 0 {
		f := float64(meta.Volume)
		volNum = &f
	} else {
		volNum = keys.ParseVolume(filename)
	}

	if meta.Number != "" {
		f, err := keys.ParseFloatStr(meta.Number)
		if err == nil {
			chNum = &f
		} else {
			chNum = keys.ParseChapter(meta.Number)
		}
	} else {
		chNum = keys.ParseChapter(filename)
	}

	yearNum := keys.ParseSeriesYear(stem)
	if volNum == nil && chNum == nil {
		stripped, _ := keys.RemoveCommonPrefix(filename, dirName)
		chNum = keys.ParseFallbackChapter(stripped)
	}

	// Build URI parts
	var uriParts []string
	if volNum != nil {
		uriParts = append(uriParts, fmt.Sprintf("v%s", keys.FormatNum(*volNum)))
	}
	if chNum != nil {
		uriParts = append(uriParts, fmt.Sprintf("ch%s", keys.FormatNum(*chNum)))
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
		titleParts = append(titleParts, fmt.Sprintf("Vol. %s", keys.FormatNum(*volNum)))
	}
	if chNum != nil {
		titleParts = append(titleParts, fmt.Sprintf("Ch. %s", keys.FormatNum(*chNum)))
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

func sanitizeURIPart(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			return '_'
		}
		return r
	}, s)
	if s == "" {
		return "_"
	}
	return s
}
