package linking

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"
)

// MatchResult counts the matcher's outcomes.
type MatchResult struct {
	Linked    int `json:"linked"`
	Review    int `json:"review"`
	Unmatched int `json:"unmatched"`
	Failed    int `json:"failed"`  // the provider failed; tried again with backoff
	Skipped   int `json:"skipped"` // changed during the lookup
}

func (r MatchResult) plus(o MatchResult) MatchResult {
	return MatchResult{r.Linked + o.Linked, r.Review + o.Review, r.Unmatched + o.Unmatched, r.Failed + o.Failed,
		r.Skipped + o.Skipped}
}

func (r *MatchResult) add(a attempt) {
	switch {
	case a.err != nil:
		r.Failed++
	case a.decision.Link != nil:
		r.Linked++
	case len(a.decision.Candidates) > 0:
		r.Review++
	default:
		r.Unmatched++
	}
}

// attempt is the matcher's work on one series, done before anything is written.
type attempt struct {
	target   metadata.Target
	query    MatchQuery
	decision MatchDecision
	entry    Fetched   // the entry to link
	stubs    []Fetched // rejected entries fetched for the first time, published with the outcome
	err      error     // the provider failed
}

// attempt matches a series with the provider and decides, without locks. Rejected entries are
// left out, also when they were merged into another or another into them.
func (s *Service) attempt(ctx context.Context, p providers.Provider, t metadata.Target, l Link) (attempt, error) {
	a := attempt{target: t}
	var err error
	if a.query, err = buildQuery(ctx, s.pool, t); err != nil {
		return a, err
	}
	// A rejected entry never fetched may have been merged upstream since: its targets are rejected
	// too. One that does not decode, or whose merges do not settle, still rejects the hops it took;
	// the provider failing to answer fails the attempt, rather than miss a target.
	if len(l.Rejected) > 0 {
		stubs, err := db.SelectScalars[string](ctx, s.pool, `SELECT external_id FROM provider_entries
			WHERE provider = $1 AND external_id = ANY($2) AND fetched_at = '-infinity'`, p.Name(), l.Rejected)
		if err != nil {
			return a, err
		}
		a.stubs = fetch(ctx, p, stubs)
	}
	for _, f := range a.stubs {
		if errors.As(f.Err, new(*ProviderError)) {
			a.stubs, a.err = nil, f.Err
			return a, nil
		}
	}
	found, err := retrieve(ctx, p, p.Match, a.query, true)
	if errors.As(err, new(*ProviderError)) {
		a.err = err
		return a, nil
	}
	if err != nil {
		return a, err
	}

	// The stubs' hops, and where an unsettled chain stopped, lead on through stored merges too.
	ids := slices.Clone(l.Rejected)
	for _, f := range a.stubs {
		ids = append(ids, f.Hops...)
		if f.Record.MergedInto != "" {
			ids = append(ids, f.Record.MergedInto)
		}
	}
	rejected, err := rejectedIDs(ctx, s.pool, p.Name(), ids)
	if err != nil {
		return a, err
	}
	finals, err := canonical(ctx, s.pool, fp.Map(found, func(f Fetched) metadata.EntryKey { return f.Entry.Key }))
	if err != nil {
		return a, err
	}
	// An entry reached through a rejected one is rejected too, also where the lookup found it directly.
	for _, f := range found {
		if slices.ContainsFunc(f.Hops, func(id string) bool { return rejected[id] }) {
			rejected[finals[f.Entry.Key].ID] = true
		}
	}
	entries := map[metadata.EntryKey]Fetched{}
	var cands []Candidate
	for _, f := range found {
		key := finals[f.Entry.Key]
		if _, dup := entries[key]; dup || rejected[key.ID] {
			continue
		}
		entries[key] = f
		cands = append(cands, Candidate{summarize(key, f.Record), evaluate(a.query, f.Record.Fields)})
	}
	a.decision = decide(cands)
	if a.decision.Link != nil {
		a.entry = entries[*a.decision.Link]
	}
	return a, nil
}

// rejectedIDs holds rejected ids and every entry their recorded merges pass through.
func rejectedIDs(ctx context.Context, q db.Querier, provider string, rejected []string) (map[string]bool, error) {
	ids, err := db.SelectScalars[string](ctx, q, `
		WITH RECURSIVE chain (id) AS (
			SELECT unnest($2::text[])
			UNION SELECT e.merged_into FROM chain
			JOIN provider_entries e ON e.provider = $1 AND e.external_id = chain.id AND e.merged_into IS NOT NULL
		)
		SELECT id FROM chain
	`, provider, rejected)
	out := map[string]bool{}
	for _, id := range ids {
		out[id] = true
	}
	return out, err
}

// lookup finds entries by title: the provider's Match for the matcher, its Search for an admin.
type lookup func(ctx context.Context, contentType, title string, limit int) ([]providers.Entry, error)

// retrieve looks the first two titles up, follows merged results to the entries they were merged
// into, and keeps those of a kind the series can take. A merged result that cannot be followed
// fails it when strict, else it is left out.
func retrieve(ctx context.Context, p providers.Provider, find lookup, q MatchQuery, strict bool) ([]Fetched, error) {
	observed := time.Now()
	var found []Fetched
	var merged []string
	for _, title := range q.Titles[:min(len(q.Titles), 2)] {
		entries, err := find(ctx, q.ContentType, title, 20)
		if err != nil {
			return nil, &ProviderError{err}
		}
		for _, e := range entries {
			rec, err := p.Decode(e)
			if err == nil && rec.MergedInto != "" {
				merged = append(merged, e.Key.ID)
				continue
			}
			found = append(found, Fetched{Requested: e.Key, Hops: []string{e.Key.ID}, ObservedAt: observed, Entry: e,
				Record: rec, Outcome: outcome(rec, err)})
		}
	}
	for _, f := range fetch(ctx, p, fp.Dedup(merged)) {
		if f.Outcome == Failed && strict {
			return nil, &ProviderError{f.Err}
		}
		found = append(found, f)
	}
	return slices.DeleteFunc(found, func(f Fetched) bool {
		return f.Outcome != Found || !supports(p, q.ContentType, f.Record)
	}), nil
}

// record writes an attempt's outcome unless the link moved past rev or the series' match inputs
// changed, publishing the entry it links first, per the lock order.
func (s *Service) record(ctx context.Context, p providers.Provider, a attempt, rev *int64) func(o *op) error {
	return func(o *op) error {
		d := a.decision
		res := slices.Clone(a.stubs)
		if d.Link != nil {
			res = append(res, a.entry)
		}
		if len(res) > 0 {
			finals, err := s.publish(ctx, o, res, a.target.LibraryID)
			if err != nil {
				return err
			}
			if d.Link != nil {
				d.Link = new(finals[a.entry.Requested])
			}
		}
		t, err := s.store.Lock(ctx, o.tx, a.target.ContentID)
		if err != nil {
			return err
		}
		now := time.Now()
		return s.write(ctx, o, t, p.Name(), Expect{rev, a.query.Fingerprint()}, func(l *Link) error {
			if a.err != nil {
				return l.MatchFailed(a.err, now)
			}
			if d.Link != nil {
				// A candidate turned out to be a rejected entry, merged with it since the lookup.
				rejected, err := rejectedIDs(ctx, o.tx, p.Name(), l.Rejected)
				if err != nil {
					return err
				}
				if rejected[d.Link.ID] {
					return metadata.ErrConflict
				}
			}
			return l.Match(d, now)
		})
	}
}

// Rematch runs the matcher on a series now; it leaves ignored, but never linked.
func (s *Service) Rematch(ctx context.Context, contentID, provider string, expectRev *int64) error {
	t, p, err := s.target(ctx, contentID, provider)
	if err != nil {
		return err
	}
	l, err := readLink(ctx, s.pool, t.LibraryID, t.URI, provider)
	if err != nil {
		return err
	}
	if l.State == StateLinked {
		return ErrLinked
	}
	a, err := s.attempt(providers.Interactive(ctx), p, t, l)
	if err != nil {
		return err
	}
	if a.err != nil {
		return a.err
	}
	return s.run(ctx, s.record(ctx, p, a, expectRev))
}

// pendingSeries selects the titled series of libraries $1 that provider $2 describes (content types
// $3) and neither links nor ignores. A series has no title until a scan writes its file layer, and
// matching it before would record a false "no match"; writing the layer makes it due.
const pendingSeries = `FROM content c
	JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri AND m.data->>'title' <> ''
	LEFT JOIN metadata_links l ON l.library_id = c.library_id AND l.uri = c.uri AND l.provider = $2
	WHERE c.library_id = ANY($1) AND c.type = ANY($3) AND (l.state IS NULL OR l.state IN ('review', 'unmatched'))`

// duePage lists up to twenty of the library's series due for matching with the provider by dueBy,
// after the URI: never matched, or due again.
func (s *Service) duePage(ctx context.Context, p providers.Provider, libraryID, after string, dueBy time.Time) ([]metadata.Target, error) {
	return db.Select[metadata.Target](ctx, s.pool, "SELECT c.id, c.library_id, c.uri, c.type "+pendingSeries+
		" AND (l.state IS NULL OR l.retry_at <= $5) AND c.uri > $4 ORDER BY c.uri LIMIT 20",
		[]string{libraryID}, p.Name(), providers.ContentTypes(p), after, dueBy)
}

// matchSeries runs the matcher on each series, skipping those of libraries scanning, adds the
// libraries it changed to c, and returns how many series it went through, the failing one
// included. A dry run writes nothing and reports each outcome instead.
func (s *Service) matchSeries(ctx context.Context, c changes, p providers.Provider, page []metadata.Target,
	scanning func(libraryID string) bool,
	dryRun func(t metadata.Target, q MatchQuery, d MatchDecision, err error)) (MatchResult, int, error) {
	var out MatchResult
	for i, t := range page {
		l, err := readLink(ctx, s.pool, t.LibraryID, t.URI, p.Name())
		if err != nil {
			return out, i + 1, err
		}
		if l.State == StateLinked || l.State == StateIgnored || scanning(t.LibraryID) {
			out.Skipped++ // decided since it was selected, or a scan changes it
			continue
		}
		a, err := s.attempt(ctx, p, t, l)
		if err != nil {
			return out, i + 1, err
		}
		if dryRun != nil {
			out.add(a)
			dryRun(t, a.query, a.decision, a.err)
			continue
		}
		if scanning(t.LibraryID) {
			out.Skipped++ // a scan started during the lookup
			continue
		}
		err = s.commit(ctx, c, s.record(ctx, p, a, l.revision()))
		if err != nil && !stale(err) && ctx.Err() == nil {
			// Recorded as a failure, so the series backs off rather than fail every time.
			slog.Warn("[linking] failed to record a match", "library", t.LibraryID, "uri", t.URI, "err", err)
			a.decision, a.stubs, a.err = MatchDecision{}, nil, err
			err = s.commit(ctx, c, s.record(ctx, p, a, l.revision()))
		}
		switch {
		case stale(err):
			out.Skipped++
		case err != nil:
			return out, i + 1, err
		default:
			out.add(a)
		}
	}
	return out, len(page), nil
}

// stale reports that a series changed, or went, during the lookup.
func stale(err error) bool {
	return errors.Is(err, metadata.ErrConflict) || errors.Is(err, metadata.ErrNotFound)
}

// MatchLibrary runs the matcher on every pending series of the library, also those backing off,
// whether or not the library matches automatically, skipping them while scanning says the library
// is scanned. A dry run writes nothing and reports each outcome instead.
func (s *Service) MatchLibrary(ctx context.Context, libraryID string, scanning func(libraryID string) bool,
	dryRun func(t metadata.Target, q MatchQuery, d MatchDecision, err error)) (MatchResult, error) {
	var out MatchResult
	c := changes{}
	defer s.notifyAll(c)
	anytime := time.Now().AddDate(1000, 0, 0)
	for _, p := range s.reg.All() {
		for after := ""; ; {
			page, err := s.duePage(ctx, p, libraryID, after, anytime)
			if err != nil {
				return out, err
			}
			if len(page) == 0 {
				break
			}
			res, _, err := s.matchSeries(ctx, c, p, page, scanning, dryRun)
			if out = out.plus(res); err != nil {
				return out, err
			}
			after = page[len(page)-1].URI
		}
	}
	return out, nil
}

// MatchNow makes the unmatched series of libraries that match automatically due for matching, and
// those in review whose inputs changed, then wakes the worker; only the given libraries' if any.
func (s *Service) MatchNow(ctx context.Context, libraryIDs []string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE metadata_links l SET retry_at = now()
		FROM libraries b
		WHERE b.id = l.library_id AND b.settings->'auto_match' = 'true'
		  AND (l.state = 'unmatched' OR (l.state = 'review' AND l.retry_at IS NOT NULL))
		  AND (COALESCE(cardinality($1::text[]), 0) = 0 OR l.library_id = ANY($1))
	`, libraryIDs)
	if err != nil {
		return err
	}
	s.Wake()
	return nil
}
