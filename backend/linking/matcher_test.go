package linking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers/providertest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// match matches pages of due series, as the worker does, until none is left.
func (e *env) match() MatchResult {
	e.t.Helper()
	ctx := context.Background()
	libs, err := e.svc.matchLibraries(ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	var out MatchResult
	for {
		c := changes{}
		res, err := e.svc.matchNext(ctx, c, libs)
		e.svc.notifyAll(c)
		if err != nil {
			e.t.Fatal(err)
		}
		if res == (MatchResult{}) {
			return out
		}
		out = out.plus(res)
	}
}

func (e *env) matchNow(libs ...string) {
	e.t.Helper()
	if err := e.svc.MatchNow(context.Background(), libs, nil); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) retitle(id, title string) {
	e.t.Helper()
	e.tx(func(tx pgx.Tx) error {
		return e.svc.store.WriteFileLayers(context.Background(), tx, "l1",
			[]metadata.FileLayer{{URI: "comic/" + id, Fields: metadata.Fields{Title: metadata.Val(title)}}}, time.Now())
	})
}

func candidateIDs(cs []Candidate) []string {
	return fp.Map(cs, func(c Candidate) string { return c.Key.ID })
}

func TestMatchBackfill(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for id, title := range map[string]string{"a": "Remote One", "b": "Remote", "c": "Nothing", "d": "Remote Two", "e": "Remote One"} {
		e.series("l1", id, title)
	}
	if err := e.svc.Link(ctx, "d", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ignore(ctx, "e", "fake", nil); err != nil {
		t.Fatal(err)
	}
	e.series("l2", "f", "Remote One")
	e.exec(`UPDATE libraries SET settings = '{"auto_match": {"fake": false}}' WHERE id = 'l2'`)
	e.notified = nil

	from := time.Now()
	if res := e.match(); res != (MatchResult{Linked: 1, Review: 1, Unmatched: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if l := e.link("a"); l.State != StateLinked || *l.ExternalID != "1" || *l.Origin != OriginAuto {
		t.Fatalf("a = %+v", l)
	}
	if l := e.link("b"); l.State != StateReview || !slices.Equal(candidateIDs(l.Candidates), []string{"1", "2"}) || l.RetryAt != nil {
		t.Fatalf("b = %+v", l)
	}
	if l := e.link("c"); l.State != StateUnmatched || !within(*l.RetryAt, from, time.Now(), 30*24*time.Hour) {
		t.Fatalf("c = %+v", l)
	}
	// Admin decisions stay.
	if l := e.link("d"); *l.ExternalID != "1" || *l.Origin != OriginManual || l.Rev != 1 {
		t.Fatalf("d = %+v", l)
	}
	if l := e.link("e"); l.State != StateIgnored || l.Rev != 1 {
		t.Fatalf("e = %+v", l)
	}
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("notified = %v, want one notification for the page", e.notified)
	}
	page, err := e.svc.Review(ctx, ReviewQuery{Tab: "auto", Limit: 10})
	if err != nil || page.Total != 1 || page.Items[0].Content.ID != "a" || page.Items[0].Link.Entry.Title != "Remote One" {
		t.Fatalf("auto-linked = %+v (%v)", page, err)
	}

	// Nothing is due: review waits for an admin, and l2 does not match automatically.
	if res := e.match(); res != (MatchResult{}) {
		t.Fatalf("second run = %+v", res)
	}
	// Match now makes the unmatched series due again, not the review, and leaves l2 alone.
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("after match now = %+v", res)
	}
	if l, err := readLink(ctx, e.pool, "l2", "comic/f", "fake"); err != nil || l.State != StateNone {
		t.Fatalf("f = %+v (%v)", l, err)
	}

	// Changed inputs make a review due again.
	e.retitle("b", "Remote Two")
	if res := e.match(); res != (MatchResult{Linked: 1}) {
		t.Fatalf("after the change = %+v", res)
	}
	if l := e.link("b"); *l.ExternalID != "2" {
		t.Fatalf("b = %+v", l)
	}
}

// The matcher only matches: a title that search would find but match does not leaves the series
// unmatched, and search is never called.
func TestMatchNeverSearches(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote On")
	from := time.Now()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if err := e.svc.Rematch(context.Background(), "s", "fake", new(e.link("s").Rev)); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateUnmatched || !within(*l.RetryAt, from, time.Now(), 30*24*time.Hour) {
		t.Fatalf("s = %+v", l)
	}
	if s := e.fake.Searches(); len(s) > 0 {
		t.Fatalf("searched %v", s)
	}
}

func TestMatchFailureBacksOff(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote One")
	e.fake.Fail(errors.New("down"))
	from := time.Now()
	if res := e.match(); res != (MatchResult{Failed: 1}) {
		t.Fatalf("result = %+v", res)
	}
	l := e.link("s")
	if l.State != StateUnmatched || l.Attempts != 1 || *l.LastError != "down" || !within(*l.RetryAt, from, time.Now(), time.Hour) {
		t.Fatalf("link = %+v", l)
	}
	if res := e.match(); res != (MatchResult{}) {
		t.Fatalf("not due, yet = %+v", res)
	}
	summary, err := e.svc.Summary(context.Background())
	if err != nil || !slices.Equal(summary.Libraries, []ReviewSummary{{LibraryID: "l1", Unmatched: 1, Failed: 1}}) {
		t.Fatalf("summary = %+v (%v)", summary, err)
	}

	e.fake.Fail(nil)
	e.matchNow("l1")
	if res := e.match(); res != (MatchResult{Linked: 1}) {
		t.Fatalf("after match now = %+v", res)
	}
}

func TestMatchNeverRelinksRejected(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote One")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Rematch(ctx, "s", "fake", new(int64(1))); !errors.Is(err, ErrLinked) {
		t.Fatalf("rematch of a linked series: err = %v", err)
	}
	if err := e.svc.Ignore(ctx, "s", "fake", new(int64(1))); err != nil {
		t.Fatal(err)
	}
	// Each result is the rejected entry: merged into it upstream, or here.
	again := providertest.Series("Remote One Again", metadata.Manga)
	again.Fields.AltTitles = metadata.Val([]string{"Remote One"})
	again.MergedInto = "1"
	e.fake.Put("5", again)
	e.fake.Put("7", providertest.Series("Remote One", metadata.Manga))
	e.exec(`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, merged_into, fetched_at, refresh_at)
		VALUES ('fake', '7', '1', '{}', '1', now(), now())`)

	if err := e.svc.Rematch(ctx, "s", "fake", new(int64(1))); !errors.Is(err, metadata.ErrConflict) {
		t.Fatalf("stale rematch: err = %v", err)
	}
	if err := e.svc.Rematch(ctx, "s", "fake", new(int64(2))); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateUnmatched || len(l.Candidates) != 0 || !slices.Equal(l.Rejected, []string{"1"}) {
		t.Fatalf("link = %+v", l)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("after match now = %+v", res)
	}

	// The rejected entry merges into another, which stays rejected.
	e.fake.Put("8", providertest.Series("Remote One", metadata.Manga))
	e.publish(merged("1", "8", time.Now()))
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("run after the merge = %+v", res)
	}
}

// An entry merged into a rejected one after the search filtered it is not linked: a refresh
// records the merge, and the match waits for it to record its own outcome.
func TestMatchRefusesAnEntryMergedIntoARejectedOne(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote One")
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, rejected, retry_at)
		VALUES ('l1', 'comic/s', 'fake', 'unmatched', '{1}', now())`)
	e.fake.Put("5", providertest.Series("Remote One", metadata.Manga))

	refresh, err := e.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = refresh.Rollback(ctx) }()
	var once sync.Once
	e.fake.OnMatch(func(string) {
		once.Do(func() {
			if _, err := e.svc.publish(ctx, newOp(refresh), []Fetched{merged("5", "1", time.Now())}); err != nil {
				t.Error(err)
			}
		})
	})
	type outcome struct {
		res MatchResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := e.svc.MatchLibrary(ctx, "l1", func(string) bool { return false }, nil)
		done <- outcome{res, err}
	}()
	dbtest.WaitForBlockedLock(t, e.pool)
	if err := refresh.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if o := <-done; o.err != nil || o.res != (MatchResult{Skipped: 1}) {
		t.Fatalf("result = %+v (%v)", o.res, o.err)
	}
	if l := e.link("s"); l.State != StateUnmatched {
		t.Fatalf("link = %+v", l)
	}
}

// A result whose merges pass through a rejected entry is left out, and publishing it records
// every hop, so the rejected entry resolves to where it went.
func TestMatchLeavesOutMergesThroughARejectedEntry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Target")
	e.fake.Put("2", providertest.Series("Rejected", metadata.Manga))
	if err := e.svc.Link(ctx, "s", "fake", "2", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ignore(ctx, "s", "fake", new(int64(1))); err != nil {
		t.Fatal(err)
	}
	alias := providertest.Series("Target", metadata.Manga)
	alias.MergedInto = "2"
	e.fake.Put("4", alias)
	e.fake.Put("2", providertest.Payload{MergedInto: "5"})
	final := providertest.Series("Final", metadata.Manga)
	final.Fields.AltTitles = metadata.Val([]string{"Target"})
	e.fake.Put("5", final)
	if err := e.svc.Rematch(ctx, "s", "fake", new(int64(2))); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateUnmatched || len(l.Candidates) != 0 {
		t.Fatalf("offered through 4 → 2 → 5: %+v", l)
	}

	e.publish(fetch(ctx, e.fake, []string{"4"})...)
	if _, into := e.entry("2"); into == nil || *into != "5" {
		t.Fatalf("2 merged into %v", into)
	}
}

// A result found both directly and through a rejected entry is left out.
func TestMatchLeavesOutAResultAlsoReachedThroughARejectedEntry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Target")
	e.fake.Put("2", providertest.Series("Rejected", metadata.Manga))
	if err := e.svc.Link(ctx, "s", "fake", "2", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Ignore(ctx, "s", "fake", new(int64(1))); err != nil {
		t.Fatal(err)
	}
	alias := providertest.Series("Target", metadata.Manga)
	alias.MergedInto = "2"
	e.fake.Put("4", alias)
	e.fake.Put("2", providertest.Payload{MergedInto: "5"})
	e.fake.Put("5", providertest.Series("Target", metadata.Manga))
	if err := e.svc.Rematch(ctx, "s", "fake", new(int64(2))); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateUnmatched || len(l.Candidates) != 0 {
		t.Fatalf("link = %+v", l)
	}
}

// A result reaching past the recorded chain of a rejected entry, through one of its hops, is
// left out, rather than conflict on every run once publishing moves the chain.
func TestMatchLeavesOutResultsThroughARejectedChain(t *testing.T) {
	e := setup(t)
	earlier := time.Now().Add(-time.Hour)
	e.series("l1", "s", "Target")
	e.publish(merged("B", "C", earlier))
	e.publish(merged("R", "B", earlier))
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, rejected, retry_at)
		VALUES ('l1', 'comic/s', 'fake', 'unmatched', '{R}', now())`)
	alias := providertest.Series("Target", metadata.Manga)
	alias.MergedInto = "B"
	e.fake.Put("A", alias)
	e.fake.Put("B", providertest.Payload{MergedInto: "D"})
	final := providertest.Series("Final", metadata.Manga)
	final.Fields.AltTitles = metadata.Val([]string{"Target"})
	e.fake.Put("D", final)

	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v", res)
	}
}

func TestMatchSkipsSeriesChangedDuringTheLookup(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote One")
	e.series("l1", "t", "Remote Two")
	e.series("l1", "u", "Gone")
	e.fake.OnMatch(func(title string) {
		switch title {
		case "Remote One":
			e.retitle("s", "Renamed")
		case "Remote Two":
			if err := e.svc.Ignore(ctx, "t", "fake", nil); err != nil {
				t.Error(err)
			}
		case "Gone":
			e.exec("DELETE FROM content WHERE id = 'u'")
		}
	})
	if res, err := e.svc.matchNext(ctx, changes{}, []libraryPlan{{"l1", map[string]autoMatch{"fake": {Library: true}}}}); err != nil || res != (MatchResult{Skipped: 3}) {
		t.Fatalf("result = %+v (%v)", res, err)
	}
	if s, t2 := e.link("s"), e.link("t"); s.State != StateNone || t2.State != StateIgnored {
		t.Fatalf("links = %+v, %+v", s, t2)
	}

	e.fake.OnMatch(nil)
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("next page = %+v", res)
	}
}

func TestMatchPages(t *testing.T) {
	e := setup(t)
	var want []string
	for i := range 25 {
		id := fmt.Sprintf("s%02d", i)
		e.series("l1", id, "Nothing "+id)
		want = append(want, "Nothing "+id)
	}
	var matched []string
	e.fake.OnMatch(func(title string) { matched = append(matched, title) })
	e.notified = nil
	if res := e.match(); res != (MatchResult{Unmatched: 25}) {
		t.Fatalf("result = %+v", res)
	}
	if !slices.Equal(matched, want) {
		t.Fatalf("matched %v", matched)
	}
	if !slices.Equal(e.notified, []string{"l1", "l1"}) {
		t.Fatalf("notified = %v, want once per page", e.notified)
	}
}

func TestResolveReview(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "a", "Remote")
	e.series("l1", "b", "Remote")
	e.match()

	page, err := e.svc.Review(ctx, ReviewQuery{Tab: "review", Limit: 1})
	if err != nil || page.Total != 2 || len(page.Items) != 1 || page.Items[0].LocalTitle != "Remote" ||
		!slices.Equal(candidateIDs(page.Items[0].Link.Candidates), []string{"1", "2"}) {
		t.Fatalf("review = %+v (%v)", page, err)
	}
	if _, err := e.svc.Review(ctx, ReviewQuery{Tab: "nope"}); !isValidation(err) {
		t.Fatalf("unknown tab: err = %v", err)
	}

	rev := e.link("a").Rev
	got := e.svc.ResolveReview(ctx, []ReviewAction{
		{ContentID: "a", Provider: "fake", Action: "link", ExternalID: "2", ExpectRev: &rev},
		{ContentID: "b", Provider: "fake", Action: "ignore", ExpectRev: new(rev - 1)},
		{ContentID: "b", Provider: "fake", Action: "unlink", ExpectRev: &rev},
	})
	want := []ReviewResult{
		{ContentID: "a", Provider: "fake", OK: true},
		{ContentID: "b", Provider: "fake", Error: metadata.ErrConflict.Error()},
		{ContentID: "b", Provider: "fake", Error: `action: unknown action "unlink"`},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("results = %+v", got)
	}
	if l := e.link("a"); *l.ExternalID != "2" || *l.Origin != OriginManual {
		t.Fatalf("a = %+v", l)
	}
	if l := e.link("b"); l.State != StateReview {
		t.Fatalf("b = %+v", l)
	}
}

func TestReviewSearchAndFilter(t *testing.T) {
	e := setup(t)
	for id, title := range map[string]string{"a": "Sousou no Frieren", "b": "Emma", "c": "Frieren Again", "d": "Nothing", "e": "frieren"} {
		e.series("l1", id, title)
	}
	e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, last_error) VALUES
		('l1', 'comic/a', 'fake', 'review', NULL), ('l1', 'comic/b', 'fake', 'review', NULL),
		('l1', 'comic/c', 'fake', 'unmatched', 'down'), ('l1', 'comic/d', 'fake', 'unmatched', NULL),
		('l1', 'comic/e', 'fake', 'review', NULL)`)
	// Prepared statements may run generic plans, which take the search only as a function argument.
	cfg := e.pool.Config()
	cfg.ConnConfig.RuntimeParams["plan_cache_mode"] = "force_generic_plan"
	generic, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer generic.Close()
	e.svc.pool = generic
	for _, c := range []struct {
		q    ReviewQuery
		want []string
	}{
		{ReviewQuery{Tab: "review", Search: "frier"}, []string{"a", "e"}},
		{ReviewQuery{Tab: "review", Search: "Frieren"}, []string{"e", "a"}},          // an exact title first
		{ReviewQuery{Tab: "review", Search: "emna"}, []string{"b"}},                  // a typo
		{ReviewQuery{Tab: "review", Search: "frieren emma"}, nil},                    // every word
		{ReviewQuery{Tab: "unmatched", Search: "frieren"}, []string{"c"}},            // the tab still applies
		{ReviewQuery{Tab: "unmatched", Failed: true}, []string{"c"}},                 // the last attempt failed
		{ReviewQuery{Tab: "review", LibraryID: "l2", Search: "frieren"}, []string{}}, // another library
	} {
		c.q.Limit = 10
		page, err := e.svc.Review(context.Background(), c.q)
		got := fp.Map(page.Items, func(it ReviewItem) string { return it.Content.ID })
		if err != nil || page.Total != len(c.want) || !slices.Equal(got, c.want) {
			t.Errorf("%+v: got %v of %d (%v), want %v", c.q, got, page.Total, err, c.want)
		}
	}
}

func TestResolveReviewFetchesEntriesTogether(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	for _, id := range []string{"a", "b", "c"} {
		e.series("l1", id, "Remote")
	}
	e.match()
	fetched, revs := len(e.fake.Fetches()), map[string]int64{}
	for _, id := range []string{"a", "b", "c"} {
		revs[id] = e.link(id).Rev
	}
	e.notified = nil
	got := e.svc.ResolveReview(ctx, []ReviewAction{
		{ContentID: "a", Provider: "fake", Action: "link", ExternalID: "1", ExpectRev: new(revs["a"])},
		{ContentID: "b", Provider: "fake", Action: "link", ExternalID: "2", ExpectRev: new(revs["b"])},
		{ContentID: "c", Provider: "fake", Action: "link", ExternalID: "2", ExpectRev: new(revs["c"] - 1)},
	})
	want := []ReviewResult{
		{ContentID: "a", Provider: "fake", OK: true},
		{ContentID: "b", Provider: "fake", OK: true},
		{ContentID: "c", Provider: "fake", Error: metadata.ErrConflict.Error()},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("results = %+v", got)
	}
	if f := e.fake.Fetches()[fetched:]; len(f) != 1 || !slices.Equal(f[0], []string{"1", "2"}) {
		t.Fatalf("fetches = %v, want one batch", f)
	}
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("notified = %v, want once", e.notified)
	}
	for id, want := range map[string]State{"a": StateLinked, "b": StateLinked, "c": StateReview} {
		if l := e.link(id); l.State != want {
			t.Errorf("%s = %+v", id, l)
		}
	}
}

func TestRejectCandidates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote")
	e.match()
	l := e.link("s")
	if err := e.svc.Reject(ctx, "s", "fake", nil, &l.Rev); !isValidation(err) {
		t.Fatalf("rejecting nothing: err = %v", err)
	}
	if after := e.link("s"); after.Rev != l.Rev || len(after.Candidates) != 2 {
		t.Fatalf("after rejecting nothing = %+v", after)
	}
	from := time.Now()
	got := e.svc.ResolveReview(ctx, []ReviewAction{{ContentID: "s", Provider: "fake", Action: "reject",
		ExternalIDs: candidateIDs(l.Candidates), ExpectRev: &l.Rev}})
	if !got[0].OK {
		t.Fatalf("results = %+v", got)
	}
	l = e.link("s")
	if l.State != StateUnmatched || !slices.Equal(l.Rejected, []string{"1", "2"}) || len(l.Candidates) > 0 ||
		!within(*l.RetryAt, from, time.Now(), 30*24*time.Hour) {
		t.Fatalf("rejected = %+v", l)
	}
	// Matching goes on without them.
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("after match now = %+v", res)
	}
}

// A rejected candidate is stored as a stub, which refreshing fetches, so a later upstream merge of it
// is recorded and rejects its target. Rejecting needs no provider.
func TestRejectedCandidateMergedLater(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote")
	e.match()
	l := e.link("s")
	fetched := len(e.fake.Fetches())
	e.fake.Fail(errors.New("down"))
	if err := e.svc.Reject(ctx, "s", "fake", append(candidateIDs(l.Candidates), "99"), &l.Rev); err != nil {
		t.Fatal(err)
	}
	if len(e.fake.Fetches()) != fetched {
		t.Fatalf("rejecting fetched %v", e.fake.Fetches()[fetched:])
	}
	l = e.link("s")
	if err := e.svc.Reject(ctx, "s", "fake", []string{"one"}, &l.Rev); !isValidation(err) {
		t.Fatalf("not an ID: err = %v", err)
	}

	e.fake.Fail(nil)
	e.fake.Put("1", providertest.Payload{MergedInto: "4"})
	e.fake.Put("4", providertest.Series("Remote", metadata.Manga))
	e.refresh(true)
	// 99 was never served: its stub goes, rather than fail forever.
	if ids := e.entryIDs(); !slices.Equal(ids, []string{"1", "2", "4"}) {
		t.Fatalf("entries = %v", ids)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v, link %+v", res, e.link("s"))
	}
	if summary, err := e.svc.Summary(ctx); err != nil || summary.Providers[0].Failing != 0 {
		t.Fatalf("providers = %+v (%v)", summary.Providers, err)
	}
}

func TestSummaryHealth(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	e.series("l1", "t", "Local")
	for id, entry := range map[string]string{"s": "1", "t": "2"} {
		if err := e.svc.Link(ctx, id, "fake", entry, nil); err != nil {
			t.Fatal(err)
		}
	}
	e.exec("UPDATE provider_entries SET raw = '{}' WHERE external_id = '1'")
	e.exec("UPDATE provider_entries SET attempts = 2, last_error = 'down' WHERE external_id = '2'")
	e.tx(func(tx pgx.Tx) error {
		_, err := e.svc.store.Recompute(ctx, tx, "l1", []string{"comic/s"})
		return err
	})
	last, err := db.SelectScalar[time.Time](ctx, e.pool, "SELECT max(fetched_at) FROM provider_entries")
	if err != nil {
		t.Fatal(err)
	}
	summary, err := e.svc.Summary(ctx)
	if p := summary.Providers; err != nil || len(p) != 1 || p[0].Provider != "fake" || p[0].Failing != 1 ||
		p[0].Undecodable != 1 || !p[0].LastFetched.Equal(last) {
		t.Fatalf("providers = %+v (%v)", p, err)
	}
	// The series show why.
	if l := e.view("s").Links[0]; l.LastError == nil {
		t.Fatalf("undecodable = %+v", l)
	}
	if l := e.view("t").Links[0]; l.RefreshAttempts != 2 || *l.RefreshError != "down" || l.RefreshAt == nil {
		t.Fatalf("failing = %+v", l)
	}
}

// mergeACandidate leaves s in review with candidates 1 and 2, then merges 1 upstream into 2, which
// also goes by "Remote Uno". Linking through 1 moves s's own candidates off it, in the same
// transaction as the link.
func (e *env) mergeACandidate() {
	e.t.Helper()
	e.series("l1", "s", "Remote")
	e.match()
	merged := providertest.Series("Remote Uno", metadata.Manga)
	merged.MergedInto = "2"
	e.fake.Put("1", merged)
	e.fake.Put("2", providertest.Payload{Fields: metadata.Fields{Title: metadata.Val("Remote Two"),
		AltTitles: metadata.Val([]string{"Remote Uno"}), Kind: metadata.Val(metadata.Manga)}})
}

func TestLinkingAMergedCandidate(t *testing.T) {
	e := setup(t)
	e.mergeACandidate()
	if err := e.svc.Link(context.Background(), "s", "fake", "1", new(e.link("s").Rev)); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); *l.ExternalID != "2" {
		t.Fatalf("link = %+v", l)
	}
}

func TestMatchingAMergedCandidate(t *testing.T) {
	e := setup(t)
	e.mergeACandidate()
	e.retitle("s", "Remote Uno")
	if res := e.match(); res != (MatchResult{Linked: 1}) {
		t.Fatalf("result = %+v", res)
	}
	if l := e.link("s"); *l.ExternalID != "2" {
		t.Fatalf("link = %+v", l)
	}
}
