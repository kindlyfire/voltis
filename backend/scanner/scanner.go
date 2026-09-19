package scanner

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"voltis/lib/fp"
	"voltis/lib/tasks"
	"voltis/models"
	"voltis/models/metaraw"
)

// FileScanner is the interface each scanner type must implement.
type FileScanner interface {
	// FileEligible returns whether a file should be scanned.
	FileEligible(path string) bool

	// ParseFile parses a file and returns the extracted data, or nil if the
	// file could not be parsed. Should be safe to call concurrently.
	ParseFile(libraryID string, file FSFile) *ParsedItem

	SeriesCover(series SeriesRef, ordered []Child) (*string, *time.Time)

	// UpdateSeries is called for each series that had at least one child
	// added, updated, or removed.
	UpdateSeries(r *repository, series *models.Content, ordered []Child)
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
	MetaRaw     models.Metadata
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
var ScanTask = &tasks.TaskDef{
	Name: "scan_library",
	Process: func(input any, tc *tasks.TaskContext) error {
		return runScan(input.(ScanInput), tc)
	},
	UnmarshalInput: func(data json.RawMessage) (any, error) {
		var v ScanInput
		err := json.Unmarshal(data, &v)
		return v, err
	},
	IsCompatibleWith: func(self any, other tasks.RunningInfo) bool {
		if other.Name != "scan_library" {
			return true
		}
		otherInput, ok := other.Input.(ScanInput)
		if !ok {
			return false
		}
		return self.(ScanInput).LibraryID != otherInput.LibraryID
	},
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

func runScan(input ScanInput, tc *tasks.TaskContext) error {
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
	walked, err := walkSources(scanSources, s.FileEligible)
	if err != nil {
		return err
	}
	files := walked.Files
	slog.Info("[scanner] found files", "count", len(files), "library", input.LibraryID)

	var failedPaths []string
	for _, f := range walked.Failures {
		failedPaths = append(failedPaths, f.Path)
		slog.Warn("[scanner] failed to read path, retaining missing entries at or below it and suppressing their deletion",
			"path", f.Path, "err", f.Err, "library", input.LibraryID)
		tc.Log("Failed to read %s: %v; missing entries at or below it were retained\n", f.Path, f.Err)
	}
	if len(failedPaths) > 0 {
		slog.Warn("[scanner] inventory incomplete, deletion suppressed within failed scopes", "paths", len(failedPaths), "library", input.LibraryID)
		tc.Log("Inventory incomplete at %d paths; deletion was suppressed within those scopes.\n", len(failedPaths))
	}

	// Load existing content
	r := newRepository(pool, input.LibraryID)
	if err := r.load(ctx); err != nil {
		return err
	}

	// Diff
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, input.FilterPaths, failedPaths, input.Force)

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
	var counts scanCounts

	fp.MapConcurrently(workList, concurrency, func(gf groupedFile) {
		parsed := s.ParseFile(input.LibraryID, gf.file)

		var parentID *string
		fp.WithMutex(&commitMu, func() {
			if commitErr != nil {
				return
			}
			parentID = applyParseResult(r, input.LibraryID, gf.file, parsed, addSet[gf.file.Path], tc.Log, &counts)
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
		Added:     int(counts.added.Load()),
		Updated:   int(counts.updated.Load()),
		Removed:   resultRemoved,
		Failed:    int(counts.failed.Load()),
		Unchanged: len(unchanged),
		Duration:  time.Since(scanStart),
	}

	tc.Result(result)
	return nil
}

type scanCounts struct {
	added   atomic.Int64
	updated atomic.Int64
	failed  atomic.Int64
}

func applyParseResult(r *repository, libraryID string, file FSFile, parsed *ParsedItem, isAdd bool, logf func(string, ...any), counts *scanCounts) *string {
	if parsed == nil {
		parentID := r.invalidateFile(file.Path)
		slog.Warn("[scanner] failed to parse file", "path", file.Path)
		counts.failed.Add(1)
		return parentID
	}

	var parentID *string
	parent := findParent(r, parsed)
	if parent != nil {
		parentID = &parent.ID
	}

	if !r.checkURIAvailable(parsed, parentID) {
		uri := makeURI(parsed, parent)
		slog.Warn("[scanner] URI conflict, skipping", "file", file.Path, "uri", uri, "parent_id", fp.DerefString(parentID))
		logf("URI conflict for file %s, skipping (uri: %s, parent_id: %s)\n", file.Path, uri, fp.DerefString(parentID))
		counts.failed.Add(1)
		return parentID
	}

	content := applyParsedItem(r, libraryID, parsed)
	if isAdd {
		counts.added.Add(1)
	} else {
		counts.updated.Add(1)
	}
	return content.ParentID
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

	content := r.findContentByFileURI(p.File.Path)
	if content == nil {
		content = r.matchDeletedItem(p.URIPart, parentID)
	}

	var old *models.Content
	if content == nil {
		r.content = append(r.content, models.Content{ID: models.MakeContentID()})
		content = &r.content[len(r.content)-1]
	} else {
		previous := *content
		old = &previous
	}
	id := content.ID

	*content = leafRow(id, libraryID, makeURI(p, series), *p, parentID, old, time.Now().UTC())
	r.markDirty(content)

	metaRow := r.getMetadata(content.URI)
	metaRow.DataRaw.File = &metaraw.RawContainer[models.Metadata]{Raw: p.MetaRaw}
	metaRow.dirty = true

	return content
}

func inheritChildMetadata(r *repository, series *models.Content, ordered []Child) {
	if len(ordered) == 0 {
		return
	}

	metaRow := r.getMetadata(series.URI)
	metaRow.DataRaw.File = &metaraw.RawContainer[models.Metadata]{
		Raw: inherit(series.URIPart, ordered),
	}
	metaRow.dirty = true
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

		children := r.childrenOf(parentID)
		byID := map[string]*models.Content{}
		for _, c := range children {
			byID[c.ID] = c
		}
		ordered := order(r.children(children))
		for i := range ordered {
			row := byID[ordered[i].ID]
			row.Order = new(i)
			ordered[i].Order = row.Order
			r.markDirty(row)
		}

		s.UpdateSeries(r, parent, ordered)
	}
}

func matchFiles(r *repository, files []FSFile, filterPaths, failedPaths []string, force bool) (toAdd, toUpdate, unchanged, toRemove []FSFile) {
	leafContent := map[string]FSFile{}
	invalidPaths := map[string]bool{}
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
		if !c.Valid {
			invalidPaths[*c.FileURI] = true
		}
	}

	fsByPath := map[string]FSFile{}
	for _, f := range files {
		fsByPath[f.Path] = f
	}

	inScope := func(path string) bool {
		return len(filterPaths) == 0 || withinAny(path, filterPaths)
	}

	for path, fsFile := range fsByPath {
		if !inScope(path) {
			continue
		}

		dbFile, exists := leafContent[path]
		if !exists {
			toAdd = append(toAdd, fsFile)
		} else if invalidPaths[path] || fsFile.HasChanged(dbFile) {
			toUpdate = append(toUpdate, fsFile)
		} else {
			unchanged = append(unchanged, fsFile)
		}
	}

	for path, dbFile := range leafContent {
		if _, exists := fsByPath[path]; exists {
			continue
		}
		if !inScope(path) || withinAny(path, failedPaths) {
			continue
		}
		toRemove = append(toRemove, dbFile)
	}

	if force {
		toUpdate = append(toUpdate, unchanged...)
		unchanged = nil
	}

	return
}
