package linking

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/metadata"

	"github.com/jackc/pgx/v5"
)

// orphans sets up series a, b, and c, a leaf a/ch1, entry 1, and then runs sql.
func orphans(t *testing.T, sql string) *env {
	t.Helper()
	e := setup(t)
	for _, id := range []string{"a", "b", "c"} {
		e.series("l1", id, "Local "+id)
	}
	e.exec("INSERT INTO content (id, uri_part, uri, type, library_id, parent_id) VALUES ('ch1', 'ch1', 'comic/a/ch1', 'comic', 'l1', 'a')")
	e.publish(found("1", "Remote One", time.Now()))
	e.exec(sql)
	return e
}

const orphanRows = `
	INSERT INTO content_metadata (uri, library_id, data_raw) VALUES
		('comic/gone1', 'l1', '{"v": 2, "rev": 1, "overrides": {"title": "Mine"}}'),
		('comic/gone2', 'l1', '{"v": 2, "rev": 1, "overrides": {"title": "Also mine"}}');
	INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin, rejected, rev) VALUES
		('l1', 'comic/gone1', 'fake', 'linked', '1', 'manual', '{}', 1),
		('l1', 'comic/gone2', 'fake', 'review', NULL, NULL, '{}', 1),
		('l1', 'comic/gone3', 'fake', 'review', NULL, NULL, '{9}', 1),
		('l1', 'comic/b', 'fake', 'ignored', NULL, NULL, '{}', 1),
		('l1', 'comic/c', 'fake', 'review', NULL, NULL, '{}', 3);
	UPDATE content_metadata SET data_raw = data_raw || '{"overrides": {"title": "Theirs"}}' WHERE uri = 'comic/a';`

func (e *env) fix(del []string, move map[string]string) error {
	return e.svc.FixOrphans(context.Background(), "l1", del, move)
}

func (e *env) orphansLeft() []string {
	e.t.Helper()
	left, err := db.SelectScalars[string](context.Background(), e.pool, `
		SELECT uri FROM content_metadata WHERE uri LIKE 'comic/gone%'
		UNION ALL SELECT uri FROM metadata_links WHERE uri LIKE 'comic/gone%'`)
	if err != nil {
		e.t.Fatal(err)
	}
	return left
}

func isOccupied(err error) bool { return errors.Is(err, metadata.ErrOccupied) }

func TestFixOrphansRefusesWithoutChanges(t *testing.T) {
	e := orphans(t, orphanRows)
	for _, c := range []struct {
		name string
		del  []string
		move map[string]string
		want func(error) bool
	}{
		{"overrides at the destination", nil, map[string]string{"comic/gone1": "comic/a"}, isOccupied},
		// gone1's overrides could move, but not its link, so neither does.
		{"a decided link at the destination", nil, map[string]string{"comic/gone1": "comic/b"}, isOccupied},
		{"overrides from two orphans", nil, map[string]string{"comic/gone1": "comic/c", "comic/gone2": "comic/c"}, isOccupied},
		{"a link onto a leaf", nil, map[string]string{"comic/gone1": "comic/a/ch1"}, isValidation},
		{"a source with content", []string{"comic/a"}, nil, isValidation},
		{"a destination without content", nil, map[string]string{"comic/gone1": "comic/gone2"}, isValidation},
	} {
		if err := e.fix(c.del, c.move); !c.want(err) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	for _, id := range []string{"b", "c"} {
		if v := e.view(id); v.Merged.Title.V != "Local "+id || v.OverridesRev != 0 || *v.Links[0].Rev != map[string]int64{"b": 1, "c": 3}[id] {
			t.Fatalf("%s = %+v, want it untouched", id, v)
		}
	}
	if len(e.orphansLeft()) != 5 {
		t.Fatalf("orphans = %v, want all kept", e.orphansLeft())
	}
}

func TestFixOrphansMovesOverPendingLinks(t *testing.T) {
	e := orphans(t, orphanRows)
	e.notified = nil
	// gone2's pending link cannot land on b's decided one, so it goes; its overrides still move.
	if err := e.fix(nil, map[string]string{"comic/gone1": "comic/c", "comic/gone2": "comic/b"}); err != nil {
		t.Fatal(err)
	}
	v := e.view("c")
	if v.Merged.Title.V != "Mine" || v.Layers[0].Fields.Title.V != "Local c" || v.OverridesRev != 1 {
		t.Fatalf("c = %+v, want gone1's overrides over its own file layer", v)
	}
	if l := v.Links[0]; l.State != StateLinked || *l.ExternalID != "1" || *l.Rev != 4 {
		t.Fatalf("c's link = %+v, want gone1's, past the review's rev", l)
	}
	b := e.view("b")
	if b.Merged.Title.V != "Also mine" || b.Links[0].State != StateIgnored || *b.Links[0].Rev != 1 {
		t.Fatalf("b = %+v, want gone2's overrides and its own link", b)
	}
	if left := e.orphansLeft(); !slices.Equal(left, []string{"comic/gone3"}) {
		t.Fatalf("orphans left = %v", left)
	}
	if !slices.Equal(e.notified, []string{"l1"}) {
		t.Fatalf("notified = %v", e.notified)
	}
}

// A link on a leaf, as on a URI that a removed series left to it, applies to nothing and repairs
// as an orphan; the leaf keeps its own metadata.
func TestFixOrphansMovesALinkOffALeaf(t *testing.T) {
	e := orphans(t, `
		INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin)
			VALUES ('l1', 'comic/a/ch1', 'fake', 'linked', '1', 'manual');
		INSERT INTO content_metadata (uri, library_id, data_raw)
			VALUES ('comic/a/ch1', 'l1', '{"v": 2, "rev": 1, "overrides": {"title": "Leaf"}}')`)
	e.tx(func(tx pgx.Tx) error {
		_, err := e.svc.store.Recompute(context.Background(), tx, "l1", []string{"comic/a/ch1"})
		return err
	})
	if title := e.view("ch1").Merged.Title.V; title != "Leaf" {
		t.Fatalf("leaf title = %q", title)
	}

	if err := e.fix(nil, map[string]string{"comic/a/ch1": "comic/c"}); err != nil {
		t.Fatal(err)
	}
	if l := e.link("c"); l.State != StateLinked || *l.ExternalID != "1" {
		t.Fatalf("c's link = %+v", l)
	}
	if v := e.view("ch1"); v.Merged.Title.V != "Leaf" || v.OverridesRev != 1 {
		t.Fatalf("leaf = %+v, want its metadata kept", v)
	}
}

func TestFixOrphansStacksAndFillsEmptyRows(t *testing.T) {
	e := orphans(t, `
		INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin) VALUES
			('l1', 'comic/gone1', 'fake', 'unmatched', NULL, NULL),
			('l1', 'comic/gone2', 'fake', 'linked', '1', 'manual');
		INSERT INTO metadata_links (library_id, uri, provider, state, rejected) VALUES ('l1', 'comic/gone3', 'fake', 'review', '{9}');
		INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ('n', 'n', 'comic/n', 'comic_series', 'l1');
		INSERT INTO content_metadata (uri, library_id, data_raw) VALUES
			('comic/gone3', 'l1', '{"v": 2, "rev": 4, "overrides": {"title": "Mine"}}')`)
	if err := e.fix(nil, map[string]string{"comic/gone1": "comic/a", "comic/gone2": "comic/a", "comic/gone3": "comic/n"}); err != nil {
		t.Fatal(err)
	}
	if l := e.link("a"); l.State != StateLinked || l.Rev != 2 {
		t.Fatalf("a's link = %+v, want gone2's over gone1's pending one", l)
	}
	l, v := e.link("n"), e.view("n")
	if l.State != StateReview || l.RetryAt == nil || l.Rev != 2 || !slices.Equal(l.Rejected, []string{"9"}) {
		t.Fatalf("n's link = %+v, want gone3's, due for matching", l)
	}
	if v.Merged.Title.V != "Mine" || v.OverridesRev != 1 {
		t.Fatalf("n = %+v, want gone3's overrides at the first rev", v)
	}
}
