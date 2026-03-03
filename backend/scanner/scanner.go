package scanner

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voltis/db"
	"voltis/lib/bufchan"
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
	Failed    int
	Unchanged int
	Duration  time.Duration
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

	scanStart := time.Now()

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

	taskBC := bufchan.NewBufChan(db.MergeTaskUpdateOpts, 200*time.Millisecond, func(tuOpts db.TaskUpdateOpts) error {
		return db.TaskUpdate(ctx, pool, opts.Hub, task, tuOpts)
	})
	defer taskBC.Close()

	taskUpdate := func(tuOpts db.TaskUpdateOpts) {
		if task == nil || opts.Hub == nil {
			return
		}
		immediate := tuOpts.Status != nil || tuOpts.Output != nil
		var err error
		if immediate {
			err = taskBC.SendNow(tuOpts)
		} else {
			err = taskBC.Send(tuOpts)
		}
		if err != nil {
			slog.Error("[scanner] task update failed", "err", err)
		}
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

	// Move removed items to deleted list (in-memory; DB delete in commitFinal)
	for _, file := range toRemove {
		for i := range r.content {
			if r.content[i].FileURI != nil && *r.content[i].FileURI == file.Path {
				r.removeContent(&r.content[i])
				break
			}
		}
	}

	// Snapshot contentD IDs to compute accurate Removed count later
	removedIDs := map[string]bool{}
	for _, c := range r.contentD {
		removedIDs[c.ID] = true
	}

	// Group toProcess files by folder and build sorted work list
	toProcess := append(toAdd, toUpdate...)
	addSet := map[string]bool{}
	for _, f := range toAdd {
		addSet[f.Path] = true
	}
	groups := groupByFolder(toProcess)

	type groupedFile struct {
		file     FSFile
		groupIdx int
	}
	var workList []groupedFile
	for i, g := range groups {
		for _, f := range g {
			workList = append(workList, groupedFile{file: f, groupIdx: i})
		}
	}

	// Per-group completion tracking
	type groupState struct {
		mu      sync.Mutex
		done    int
		size    int
		parents map[string]bool
	}
	states := fp.Map(groups, func(g []FSFile) groupState {
		return groupState{size: len(g), parents: map[string]bool{}}
	})

	// Process files concurrently, commit per group
	progressTotal := len(toProcess)
	var progressProcessed atomic.Int64
	var commitMu sync.Mutex
	var commitErr error
	var resultAdded, resultUpdated, resultFailed atomic.Int64

	fp.MapConcurrently(workList, opts.Concurrency, func(gf groupedFile) {
		parsed := s.ParseFile(libraryID, gf.file)

		var parentID *string
		fp.WithMutex(&commitMu, func() {
			if commitErr != nil {
				return
			}
			if parsed != nil {
				content := applyParsedItem(r, libraryID, parsed)
				if !r.checkURIAvailable(content) {
					slog.Warn("[scanner] URI conflict, skipping", "file", gf.file.Path, "uri", content.URI)
					resultFailed.Add(1)
				} else {
					if addSet[gf.file.Path] {
						resultAdded.Add(1)
					} else {
						resultUpdated.Add(1)
					}
					if content.ParentID != nil {
						parentID = content.ParentID
					}
				}
			} else {
				existing := r.findContentByFileURI(gf.file.Path)
				if existing != nil {
					parentID = existing.ParentID
					r.removeContent(existing)
				}
				slog.Warn("[scanner] failed to parse file", "path", gf.file.Path)
				resultFailed.Add(1)
			}
		})

		state := &states[gf.groupIdx]
		var complete bool
		fp.WithMutex(&state.mu, func() {
			state.done++
			if parentID != nil {
				state.parents[*parentID] = true
			}
			complete = state.done == state.size
		})

		if complete {
			fp.WithMutex(&commitMu, func() {
				if commitErr != nil {
					return
				}
				updateGroupSeries(s, r, state.parents)
				commitErr = r.commitGroup(ctx)
			})
		}

		processed := int(progressProcessed.Add(1))
		taskUpdate(db.TaskUpdateOpts{
			Progress: map[string]int{
				"total":     progressTotal,
				"processed": processed,
			},
		})
	})

	if commitErr != nil {
		scanErr = commitErr
		return nil, commitErr
	}

	if err := r.commitFinal(ctx); err != nil {
		scanErr = err
		return nil, err
	}

	// Compute accurate Removed count: initial toRemove items still in contentD
	resultRemoved := 0
	for _, c := range r.contentD {
		if removedIDs[c.ID] {
			resultRemoved++
		}
	}

	result := &ScanResult{
		Added:     int(resultAdded.Load()),
		Updated:   int(resultUpdated.Load()),
		Removed:   resultRemoved,
		Failed:    int(resultFailed.Load()),
		Unchanged: len(unchanged),
		Duration:  time.Since(scanStart),
	}

	// Emit actual counts
	taskUpdate(db.TaskUpdateOpts{
		Output: map[string]int{
			"added":     result.Added,
			"updated":   result.Updated,
			"removed":   result.Removed,
			"failed":    result.Failed,
			"unchanged": result.Unchanged,
		},
	})

	taskUpdate(db.TaskUpdateOpts{Status: new(models.TaskStatusCompleted)})

	return result, nil
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

func groupByFolder(files []FSFile) [][]FSFile {
	byFolder := map[string][]FSFile{}
	for _, f := range files {
		folder := filepath.Dir(f.Path)
		byFolder[folder] = append(byFolder[folder], f)
	}
	folders := make([]string, 0, len(byFolder))
	for folder := range byFolder {
		folders = append(folders, folder)
	}
	sort.Strings(folders)
	return fp.Map(folders, func(folder string) []FSFile {
		return byFolder[folder]
	})
}

func updateGroupSeries(s FileScanner, r *repository, parents map[string]bool) {
	for parentID := range parents {
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

		sort.Slice(items, func(i, j int) bool {
			return compareOrderParts(items[i].OrderParts, items[j].OrderParts) < 0
		})
		for i, item := range items {
			item.Order = new(i)
			r.markDirty(item)
		}

		s.UpdateSeries(r, parent, items)
	}
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
