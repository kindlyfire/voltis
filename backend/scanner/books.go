package scanner

import (
	"path/filepath"
	"strings"
	"time"

	"voltis/lib/epub"
	"voltis/models"
)

// BooksScanner implements FileScanner for EPUB books.
type BooksScanner struct{}

func (bs *BooksScanner) FileEligible(path string) bool {
	return strings.ToLower(filepath.Ext(path)) == ".epub"
}

func (bs *BooksScanner) ScanFile(r *repository, libraryID string, file FSFile) *models.Content {
	path := file.Path

	meta, err := epub.ReadMetadata(path)
	if err != nil {
		slog_scan("failed to read epub metadata", "path", path, "err", err)
		return nil
	}

	// Resolve series if present
	var series *models.Content
	if meta.Series != "" {
		seriesURIPart := meta.Series
		seriesURI := "book/" + seriesURIPart
		series = r.getSeries(seriesURI, seriesURIPart, nil, "book_series", meta.Series)
	}

	// Title from metadata, falling back to filename stem
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	title := meta.Title
	if title == "" {
		title = stem
	}

	uriPart := stem

	// Order parts
	var orderParts []*float32
	if meta.HasSeriesIndex {
		f := float32(meta.SeriesIndex)
		orderParts = append(orderParts, &f)
	} else {
		f := float32(0)
		orderParts = append(orderParts, &f)
	}

	// Cover URI
	var coverURI *string
	if meta.CoverPath != "" && epub.ValidateCoverPath(path, meta.CoverPath) {
		coverURI = new(path + "/" + meta.CoverPath)
	}

	// Find or create content
	existing := r.findContentByFileURI(file.Path)
	content := existing
	var parentID *string
	if series != nil {
		parentID = &series.ID
	}
	if content == nil {
		content = r.matchDeletedItem(uriPart, parentID)
	}

	now := time.Now().UTC()
	if content == nil {
		newContent := models.Content{
			ID:        models.MakeContentID(),
			LibraryID: libraryID,
			Type:      "book",
			CreatedAt: now,
		}
		r.content = append(r.content, newContent)
		content = &r.content[len(r.content)-1]
	}

	content.FileURI = new(file.Path)
	content.URIPart = uriPart
	content.Valid = true
	content.ParentID = parentID
	content.UpdatedAt = now
	content.FileMtime = &file.Mtime
	content.FileSize = new(int(file.Size))
	content.OrderParts = orderParts
	content.CoverURI = coverURI

	if series != nil {
		content.URI = series.URI + "/" + uriPart
	} else {
		content.URI = "book/" + uriPart
	}

	r.markDirty(content)

	// Metadata
	fileMeta := map[string]any{"title": title}
	if len(meta.Authors) > 0 {
		fileMeta["authors"] = meta.Authors
	}
	if meta.Description != "" {
		fileMeta["description"] = meta.Description
	}
	if meta.Publisher != "" {
		fileMeta["publisher"] = meta.Publisher
	}
	if meta.Language != "" {
		fileMeta["language"] = meta.Language
	}
	if meta.PublicationDate != "" {
		fileMeta["publication_date"] = meta.PublicationDate
	}
	if meta.Series != "" {
		fileMeta["series"] = meta.Series
	}
	if meta.HasSeriesIndex {
		fileMeta["series_index"] = meta.SeriesIndex
	}

	metaRow := r.getMetadata(content.URI)
	metaRow.setSource("file", fileMeta, nil)

	return content
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
