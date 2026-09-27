package linking

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/metadata"
	"voltis/providers/providertest"
)

func (e *env) step() time.Duration {
	e.t.Helper()
	wait, err := e.svc.step(context.Background(), e.paused)
	if err != nil {
		e.t.Fatal(err)
	}
	return wait
}

// untilIdle steps until the worker waits, and returns how long.
func (e *env) untilIdle() time.Duration {
	e.t.Helper()
	for {
		if wait := e.step(); wait > 0 {
			return wait
		}
	}
}

func (e *env) woken() bool {
	select {
	case <-e.svc.wake:
		return true
	default:
		return false
	}
}

func TestWorkerMatchesLibrariesInTurn(t *testing.T) {
	e := setup(t)
	for i := range 25 {
		e.series("l1", fmt.Sprintf("a%02d", i), "Nothing")
	}
	for i := range 21 {
		e.series("l2", fmt.Sprintf("b%02d", i), "Nothing")
	}
	var libs []string
	for e.step() == 0 {
		libs = append(libs, e.svc.Status().LibraryID)
	}
	if !slices.Equal(libs, []string{"l1", "l2", "l1", "l2"}) {
		t.Fatalf("matched %v", libs)
	}
	e.step() // idle again: the last pass stays
	st := e.svc.Status()
	if st.Activity != Idle || st.Matched != (MatchResult{Unmatched: 46}) || st.MatchPass.Counts != st.Matched ||
		st.MatchPass.Finished == nil || st.RefreshPass != (Pass[RefreshResult]{}) {
		t.Fatalf("status = %+v", st)
	}
}

// Library IDs mix cases, so the database's order is not the bytes'.
func TestWorkerTakesLibrariesInTurnWhateverTheirCase(t *testing.T) {
	e := setup(t)
	for i := range 21 {
		e.series("l_aX", fmt.Sprintf("a%02d", i), "Nothing")
		e.series("l_BY", fmt.Sprintf("b%02d", i), "Nothing")
	}
	var libs []string
	for e.step() == 0 {
		libs = append(libs, e.svc.Status().LibraryID)
	}
	if !slices.Equal(libs, []string{"l_aX", "l_BY", "l_aX", "l_BY"}) {
		t.Fatalf("matched %v", libs)
	}
}

// Match now leaves libraries that do not match automatically alone, also when named.
func TestMatchNowIgnoresLibrariesOff(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote One")
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
		VALUES ('l1', 'comic/s', 'fake', 'unmatched', now() + interval '1 day')`)
	e.series("l1", "t", "Remote Two")
	e.exec(`UPDATE libraries SET settings = '{}'`)
	e.matchNow("l1")
	e.untilIdle()
	if s, u := e.link("s"), e.link("t"); s.State != StateUnmatched || s.RetryAt.Before(time.Now()) || u.State != StateNone {
		t.Fatalf("links = %+v, %+v", s, u)
	}
}

// A series without a title, whose file layer no scan wrote yet, is not matched until one does.
func TestWorkerSkipsUntitledSeries(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "")
	e.exec(`INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ('t', 't', 'comic/t', 'comic_series', 'l1')`)
	e.series("l1", "u", "Remote Two")
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
		VALUES ('l1', 'comic/u', 'fake', 'unmatched', now() + interval '1 day')`)
	e.retitle("u", "")
	if wait := e.untilIdle(); wait < 14*time.Minute || e.svc.Status().Matched != (MatchResult{}) {
		t.Fatalf("waits %v, status %+v", wait, e.svc.Status())
	}
	if res, err := e.svc.MatchLibrary(context.Background(), "l1", func(string) bool { return false }, nil); err != nil ||
		res != (MatchResult{}) {
		t.Fatalf("match library = %+v (%v)", res, err)
	}
	for id, title := range map[string]string{"s": "Remote One", "t": "Nothing", "u": "Remote Two"} {
		e.retitle(id, title)
	}
	e.untilIdle()
	if s, t2, u := e.link("s"), e.link("t"), e.link("u"); s.State != StateLinked || t2.State != StateUnmatched ||
		u.State != StateLinked {
		t.Fatalf("links = %+v, %+v, %+v", s, t2, u)
	}
}

func TestIdleWorkerSleepsUntilTheNextDueTime(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote One")
	e.series("l1", "u", "Nothing")
	e.series("l2", "v", "Nothing")
	e.exec(`UPDATE libraries SET settings = '{}' WHERE id = 'l2'`)
	if wait := e.untilIdle(); wait < 14*time.Minute || wait > 15*time.Minute {
		t.Fatalf("waits %v, want the 15 minutes cap", wait)
	}
	snapshot := func() string {
		s, err := db.SelectScalar[string](context.Background(), e.pool, `SELECT
			(SELECT jsonb_agg(l ORDER BY uri) FROM metadata_links l)::text ||
			(SELECT jsonb_agg(e ORDER BY external_id) FROM provider_entries e)::text`)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	before, notified := snapshot(), len(e.notified)
	e.step()
	if snapshot() != before || len(e.notified) != notified {
		t.Fatal("an idle step wrote or notified")
	}

	e.exec("UPDATE metadata_links SET retry_at = now() + interval '5 minutes' WHERE uri = 'comic/u'")
	if wait := e.step(); wait < 4*time.Minute || wait > 5*time.Minute {
		t.Fatalf("waits %v for a retry due in 5 minutes", wait)
	}
	e.exec("UPDATE provider_entries SET refresh_at = now() + interval '2 minutes'")
	if wait := e.step(); wait < time.Minute || wait > 2*time.Minute {
		t.Fatalf("waits %v for a refresh due in 2 minutes", wait)
	}
	// l2 does not match automatically: its series are not waited for.
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
		VALUES ('l2', 'comic/v', 'fake', 'unmatched', now() - interval '1 minute')`)
	if wait := e.step(); wait < time.Minute {
		t.Fatalf("waits %v", wait)
	}
}

func TestMatchNowAndRefreshNowMakeRowsDue(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "a", "Nothing")
	e.series("l1", "b", "Remote")
	e.series("l1", "c", "Remote")
	e.series("l1", "s", "Remote One")
	e.series("l2", "d", "Nothing")
	e.match()
	e.exec("UPDATE metadata_links SET retry_at = now() + interval '1 day' WHERE uri = 'comic/c'") // its inputs changed
	e.exec(`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
		VALUES ('fake', 'unused', 'unused', '{}', now(), now() + interval '1 day')`)
	due := func(sql string) []string {
		ids, err := db.SelectScalars[string](ctx, e.pool, sql)
		if err != nil {
			t.Fatal(err)
		}
		return ids
	}

	e.matchNow("l1")
	if got := due("SELECT uri FROM metadata_links WHERE retry_at <= now() ORDER BY uri"); !slices.Equal(got, []string{"comic/a", "comic/c"}) ||
		!e.woken() {
		t.Fatalf("due %v", got)
	}
	if err := e.svc.RefreshNow(ctx); err != nil {
		t.Fatal(err)
	}
	if got := due("SELECT external_id FROM provider_entries WHERE refresh_at <= now()"); !slices.Equal(got, []string{"1"}) ||
		!e.woken() {
		t.Fatalf("due %v", got)
	}
}

func TestPausedWorkerOnlyRefreshes(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.series("l1", "t", "Remote Two")
	e.exec("UPDATE provider_entries SET refresh_at = now()")
	e.fake.Put("1", providertest.Series("Refreshed", metadata.Manga))
	e.paused = true

	if wait := e.untilIdle(); wait < 14*time.Minute {
		t.Fatalf("waits %v for a series it may not match", wait)
	}
	if st := e.svc.Status(); !st.Paused || st.Refreshed != (RefreshResult{Refreshed: 1}) || st.MatchPass != (Pass[MatchResult]{}) ||
		e.link("t").State != StateNone || e.view("s").Merged.Title.V != "Refreshed" {
		t.Fatalf("status = %+v", st)
	}
	e.paused = false
	e.untilIdle()
	if st := e.svc.Status(); st.Paused || st.Matched != (MatchResult{Linked: 1}) {
		t.Fatalf("status = %+v", st)
	}
}

func TestWorkerResumesAfterARestart(t *testing.T) {
	e := setup(t)
	for i := range 25 {
		e.series("l1", fmt.Sprintf("s%02d", i), "Nothing")
	}
	e.step()
	e.svc = New(e.pool, e.svc.store, e.svc.reg, e.svc.covers, e.svc.notify)
	e.untilIdle()
	if st := e.svc.Status(); st.Matched != (MatchResult{Unmatched: 5}) {
		t.Fatalf("status = %+v", st)
	}
}

// Rows an older DataVersion derived are recomputed in batches before any other work, keep their
// data until then, and are resumed after a restart.
func TestWorkerRecomputesStaleRowsFirst(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote One")
	e.exec(`INSERT INTO content_metadata (uri, library_id, data_raw, data)
		SELECT format('comic/x%s', i), 'l1', jsonb_build_object('v', 2, 'file', jsonb_build_object('title', i::text)), '{"title": "Old"}'
		FROM generate_series(1000, 1599) i`)
	e.exec("UPDATE content_metadata SET data_version = 0")
	stale := func() []string {
		t.Helper()
		uris, err := db.SelectScalars[string](ctx, e.pool,
			"SELECT uri FROM content_metadata WHERE data_version < $1 OR data->>'title' = 'Old' ORDER BY uri", metadata.DataVersion)
		if err != nil {
			t.Fatal(err)
		}
		return uris
	}

	if wait := e.step(); wait != 0 || e.svc.Status().Activity != Recomputing || e.svc.Status().Stale != 101 {
		t.Fatalf("waits %v, status %+v", wait, e.svc.Status())
	}
	if left := stale(); len(left) != 101 || left[0] != "comic/x1499" || e.link("s").State != StateNone {
		t.Fatalf("stale after a batch: %d from %v; s = %+v", len(left), left[:1], e.link("s"))
	}
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("notified = %v", e.notified)
	}

	e.svc = New(e.pool, e.svc.store, e.svc.reg, e.svc.covers, e.svc.notify)
	e.untilIdle()
	if left := stale(); len(left) != 0 || e.svc.Status().Stale != 0 || e.link("s").State != StateLinked {
		t.Fatalf("stale = %v, status %+v, s = %+v", left, e.svc.Status(), e.link("s"))
	}
}

func TestIdleWorkerCollectsHourly(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	unused := func(id string) {
		e.exec(`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
			VALUES ('fake', $1, $1, '{}', now(), now() + interval '1 day')`, id)
		e.exec("UPDATE provider_entries SET refresh_at = now() WHERE external_id = '1'")
	}
	unused("x")
	e.untilIdle()
	unused("y")
	e.untilIdle()
	if got := e.entryIDs(); !slices.Equal(got, []string{"1", "y"}) {
		t.Fatalf("entries = %v, want one collection in the hour", got)
	}
	e.svc.collectedAt = e.svc.collectedAt.Add(-time.Hour)
	e.exec("UPDATE provider_entries SET refresh_at = now() WHERE external_id = '1'")
	e.untilIdle()
	if got := e.entryIDs(); !slices.Equal(got, []string{"1"}) {
		t.Fatalf("entries = %v", got)
	}
}

func TestRunWakesAndStops(t *testing.T) {
	e := setup(t)
	defer func(d time.Duration) { pushDelay = d }(pushDelay)
	pushDelay = 0
	e.series("l1", "s", "Remote One")
	statuses := make(chan WorkerStatus, 100)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.svc.Run(ctx, nil, func(string) bool { return false }, func(st WorkerStatus) { statuses <- st })
		close(done)
	}()
	idleWith := func(linked int) {
		t.Helper()
		for {
			select {
			case st := <-statuses:
				if st.Activity == Idle && st.Matched.Linked == linked {
					return
				}
			case <-time.After(10 * time.Second):
				t.Fatalf("never idle with %d linked", linked)
			}
		}
	}
	idleWith(1)
	e.series("l1", "t", "Remote Two")
	e.svc.Wake()
	idleWith(2)
	cancel()
	<-done
}

// A series read between two flushes of a scan may lack the members that would contradict a
// match, so a library is matched once its scans are over.
func TestWorkerDefersLibrariesBeingScanned(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote One")
	e.series("l1", "u", "Nothing")
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
		VALUES ('l1', 'comic/u', 'fake', 'unmatched', now() - interval '1 minute')`)
	scanning := true
	e.svc.scanning = func(string) bool { return scanning }
	if wait := e.untilIdle(); wait < 14*time.Minute || e.link("s").State != StateNone {
		t.Fatalf("waits %v; matched during a scan: %+v", wait, e.link("s"))
	}

	// A scan starting during the lookup defers the page's series too.
	scanning = false
	e.fake.OnMatch(func(string) { scanning = true })
	e.step()
	if st := e.svc.Status(); st.Matched != (MatchResult{Skipped: 2}) || e.link("s").State != StateNone {
		t.Fatalf("status = %+v", st)
	}
	scanning = false
	e.fake.OnMatch(nil)
	e.untilIdle()
	if s, u := e.link("s"), e.link("u"); s.State != StateLinked || u.RetryAt.Before(time.Now()) {
		t.Fatalf("after the scan: %+v, %+v", s, u)
	}
}

// A match that keeps failing neither holds refreshes back nor loses the work done before it.
func TestWorkerKeepsGoingPastAMatchError(t *testing.T) {
	e := setup(t)
	e.series("l1", "a", "Nothing")
	e.series("l1", "bad", "Nothing")
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.exec("UPDATE provider_entries SET refresh_at = now()")
	e.exec(`CREATE FUNCTION refuse() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse BEFORE INSERT ON metadata_links FOR EACH ROW
		WHEN (NEW.uri = 'comic/bad') EXECUTE FUNCTION refuse()`)

	if _, err := e.svc.step(context.Background(), false); err == nil {
		t.Fatal("no error")
	}
	if st := e.svc.Status(); st.Matched != (MatchResult{Unmatched: 1}) || st.Refreshed != (RefreshResult{Refreshed: 1}) {
		t.Fatalf("status = %+v", st)
	}
}

// A series that fails every time does not hold back those after it.
func TestWorkerMovesPastAFailingSeries(t *testing.T) {
	e := setup(t)
	for _, id := range []string{"a", "b", "c"} {
		e.series("l1", id, "Nothing")
	}
	e.exec(`CREATE FUNCTION refuse() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse BEFORE INSERT ON metadata_links FOR EACH ROW
		WHEN (NEW.uri = 'comic/a') EXECUTE FUNCTION refuse()`)
	if _, err := e.svc.step(context.Background(), false); err == nil {
		t.Fatal("no error")
	}
	e.step()
	if b, c := e.link("b"), e.link("c"); b.State != StateUnmatched || c.State != StateUnmatched {
		t.Fatalf("links = %+v, %+v", b, c)
	}
}
