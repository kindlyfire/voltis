package linking

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"voltis/db"

	"github.com/jackc/pgx/v5"
)

type RefreshResult struct {
	Refreshed int `json:"refreshed"` // found or deleted upstream
	Failed    int `json:"failed"`    // retried with backoff
}

func (r RefreshResult) plus(o RefreshResult) RefreshResult {
	return RefreshResult{r.Refreshed + o.Refreshed, r.Failed + o.Failed}
}

// refersTo is a condition that link l refers to the entry with id: links, rejects, or offers it
// as a candidate. Each branch has an index.
func refersTo(id string) string {
	return `(l.external_id = ` + id + ` OR l.rejected @> ARRAY[` + id + `]
		OR l.candidates @> jsonb_build_array(jsonb_build_object('key', jsonb_build_object('id', ` + id + `))))`
}

// usedEntries selects the entries of provider $1 that series still use, directly or through merges;
// tombstones are never refreshed.
var usedEntries = `FROM provider_entries e
	WHERE e.provider = $1 AND e.merged_into IS NULL
	  AND EXISTS (SELECT 1 FROM provider_entries a
	              WHERE a.provider = e.provider AND a.canonical_id = e.external_id AND EXISTS (
	                  SELECT 1 FROM metadata_links l
	                  WHERE l.provider = a.provider AND ` + refersTo("a.external_id") + `))`

// refreshBatch fetches a batch of each provider's used entries due for a refresh, outside any
// transaction, then publishes it, adding the libraries it changed to c; an entry that fails to
// publish is recorded as failed, so it backs off rather than block the others.
func (s *Service) refreshBatch(ctx context.Context, c changes) (RefreshResult, error) {
	var out RefreshResult
	for _, p := range s.reg.All() {
		ids, err := db.SelectScalars[string](ctx, s.pool, "SELECT e.external_id "+usedEntries+
			" AND e.refresh_at <= now() ORDER BY e.refresh_at LIMIT $2", p.Name(), p.MaxBatch())
		if err != nil {
			return out, err
		}
		if len(ids) == 0 {
			continue
		}
		s.update(func(st *WorkerStatus) { st.Activity, st.LibraryID = Refreshing, "" })
		res := fetch(ctx, p, ids)
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		publish := func(res []Fetched) error {
			return s.commit(ctx, c, func(o *op) error {
				_, err := s.publish(ctx, o, res)
				return err
			})
		}
		if publish(res) != nil {
			if ctx.Err() != nil {
				return out, ctx.Err()
			}
			// Publish each alone, so that only the entries failing it are recorded as failed. Failing
			// to record that stops the worker for a while, rather than fetch them again at once.
			for i := range res {
				one := res[i : i+1]
				if err := publish(one); err != nil {
					one[0].Outcome, one[0].Err = Failed, err
					if err := publish(one); err != nil {
						return out.plus(tally(res[:i])), fmt.Errorf("record a refresh failure of %v: %w", one[0].Requested, err)
					}
				}
			}
		}
		t := tally(res)
		out = out.plus(t)
		if i := slices.IndexFunc(res, func(f Fetched) bool { return f.Outcome != Found && f.Outcome != Deleted }); i >= 0 {
			slog.Warn("[linking] refresh failed", "provider", p.Name(), "count", t.Failed,
				"first", res[i].Requested.ID, "err", res[i].Err)
		}
	}
	return out, nil
}

// tally counts the outcomes of published results.
func tally(res []Fetched) RefreshResult {
	var out RefreshResult
	for _, f := range res {
		if f.Outcome == Found || f.Outcome == Deleted {
			out.Refreshed++
		} else {
			out.Failed++
		}
	}
	return out
}

// RefreshNow makes every entry that series use due for a refresh.
func (s *Service) RefreshNow(ctx context.Context) error {
	err := db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockProviders(ctx, tx); err != nil {
			return err
		}
		for _, p := range s.providerNames() {
			if _, err := tx.Exec(ctx, `UPDATE provider_entries SET refresh_at = now()
				WHERE provider = $1 AND external_id IN (SELECT e.external_id `+usedEntries+")", p); err != nil {
				return err
			}
		}
		return nil
	})
	s.Wake()
	return err
}

// collect deletes the entries nothing refers to. Tombstones stay a while, so a fetch that started
// before the merge cannot recreate the entry; an entry something was merged into stays, keeping
// the chains through it whole. It reads every reference at once, which beats probing per entry.
func (s *Service) collect(ctx context.Context) error {
	return db.WithTx(ctx, s.pool, func(tx pgx.Tx) error {
		if err := s.lockProviders(ctx, tx); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			WITH refs (provider, external_id) AS (
				SELECT provider, external_id FROM metadata_links WHERE external_id IS NOT NULL
				UNION ALL SELECT provider, unnest(rejected) FROM metadata_links
				UNION ALL SELECT l.provider, c->'key'->>'id' FROM metadata_links l, jsonb_array_elements(l.candidates) c)
			DELETE FROM provider_entries e
			WHERE (e.merged_into IS NULL OR e.fetched_at < now() - interval '30 days')
			  AND NOT EXISTS (SELECT 1 FROM refs r WHERE r.provider = e.provider AND r.external_id = e.external_id)
			  AND NOT EXISTS (SELECT 1 FROM provider_entries m WHERE m.provider = e.provider AND m.merged_into = e.external_id)
		`)
		return err
	})
}

// lockProviders takes every provider's lock, in the lock order.
func (s *Service) lockProviders(ctx context.Context, tx pgx.Tx) error {
	for _, p := range slices.Sorted(slices.Values(s.providerNames())) {
		if err := db.LockProvider(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}
