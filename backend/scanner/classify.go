package scanner

import (
	"cmp"
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
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))

	index := 0.0
	if meta.HasSeriesIndex {
		index = meta.SeriesIndex
	}

	var coverSuffix *string
	if coverValid {
		coverSuffix = new(meta.CoverPath)
	}

	fileMeta := models.Metadata{
		Title:           cmp.Or(meta.Title, stem),
		Description:     meta.Description,
		Publisher:       meta.Publisher,
		Language:        meta.Language,
		PublicationDate: meta.PublicationDate,
		Series:          meta.Series,
		SeriesIndex:     index,
	}
	for _, a := range meta.Authors {
		fileMeta.Staff = append(fileMeta.Staff, models.StaffEntry{Name: a, Role: "author"})
	}

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
		OrderParts:  []*float32{new(float32(index))},
		CoverSuffix: coverSuffix,
		MetaRaw:     fileMeta,
	}
}

func classifyComic(file FSFile, meta models.Metadata, year int, pages []comic.PageInfo) *ParsedItem {
	path := file.Path

	dir := filepath.Dir(path)
	dirName := filepath.Base(dir)
	fallbackName, fallbackYear := keys.ParseSeriesName(dirName)

	seriesName := cmp.Or(meta.Series, fallbackName)
	seriesYear := fallbackYear
	if year != 0 {
		seriesYear = &year
	}

	seriesURIPart := seriesName
	if seriesYear != nil {
		seriesURIPart = fmt.Sprintf("%s_%d", seriesName, *seriesYear)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	filename := keys.CleanSeriesName(stem)

	var volNum, chNum *float64
	if meta.Volume != 0 {
		volNum = new(float64(meta.Volume))
	} else {
		volNum = keys.ParseVolume(filename)
	}

	if meta.Number != "" {
		if f, err := keys.ParseFloatStr(meta.Number); err == nil {
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

	var uriParts, titleParts []string
	orderParts := make([]*float32, 2)
	if volNum != nil {
		n := keys.FormatNum(*volNum)
		uriParts = append(uriParts, "v"+n)
		titleParts = append(titleParts, "Vol. "+n)
		orderParts[0] = new(float32(*volNum))
	}
	if chNum != nil {
		n := keys.FormatNum(*chNum)
		uriParts = append(uriParts, "ch"+n)
		titleParts = append(titleParts, "Ch. "+n)
		orderParts[1] = new(float32(*chNum))
	}
	if len(uriParts) == 0 {
		if yearNum == nil {
			return nil
		}
		uriParts = append(uriParts, fmt.Sprintf("y%d", *yearNum))
		titleParts = append(titleParts, fmt.Sprintf("%s (%d)", seriesName, *yearNum))
	}
	meta.Title = cmp.Or(meta.Title, strings.Join(titleParts, " "))

	pageTuples := fp.Map(pages, func(p comic.PageInfo) any {
		return []any{p.Name, p.Width, p.Height}
	})
	fd, _ := json.Marshal(map[string]any{"pages": pageTuples})

	return &ParsedItem{
		File:        file,
		URIPrefix:   "comic",
		ContentType: "comic",
		URIPart:     strings.Join(uriParts, "_"),
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
