package scanner

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FileScanner is the interface each scanner type must implement.
type FileScanner interface {
	// FileEligible returns whether a file should be scanned.
	FileEligible(path string) bool

	// ScanFile scans a single file and returns the resulting content, or nil if
	// the file could not be parsed.
	ScanFile(r *repository, libraryID string, file FSFile) *models.Content

	// UpdateSeries is called for each series that had at least one child
	// added, updated, or removed.
	UpdateSeries(r *repository, series *models.Content, items []*models.Content)
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
}

func newFileScanner(libraryType string) FileScanner {
	switch libraryType {
	case "comics":
		return &ComicsScanner{}
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

	// Determine sources
	scanSources := sources
	if len(opts.FilterPaths) > 0 {
		scanSources = opts.FilterPaths
	}

	// Walk filesystem
	files, err := walkSources(scanSources, s.FileEligible)
	if err != nil {
		return nil, err
	}
	slog.Info("[scanner] found files", "count", len(files), "library", libraryID)

	// Load existing content
	r := newRepository(pool, libraryID)
	if err := r.load(ctx); err != nil {
		return nil, err
	}

	// Diff
	toAdd, toUpdate, unchanged, toRemove := matchFiles(r, files, opts)

	slog.Info("[scanner] diff",
		"add", len(toAdd), "update", len(toUpdate),
		"unchanged", len(unchanged), "remove", len(toRemove),
	)

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
	parentsWithUpdates := &sync.Map{}
	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, file := range toProcess {
		wg.Add(1)
		go func(f FSFile) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			result := s.ScanFile(r, libraryID, f)

			mu.Lock()
			defer mu.Unlock()

			if result != nil {
				if !r.checkURIAvailable(result) {
					slog.Warn("[scanner] URI conflict, skipping", "file", f.Path, "uri", result.URI)
					return
				}
				if result.ParentID != nil {
					parentsWithUpdates.Store(*result.ParentID, true)
				}
			} else {
				existing := r.findContentByFileURI(f.Path)
				if existing != nil {
					if existing.ParentID != nil {
						parentsWithUpdates.Store(*existing.ParentID, true)
					}
					r.removeContent(existing)
				}
				slog.Warn("[scanner] failed to parse file", "path", f.Path)
			}
		}(file)
	}
	wg.Wait()

	// Update series that had changes
	parentsWithUpdates.Range(func(key, _ any) bool {
		parentID := key.(string)
		var parent *models.Content
		for i := range r.content {
			if r.content[i].ID == parentID {
				parent = &r.content[i]
				break
			}
		}
		if parent == nil {
			return true
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
		return true
	})

	// Commit
	if err := r.commit(ctx); err != nil {
		return nil, err
	}

	return &ScanResult{
		Added:     len(toAdd),
		Updated:   len(toUpdate),
		Removed:   len(toRemove),
		Unchanged: len(unchanged),
	}, nil
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
