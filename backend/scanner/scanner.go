package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"voltis/lib/fp"
	"voltis/lib/tasks"
	"voltis/models"
	"voltis/models/metaraw"
)

type FileScanner interface {
	FileEligible(path string) bool
	ParseFile(file FSFile) *ParsedItem

	SeriesCover(series SeriesRef, ordered []Child) (*string, *time.Time)
	UpdateSeries(r *repository, series *models.Content, ordered []Child)
}

type ParsedSeries struct {
	URIPrefix   string
	URIPart     string
	ContentType string
	Title       string
	FileURI     *string
}

type ParsedItem struct {
	File        FSFile
	Series      *ParsedSeries
	URIPrefix   string
	ContentType string
	URIPart     string
	OrderParts  []*float32
	CoverSuffix *string
	FileData    json.RawMessage
	MetaRaw     models.Metadata
}

type Result struct {
	File FSFile
	Item *ParsedItem
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

type Counts struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
}

type Progress struct {
	Phase     string `json:"phase"`
	Found     int    `json:"found"`
	Total     int    `json:"total"`
	Processed int    `json:"processed"`
	Unchanged int    `json:"unchanged"`
	Failed    int    `json:"failed"`
	Saved     Counts `json:"saved"`
	CommitSeq int    `json:"commit_seq"`
}

type CatalogChanged struct {
	LibraryID string `json:"library_id"`
	TaskID    string `json:"task_id"`
	CommitSeq int    `json:"commit_seq"`
}

type Notifier interface{ CatalogChanged(CatalogChanged) }

var (
	notifierMu sync.RWMutex
	notifier   Notifier
)

func SetNotifier(n Notifier) {
	notifierMu.Lock()
	notifier = n
	notifierMu.Unlock()
}

func notifyCatalog(libraryID, taskID string, seq int) {
	notifierMu.RLock()
	n := notifier
	notifierMu.RUnlock()
	if n != nil {
		n.CatalogChanged(CatalogChanged{LibraryID: libraryID, TaskID: taskID, CommitSeq: seq})
	}
}

func unmarshalScanInput(data json.RawMessage) (any, error) {
	var v ScanInput
	err := json.Unmarshal(data, &v)
	return v, err
}

func scanCompatible(self any, other tasks.RunningInfo) bool {
	if other.Name != "scan_library" {
		return true
	}
	otherInput, ok := other.Input.(ScanInput)
	if !ok {
		return false
	}
	return self.(ScanInput).LibraryID != otherInput.LibraryID
}

var ScanTask = &tasks.TaskDef{
	Name: "scan_library",
	Process: func(input any, tc *tasks.TaskContext) (any, error) {
		return runLegacyScan(input.(ScanInput), tc)
	},
	UnmarshalInput:   unmarshalScanInput,
	IsCompatibleWith: scanCompatible,
}

func NewScanTask(notify Notifier) *tasks.TaskDef {
	return &tasks.TaskDef{
		Name: "scan_library",
		Process: func(input any, tc *tasks.TaskContext) (any, error) {
			return runScan(tc.Context(), input.(ScanInput), tc, notify)
		},
		UnmarshalInput:   unmarshalScanInput,
		IsCompatibleWith: scanCompatible,
	}
}

func parseWorker(ctx context.Context, s FileScanner, jobs <-chan FSFile, results chan<- Result) {
	for f := range jobs {
		if ctx.Err() != nil {
			return
		}
		item := s.ParseFile(f)
		select {
		case results <- Result{File: f, Item: item}:
		case <-ctx.Done():
			return
		}
	}
}

func runScan(ctx context.Context, in ScanInput, tc *tasks.TaskContext, notify Notifier) (ScanResult, error) {
	start := time.Now()
	tc.Progress(Progress{Phase: "walking"})

	s := newFileScanner(in.LibraryType)
	if s == nil {
		return ScanResult{}, fmt.Errorf("unsupported library type: %s", in.LibraryType)
	}

	pool := tc.Pool()
	res := newResolver()
	fps, err := loadFingerprints(ctx, pool, in.LibraryID)
	if err != nil {
		return ScanResult{}, err
	}
	refs, err := loadSeries(ctx, pool, in.LibraryID)
	if err != nil {
		return ScanResult{}, err
	}

	roots := in.Sources
	if len(in.FilterPaths) > 0 {
		roots = in.FilterPaths
	}
	workers := in.Concurrency
	if workers <= 0 {
		workers = 10
	}

	slog_scan("starting scan", "library", in.LibraryID, "type", in.LibraryType, "force", in.Force,
		"filter_paths", in.FilterPaths, "concurrency", workers)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	events := make(chan Event, 256)
	walkDone := make(chan error, 1)
	jobs := make(chan FSFile, workers)
	results := make(chan Result, workers)
	flushes := make(chan flush, 1)
	done := make(chan committed, 1)

	w := newWriter(in, tc, notify, res, fps, refs)

	var wg sync.WaitGroup
	wg.Go(func() {
		err := walk(ctx, roots, s.FileEligible, events)
		close(events)
		walkDone <- err
	})
	for range workers {
		wg.Go(func() { parseWorker(ctx, s, jobs, results) })
	}
	wg.Go(func() { commitLoop(ctx, pool, s, in.LibraryID, flushes, done) })

	err = w.run(ctx, events, walkDone, jobs, results, flushes, done)
	cancel()
	wg.Wait()

	return ScanResult{
		Added:     w.prog.Saved.Added,
		Updated:   w.prog.Saved.Updated,
		Removed:   w.prog.Saved.Removed,
		Failed:    w.prog.Failed,
		Unchanged: w.prog.Unchanged,
		Duration:  time.Since(start),
	}, err
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

func runLegacyScan(input ScanInput, tc *tasks.TaskContext) (ScanResult, error) {
	ctx := tc.Context()
	pool := tc.Pool()

	concurrency := input.Concurrency
	if concurrency <= 0 {
		concurrency = 10
	}

	scanStart := time.Now()

	var progMu sync.Mutex
	prog := Progress{Phase: "walking"}
	emit := func(fn func(p *Progress)) {
		progMu.Lock()
		defer progMu.Unlock()
		fn(&prog)
		tc.Progress(prog)
	}
	emit(func(p *Progress) {})

	s := newFileScanner(input.LibraryType)
	if s == nil {
		return ScanResult{}, fmt.Errorf("unsupported library type: %s", input.LibraryType)
	}

	slog_scan("starting scan", "library", input.LibraryID, "type", input.LibraryType, "force", input.Force, "filter_paths", input.FilterPaths, "concurrency", concurrency)

	scanSources := input.Sources
	if len(input.FilterPaths) > 0 {
		scanSources = input.FilterPaths
	}

	walked, err := walkSources(scanSources, s.FileEligible)
	if err != nil {
		return ScanResult{}, err
	}
	files := walked.Files
	slog.Info("[scanner] found files", "count", len(files), "library", input.LibraryID)
	emit(func(p *Progress) { p.Found = len(files) })

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

	r := newRepository(pool, input.LibraryID)
	if err := r.load(ctx); err != nil {
		return ScanResult{}, err
	}

	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, input.FilterPaths, failedPaths, input.Force)

	slog.Info("[scanner] diff",
		"add", len(toAdd), "update", len(toUpdate),
		"unchanged", len(unchanged), "remove", len(toRemove),
	)

	for _, file := range toRemove {
		if c := r.findContentByFileURI(file.Path); c != nil {
			r.removeContent(c)
		}
	}

	removedIDs := map[string]bool{}
	for _, c := range r.deletedContent {
		removedIDs[c.ID] = true
	}
	r.retarget()

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

	type groupState struct {
		mu      sync.Mutex
		done    int
		size    int
		parents map[string]bool
	}
	states := fp.Map(groups, func(g []FSFile) groupState {
		return groupState{size: len(g), parents: map[string]bool{}}
	})

	var commitMu sync.Mutex
	var commitErr error
	var counts scanCounts
	var batch Counts

	emit(func(p *Progress) {
		p.Phase = "parsing"
		p.Total = len(toProcess)
		p.Unchanged = len(unchanged)
	})

	fp.MapConcurrently(workList, concurrency, func(gf groupedFile) {
		parsed := s.ParseFile(gf.file)

		var parentID *string
		fp.WithMutex(&commitMu, func() {
			if commitErr != nil {
				return
			}
			parentID = applyParseResult(r, input.LibraryID, gf.file, parsed, addSet[gf.file.Path], tc.Log, &counts, &batch)
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

		committedSeq := 0
		if complete {
			fp.WithMutex(&commitMu, func() {
				if commitErr != nil {
					return
				}
				updateGroupSeries(s, r, state.parents)
				if commitErr = r.commitGroup(ctx); commitErr != nil {
					return
				}
				saved := batch
				batch = Counts{}
				emit(func(p *Progress) {
					p.Saved.Added += saved.Added
					p.Saved.Updated += saved.Updated
					p.CommitSeq++
					committedSeq = p.CommitSeq
				})
			})
		}

		emit(func(p *Progress) {
			p.Processed++
			p.Failed = int(counts.failed.Load())
		})

		if committedSeq > 0 {
			notifyCatalog(input.LibraryID, tc.ID(), committedSeq)
		}
	})

	if commitErr != nil {
		return ScanResult{}, commitErr
	}

	if len(r.moved) > 0 {
		updateGroupSeries(s, r, r.moved)
		if err := r.commitGroup(ctx); err != nil {
			return ScanResult{}, err
		}
	}

	emit(func(p *Progress) { p.Phase = "saving" })

	if err := r.commitFinal(ctx); err != nil {
		return ScanResult{}, err
	}

	resultRemoved := 0
	for _, c := range r.deletedContent {
		if removedIDs[c.ID] {
			resultRemoved++
		}
	}

	finalSeq := 0
	emit(func(p *Progress) {
		p.Saved.Removed += resultRemoved
		p.CommitSeq++
		p.Failed = int(counts.failed.Load())
		p.Phase = "done"
		finalSeq = p.CommitSeq
	})
	notifyCatalog(input.LibraryID, tc.ID(), finalSeq)

	return ScanResult{
		Added:     int(counts.added.Load()),
		Updated:   int(counts.updated.Load()),
		Removed:   resultRemoved,
		Failed:    int(counts.failed.Load()),
		Unchanged: len(unchanged),
		Duration:  time.Since(scanStart),
	}, nil
}

type scanCounts struct {
	added   atomic.Int64
	updated atomic.Int64
	failed  atomic.Int64
}

func applyParseResult(r *repository, libraryID string, file FSFile, parsed *ParsedItem, isAdd bool, logf func(string, ...any), counts *scanCounts, batch *Counts) *string {
	if parsed == nil {
		parentID := r.invalidateFile(file.Path)
		slog.Warn("[scanner] failed to parse file", "path", file.Path)
		counts.failed.Add(1)
		return parentID
	}

	parent, ok := findParent(r, parsed)
	if !ok {
		uri := parsed.Series.URIPrefix + "/" + parsed.Series.URIPart
		slog.Warn("[scanner] series key conflict, skipping", "file", file.Path, "uri", uri)
		logf("Series key conflict for file %s, skipping (uri: %s)\n", file.Path, uri)
		counts.failed.Add(1)
		return nil
	}

	var parentID *string
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
		batch.Added++
	} else {
		counts.updated.Add(1)
		batch.Updated++
	}
	return content.ParentID
}

func findParent(r *repository, p *ParsedItem) (*models.Content, bool) {
	if p.Series == nil {
		return nil, true
	}
	uri := p.Series.URIPrefix + "/" + p.Series.URIPart
	c := r.getSeries(uri, p.Series.URIPart, p.Series.FileURI, p.Series.ContentType, p.Series.Title)
	return c, c != nil
}

func makeURI(p *ParsedItem, series *models.Content) string {
	if series != nil {
		return series.URI + "/" + p.URIPart
	}
	return p.URIPrefix + "/" + p.URIPart
}

func applyParsedItem(r *repository, libraryID string, p *ParsedItem) *models.Content {
	var parentID *string
	series, _ := findParent(r, p)
	if series != nil {
		parentID = &series.ID
		r.placeSeries(series, p.Series.FileURI)
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
		r.reparent(content, parentID)
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
		Raw: inherit(seriesRef(series), ordered),
	}
	metaRow.dirty = true
}

func groupByFolder(files []FSFile) [][]FSFile {
	byFolder := map[string][]FSFile{}
	for _, f := range files {
		folder := filepath.Dir(f.Path)
		byFolder[folder] = append(byFolder[folder], f)
	}
	return fp.Map(slices.Sorted(maps.Keys(byFolder)), func(folder string) []FSFile {
		return byFolder[folder]
	})
}

func updateGroupSeries(s FileScanner, r *repository, parents map[string]bool) {
	for parentID := range parents {
		parent := r.byID(parentID)
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
