package scanner

import (
	"path/filepath"
	"strings"

	"voltis/lib/epub"
	"voltis/models"
	"voltis/models/contentmeta"
)

// BooksScanner implements FileScanner for EPUB books.
type BooksScanner struct{}

func (bs *BooksScanner) FileEligible(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".epub"
}

func (bs *BooksScanner) ParseFile(libraryID string, file FSFile) *ParsedItem {
	path := file.Path

	meta, err := epub.ReadMetadata(path)
	if err != nil {
		slog_scan("failed to read epub metadata", "path", path, "err", err)
		return nil
	}

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
	if meta.CoverPath != "" && epub.ValidateCoverPath(path, meta.CoverPath) {
		coverSuffix = new(meta.CoverPath)
	}

	// Metadata
	fileMeta := contentmeta.Metadata{Title: title}
	for _, a := range meta.Authors {
		fileMeta.Staff = append(fileMeta.Staff, contentmeta.StaffEntry{Name: a, Role: "author"})
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

	return &ParsedItem{
		File:        file,
		Series:      series,
		URIPrefix:   "book",
		ContentType: "book",
		URIPart:     stem,
		OrderParts:  orderParts,
		CoverSuffix: coverSuffix,
		Meta:        fileMeta,
	}
}

func (bs *BooksScanner) UpdateSeries(r *repository, series *models.Content, items []*models.Content) {
	inheritChildMetadata(r, series, items)

	// Set series cover from first child's cover
	if len(items) > 0 {
		series.CoverURI = items[0].CoverURI
		series.FileMtime = items[0].FileMtime
	}
	r.markDirty(series)
}
