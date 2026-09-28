package linking

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"voltis/db"
	"voltis/metadata"
	"voltis/providers/providertest"
)

// Matching fetches the rejected stubs first, so a rejected entry merged upstream rejects its target
// before the refresh gets to it.
func TestMatchFetchesRejectedStubs(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote Two")
	e.fake.Put("1", providertest.Payload{MergedInto: "2"})
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	e.matchNow()
	e.fake.Fail(errors.New("down"))
	if res := e.match(); res != (MatchResult{Failed: 1}) {
		t.Fatalf("stubs unavailable: result = %+v", res)
	}
	e.fake.Fail(nil)
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v, link %+v", res, e.link("s"))
	}
	if _, into := e.entry("1"); into == nil || *into != "2" {
		t.Fatalf("merge of 1 not recorded: %v", into)
	}
}

// A rejected stub that does not decode rejects what it can, and never blocks matching.
func TestUndecodableStubDoesNotBlockMatching(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote Two")
	e.fake.Put("1", json.RawMessage(`{}`))
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Linked: 1}) {
		t.Fatalf("result = %+v, link %+v", res, e.link("s"))
	}
}

// A stub's hops lead on through merges already stored: 1 → 2 (does not decode) → 4 (stored).
func TestStubHopsFollowStoredMerges(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote Four")
	e.fake.Put("1", providertest.Payload{MergedInto: "2"})
	e.fake.Put("2", json.RawMessage(`{}`))
	e.fake.Put("4", providertest.Series("Remote Four", metadata.Manga))
	e.exec(`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, merged_into, fetched_at, refresh_at)
		VALUES ('fake', '4', '4', '{}', NULL, now(), now()), ('fake', '2', '4', '{}', '4', now(), now())`)
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v, link %+v", res, e.link("s"))
	}
}

// A chain too long to settle still rejects where it stopped: 1 → 4 → … → 8 → 2.
func TestUnsettledStubRejectsWhereItStopped(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote Two")
	chain := []string{"1", "4", "5", "6", "7", "8", "2"}
	for i, id := range chain[:len(chain)-1] {
		e.fake.Put(id, providertest.Payload{MergedInto: chain[i+1]})
	}
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"1"}, nil); err != nil {
		t.Fatal(err)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("result = %+v, link %+v", res, e.link("s"))
	}
}

func TestRejectWakesTheWorkerForItsStubs(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if e.woken() {
		t.Fatal("woken before")
	}
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"2"}, nil); err != nil {
		t.Fatal(err)
	}
	if !e.woken() {
		t.Fatal("not woken")
	}
	// Stubs count as nothing fetched.
	summary, err := e.svc.Summary(ctx)
	if err != nil || summary.Providers[0].LastFetched != nil {
		t.Fatalf("providers = %+v (%v)", summary.Providers, err)
	}
}

// A stub deleted upstream keeps the deletion payload, which decodes, rather than its own.
func TestDeletedStubTakesTheDeletion(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Linked")
	e.series("l1", "t", "Rejected")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reject(ctx, "t", "fake", []string{"2"}, nil); err != nil {
		t.Fatal(err)
	}
	e.fake.Put("1", providertest.Payload{MergedInto: "2"})
	e.fake.Put("2", providertest.Payload{Deleted: true})
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	if l := e.view("s").Links[0]; l.LastError != nil || !l.Deleted {
		t.Fatalf("link = %+v", l)
	}
}

// GC keeps a stub while it is referenced, and a stub never replaces a stored entry.
func TestStubsAreKeptAndNeverOverwrite(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	e.series("l1", "t", "Local")
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"2"}, nil); err != nil {
		t.Fatal(err)
	}
	e.collect()
	if n, err := db.SelectScalar[int](ctx, e.pool, "SELECT count(*) FROM provider_entries"); err != nil || n != 1 {
		t.Fatalf("entries = %d (%v)", n, err)
	}
	if err := e.svc.Link(ctx, "t", "fake", "2", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Reject(ctx, "s", "fake", []string{"2"}, new(int64(1))); err != nil {
		t.Fatal(err)
	}
	if l := e.view("t").Links[0]; l.Entry == nil || l.Entry.Title != "Remote Two" {
		t.Fatalf("link = %+v", l)
	}
	if title, _ := e.entry("2"); title != "Remote Two" {
		t.Fatalf("entry title = %q", title)
	}
}
