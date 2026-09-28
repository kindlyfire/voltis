package linking

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"slices"
	"time"

	"voltis/db"
	"voltis/metadata"
	"voltis/models"
	"voltis/providers"
)

// The worker recomputes metadata derived by an older DataVersion, matches series, and refreshes
// entries in the background. Their due state lives in the database, so it only needs waking when
// rows become due sooner than it expects.

const (
	Idle        = "idle"
	Recomputing = "recomputing"
	Matching    = "matching"
	Refreshing  = "refreshing"
)

type WorkerStatus struct {
	Activity    string              `json:"activity"`             // idle | recomputing | matching | refreshing
	LibraryID   string              `json:"library_id,omitempty"` // the library being matched
	Stale       int                 `json:"stale"`                // rows left for recomputing
	Paused      bool                `json:"paused"`               // matching is paused
	Matched     MatchResult         `json:"matched"`              // since the server started
	Refreshed   RefreshResult       `json:"refreshed"`
	MatchPass   Pass[MatchResult]   `json:"match_pass"` // the running or last one
	RefreshPass Pass[RefreshResult] `json:"refresh_pass"`
}

// Pass is a run of rounds that had work.
type Pass[T any] struct {
	Counts   T          `json:"counts"`
	Finished *time.Time `json:"finished"` // nil while it runs
}

// advance adds a round's counts to the running pass, or starts one, and to total. A round without
// any finishes the running pass.
func advance[T interface {
	comparable
	plus(T) T
}](p *Pass[T], total *T, counts T) {
	var none T
	running := p.Finished == nil && p.Counts != none
	switch {
	case counts == none:
		if running {
			p.Finished = new(time.Now())
		}
		return
	case !running:
		*p = Pass[T]{}
	}
	p.Counts, *total = p.Counts.plus(counts), (*total).plus(counts)
}

var pushDelay = time.Second

// update changes the status, then pushes it once pushDelay passed, with any other change made
// meanwhile.
func (s *Service) update(change func(*WorkerStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := s.status
	change(&s.status)
	if s.status == before || s.onStatus == nil || s.pushing {
		return
	}
	s.pushing = true
	time.AfterFunc(pushDelay, func() {
		s.mu.Lock()
		st, onStatus := s.status, s.onStatus
		s.pushing = false
		s.mu.Unlock()
		onStatus(st)
	})
}

func (s *Service) Status() WorkerStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Wake makes the worker look for work now.
func (s *Service) Wake() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run is the worker, until ctx ends. paused stops matching; scanning tells whether a scan of a
// library runs or waits, which defers matching it; onStatus receives the status as it changes.
func (s *Service) Run(ctx context.Context, paused func() bool, scanning func(libraryID string) bool,
	onStatus func(WorkerStatus)) {
	s.scanning = scanning
	s.mu.Lock()
	s.onStatus = onStatus
	s.mu.Unlock()
	for ctx.Err() == nil {
		wait, err := s.step(ctx, paused != nil && paused())
		if err != nil && ctx.Err() == nil {
			slog.Error("[linking] background work failed", "err", err)
			s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Idle, "" })
			wait = time.Minute
		}
		select {
		case <-ctx.Done():
		case <-s.wake:
		case <-time.After(wait):
		}
	}
}

// step recomputes a batch of stale rows, or else matches a page of series and refreshes a batch
// of entries, then notifies each library it changed once. It returns how long to wait: not at all
// after some work, else until the next series or entry falls due, at most 15 minutes. Idle, it
// collects unused entries and covers, at most hourly.
func (s *Service) step(ctx context.Context, paused bool) (time.Duration, error) {
	c := changes{}
	defer s.notifyAll(c)
	s.update(func(st *WorkerStatus) { st.Paused = paused })
	if did, err := s.recomputeStale(ctx, c); did || err != nil {
		return 0, err
	}
	var libs []libraryPlan
	var matched MatchResult
	var matchErr, refreshErr error
	if !paused {
		if libs, matchErr = s.matchLibraries(ctx); matchErr == nil {
			matched, matchErr = s.matchNext(ctx, c, libs)
		}
	}
	// A match failing again and again must not hold refreshes back.
	var refreshed RefreshResult
	if ctx.Err() == nil {
		refreshed, refreshErr = s.refreshBatch(ctx, c)
	}

	s.update(func(st *WorkerStatus) {
		if matched != (MatchResult{}) || matchErr == nil {
			advance(&st.MatchPass, &st.Matched, matched)
		}
		if refreshed != (RefreshResult{}) || refreshErr == nil {
			advance(&st.RefreshPass, &st.Refreshed, refreshed)
		}
	})
	if err := errors.Join(matchErr, refreshErr); err != nil {
		return 0, err
	}
	if matched != (MatchResult{}) || refreshed != (RefreshResult{}) {
		return 0, nil
	}
	if time.Since(s.collectedAt) >= time.Hour {
		s.collectedAt = time.Now()
		if err := errors.Join(s.collect(ctx), s.covers.GC(ctx, s.pool)); err != nil {
			return 0, err
		}
	}
	s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Idle, "" })
	return s.nextDue(ctx, libs)
}

// recomputeStale recomputes a batch of rows an older DataVersion derived, and reports whether
// there was any. Recomputed rows leave the selection, so each batch makes progress. Every writer
// derives rows at the current version, so once a batch finds none, none are left.
func (s *Service) recomputeStale(ctx context.Context, c changes) (bool, error) {
	if s.recomputed {
		return false, nil
	}
	if s.Status().Stale == 0 {
		n, err := db.SelectScalar[int](ctx, s.pool,
			"SELECT count(*) FROM content_metadata WHERE data_version < $1", metadata.DataVersion)
		if err != nil {
			return false, err
		}
		s.update(func(st *WorkerStatus) { st.Stale = n })
	}
	type key struct {
		URI       string `db:"uri"`
		LibraryID string `db:"library_id"`
	}
	keys, err := db.Select[key](ctx, s.pool, `
		SELECT uri, library_id FROM content_metadata WHERE data_version < $1
		ORDER BY uri, library_id LIMIT 500
	`, metadata.DataVersion)
	if err != nil || len(keys) == 0 {
		s.recomputed = err == nil
		return false, err
	}
	s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Recomputing, "" })
	byLib := map[string][]string{}
	for _, k := range keys {
		byLib[k.LibraryID] = append(byLib[k.LibraryID], k.URI)
	}
	for _, lib := range slices.Sorted(maps.Keys(byLib)) {
		err := s.commit(ctx, c, func(o *op) error {
			for _, uri := range byLib[lib] {
				o.touch(lib, uri)
			}
			return db.LockMetadata(ctx, o.tx, lib)
		})
		if err != nil {
			return false, err
		}
	}
	s.update(func(st *WorkerStatus) { st.Stale = max(st.Stale-len(keys), 0) })
	return true, nil
}

// libraryPlan is what the worker matches in a library: its coverage by provider.
type libraryPlan struct {
	ID string
	By map[string]autoMatch
}

// matchLibraries lists the libraries that match automatically with some provider, but not while a
// scan of them runs or waits, since a series read between two of its flushes may lack members that
// would contradict a match.
func (s *Service) matchLibraries(ctx context.Context) ([]libraryPlan, error) {
	libs, err := db.Select[models.Library](ctx, s.pool, "SELECT * FROM libraries ORDER BY id")
	if err != nil {
		return nil, err
	}
	var plans []libraryPlan
	for _, lib := range libs {
		if s.scanning(lib.ID) {
			continue
		}
		plan := libraryPlan{lib.ID, map[string]autoMatch{}}
		for _, p := range s.reg.All() {
			if cov := autoMatchOf(lib, p.Name()); cov.any() {
				plan.By[p.Name()] = cov
			}
		}
		if len(plan.By) > 0 {
			plans = append(plans, plan)
		}
	}
	return plans, nil
}

// matchNext matches a page of due series from the next of libs in turn that has any, continuing
// where its last page stopped and then from its start.
func (s *Service) matchNext(ctx context.Context, c changes, libs []libraryPlan) (MatchResult, error) {
	start := slices.IndexFunc(libs, func(l libraryPlan) bool { return l.ID == s.lastLib }) + 1
	for i := range libs {
		lib := libs[(start+i)%len(libs)]
		for _, p := range s.reg.All() {
			cov, ok := lib.By[p.Name()]
			if !ok {
				continue
			}
			key := [2]string{lib.ID, p.Name()}
			for _, after := range slices.Compact([]string{s.cursors[key], ""}) {
				page, err := s.duePage(ctx, p, lib.ID, cov, after, time.Now())
				if err != nil {
					return MatchResult{}, err
				}
				if len(page) > 0 {
					s.lastLib = lib.ID
					s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Matching, lib.ID })
					res, n, err := s.matchSeries(ctx, c, p, page, s.scanning, nil)
					s.cursors[key] = page[n-1].URI
					return res, err
				}
			}
			delete(s.cursors, key)
		}
	}
	return MatchResult{}, nil
}

// nextDue is how long until the next series of libs or used entry falls due, at most 15 minutes.
func (s *Service) nextDue(ctx context.Context, libs []libraryPlan) (time.Duration, error) {
	var due []*time.Time
	for _, lib := range libs {
		for _, p := range s.reg.All() {
			cov, ok := lib.By[p.Name()]
			if !ok {
				continue
			}
			covered, args := cov.sql(4)
			// The first in idx_metadata_links_retry order that is covered, rather than the least of all.
			retry, err := db.SelectScalar[*time.Time](ctx, s.pool,
				"SELECT (SELECT l.retry_at "+pendingSeries+" AND l.retry_at IS NOT NULL"+covered+" ORDER BY l.retry_at LIMIT 1)",
				append([]any{customPlan, lib.ID, p.Name(), providers.ContentTypes(p)}, args...)...)
			if err != nil {
				return 0, err
			}
			due = append(due, retry)
		}
	}
	for _, p := range s.reg.All() {
		refresh, err := db.SelectScalar[*time.Time](ctx, s.pool,
			"SELECT (SELECT e.refresh_at "+usedEntries+" ORDER BY e.refresh_at LIMIT 1)", p.Name())
		if err != nil {
			return 0, err
		}
		due = append(due, refresh)
	}
	next := time.Now().Add(15 * time.Minute)
	for _, t := range due {
		if t != nil && t.Before(next) {
			next = *t
		}
	}
	return max(time.Until(next), 0), nil
}
