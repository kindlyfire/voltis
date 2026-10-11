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

// sentinelIndex and above is calibre's index for books without one.
const sentinelIndex = 10000

func classifyBook(file FSFile, meta epub.Metadata, coverValid bool, words *epub.WordCounts, infer bool) ParsedItem {
	stem := strings.TrimSuffix(filepath.Base(file.Path), filepath.Ext(file.Path))

	index, hasIndex := meta.SeriesIndex, meta.HasSeriesIndex

	// The inferred name and volume group and order the book; the volume shows as its index.
	series, inferred := meta.Series, false
	if series == "" && infer {
		if name, vol, ok := inferBookSeries(file.Path, meta.Title, stem); ok {
			series, inferred = name, true
			if !hasIndex {
				index, hasIndex = vol, true
			}
		}
	}

	item := ParsedItem{
		File:          file,
		URIPrefix:     "book",
		ContentType:   "book",
		URIPart:       sanitizeURIPart(stem),
		IndexInferred: inferred && !meta.HasSeriesIndex,
		MetaRaw: metadata.Fields{
			Title:           metadata.Val(cmp.Or(meta.Title, stem)),
			Description:     metadata.Set(meta.Description),
			Language:        metadata.Set(meta.Language),
			PublicationDate: metadata.Set(meta.PublicationDate),
			Series:          metadata.Set(meta.Series),
		},
	}
	if hasIndex {
		item.MetaRaw.SeriesIndex = metadata.Val(index)
	}
	item.OrderParts = scannedPosition("book", item.MetaRaw)
	// A calibre sentinel index reads as unnumbered, and marks a special.
	if (meta.HasSeriesIndex && meta.SeriesIndex >= sentinelIndex) || keys.SpecialMarker(stem) || keys.SpecialMarker(meta.Title) {
		item.MetaRaw.Special = metadata.Val(true)
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
	if keys.IsBookExtra(title) || keys.IsBookExtra(stem) {
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
	// Without the series name, so "C3 Unit v02" doesn't read as chapter 3.
	name := keys.RemoveCommonPrefix(keys.CleanSeriesName(stem), dirName)

	// Only SP blocks the fallback chapter: "Omake 3" stays chapter 3.
	sp, _ := keys.SPMarker(name)
	spNumber, _ := keys.IsSpecialNumber(meta.Number.V)

	var volNum, chNum *float64
	if n, ok := meta.Number.Get(); ok {
		chNum = keys.ParseNumber(n)
	} else {
		volNum = keys.ParseVolume(name)
		chNum = keys.ParseChapter(name)
	}
	if v, ok := meta.Volume.Get(); ok {
		volNum = cmp.Or(keys.ParseVolumeStr(v), volNum)
	}

	yearNum := keys.ParseSeriesYear(stem)
	if volNum == nil && chNum == nil && !sp && !spNumber {
		chNum = keys.ParseFallbackChapter(name)
	}

	// The file layer holds the numbers used, so overrides can be reset to them.
	meta.Volume = scannedNum(meta.Volume, volNum, keys.ParseVolumeStr)
	meta.Number = scannedNum(meta.Number, chNum, keys.ParseNumber)

	var uriParts, titleParts []string
	if volNum != nil {
		n := keys.FormatNum(*volNum)
		uriParts = append(uriParts, "v"+n)
		titleParts = append(titleParts, "Vol. "+n)
	}
	if chNum != nil {
		n := keys.FormatNum(*chNum)
		uriParts = append(uriParts, "ch"+n)
		titleParts = append(titleParts, "Ch. "+n)
	}
	// SP items and unnumbered ones take the stem, which keeps them apart from regular items and
	// each other; an unnumbered one-shot named like its folder keeps its year key.
	if len(uriParts) > 0 && (sp || spNumber) {
		uriParts = []string{stem}
	}
	if len(uriParts) == 0 {
		switch {
		case keys.HasText(name):
			uriParts, titleParts = []string{stem}, []string{keys.UnnumberedTitle(name)}
		case yearNum != nil:
			uriParts = []string{fmt.Sprintf("y%d", *yearNum)}
			titleParts = []string{fmt.Sprintf("%s (%d)", seriesName, *yearNum)}
		default:
			uriParts, titleParts = []string{stem}, []string{seriesName}
		}
	}
	meta.Title = cmp.Or(meta.Title, metadata.Val(strings.Join(titleParts, " ")))
	if spNumber || keys.SpecialMarker(name) || strings.EqualFold(strings.TrimSpace(meta.Format.V), "special") {
		meta.Special = metadata.Val(true)
	}

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
		OrderParts:  scannedPosition("comic", meta),
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

// scannedNum keeps a ComicInfo value that reads as the number used ("Ch. 5"), else stores the
// number itself.
func scannedNum(o metadata.Opt[string], num *float64, parse func(string) *float64) metadata.Opt[string] {
	if fp.PtrEq(parse(o.V), num) {
		return o
	}
	if num == nil {
		return metadata.Opt[string]{}
	}
	return metadata.Val(keys.FormatNum(*num))
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
