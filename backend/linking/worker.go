package linking

import (
	"context"
	"encoding/json"
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
	Retrying    bool                `json:"retrying"`             // waiting out the error backoff
	MatchPass   Pass[MatchResult]   `json:"match_pass"`           // the running or last finished one
	RefreshPass Pass[RefreshResult] `json:"refresh_pass"`
}

// Pass is the running or last finished run of rounds that had work, for one kind.
type Pass[T any] struct {
	Counts  T          `json:"counts"`
	Started *time.Time `json:"started"` // nil: none yet
	Ended   *time.Time `json:"ended"`   // end of its last round with work
	Running bool       `json:"running"`
}

// counter is a round's outcome: comparable, so a zero value means none happened, and summable into
// a pass's running total.
type counter[T any] interface {
	comparable
	plus(T) T
}

// advancePass adds a round's counts to the selected pass, starting one if none runs. A round
// without any finishes it, whether or not it errored, and saves it outside the status lock.
func advancePass[T counter[T]](ctx context.Context, s *Service, kind string, pass func(*WorkerStatus) *Pass[T],
	counts T, start, end time.Time) {
	var finished *Pass[T]
	s.update(func(st *WorkerStatus) {
		p := pass(st)
		var none T
		switch {
		case counts == none:
			if p.Running {
				p.Running = false
				finished = new(*p)
			}
			return
		case !p.Running:
			*p = Pass[T]{Started: &start}
		}
		p.Counts, p.Ended, p.Running = p.Counts.plus(counts), &end, true
	})
	if finished == nil {
		return
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO metadata_passes (kind, started_at, ended_at, counts) VALUES ($1, $2, $3, $4)
		ON CONFLICT (kind) DO UPDATE SET
			started_at = EXCLUDED.started_at, ended_at = EXCLUDED.ended_at, counts = EXCLUDED.counts
	`, kind, finished.Started, finished.Ended, finished.Counts)
	if err != nil && ctx.Err() == nil {
		slog.Error("[linking] save pass", "kind", kind, "err", err)
	}
}

// loadPass decodes a saved pass's counts into p.
func loadPass[T any](p *Pass[T], counts json.RawMessage, started, ended time.Time) error {
	if err := json.Unmarshal(counts, &p.Counts); err != nil {
		return err
	}
	p.Started, p.Ended = &started, &ended
	return nil
}

// loadPasses loads each kind's last finished pass, so it survives a restart.
func (s *Service) loadPasses(ctx context.Context) error {
	type row struct {
		Kind      string          `db:"kind"`
		StartedAt time.Time       `db:"started_at"`
		EndedAt   time.Time       `db:"ended_at"`
		Counts    json.RawMessage `db:"counts"`
	}
	rows, err := db.Select[row](ctx, s.pool, "SELECT kind, started_at, ended_at, counts FROM metadata_passes")
	if err != nil {
		return err
	}
	var errs []error
	s.update(func(st *WorkerStatus) {
		for _, r := range rows {
			switch r.Kind {
			case "match":
				errs = append(errs, loadPass(&st.MatchPass, r.Counts, r.StartedAt, r.EndedAt))
			case "refresh":
				errs = append(errs, loadPass(&st.RefreshPass, r.Counts, r.StartedAt, r.EndedAt))
			}
		}
	})
	return errors.Join(errs...)
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
	if err := s.loadPasses(ctx); err != nil {
		slog.Error("[linking] load passes", "err", err)
	}
	for ctx.Err() == nil {
		wait, err := s.step(ctx, paused != nil && paused())
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Error("[linking] background work failed", "err", err)
			s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID, st.Retrying = Idle, "", true })
			wait = time.Minute
		case err == nil:
			s.update(func(st *WorkerStatus) { st.Retrying = false })
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
	var matchErr error
	matchStart := time.Now()
	if !paused {
		if libs, matchErr = s.matchLibraries(ctx); matchErr == nil {
			matched, matchErr = s.matchNext(ctx, c, libs)
		}
	}
	if ctx.Err() == nil {
		advancePass(ctx, s, "match", func(st *WorkerStatus) *Pass[MatchResult] { return &st.MatchPass },
			matched, matchStart, time.Now())
	}

	// A match failing again and again must not hold refreshes back.
	var refreshed RefreshResult
	var refreshErr error
	refreshStart := time.Now()
	if ctx.Err() == nil {
		refreshed, refreshErr = s.refreshBatch(ctx, c)
	}
	if ctx.Err() == nil {
		advancePass(ctx, s, "refresh", func(st *WorkerStatus) *Pass[RefreshResult] { return &st.RefreshPass },
			refreshed, refreshStart, time.Now())
	}
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
// derives rows at the current version, so once a batch finds none, none are left. Rows inserted
// without a derivation have version 0 and are derived once.
// No index serves data_version, so after a version bump each batch is a sequential scan.
func (s *Service) recomputeStale(ctx context.Context, c changes) (bool, error) {
	if s.recomputed {
		return false, nil
	}
	if s.Status().Stale == 0 {
		n, err := db.SelectScalar[int](ctx, s.pool,
			"SELECT count(*) FROM content WHERE data_version < $1", metadata.DataVersion)
		if err != nil {
			return false, err
		}
		s.update(func(st *WorkerStatus) { st.Stale = n })
	}
	type key struct {
		ID        string `db:"id"`
		LibraryID string `db:"library_id"`
	}
	keys, err := db.Select[key](ctx, s.pool,
		"SELECT id, library_id FROM content WHERE data_version < $1 LIMIT 500", metadata.DataVersion)
	if err != nil || len(keys) == 0 {
		s.recomputed = err == nil
		return false, err
	}
	s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Recomputing, "" })
	byLib := map[string][]string{}
	for _, k := range keys {
		byLib[k.LibraryID] = append(byLib[k.LibraryID], k.ID)
	}
	for _, lib := range slices.Sorted(maps.Keys(byLib)) {
		err := s.commit(ctx, c, func(o *op) error {
			for _, id := range byLib[lib] {
				o.touch(lib, id)
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
