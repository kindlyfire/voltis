package linking

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/providertest"
)

// refresh refreshes due entries, all with force, batch by batch as the worker does.
func (e *env) refresh(force bool) RefreshResult {
	e.t.Helper()
	ctx := context.Background()
	if force {
		if err := e.svc.RefreshNow(ctx); err != nil {
			e.t.Fatal(err)
		}
	}
	var out RefreshResult
	for {
		c := changes{}
		res, err := e.svc.refreshBatch(ctx, c)
		e.svc.notifyAll(c)
		if err != nil {
			e.t.Fatal(err)
		}
		if res == (RefreshResult{}) {
			return out
		}
		out = out.plus(res)
	}
}

func (e *env) collect() {
	e.t.Helper()
	if err := e.svc.collect(context.Background()); err != nil {
		e.t.Fatal(err)
	}
}

type entryState struct {
	Deleted   bool      `db:"deleted"`
	RefreshAt time.Time `db:"refresh_at"`
	Attempts  int       `db:"attempts"`
	LastError *string   `db:"last_error"`
}

func (e *env) entryState(id string) entryState {
	e.t.Helper()
	s, err := db.SelectOne[entryState](context.Background(), e.pool,
		"SELECT deleted, refresh_at, attempts, last_error FROM provider_entries WHERE external_id = $1", id)
	if err != nil {
		e.t.Fatal(err)
	}
	return s
}

func (e *env) entryIDs() []string {
	e.t.Helper()
	ids, err := db.SelectScalars[string](context.Background(), e.pool,
		"SELECT external_id FROM provider_entries ORDER BY external_id")
	if err != nil {
		e.t.Fatal(err)
	}
	return ids
}

// within reports whether t is d ± 10% after the window [from, to].
func within(t, from, to time.Time, d time.Duration) bool {
	return !t.Before(from.Add(d*9/10)) && !t.After(to.Add(d*11/10))
}

func TestRefreshAtCadence(t *testing.T) {
	now := time.Now()
	week, twoMonths := 7*24*time.Hour, 60*24*time.Hour
	for _, c := range []struct {
		name  string
		rec   providers.Record
		every time.Duration
	}{
		{"releasing", providers.Record{Fields: metadata.Fields{Status: metadata.Val(metadata.Releasing)}}, week},
		{"completed", providers.Record{Fields: metadata.Fields{Status: metadata.Val(metadata.Completed)}}, twoMonths},
		{"no status", providers.Record{}, twoMonths},
		{"deleted", providers.Record{Fields: metadata.Fields{Status: metadata.Val(metadata.Releasing)}, Deleted: true}, twoMonths},
	} {
		for range 100 {
			if at := refreshAt(now, c.rec); !within(at, now, now, c.every) {
				t.Fatalf("%s: refresh after %v", c.name, at.Sub(now))
			}
		}
	}
}

func TestRefreshDue(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c", "d"} {
		e.series("l1", id, "Local")
	}
	e.fake.Put("4", providertest.Series("Remote Four", metadata.Manga))
	e.fake.Put("5", providertest.Series("Remote Five", metadata.Manga))
	for id, entry := range map[string]string{"a": "1", "b": "2", "c": "4", "d": "5"} {
		if err := e.svc.Link(ctx, id, "fake", entry, nil); err != nil {
			t.Fatal(err)
		}
	}
	e.exec("DELETE FROM content WHERE id = 'd'") // its link is orphaned
	e.exec("UPDATE provider_entries SET refresh_at = now() WHERE external_id <> '4'")
	e.fake.Put("1", providertest.Payload{MergedInto: "3"})
	e.fake.Put("3", providertest.Payload{MergedInto: "6"})
	e.fake.Put("6", providertest.Series("Remote Six", metadata.Manga))
	e.fake.Put("2", providertest.Payload{Deleted: true})
	before := len(e.fake.Fetches())

	if res := e.refresh(false); res != (RefreshResult{Refreshed: 2}) {
		t.Fatalf("result = %+v", res)
	}
	if got := slices.Concat(e.fake.Fetches()[before:]...); !slices.Equal(got, []string{"1", "2", "3", "6"}) {
		t.Fatalf("fetched %v; not due or orphaned entries are skipped", got)
	}
	if l := e.view("a").Links[0]; l.Entry.Key.ID != "6" || l.Entry.Title != "Remote Six" {
		t.Fatalf("merged link = %+v", l)
	}
	deleted := e.view("b")
	if l := deleted.Links[0]; !l.Deleted || l.Entry.Title != "Remote Two" || deleted.Merged.Title.V != "Remote Two" {
		t.Fatalf("deleted link = %+v, merged %+v", l, deleted.Merged)
	}
	if s := e.entryState("2"); !s.Deleted || s.RefreshAt.Before(time.Now().Add(50*24*time.Hour)) {
		t.Fatalf("deleted entry = %+v", s)
	}
	// The tombstones of both hops are young; the orphaned link keeps its entry.
	if got := e.entryIDs(); !slices.Equal(got, []string{"1", "2", "3", "4", "5", "6"}) {
		t.Fatalf("entries = %v", got)
	}

	before = len(e.fake.Fetches())
	e.refresh(true)
	if got := slices.Sorted(slices.Values(slices.Concat(e.fake.Fetches()[before:]...))); !slices.Equal(got, []string{"2", "4", "6"}) {
		t.Fatalf("forced fetch = %v; tombstones and orphans are skipped", got)
	}
}

// A refresh whose snapshots come back unchanged rewrites no metadata or link and notifies no
// library; a changed one notifies its library once.
func TestUnchangedRefreshIsSilent(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for _, s := range []struct{ lib, id, entry string }{{"l1", "s", "1"}, {"l1", "t", "1"}, {"l1", "u", "2"}, {"l2", "v", "2"}} {
		e.series(s.lib, s.id, "Local")
		if err := e.svc.Link(ctx, s.id, "fake", s.entry, nil); err != nil {
			t.Fatal(err)
		}
	}
	versions := func() string {
		t.Helper()
		v, err := db.SelectScalar[string](ctx, e.pool, `SELECT
			(SELECT string_agg(xmin::text, ',' ORDER BY uri) FROM content_metadata) || ';' ||
			(SELECT string_agg(xmin::text, ',' ORDER BY uri) FROM metadata_links)`)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	before, fetched := versions(), e.entryState("1").RefreshAt
	e.notified = nil
	if res := e.refresh(true); res != (RefreshResult{Refreshed: 2}) || e.entryState("1").RefreshAt.Equal(fetched) {
		t.Fatalf("result = %+v", res)
	}
	if versions() != before || len(e.notified) != 0 {
		t.Fatalf("an unchanged refresh wrote rows or notified %v", e.notified)
	}

	e.fake.Put("1", providertest.Series("Renamed", metadata.Manga))
	e.refresh(true)
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("notified = %v, want l1 once", e.notified)
	}
}

// A refresh that changes what readers see of an entry notifies once, even when data stays the same,
// as for a deletion or a merge of a candidate; seeing the same again does not.
func TestRefreshNotifiesEntryChanges(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	e.series("l2", "t", "Local")
	for id, entry := range map[string]string{"s": "1", "t": "2"} {
		if err := e.svc.Link(context.Background(), id, "fake", entry, nil); err != nil {
			t.Fatal(err)
		}
	}
	e.series("l3", "u", "Local")
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, candidates)
		VALUES ('l3', 'comic/u', 'fake', 'review', '[{"key": {"provider": "fake", "id": "2"}}]')`)
	e.fake.Put("1", providertest.Payload{Deleted: true})
	e.fake.Put("2", providertest.Payload{MergedInto: "4"})
	e.fake.Put("4", providertest.Series("Remote Two", metadata.Manga))
	for _, want := range [][]string{{"l1", "l2", "l3"}, nil} {
		e.notified = nil
		e.refresh(true)
		if !slices.Equal(slices.Sorted(slices.Values(e.notified)), want) {
			t.Fatalf("notified = %v, want %v", e.notified, want)
		}
	}
	if v := e.view("s"); !v.Links[0].Deleted || v.Merged.Title.V != "Remote One" {
		t.Fatalf("s = %+v", v.Links[0])
	}
}

func TestRefreshBacksOff(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}

	e.fake.Fail(errors.New("down"))
	for attempt, wait := range []time.Duration{time.Hour, 2 * time.Hour} {
		from := time.Now()
		e.notified = nil
		if res := e.refresh(true); res.Failed != 1 {
			t.Fatalf("result = %+v", res)
		}
		// Its health shows in the series' view and the summary.
		if !slices.Equal(e.notified, []string{"l1"}) {
			t.Fatalf("attempt %d: notified = %v", attempt+1, e.notified)
		}
		s := e.entryState("1")
		if s.Attempts != attempt+1 || s.LastError == nil || *s.LastError != "down" ||
			s.RefreshAt.Before(from.Add(wait)) || s.RefreshAt.After(time.Now().Add(wait)) {
			t.Fatalf("attempt %d: entry = %+v", attempt+1, s)
		}
	}
	if l := e.view("s").Links[0]; l.Entry.Title != "Remote One" {
		t.Fatalf("snapshot lost: %+v", l)
	}

	e.fake.Fail(nil)
	e.fake.Put("1", providertest.Payload{}) // no longer served as is: decoding fails
	e.refresh(true)
	if s := e.entryState("1"); s.Attempts != 3 || *s.LastError != "fake: no title" {
		t.Fatalf("undecodable entry = %+v", s)
	}

	e.fake.Put("1", providertest.Series("Back", metadata.Manga))
	e.refresh(true)
	if s := e.entryState("1"); s.Attempts != 0 || s.LastError != nil {
		t.Fatalf("recovered entry = %+v", s)
	}

	// Recovering through a merge clears the failure: tombstones never count as failing.
	e.fake.Fail(errors.New("down"))
	e.refresh(true)
	e.fake.Fail(nil)
	merged := providertest.Series("Back", metadata.Manga)
	merged.MergedInto = "2"
	e.fake.Put("1", merged)
	e.notified = nil
	e.refresh(true)
	if s := e.entryState("1"); s.Attempts != 0 || s.LastError != nil {
		t.Fatalf("merged entry = %+v", s)
	}
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("recovery notified = %v", e.notified)
	}
	e.exec("UPDATE provider_entries SET attempts = 3 WHERE external_id = '1'")
	if summary, err := e.svc.Summary(context.Background()); err != nil || summary.Providers[0].Failing != 0 {
		t.Fatalf("providers = %+v (%v)", summary.Providers, err)
	}
}

func TestRefreshMissingEntryBacksOff(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.fake.Put("1", providertest.Payload{MergedInto: "99"})
	e.refresh(true)
	if s := e.entryState("1"); s.Attempts != 1 || *s.LastError != "not served upstream" {
		t.Fatalf("entry = %+v", s)
	}
	if l := e.view("s").Links[0]; *l.ExternalID != "1" {
		t.Fatalf("link = %+v", l)
	}
}

func TestCollectUnusedEntries(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	e.exec(`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, merged_into, fetched_at, refresh_at) VALUES
		('fake', 'unused', 'unused', '{}', NULL, now(), now() + interval '1 day'),
		('fake', 'young', 'target', '{}', 'target', now() - interval '29 days', now()),
		('fake', 'target', 'target', '{}', NULL, now(), now() + interval '1 day'),
		('fake', 'old', 'unused', '{}', 'unused', now() - interval '31 days', now()),
		('fake', 'rejected', 'unused', '{}', 'unused', now() - interval '31 days', now()),
		('fake', 'offered', 'unused', '{}', 'unused', now() - interval '31 days', now())`)
	cands := `[{"key": {"provider": "fake", "id": "offered"}}]`
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, candidates, rejected)
		VALUES ('l1', 'comic/s', 'fake', 'review', $1, '{rejected}')`, cands)

	e.collect()
	// The unused entry stays while tombstones point at it.
	if got := e.entryIDs(); !slices.Equal(got, []string{"offered", "rejected", "target", "unused", "young"}) {
		t.Fatalf("entries = %v", got)
	}
	e.exec("DELETE FROM metadata_links")
	e.collect()
	e.collect()
	if got := e.entryIDs(); !slices.Equal(got, []string{"target", "young"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestRefreshContinuesPastAFailedBatch(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.fake.Put("3", providertest.Series("Three", metadata.Manga))
	for _, id := range []string{"1", "2", "3"} {
		e.series("l1", "s"+id, "Local")
		if err := e.svc.Link(ctx, "s"+id, "fake", id, nil); err != nil {
			t.Fatal(err)
		}
	}
	// Publishing 1 fails: storing a, which it moved to, is refused.
	e.fake.Put("1", providertest.Payload{MergedInto: "a"})
	e.fake.Put("a", providertest.Series("A", metadata.Manga))
	e.exec(`CREATE FUNCTION refuse() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse BEFORE INSERT ON provider_entries FOR EACH ROW
		WHEN (NEW.external_id = 'a') EXECUTE FUNCTION refuse()`)
	e.exec("UPDATE provider_entries SET refresh_at = now()")
	e.fake.Put("3", providertest.Series("Three Again", metadata.Manga))

	if res := e.refresh(false); res != (RefreshResult{Refreshed: 2, Failed: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if s := e.entryState("1"); s.Attempts != 1 || s.LastError == nil || s.RefreshAt.Before(time.Now().Add(50*time.Minute)) {
		t.Fatalf("entry 1 = %+v", s)
	}
	if s := e.entryState("2"); s.Attempts != 0 || s.LastError != nil {
		t.Fatalf("its batch-mate = %+v", s)
	}
	if title := e.view("s3").Merged.Title.V; title != "Three Again" {
		t.Fatalf("the next batch was not published: title %q", title)
	}
}

// Collection waits for a publish of the same provider, which may attach the entry it would delete.
func TestCollectWaitsForAPublish(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	e.publish(found("1", "Old", time.Now().Add(-time.Hour)))

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := e.svc.publish(ctx, newOp(tx), []Fetched{found("1", "New", time.Now())}, "l1"); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- e.svc.collect(ctx)
	}()
	dbtest.WaitForBlockedLock(t, e.pool)
	if _, err := tx.Exec(ctx, `INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin)
		VALUES ('l1', 'comic/s', 'fake', 'linked', '1', 'manual')`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := e.entryIDs(); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("entries = %v", got)
	}
}

// A refresh failure that cannot be recorded stops the batch, keeping the counts of those before it.
func TestRefreshCountsWhatItDidBeforeAnError(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for _, id := range []string{"1", "2"} {
		e.series("l1", "s"+id, "Local")
		if err := e.svc.Link(ctx, "s"+id, "fake", id, nil); err != nil {
			t.Fatal(err)
		}
	}
	e.exec("UPDATE provider_entries SET refresh_at = now()")
	e.exec(`CREATE FUNCTION refuse() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse BEFORE UPDATE ON provider_entries FOR EACH ROW
		WHEN (NEW.external_id = '2') EXECUTE FUNCTION refuse()`)
	if res, err := e.svc.refreshBatch(ctx, changes{}); err == nil || res != (RefreshResult{Refreshed: 1}) {
		t.Fatalf("result = %+v (%v)", res, err)
	}
}
