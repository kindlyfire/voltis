package linking

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math/rand/v2"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"

	"github.com/jackc/pgx/v5"
)

type FetchOutcome int

const (
	Found FetchOutcome = iota
	Deleted
	Unavailable
	Failed
)

type Fetched struct {
	Requested  metadata.EntryKey
	Hops       []string  // every id fetched, Requested first and Entry.Key last; publish records each merge
	ObservedAt time.Time // captured before the request
	Entry      providers.Entry
	Record     providers.Record
	Outcome    FetchOutcome
	Err        error
}

const maxHops = 5

var errUnavailable = errors.New("not served upstream")

// fetch reads entries in batches, following merges. It does not check kinds: attaching does,
// while a refresh keeps a link whose upstream type changed. A provider failing to answer is a
// *ProviderError, unlike an entry that does not decode or whose merges do not settle.
func fetch(ctx context.Context, p providers.Provider, ids []string) []Fetched {
	out := make([]Fetched, len(ids))
	seen := make([]map[string]bool, len(ids))
	pending := map[string][]int{}
	observed := time.Now()
	for i, id := range ids {
		key := metadata.EntryKey{Provider: p.Name(), ID: id}
		out[i] = Fetched{Requested: key, ObservedAt: observed}
		seen[i] = map[string]bool{id: true}
		pending[id] = append(pending[id], i)
	}
	for hop := 0; len(pending) > 0; hop++ {
		next := map[string][]int{}
		for chunk := range slices.Chunk(slices.Sorted(maps.Keys(pending)), p.MaxBatch()) {
			entries, err := p.Fetch(ctx, chunk)
			for _, id := range chunk {
				i := slices.IndexFunc(entries, func(e providers.Entry) bool { return e.Key.ID == id })
				for _, n := range pending[id] {
					f := &out[n]
					switch {
					case err != nil:
						f.Outcome, f.Err = Failed, &ProviderError{err}
					case i < 0:
						f.Outcome, f.Err = Unavailable, errUnavailable
					default:
						f.Entry, f.Hops = entries[i], append(f.Hops, id)
						f.Record, f.Err = p.Decode(f.Entry)
						f.Outcome = outcome(f.Record, f.Err)
						if into := f.Record.MergedInto; f.Err == nil && into != "" {
							if seen[n][into] || hop == maxHops {
								f.Outcome, f.Err = Failed, fmt.Errorf("%s: merges from %s do not settle", p.Name(), f.Requested.ID)
								continue
							}
							seen[n][into] = true
							next[into] = append(next[into], n)
						}
					}
				}
			}
		}
		pending = next
	}
	return out
}

func outcome(r providers.Record, err error) FetchOutcome {
	switch {
	case err != nil:
		return Failed
	case r.Deleted:
		return Deleted
	}
	return Found // or merged, until the target is read
}

// op is a transaction that recomputes the rows it touched before committing.
type op struct {
	tx      pgx.Tx
	dirty   map[string]map[string]bool // library -> uris
	changed changes
}

func newOp(tx pgx.Tx) *op {
	return &op{tx: tx, dirty: map[string]map[string]bool{}, changed: changes{}}
}

// touch marks a row for recomputing, which changes its library only if its data changes.
func (o *op) touch(libraryID, uri string) {
	if o.dirty[libraryID] == nil {
		o.dirty[libraryID] = map[string]bool{}
	}
	o.dirty[libraryID][uri] = true
}

// wrote marks a row whose links or layers were written, which changes its library.
func (o *op) wrote(libraryID, uri string) {
	o.touch(libraryID, uri)
	o.changed[libraryID] = true
}

// changes collects the libraries whose metadata commits changed, to notify each once.
type changes map[string]bool

func (s *Service) notifyAll(c changes) {
	for _, lib := range slices.Sorted(maps.Keys(c)) {
		s.notify(lib)
	}
}

// run commits fn and notifies the libraries it changed.
func (s *Service) run(ctx context.Context, fn func(o *op) error) error {
	c := changes{}
	err := s.commit(ctx, c, fn)
	s.notifyAll(c)
	return err
}

// commit runs fn in a transaction that recomputes what it touched, and adds the libraries it
// changed to c.
func (s *Service) commit(ctx context.Context, c changes, fn func(o *op) error) error {
	o := newOp(nil)
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		o.tx = tx
		if err := fn(o); err != nil {
			return err
		}
		for _, lib := range slices.Sorted(maps.Keys(o.dirty)) {
			changed, err := s.store.Recompute(ctx, tx, lib, slices.Collect(maps.Keys(o.dirty[lib])))
			if err != nil {
				return err
			}
			if changed {
				o.changed[lib] = true
			}
		}
		return nil
	})
	if err == nil {
		maps.Copy(c, o.changed)
	}
	return err
}

// write applies a change to one link. It takes no locks: callers have published or called
// Store.Lock, which holds the library's metadata lock that every link writer takes.
func (s *Service) write(ctx context.Context, o *op, t metadata.Target, provider string, expect Expect,
	change func(*Link) error) error {
	l, err := readLink(ctx, o.tx, t.LibraryID, t.URI, provider)
	if err != nil {
		return err
	}
	if !expect.holds(l) {
		return metadata.ErrConflict
	}
	if expect.Fingerprint != "" {
		q, err := buildQuery(ctx, o.tx, t)
		if err != nil {
			return err
		}
		if q.Fingerprint() != expect.Fingerprint {
			return metadata.ErrConflict
		}
	}
	if err := change(&l); err != nil {
		return err
	}
	if err := save(ctx, o.tx, l); err != nil {
		return err
	}
	o.wrote(t.LibraryID, t.URI)
	return nil
}

const (
	entryUpsert = `
		INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
		VALUES ($1, $2, $2, $3, $4, $5)
		ON CONFLICT (provider, external_id) DO UPDATE SET
			raw = EXCLUDED.raw, fetched_at = EXCLUDED.fetched_at, refresh_at = EXCLUDED.refresh_at,
			deleted = false, attempts = 0, last_error = NULL
		WHERE provider_entries.fetched_at < EXCLUDED.fetched_at` + entryVisible
	// A deletion keeps the last snapshot; the tombstone payload is stored only for a new entry or a
	// stub, which has none.
	entryDeleted = `
		INSERT INTO provider_entries (provider, external_id, canonical_id, raw, deleted, fetched_at, refresh_at)
		VALUES ($1, $2, $2, $3, true, $4, $5)
		ON CONFLICT (provider, external_id) DO UPDATE SET
			raw = CASE WHEN provider_entries.fetched_at = '-infinity' THEN EXCLUDED.raw ELSE provider_entries.raw END,
			deleted = true, fetched_at = EXCLUDED.fetched_at, refresh_at = EXCLUDED.refresh_at,
			attempts = 0, last_error = NULL
		WHERE provider_entries.fetched_at < EXCLUDED.fetched_at` + entryVisible
	// entryVisible tells whether readers see the upsert: a new snapshot, deletion state, or health.
	entryVisible = `
		RETURNING old.raw IS DISTINCT FROM new.raw OR old.deleted IS DISTINCT FROM new.deleted
			OR old.attempts IS DISTINCT FROM new.attempts`
	entryMerged = `
		INSERT INTO provider_entries (provider, external_id, canonical_id, raw, merged_into, fetched_at, refresh_at)
		VALUES ($1, $2, $2, '{}', $3, $4, $4)
		ON CONFLICT (provider, external_id) DO UPDATE SET
			merged_into = EXCLUDED.merged_into, fetched_at = EXCLUDED.fetched_at, attempts = 0, last_error = NULL
		WHERE provider_entries.fetched_at < EXCLUDED.fetched_at`
	// A stub stands for an entry never fetched, due for fetching now. It has no snapshot: only linked
	// entries are decoded, and linking fetches the entry, which replaces the stub.
	entryStub = `
		INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
		SELECT $1, id, id, '{}', '-infinity', now() FROM unnest($2::text[]) id
		ON CONFLICT DO NOTHING`
	// A stub the provider does not serve goes: it never existed.
	entryStubGone = `
		DELETE FROM provider_entries WHERE provider = $1 AND external_id = $2 AND fetched_at = '-infinity'`
	// A failed refresh keeps the snapshot and retries after 1h·2ⁿ, at most a week. Readers see its
	// health change.
	entryFailed = `
		UPDATE provider_entries SET attempts = attempts + 1, last_error = $3,
			refresh_at = now() + interval '1 hour' * LEAST(2 ^ attempts, 168)
		WHERE provider = $1 AND external_id = $2 AND fetched_at < $4
		RETURNING true`
)

// aliases selects the entries e resolving as those in k (provider, id) do.
const aliases = `FROM unnest($1::text[], $2::text[]) AS k(provider, id)
	JOIN provider_entries r ON r.provider = k.provider AND r.external_id = k.id
	JOIN provider_entries e ON e.provider = r.provider AND e.canonical_id = r.canonical_id`

// consumers selects the linked series reading the same entries as those in k.
const consumers = aliases + `
	JOIN metadata_links l ON l.provider = e.provider AND l.external_id = e.external_id AND l.state = 'linked'`

// publish stores fetched snapshots, or failures to fetch them, records merges, marks every series
// reading them for recomputing, and marks as changed the libraries whose entries changed
// snapshot, deletion, health or canonical target. It follows the lock order: the providers, then the metadata
// locks of the libraries reading the entries and extraLibs, sorted. It returns the canonical keys
// of the requested and fetched keys of the results found or deleted.
func (s *Service) publish(ctx context.Context, o *op, res []Fetched, extraLibs ...string) (map[metadata.EntryKey]metadata.EntryKey, error) {
	names := fp.Map(res, func(f Fetched) string { return f.Requested.Provider })
	for _, p := range slices.Compact(slices.Sorted(slices.Values(names))) {
		if err := db.LockProvider(ctx, o.tx, p); err != nil {
			return nil, err
		}
	}
	var keys, visible []metadata.EntryKey
	for _, f := range res {
		if f.Outcome == Unavailable {
			if _, err := o.tx.Exec(ctx, entryStubGone, f.Requested.Provider, f.Requested.ID); err != nil {
				return nil, err
			}
		}
		if f.Outcome != Found && f.Outcome != Deleted {
			var failed bool
			err := o.tx.QueryRow(ctx, entryFailed, f.Requested.Provider, f.Requested.ID, f.Err.Error(), f.ObservedAt).Scan(&failed)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
			if failed {
				visible = append(visible, f.Requested)
			}
			continue
		}
		sql := entryUpsert
		if f.Outcome == Deleted {
			sql = entryDeleted
		}
		var seen bool
		err := o.tx.QueryRow(ctx, sql, f.Entry.Key.Provider, f.Entry.Key.ID, f.Entry.Raw, f.ObservedAt,
			refreshAt(f.ObservedAt, f.Record)).Scan(&seen)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		if seen {
			visible = append(visible, f.Entry.Key)
		}
		// From the last hop back, so that each merges into an entry already resolved.
		for i := len(f.Hops) - 2; i >= 0; i-- {
			from := metadata.EntryKey{Provider: f.Entry.Key.Provider, ID: f.Hops[i]}
			into := metadata.EntryKey{Provider: f.Entry.Key.Provider, ID: f.Hops[i+1]}
			merged, err := merge(ctx, o.tx, from, into, f.ObservedAt)
			if err != nil {
				return nil, err
			}
			if merged {
				visible = append(visible, from)
			}
		}
		keys = append(keys, f.Requested, f.Entry.Key)
	}
	finals, err := canonical(ctx, o.tx, keys)
	if err != nil {
		return nil, err
	}
	provs, ids := columns(keys)

	libs, err := db.SelectScalars[string](ctx, o.tx, "SELECT DISTINCT l.library_id "+consumers, provs, ids)
	if err != nil {
		return nil, err
	}
	for _, lib := range slices.Compact(slices.Sorted(slices.Values(append(libs, extraLibs...)))) {
		if err := db.LockMetadata(ctx, o.tx, lib); err != nil {
			return nil, err
		}
	}
	var lib, uri string
	rows, err := o.tx.Query(ctx, "SELECT l.library_id, l.uri "+consumers, provs, ids)
	if err != nil {
		return nil, err
	}
	if _, err = pgx.ForEachRow(rows, []any{&lib, &uri}, func() error {
		o.touch(lib, uri)
		return nil
	}); err != nil {
		return nil, err
	}
	provs, ids = columns(visible)
	// Review rows show candidates and rejections too.
	changed, err := db.SelectScalars[string](ctx, o.tx, "SELECT DISTINCT l.library_id "+aliases+
		" JOIN metadata_links l ON l.provider = e.provider AND "+refersTo("e.external_id"), provs, ids)
	if err != nil {
		return nil, err
	}
	for _, lib := range changed {
		o.changed[lib] = true
	}
	return finals, nil
}

// merge records that upstream merged from into into, unless an older observation said so, or into
// leads back to from, which would make a cycle: the older merge then stands until the entries are
// fetched again. Every entry whose chain passes through from then resolves as into does. It
// reports whether that moved any entry's canonical id.
func merge(ctx context.Context, tx pgx.Tx, from, into metadata.EntryKey, observed time.Time) (bool, error) {
	via, err := db.SelectScalars[string](ctx, tx, `
		WITH RECURSIVE via (id) AS (
			SELECT $2::text
			UNION SELECT e.external_id FROM via JOIN provider_entries e ON e.provider = $1 AND e.merged_into = via.id
		)
		SELECT id FROM via
	`, from.Provider, from.ID)
	if err != nil {
		return false, err
	}
	if slices.Contains(via, into.ID) {
		slog.Warn("[linking] ignoring a merge that reverses an earlier one", "from", from, "into", into)
		return false, nil
	}
	tag, err := tx.Exec(ctx, entryMerged, from.Provider, from.ID, into.ID, observed)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	tag, err = tx.Exec(ctx, `
		UPDATE provider_entries SET canonical_id = t.canonical_id
		FROM (SELECT canonical_id FROM provider_entries WHERE provider = $1 AND external_id = $2) t
		WHERE provider = $1 AND external_id = ANY($3) AND provider_entries.canonical_id <> t.canonical_id
	`, from.Provider, into.ID, via)
	return tag.RowsAffected() > 0, err
}

// canonical maps keys to the entries their recorded merges lead to; a key with no entry maps to
// itself.
func canonical(ctx context.Context, q db.Querier, keys []metadata.EntryKey) (map[metadata.EntryKey]metadata.EntryKey, error) {
	out := make(map[metadata.EntryKey]metadata.EntryKey, len(keys))
	for _, k := range keys {
		out[k] = k
	}
	provs, ids := columns(keys)
	rows, err := q.Query(ctx, `
		SELECT e.provider, e.external_id, e.canonical_id FROM unnest($1::text[], $2::text[]) AS k(provider, id)
		JOIN provider_entries e ON e.provider = k.provider AND e.external_id = k.id
	`, provs, ids)
	if err != nil {
		return nil, err
	}
	var k metadata.EntryKey
	var id string
	_, err = pgx.ForEachRow(rows, []any{&k.Provider, &k.ID, &id}, func() error {
		out[k] = metadata.EntryKey{Provider: k.Provider, ID: id}
		return nil
	})
	return out, err
}

// columns splits keys into the arrays that queries unnest.
func columns(keys []metadata.EntryKey) (provs, ids []string) {
	return fp.Map(keys, func(k metadata.EntryKey) string { return k.Provider }),
		fp.Map(keys, func(k metadata.EntryKey) string { return k.ID })
}

// refreshAt spreads refreshes: weekly while a series may still change, else every two months.
func refreshAt(observed time.Time, r providers.Record) time.Time {
	every := 60 * 24 * time.Hour
	if s, ok := r.Fields.Status.Get(); ok && s.Active() && !r.Deleted {
		every = 7 * 24 * time.Hour
	}
	return observed.Add(time.Duration(float64(every) * (0.9 + 0.2*rand.Float64())))
}
