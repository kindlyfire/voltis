package scanner

import (
	"path/filepath"
	"strings"
	"time"

	"voltis/lib/epub"
)

type BooksScanner struct{}

func (bs *BooksScanner) FileEligible(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".epub"
}

func (bs *BooksScanner) ParseFile(file FSFile) *ParsedItem {
	meta, err := epub.ReadMetadata(file.Path)
	if err != nil {
		slog_scan("failed to read epub metadata", "path", file.Path, "err", err)
		return nil
	}

	words, err := epub.CountWords(file.Path)
	if err != nil {
		slog_scan("failed to count epub text", "path", file.Path, "err", err)
	}

	coverValid := meta.CoverPath != "" && epub.ValidateCoverPath(file.Path, meta.CoverPath)
	return new(classifyBook(file, *meta, coverValid, words))
}

func (bs *BooksScanner) SeriesCover(series SeriesRef, ordered []Child) (*string, *time.Time) {
	if len(ordered) == 0 {
		return nil, nil
	}
	return ordered[0].CoverURI, ordered[0].FileMtime
}
