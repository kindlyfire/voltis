package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FileScanner is the interface each scanner type must implement.
type FileScanner interface {
	// FileEligible returns whether a file should be scanned.
	FileEligible(path string) bool

	// ParseFile parses a file and returns the extracted data, or nil if the
	// file could not be parsed. Should be safe to call concurrently.
	ParseFile(libraryID string, file FSFile) *ParsedItem

	// UpdateSeries is called for each series that had at least one child
	// added, updated, or removed.
	UpdateSeries(r *repository, series *models.Content, items []*models.Content)
}

type ParsedSeries struct {
	URIPrefix   string // "comic" or "book"
	URIPart     string
	ContentType string // "comic_series" or "book_series"
	Title       string
	FileURI     *string // directory path for comics, nil for books
}

type ParsedItem struct {
	File        FSFile
	Series      *ParsedSeries // nil if standalone (book without series)
	URIPrefix   string        // "comic" or "book"
	ContentType string        // "comic" or "book"
	URIPart     string
	OrderParts  []*float32
	CoverSuffix *string // appended to file path for cover URI
	FileData    json.RawMessage
	Meta        map[string]any
	MetaRaw     map[string]any // ComicInfo raw, nil for books
}

type ScanResult struct {
	Added     int
	Updated   int
	Removed   int
	Unchanged int
}

type ScanOptions struct {
	Force       bool
	FilterPaths []string
	Concurrency int
	Hub         db.TaskEventBroadcaster
}

func newFileScanner(libraryType string) FileScanner {
	switch libraryType {
	case "comics":
		return &ComicsScanner{}
	case "books":
		return &BooksScanner{}
	default:
		return nil
	}
}

func Scan(ctx context.Context, pool *pgxpool.Pool, libraryID string, libraryType string, sources []string, opts ScanOptions) (*ScanResult, error) {
	if opts.Concurrency <= 0 {
		opts.Concurrency = 10
	}

	s := newFileScanner(libraryType)
	if s == nil {
		return nil, nil
	}

	slog_scan("starting scan", "library", libraryID, "type", libraryType, "force", opts.Force, "filter_paths", opts.FilterPaths, "concurrency", opts.Concurrency)

	// Create task
	var task *models.Task
	if opts.Hub != nil {
		now := time.Now().UTC()
		input, _ := json.Marshal(map[string]any{
			"library_id":   libraryID,
			"force":        opts.Force,
			"filter_paths": opts.FilterPaths,
		})
		task = &models.Task{
			ID:        models.MakeTaskID(),
			CreatedAt: now,
			UpdatedAt: now,
			Name:      "scan",
			Status:    models.TaskStatusInProgress,
			Input:     input,
			Output:    json.RawMessage("{}"),
		}
		if err := db.TaskCreate(ctx, pool, task); err != nil {
			return nil, err
		}
	}

	var taskUpdateMutex sync.Mutex
	taskUpdate := func(tuOpts db.TaskUpdateOpts) {
		if task == nil || opts.Hub == nil {
			return
		}
		fp.WithMutex(&taskUpdateMutex, func() {
			if err := db.TaskUpdate(ctx, pool, opts.Hub, task, tuOpts); err != nil {
				slog.Error("[scanner] task update failed", "err", err)
			}
		})
	}

	// On failure, mark task as failed
	var scanErr error
	defer func() {
		if scanErr != nil && task != nil {
			failed := models.TaskStatusFailed
			taskUpdate(db.TaskUpdateOpts{Status: &failed})
		}
	}()

	// Determine sources
	scanSources := sources
	if len(opts.FilterPaths) > 0 {
		scanSources = opts.FilterPaths
	}

	// Walk filesystem
	files, err := walkSources(scanSources, s.FileEligible)
	if err != nil {
		scanErr = err
		return nil, err
	}
	slog.Info("[scanner] found files", "count", len(files), "library", libraryID)

	// Load existing content
	r := newRepository(pool, libraryID)
	if err := r.load(ctx); err != nil {
		scanErr = err
		return nil, err
	}

	// Diff
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, opts)

	slog.Info("[scanner] diff",
		"add", len(toAdd), "update", len(toUpdate),
		"unchanged", len(unchanged), "remove", len(toRemove),
	)

	// Emit summary
	taskUpdate(db.TaskUpdateOpts{
		Output: map[string]int{
			"to_add":    len(toAdd),
			"to_update": len(toUpdate),
			"to_remove": len(toRemove),
			"unchanged": len(unchanged),
		},
	})

	// Move removed items to deleted list
	for _, file := range toRemove {
		for i := range r.content {
			if r.content[i].FileURI != nil && *r.content[i].FileURI == file.Path {
				r.removeContent(&r.content[i])
				break
			}
		}
	}

	// Process added + updated files concurrently
	toProcess := append(toAdd, toUpdate...)
	progressTotal := len(toProcess)
	var progressProcessed atomic.Int64
	parentsWithUpdates := map[string]bool{}

	type parsedResult struct {
		file   FSFile
		parsed *ParsedItem
	}
	parsedResults := make([]parsedResult, len(toProcess))
	fp.MapConcurrently(toProcess, opts.Concurrency, func(f FSFile) {
		item := s.ParseFile(libraryID, f)
		idx := int(progressProcessed.Add(1)) - 1
		parsedResults[idx] = parsedResult{file: f, parsed: item}
		taskUpdate(db.TaskUpdateOpts{
			Progress: map[string]int{
				"total":     progressTotal,
				"processed": idx + 1,
			},
		})
	})

	for _, pr := range parsedResults {
		if pr.parsed != nil {
			result := applyParsedItem(r, libraryID, pr.parsed)
			if !r.checkURIAvailable(result) {
				slog.Warn("[scanner] URI conflict, skipping", "file", pr.file.Path, "uri", result.URI)
			} else if result.ParentID != nil {
				parentsWithUpdates[*result.ParentID] = true
			}
		} else {
			existing := r.findContentByFileURI(pr.file.Path)
			if existing != nil {
				if existing.ParentID != nil {
					parentsWithUpdates[*existing.ParentID] = true
				}
				r.removeContent(existing)
			}
			slog.Warn("[scanner] failed to parse file", "path", pr.file.Path)
		}
	}

	// Update series that had changes
	for parentID := range parentsWithUpdates {
		var parent *models.Content
		for i := range r.content {
			if r.content[i].ID == parentID {
				parent = &r.content[i]
				break
			}
		}
		if parent == nil {
			continue
		}
		items := r.childrenOf(parentID)

		// Sort and assign order (generic across all scanner types)
		sort.Slice(items, func(i, j int) bool {
			return compareOrderParts(items[i].OrderParts, items[j].OrderParts) < 0
		})
		for i, item := range items {
			item.Order = new(i)
			r.markDirty(item)
		}

		s.UpdateSeries(r, parent, items)
	}

	if err := r.commit(ctx); err != nil {
		scanErr = err
		return nil, err
	}

	taskUpdate(db.TaskUpdateOpts{Status: new(models.TaskStatusCompleted)})

	return &ScanResult{
		Added:     len(toAdd),
		Updated:   len(toUpdate),
		Removed:   len(toRemove),
		Unchanged: len(unchanged),
	}, nil
}

func applyParsedItem(r *repository, libraryID string, p *ParsedItem) *models.Content {
	var series *models.Content
	var parentID *string
	if p.Series != nil {
		uri := p.Series.URIPrefix + "/" + p.Series.URIPart
		series = r.getSeries(uri, p.Series.URIPart, p.Series.FileURI, p.Series.ContentType, p.Series.Title)
		parentID = &series.ID
	}

	existing := r.findContentByFileURI(p.File.Path)
	content := existing
	if content == nil {
		content = r.matchDeletedItem(p.URIPart, parentID)
	}

	now := time.Now().UTC()
	if content == nil {
		newContent := models.Content{
			ID:        models.MakeContentID(),
			LibraryID: libraryID,
			Type:      p.ContentType,
			CreatedAt: now,
		}
		r.content = append(r.content, newContent)
		content = &r.content[len(r.content)-1]
	}

	content.FileURI = new(p.File.Path)
	content.URIPart = p.URIPart
	content.Valid = true
	content.ParentID = parentID
	content.UpdatedAt = now
	content.FileMtime = new(p.File.Mtime.UTC())
	content.FileSize = new(int(p.File.Size))
	content.OrderParts = p.OrderParts
	content.FileData = p.FileData

	if series != nil {
		content.URI = series.URI + "/" + p.URIPart
	} else {
		content.URI = p.URIPrefix + "/" + p.URIPart
	}

	if p.CoverSuffix != nil {
		content.CoverURI = new(p.File.Path + "/" + *p.CoverSuffix)
	}

	r.markDirty(content)

	metaRow := r.getMetadata(content.URI)
	metaRow.setSource("file", p.Meta, p.MetaRaw)

	return content
}

var seriesInheritedFields = []string{
	"authors", "publisher", "language", "genre", "age_rating",
	"manga", "imprint", "description", "publication_date",
}

func inheritChildMetadata(r *repository, series *models.Content, items []*models.Content) {
	if len(items) == 0 {
		return
	}

	inherited := map[string]any{}
	for _, item := range items {
		childMeta := r.getMetadata(item.URI)
		for _, field := range seriesInheritedFields {
			if _, ok := inherited[field]; ok {
				continue
			}
			if v, ok := childMeta.Data[field]; ok {
				inherited[field] = v
			}
		}
		if len(inherited) == len(seriesInheritedFields) {
			break
		}
	}

	// Derive title from first child's "series" field
	var seriesTitle string
	for _, item := range items {
		childMeta := r.getMetadata(item.URI)
		if s, ok := childMeta.Data["series"].(string); ok && s != "" {
			seriesTitle = s
			break
		}
	}
	if seriesTitle == "" {
		seriesTitle = series.URIPart
	}
	inherited["title"] = seriesTitle

	metaRow := r.getMetadata(series.URI)
	existing := map[string]any{}
	if raw, ok := metaRow.DataRaw["file"]; ok {
		var entry struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(raw, &entry) == nil && entry.Data != nil {
			existing = entry.Data
		}
	}
	for k, v := range inherited {
		existing[k] = v
	}
	metaRow.setSource("file", existing, nil)
}

func matchFiles(r *repository, files []FSFile, opts ScanOptions) (toAdd, toUpdate, unchanged, toRemove []FSFile) {
	leafContent := map[string]FSFile{}
	for _, c := range r.content {
		if c.Type != "comic" && c.Type != "book" {
			continue
		}
		if c.FileURI == nil {
			continue
		}
		var mtime time.Time
		if c.FileMtime != nil {
			mtime = *c.FileMtime
		}
		var size int64
		if c.FileSize != nil {
			size = int64(*c.FileSize)
		}
		leafContent[*c.FileURI] = FSFile{
			Path:  *c.FileURI,
			Mtime: mtime,
			Size:  size,
		}
	}

	fsByPath := map[string]FSFile{}
	for _, f := range files {
		fsByPath[f.Path] = f
	}

	for path, fsFile := range fsByPath {
		if len(opts.FilterPaths) > 0 {
			matched := false
			for _, fp := range opts.FilterPaths {
				if strings.HasPrefix(path, fp) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		dbFile, exists := leafContent[path]
		if !exists {
			toAdd = append(toAdd, fsFile)
		} else if fsFile.HasChanged(dbFile) {
			toUpdate = append(toUpdate, fsFile)
		} else {
			unchanged = append(unchanged, fsFile)
		}
	}

	for path, dbFile := range leafContent {
		if _, exists := fsByPath[path]; !exists {
			if len(opts.FilterPaths) > 0 {
				matched := false
				for _, fp := range opts.FilterPaths {
					if strings.HasPrefix(path, fp) {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			}
			toRemove = append(toRemove, dbFile)
		}
	}

	if opts.Force {
		toUpdate = append(toUpdate, unchanged...)
		unchanged = nil
	}

	return
}
