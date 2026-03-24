package scanner

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"voltis/lib/fp"
	"voltis/lib/tasks"
	"voltis/models"
	"voltis/models/contentmeta"
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
	Meta        contentmeta.Metadata
	MetaRaw     map[string]any // ComicInfo raw, nil for books
}

type ScanInput struct {
	LibraryID   string   `json:"library_id"`
	LibraryType string   `json:"library_type"`
	Sources     []string `json:"sources"`
	Force       bool     `json:"force"`
	FilterPaths []string `json:"filter_paths,omitempty"`
	Concurrency int      `json:"concurrency"`
}

type ScanResult struct {
	Added     int           `json:"added"`
	Updated   int           `json:"updated"`
	Removed   int           `json:"removed"`
	Failed    int           `json:"failed"`
	Unchanged int           `json:"unchanged"`
	Duration  time.Duration `json:"duration"`
}

// ScanTask is the task definition for library scans.
var ScanTask = tasks.Define(tasks.DefineOpts[ScanInput, ScanResult]{
	Name:    "scan_library",
	Process: runScan,
})

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

func runScan(input ScanInput, tc *tasks.TaskContext[ScanResult]) error {
	ctx := tc.Context()
	pool := tc.Pool()

	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	scanStart := time.Now()

	s := newFileScanner(input.LibraryType)
	if s == nil {
		return fmt.Errorf("unsupported library type: %s", input.LibraryType)
	}

	slog_scan("starting scan", "library", input.LibraryID, "type", input.LibraryType, "force", input.Force, "filter_paths", input.FilterPaths, "concurrency", concurrency)

	// Determine sources
	scanSources := input.Sources
	if len(input.FilterPaths) > 0 {
		scanSources = input.FilterPaths
	}

	// Walk filesystem
	files, err := walkSources(scanSources, s.FileEligible)
	if err != nil {
		return err
	}
	slog.Info("[scanner] found files", "count", len(files), "library", input.LibraryID)

	// Load existing content
	r := newRepository(pool, input.LibraryID)
	if err := r.load(ctx); err != nil {
		return err
	}

	// Diff
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, input.FilterPaths, input.Force)

	slog.Info("[scanner] diff",
		"add", len(toAdd), "update", len(toUpdate),
		"unchanged", len(unchanged), "remove", len(toRemove),
	)

	// Emit summary
	tc.Progress(map[string]int{
		"to_add":    len(toAdd),
		"to_update": len(toUpdate),
		"to_remove": len(toRemove),
		"unchanged": len(unchanged),
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

	fp.MapConcurrently(workList, concurrency, func(gf groupedFile) {
		parsed := s.ParseFile(input.LibraryID, gf.file)

		var parentID *string
		fp.WithMutex(&commitMu, func() {
			if commitErr != nil {
				return
			}
			if parsed != nil {
				parent := findParent(r, parsed)
				if parent != nil {
					parentID = &parent.ID
				}

				if !r.checkURIAvailable(parsed, parentID) {
					uri := makeURI(parsed, parent)
					slog.Warn("[scanner] URI conflict, skipping", "file", gf.file.Path, "uri", uri, "parent_id", fp.DerefString(parentID))
					tc.Log("URI conflict for file %s, skipping (uri: %s, parent_id: %s)\n",
						gf.file.Path, uri, fp.DerefString(parentID))
					resultFailed.Add(1)
				} else {
					content := applyParsedItem(r, input.LibraryID, parsed)
					if addSet[gf.file.Path] {
						resultAdded.Add(1)
					} else {
						resultUpdated.Add(1)
					}
					parentID = content.ParentID
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
		tc.Progress(map[string]int{
			"total":     progressTotal,
			"processed": processed,
		})
	})

	if commitErr != nil {
		return commitErr
	}

	if err := r.commitFinal(ctx); err != nil {
		return err
	}

	// Compute accurate Removed count: initial toRemove items still in contentD
	resultRemoved := 0
	for _, c := range r.contentD {
		if removedIDs[c.ID] {
			resultRemoved++
		}
	}

	result := ScanResult{
		Added:     int(resultAdded.Load()),
		Updated:   int(resultUpdated.Load()),
		Removed:   resultRemoved,
		Failed:    int(resultFailed.Load()),
		Unchanged: len(unchanged),
		Duration:  time.Since(scanStart),
	}

	tc.Result(result)
	return nil
}

func findParent(r *repository, p *ParsedItem) *models.Content {
	if p.Series == nil {
		return nil
	}
	uri := p.Series.URIPrefix + "/" + p.Series.URIPart
	return r.getSeries(uri, p.Series.URIPart, p.Series.FileURI, p.Series.ContentType, p.Series.Title)
}

func makeURI(p *ParsedItem, series *models.Content) string {
	if series != nil {
		return series.URI + "/" + p.URIPart
	}
	return p.URIPrefix + "/" + p.URIPart
}

func applyParsedItem(r *repository, libraryID string, p *ParsedItem) *models.Content {
	var parentID *string
	series := findParent(r, p)
	if series != nil {
		parentID = &series.ID
	}

	existing := r.findContentByFileURI(p.File.Path)
	content := existing
	if content == nil {
		content = r.matchDeletedItem(p.URIPart, parentID)
	}

	now := time.Now().UTC()
	if content == nil {
		r.content = append(r.content, models.Content{
			ID:        models.MakeContentID(),
			LibraryID: libraryID,
			Type:      p.ContentType,
			CreatedAt: now,
		})
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
	content.URI = makeURI(p, series)

	if p.CoverSuffix != nil {
		content.CoverURI = new(p.File.Path + "/" + *p.CoverSuffix)
	}

	r.markDirty(content)

	metaRow := r.getMetadata(content.URI)
	metaRow.setSource("file", p.Meta, p.MetaRaw)

	return content
}

func inheritChildMetadata(r *repository, series *models.Content, items []*models.Content) {
	if len(items) == 0 {
		return
	}

	var inherited contentmeta.Metadata
	// Inherit from first child that has each field set
	for _, item := range items {
		childMeta := r.getMetadata(item.URI)
		m := childMeta.Data
		if inherited.Staff == nil && len(m.Staff) > 0 {
			inherited.Staff = m.Staff
		}
		if inherited.Publisher == "" {
			inherited.Publisher = m.Publisher
		}
		if inherited.Language == "" {
			inherited.Language = m.Language
		}
		if inherited.Genre == "" {
			inherited.Genre = m.Genre
		}
		if inherited.AgeRating == "" {
			inherited.AgeRating = m.AgeRating
		}
		if inherited.Manga == "" {
			inherited.Manga = m.Manga
		}
		if inherited.Imprint == "" {
			inherited.Imprint = m.Imprint
		}
		if inherited.Description == "" {
			inherited.Description = m.Description
		}
		if inherited.PublicationDate == "" {
			inherited.PublicationDate = m.PublicationDate
		}
	}

	// Derive title from first child's "series" field
	var seriesTitle string
	for _, item := range items {
		childMeta := r.getMetadata(item.URI)
		if childMeta.Data.Series != "" {
			seriesTitle = childMeta.Data.Series
			break
		}
	}
	if seriesTitle == "" {
		seriesTitle = series.URIPart
	}
	inherited.Title = seriesTitle

	metaRow := r.getMetadata(series.URI)
	var existing contentmeta.Metadata
	if raw, ok := metaRow.DataRaw["file"]; ok {
		existing, _ = contentmeta.ParseLayerEntry(raw)
	}
	// Merge: inherited fills in gaps in existing
	result := contentmeta.Merge(inherited, existing)
	// But always update title from inheritance
	result.Title = inherited.Title
	metaRow.setSource("file", result, nil)
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

func matchFiles(r *repository, files []FSFile, filterPaths []string, force bool) (toAdd, toUpdate, unchanged, toRemove []FSFile) {
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
		if len(filterPaths) > 0 {
			matched := false
			for _, fp := range filterPaths {
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
			if len(filterPaths) > 0 {
				matched := false
				for _, fp := range filterPaths {
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

	if force {
		toUpdate = append(toUpdate, unchanged...)
		unchanged = nil
	}

	return
}
