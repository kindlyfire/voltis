package scanner

import (
	"os"
	"path/filepath"
	"time"

	"voltis/lib/comic"
	"voltis/models"
)

type ComicsScanner struct{}

var coverNames = []string{"cover.jpg", "cover.jpeg", "cover.png", "cover.webp"}

func (cs *ComicsScanner) FileEligible(path string) bool {
	return isComicFile(path)
}

func (cs *ComicsScanner) ParseFile(file FSFile) *ParsedItem {
	pages, comicInfo := comic.Scan(file.Path)

	if len(pages) == 0 {
		return nil
	}

	var meta models.Metadata
	year := 0
	if comicInfo != nil {
		meta = comic.ComicInfoToMetadata(comicInfo)
		year = comicInfo.Year
	}

	return classifyComic(file, meta, year, pages)
}

func (cs *ComicsScanner) SeriesCover(series SeriesRef, ordered []Child) (*string, *time.Time) {
	if series.FileURI != nil {
		for _, name := range coverNames {
			coverPath := filepath.Join(*series.FileURI, name)
			if info, err := os.Stat(coverPath); err == nil && !info.IsDir() {
				return new(coverPath), new(info.ModTime().UTC())
			}
		}
	}
	if len(ordered) > 0 {
		return ordered[0].CoverURI, ordered[0].FileMtime
	}
	return nil, nil
}

func (cs *ComicsScanner) UpdateSeries(r *repository, series *models.Content, ordered []Child) {
	inheritChildMetadata(r, series, ordered)

	series.CoverURI, series.FileMtime = cs.SeriesCover(seriesRef(series), ordered)
	r.markDirty(series)
}
