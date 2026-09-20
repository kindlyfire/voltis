package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"voltis/lib/tasks"
	"voltis/models"
)

type FileScanner interface {
	FileEligible(path string) bool
	ParseFile(file FSFile) *ParsedItem

	SeriesCover(series SeriesRef, ordered []Child) (*string, *time.Time)
}

type ParsedSeries struct {
	URIPrefix   string
	URIPart     string
	ContentType string
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
	Counts
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

func slog_scan(msg string, args ...any) {
	slog.Info("[scanner] "+msg, args...)
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
	return ok && self.(ScanInput).LibraryID != otherInput.LibraryID
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
		Counts:    w.prog.Saved,
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
