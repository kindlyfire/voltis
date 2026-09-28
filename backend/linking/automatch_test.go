package linking

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"
)

func library(settings, sources string) models.Library {
	return models.Library{Settings: models.JSONB(settings), Sources: models.JSONB(sources)}
}

func (e *env) coverage(lib string) autoMatch {
	e.t.Helper()
	l, err := db.SelectOne[models.Library](context.Background(), e.pool, "SELECT * FROM libraries WHERE id = $1", lib)
	if err != nil {
		e.t.Fatal(err)
	}
	return autoMatchOf(l, "fake")
}

func (e *env) due(lib string, cov autoMatch) []string {
	e.t.Helper()
	p, _ := e.svc.reg.Get("fake")
	page, err := e.svc.duePage(context.Background(), p, lib, cov, "", time.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	return fp.Map(page, func(t metadata.Target) string { return strings.TrimPrefix(t.URI, "comic/") })
}

func TestAutoMatchCoverage(t *testing.T) {
	e := setup(t)
	for id, leaves := range map[string][]string{
		"a":     {"/lib/a/1.cbz"},                 // inherits
		"b":     {"/lib/b/1.cbz"},                 // on
		"c":     {"/lib/c/1.cbz"},                 // off
		"m":     {"/lib/b/m/1.cbz"},               // off, inside on
		"n":     {"/lib/c/n/1.cbz"},               // inherits, inside off
		"span":  {"/lib/c/2.cbz", "/lib/b/2.cbz"}, // off and on
		"none":  {"/elsewhere/1.cbz"},             // under no source
		"empty": nil,
	} {
		e.series("l1", id, "Title")
		for _, l := range leaves {
			e.leaf(id, l)
		}
	}
	sources := `[{"path_uri": "/lib/a"}, {"path_uri": "/lib/b", "settings": {"auto_match": {"fake": true}}},
		{"path_uri": "/lib/c/", "settings": {"auto_match": {"fake": false}}}, {"path_uri": "/lib/c/n"},
		{"path_uri": "/lib/b/m", "settings": {"auto_match": {"fake": false}}}]`
	all := []string{"a", "b", "c", "empty", "m", "n", "none", "span"}
	for _, c := range []struct {
		settings, sources string
		want              []string
	}{
		{`{"auto_match": {"fake": true}}`, sources, []string{"a", "b", "empty", "n", "none", "span"}},
		{`{"auto_match": {"fake": false}}`, sources, []string{"b", "span"}},
		{`{"auto_match": {}}`, sources, []string{"b", "span"}},
		// The fast path.
		{`{"auto_match": {"fake": true}}`, `[{"path_uri": "/lib/a"}, {"path_uri": "/lib/b", "settings": {"auto_match": {"fake": true}}}]`, all},
	} {
		e.sources("l1", c.settings, c.sources)
		cov := e.coverage("l1")
		if got := e.due("l1", cov); !slices.Equal(got, c.want) {
			t.Errorf("%s %s: due %v, want %v", c.settings, c.sources, got, c.want)
		}
	}
	if cov := e.coverage("l1"); cov.Scopes != nil {
		t.Errorf("fast path = %+v", cov)
	}
	e.sources("l1", `{}`, `[{"path_uri": "/lib/a"}, {"path_uri": "/lib/b", "settings": {"auto_match": {"fake": false}}}]`)
	if cov := e.coverage("l1"); cov.any() || cov.Scopes != nil {
		t.Errorf("off = %+v", cov)
	}
}

func TestAutoMatchOf(t *testing.T) {
	got := autoMatchOf(library(`{"auto_match": {"fake": true}}`, `[{"path_uri": "."}, {"path_uri": "/"},
		{"path_uri": "rel/x", "settings": {"auto_match": {"fake": false}}}, {"path_uri": "/lib/"}, {"path_uri": "/lib"},
		{"path_uri": "/lib/b", "settings": {"auto_match": {"other": false}}}]`), "fake")
	want := []scope{{"/lib/b/", true}, {"rel/x/", false}, {"/lib/", true}, {"/lib/", true}, {"/", true}}
	if !got.Library || !slices.Equal(got.Scopes, want) {
		t.Fatalf("got %+v", got)
	}
	// Overrides that resolve like the library take the fast path.
	if got := autoMatchOf(library(`{"auto_match": {"fake": true}}`, `[{"path_uri": "/a", "settings": {"auto_match": {"fake": true}}}]`),
		"fake"); !got.Library || got.Scopes != nil {
		t.Fatalf("got %+v", got)
	}
	if got := autoMatchOf(models.Library{}, "fake"); got.any() || got.Scopes != nil {
		t.Fatalf("zero library = %+v", got)
	}
}

func TestAutoMatchGrew(t *testing.T) {
	on, off := `{"auto_match": {"fake": true}}`, `{}`
	src := func(path, value string) string {
		if value == "" {
			return `{"path_uri": "` + path + `"}`
		}
		return `{"path_uri": "` + path + `", "settings": {"auto_match": {"fake": ` + value + `}}}`
	}
	for _, c := range []struct {
		name          string
		before, after models.Library
		want          bool
	}{
		{"library on", library(off, `[]`), library(on, `[]`), true},
		{"library off", library(on, `[]`), library(off, `[]`), false},
		{"a new library on", models.Library{}, library(on, `[]`), true},
		{"a new library off", models.Library{}, library(off, `[]`), false},
		{"a source on", library(off, "["+src("/a", "")+"]"), library(off, "["+src("/a", "true")+"]"), true},
		{"a source off", library(on, "["+src("/a", "")+"]"), library(on, "["+src("/a", "false")+"]"), false},
		{"widening an off source", library(on, "["+src("/a/b", "false")+"]"), library(on, "["+src("/a", "false")+"]"), false},
		{"narrowing an on source", library(off, "["+src("/a", "true")+"]"), library(off, "["+src("/a/b", "true")+"]"), false},
		{"narrowing an off source", library(on, "["+src("/a", "false")+"]"), library(on, "["+src("/a/b", "false")+"]"), true},
		{"a nested source inheriting under an off parent", library(on, "["+src("/a", "false")+","+src("/a/b", "false")+"]"),
			library(on, "["+src("/a", "false")+","+src("/a/b", "")+"]"), true},
		{"another provider", library(off, `[]`), library(`{"auto_match": {"other": true}}`, "["+src("/a", "")+"]"), false},
	} {
		if got := AutoMatchGrew(c.before, c.after, "fake"); got != c.want {
			t.Errorf("%s: grew = %v", c.name, got)
		}
	}
}

// The coverage conditions are checked by running them; here, what they take.
func TestAutoMatchSQL(t *testing.T) {
	if frag, args := (autoMatch{Library: true}).sql(6); frag != "" || args != nil {
		t.Fatalf("fast path = %q %v", frag, args)
	}
	// Only the prefixes resolving unlike the library bound the leaves.
	for _, c := range []struct {
		cov  autoMatch
		want []any
	}{
		{autoMatch{Scopes: []scope{{"/a/b/", false}, {"/a/", true}}}, []any{false, "/a/b/", false, "/a/", true, "/a0"}},
		{autoMatch{Library: true, Scopes: []scope{{"/a/b/", true}, {"/a/", false}, {"/", true}}},
			[]any{true, "/a/b/", true, "/a/", false, "/a0", "/", true}},
	} {
		if _, args := c.cov.sql(3); !slices.Equal(args, c.want) {
			t.Errorf("%+v: args = %v", c.cov, args)
		}
	}
}

// Match now makes due only the series that match automatically, of the providers given, in every
// library when none is.
func TestMatchNowFollowsCoverage(t *testing.T) {
	e := setup(t)
	for lib, series := range map[string][]string{"l1": {"on", "off"}, "l2": {"other"}} {
		for _, id := range series {
			e.series(lib, id, "Nothing")
			e.leaf(id, "/"+id+"/1.cbz")
			e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
				VALUES ($1, 'comic/' || $2, 'fake', 'unmatched', now() + interval '1 day')`, lib, id)
		}
	}
	e.sources("l1", `{}`, `[{"path_uri": "/on", "settings": {"auto_match": {"fake": true}}}]`)
	due := func() []string {
		t.Helper()
		uris, err := db.SelectScalars[string](context.Background(), e.pool,
			"SELECT uri FROM metadata_links WHERE retry_at <= now() ORDER BY uri")
		if err != nil {
			t.Fatal(err)
		}
		e.exec("UPDATE metadata_links SET retry_at = now() + interval '1 day'")
		return uris
	}
	if err := e.svc.MatchNow(context.Background(), nil, []string{"other"}); err != nil || len(due()) != 0 || !e.woken() {
		t.Fatalf("another provider: %v", err)
	}
	e.matchNow()
	if got := due(); !slices.Equal(got, []string{"comic/on", "comic/other"}) {
		t.Fatalf("due %v", got)
	}
	e.matchNow("l1")
	if got := due(); !slices.Equal(got, []string{"comic/on"}) {
		t.Fatalf("due %v", got)
	}
}

// The command matches every pending series, whether or not its library matches automatically.
func TestMatchLibraryIgnoresCoverage(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Nothing")
	e.series("l1", "t", "Nothing")
	e.leaf("t", "/off/1.cbz")
	e.sources("l1", `{}`, `[{"path_uri": "/off", "settings": {"auto_match": {"fake": false}}}]`)
	res, err := e.svc.MatchLibrary(context.Background(), "l1", func(string) bool { return false }, nil)
	if err != nil || res != (MatchResult{Unmatched: 2}) {
		t.Fatalf("result = %+v (%v)", res, err)
	}
}

// The worker waits for series it matches, and for refreshes when it matches none.
func TestNextDueFollowsCoverage(t *testing.T) {
	e := setup(t)
	for id, retry := range map[string]string{"on": "5 minutes", "off": "1 minute"} {
		e.series("l1", id, "Nothing")
		e.leaf(id, "/"+id+"/1.cbz")
		e.exec(`INSERT INTO metadata_links (library_id, uri, provider, state, retry_at)
			VALUES ('l1', 'comic/' || $1, 'fake', 'unmatched', now() + $2::interval)`, id, retry)
	}
	e.series("l1", "s", "Local")
	if err := e.svc.Link(context.Background(), "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.exec("UPDATE provider_entries SET refresh_at = now() + interval '10 minutes'")
	e.sources("l1", `{}`, `[{"path_uri": "/on", "settings": {"auto_match": {"fake": true}}}]`)
	if wait := e.untilIdle(); wait < 4*time.Minute || wait > 5*time.Minute {
		t.Fatalf("waits %v for the covered retry", wait)
	}
	e.sources("l1", `{}`, `[]`)
	if wait := e.untilIdle(); wait < 9*time.Minute || wait > 10*time.Minute {
		t.Fatalf("waits %v for the refresh", wait)
	}
}
