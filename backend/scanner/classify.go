package scanner

import (
	"cmp"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"unicode"

	"voltis/lib/comic"
	"voltis/lib/epub"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/scanner/keys"
)

func classifyBook(file FSFile, meta epub.Metadata, coverValid bool, words *epub.WordCounts, infer bool) ParsedItem {
	stem := strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))

	var order *float32
	if meta.HasSeriesIndex {
		order = new(float32(meta.SeriesIndex))
	}

	// The inferred name and volume only group and order the book; child metadata stays as read.
	series, inferred := meta.Series, false
	if series == "" && infer {
		if name, vol, ok := inferBookSeries(file.Path, meta.Title, stem); ok {
			series, inferred = name, true
			order = cmp.Or(order, new(float32(vol)))
		}
	}

	item := ParsedItem{
		File:        file,
		URIPrefix:   "book",
		ContentType: "book",
		URIPart:     sanitizeURIPart(stem),
		OrderParts:  []*float32{order},
		MetaRaw: metadata.Fields{
			Title:           metadata.Val(cmp.Or(meta.Title, stem)),
			Description:     metadata.Set(meta.Description),
			Language:        metadata.Set(meta.Language),
			PublicationDate: metadata.Set(meta.PublicationDate),
			Series:          metadata.Set(meta.Series),
		},
	}
	if meta.HasSeriesIndex {
		item.MetaRaw.SeriesIndex = metadata.Val(meta.SeriesIndex)
	}
	if meta.Publisher != "" {
		item.MetaRaw.Publishers = metadata.Val([]string{meta.Publisher})
	}
	if coverValid {
		item.CoverSuffix = new(meta.CoverPath)
	}
	if words != nil && len(words.Docs) > 0 {
		item.FileData, _ = json.Marshal(map[string]any{"words": words.Docs})
		if !words.FixedLayout {
			item.WordCount = &words.Linear
		}
	}
	item.MetaRaw.Staff = metadata.SetList(fp.Map(meta.Authors, func(a string) metadata.Staff {
		return metadata.Staff{Name: a, Role: "author"}
	}))
	if series != "" {
		item.Series = &ParsedSeries{
			URIPrefix:   "book",
			URIPart:     sanitizeURIPart(series),
			ContentType: "book_series",
			Inferred:    inferred,
		}
	}
	return item
}

// inferBookSeries takes the series name and volume from the title, else from the filename, never
// pairing a name from one with a volume from the other.
func inferBookSeries(path, title, stem string) (string, float64, bool) {
	if keys.IsBookSpecial(title) || keys.IsBookSpecial(stem) {
		return "", 0, false
	}
	name, vol, ok := keys.ParseBookVolume(title)
	fileName, fileVol, fileOK := keys.ParseBookFileVolume(stem)
	if ok && fileOK && vol != fileVol {
		slog.Warn("[scanner] title and filename volumes differ, not inferring a series",
			"path", path, "title_volume", vol, "file_volume", fileVol)
		return "", 0, false
	}
	if ok {
		return name, vol, true
	}
	return fileName, fileVol, fileOK
}

func classifyComic(file FSFile, meta metadata.Fields, year *int, pages []comic.PageInfo) *ParsedItem {
	path := file.Path

	dir := filepath.Dir(path)
	dirName := filepath.Base(dir)
	fallbackName, fallbackYear := keys.ParseSeriesName(dirName)

	seriesName := cmp.Or(meta.Series.V, fallbackName)
	seriesYear := cmp.Or(year, fallbackYear)

	seriesURIPart := seriesName
	if seriesYear != nil {
		seriesURIPart = fmt.Sprintf("%s_%d", seriesName, *seriesYear)
	}

	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	filename := keys.CleanSeriesName(stem)

	volNum := keys.ParseVolume(filename)
	if v, ok := meta.Volume.Get(); ok {
		if f, err := keys.ParseFloatStr(v); err == nil {
			volNum = &f
		}
	}

	var chNum *float64
	if n, ok := meta.Number.Get(); ok {
		if f, err := keys.ParseFloatStr(n); err == nil {
			chNum = &f
		} else {
			chNum = keys.ParseChapter(n)
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
	meta.Title = cmp.Or(meta.Title, metadata.Val(strings.Join(titleParts, " ")))

	// Archive page sizes are computed when the reader first opens the comic.
	pageTuples := fp.Map(pages, func(p comic.PageInfo) any {
		if !p.Sized {
			return []any{p.Name}
		}
		return []any{p.Name, p.Width, p.Height}
	})
	fd, _ := json.Marshal(map[string]any{"pages": pageTuples})

	return &ParsedItem{
		File:        file,
		URIPrefix:   "comic",
		ContentType: "comic",
		URIPart:     sanitizeURIPart(strings.Join(uriParts, "_")),
		OrderParts:  orderParts,
		CoverSuffix: new(pages[0].Name),
		FileData:    fd,
		MetaRaw:     meta,
		PageCount:   new(len(pages)),
		Series: &ParsedSeries{
			URIPrefix:   "comic",
			URIPart:     sanitizeURIPart(seriesURIPart),
			ContentType: "comic_series",
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
	return cmp.Or(s, "_")
}
