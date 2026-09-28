package linking

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"

	"voltis/metadata"
	"voltis/providers/providertest"
)

func (e *env) undo(contentID string, rev int64) error {
	return e.svc.Undo(context.Background(), contentID, "fake", rev)
}

func TestUndoLinkFromNone(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.undo("s", 2); !errors.Is(err, ErrNoUndo) {
		t.Fatalf("stale rev: err = %v", err)
	}
	if err := e.undo("s", 1); err != nil {
		t.Fatal(err)
	}
	if v := e.view("s"); v.Links[0].State != StateNone || len(v.Layers) != 2 || v.Merged.Title.V != "Local" {
		t.Fatalf("undone = %+v", v)
	}
	if n := len(e.svc.undos.m); n != 0 {
		t.Fatalf("%d undos left", n)
	}
	if err := e.undo("s", 1); !errors.Is(err, ErrNoUndo) {
		t.Fatalf("second undo: err = %v", err)
	}
}

func TestUndoRelink(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Link(ctx, "s", "fake", "2", new(int64(1))); err != nil {
		t.Fatal(err)
	}
	if err := e.undo("s", 2); err != nil {
		t.Fatal(err)
	}
	v := e.view("s")
	if l := v.Links[0]; *l.ExternalID != "1" || len(l.Rejected) != 0 || *l.Rev != 3 ||
		v.Layers[1].Source != "fake" || v.Merged.Title.V != "Remote One" {
		t.Fatalf("undone = %+v", v)
	}
}

func TestUndoRelinkRejected(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := e.svc.Ignore(ctx, "s", "fake", new(int64(1))); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Link(ctx, "s", "fake", "1", new(int64(2))); err != nil {
		t.Fatal(err)
	}
	if err := e.undo("s", 3); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateIgnored || !slices.Equal(l.Rejected, []string{"1"}) {
		t.Fatalf("undone = %+v", l)
	}
}

func TestUndoRematchFromNone(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Remote")
	if err := e.svc.Rematch(context.Background(), "s", "fake", nil); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateReview || l.Rev != 1 {
		t.Fatalf("rematched = %+v", l)
	}
	if err := e.undo("s", 1); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateNone {
		t.Fatalf("undone = %+v", l)
	}
}

func TestUndoRestoresExactly(t *testing.T) {
	ctx := context.Background()
	for name, decide := range map[string]func(e *env, l Link) (int64, error){
		"reject": func(e *env, l Link) (int64, error) {
			return e.svc.Reject(ctx, "s", "fake", candidateIDs(l.Candidates), &l.Rev)
		},
		"ignore": func(e *env, l Link) (int64, error) { return e.svc.Ignore(ctx, "s", "fake", &l.Rev) },
		"rematch": func(e *env, l Link) (int64, error) {
			p := providertest.Series("Remote Four", metadata.Manga)
			p.Fields.AltTitles = metadata.Val([]string{"Remote"})
			e.fake.Put("4", p)
			err := e.svc.Rematch(ctx, "s", "fake", &l.Rev)
			if l := e.link("s"); len(l.Candidates) != 3 {
				e.t.Fatalf("rematched = %+v", l)
			}
			return l.Rev + 1, err
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			e.series("l1", "s", "Remote")
			e.match()
			e.exec("UPDATE metadata_links SET retry_at = now() + interval '10 days', attempts = 2, last_error = 'down'")
			before := e.link("s")
			rev, err := decide(e, before)
			if err != nil {
				t.Fatal(err)
			}
			if l := e.link("s"); l.Attempts != 0 || l.Rev != rev {
				t.Fatalf("decided = %+v, rev %d", l, rev)
			}
			if err := e.undo("s", rev); err != nil {
				t.Fatal(err)
			}
			after := e.link("s")
			if after.Rev != rev+1 {
				t.Errorf("rev = %d", after.Rev)
			}
			after.Rev, after.UpdatedAt = before.Rev, before.UpdatedAt
			if !reflect.DeepEqual(after, before) {
				t.Errorf("undone = %+v, want %+v", after, before)
			}
		})
	}
}

func TestUndoRefused(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote")
	e.match()

	// Matching wrote since.
	rev, err := e.svc.Reject(ctx, "s", "fake", candidateIDs(e.link("s").Candidates), new(e.link("s").Rev))
	if err != nil {
		t.Fatal(err)
	}
	e.matchNow()
	if res := e.match(); res != (MatchResult{Unmatched: 1}) {
		t.Fatalf("match = %+v", res)
	}
	if err := e.undo("s", rev); !errors.Is(err, ErrNoUndo) {
		t.Fatalf("after matching: err = %v", err)
	}

	// Expired.
	if rev, err = e.svc.Ignore(ctx, "s", "fake", new(e.link("s").Rev)); err != nil {
		t.Fatal(err)
	}
	e.svc.undos.now = func() time.Time { return time.Now().Add(undoWindow + time.Second) }
	if err := e.undo("s", rev); !errors.Is(err, ErrNoUndo) {
		t.Fatalf("expired: err = %v", err)
	}
	e.svc.undos.now = time.Now

	// Rolled back.
	e.svc.undos.m = map[linkKey]undo{}
	e.series("l1", "t", "Local")
	e.exec(`CREATE FUNCTION refuse() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'refused'; END $$ LANGUAGE plpgsql`)
	e.exec(`CREATE TRIGGER refuse BEFORE INSERT ON metadata_links FOR EACH ROW
		WHEN (NEW.uri = 'comic/t') EXECUTE FUNCTION refuse()`)
	revs, errs := e.svc.LinkAll(ctx, []LinkRequest{{"s", "fake", "1", new(e.link("s").Rev)}, {"t", "fake", "1", nil}})
	if errs[0] == nil || errs[1] == nil || !slices.Equal(revs, []int64{0, 0}) || len(e.svc.undos.m) != 0 {
		t.Fatalf("rolled back: revs %v, errs %v, undos %v", revs, errs, e.svc.undos.m)
	}
}

func TestUndoKeepsEarliestRetry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote")
	e.match()
	e.exec("UPDATE metadata_links SET retry_at = now() + interval '10 days'")
	rev, err := e.svc.Reject(ctx, "s", "fake", candidateIDs(e.link("s").Candidates), new(e.link("s").Rev))
	if err != nil {
		t.Fatal(err)
	}
	from := time.Now()
	e.matchNow()
	if err := e.undo("s", rev); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateReview || l.RetryAt.Before(from) || l.RetryAt.After(time.Now()) {
		t.Fatalf("undone = %+v", l)
	}
}

func TestUndoDropsTheDecisionsRetry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote")
	e.match()
	rev, err := e.svc.Reject(ctx, "s", "fake", candidateIDs(e.link("s").Candidates), new(e.link("s").Rev))
	if err != nil {
		t.Fatal(err)
	}
	if err := e.undo("s", rev); err != nil {
		t.Fatal(err)
	}
	if l := e.link("s"); l.State != StateReview || l.RetryAt != nil {
		t.Fatalf("undone = %+v", l)
	}
}

func TestResolveReviewUndo(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "a", "Remote")
	e.series("l1", "b", "Remote")
	e.match()
	rev := e.link("a").Rev
	got := e.svc.ResolveReview(ctx, []ReviewAction{
		{ContentID: "a", Provider: "fake", Action: "ignore", ExpectRev: &rev},
		{ContentID: "b", Provider: "fake", Action: "ignore", ExpectRev: &rev},
	})
	if !got[0].OK || !got[1].OK || *got[0].Rev != rev+1 || *got[1].Rev != rev+1 {
		t.Fatalf("results = %+v", got)
	}
	got = e.svc.ResolveReview(ctx, []ReviewAction{
		{ContentID: "a", Provider: "fake", Action: "undo", ExpectRev: got[0].Rev},
		{ContentID: "b", Provider: "fake", Action: "undo", ExpectRev: new(rev)},
	})
	want := []ReviewResult{
		{ContentID: "a", Provider: "fake", OK: true},
		{ContentID: "b", Provider: "fake", Error: ErrNoUndo.Error()},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("undo results = %+v", got)
	}
	if a, b := e.link("a"), e.link("b"); a.State != StateReview || b.State != StateIgnored {
		t.Fatalf("a = %+v, b = %+v", a, b)
	}
}
