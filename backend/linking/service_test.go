package linking

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/db/dbtest"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/providertest"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type env struct {
	t        *testing.T
	pool     *pgxpool.Pool
	svc      *Service
	fake     *providertest.Provider
	mu       sync.Mutex
	notified []string
	paused   bool // matching, for the worker's steps
}

func setup(t *testing.T) *env {
	t.Helper()
	e := &env{t: t, pool: dbtest.Pool(t), fake: providertest.New()}
	reg := providers.NewRegistry(e.fake)
	e.svc = New(e.pool, metadata.NewStore(reg), reg, covers.New(t.TempDir()), func(lib string) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.notified = append(e.notified, lib)
	})
	// Both also go by "Remote", which matches them alike.
	for id, title := range map[string]string{"1": "Remote One", "2": "Remote Two"} {
		p := providertest.Series(title, metadata.Manga)
		p.Fields.AltTitles = metadata.Val([]string{"Remote"})
		e.fake.Put(id, p)
	}
	e.fake.Put("3", providertest.Series("Remote Novel", metadata.Novel))
	return e
}

func (e *env) exec(sql string, args ...any) {
	e.t.Helper()
	if _, err := e.pool.Exec(context.Background(), sql, args...); err != nil {
		e.t.Fatalf("exec %s: %v", sql, err)
	}
}

// series adds a comic series with a file layer titled after it, in a library that matches automatically.
func (e *env) series(lib, id, title string) {
	e.t.Helper()
	e.exec(`INSERT INTO libraries (id, name, type, settings) VALUES ($1, 'lib', 'comics', '{"auto_match": {"fake": true}}')
		ON CONFLICT DO NOTHING`, lib)
	e.exec(`INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ($1, $1, 'comic/' || $1, 'comic_series', $2)`, id, lib)
	e.tx(func(tx pgx.Tx) error {
		return e.svc.store.WriteFileLayers(context.Background(), tx,
			[]metadata.FileLayer{{ContentID: id, Fields: metadata.Fields{Title: metadata.Val(title)}}}, time.Now())
	})
}

// leaf adds a file of the series at fileURI.
func (e *env) leaf(series, fileURI string) {
	e.t.Helper()
	e.exec(`INSERT INTO content (id, uri_part, uri, file_uri, type, parent_id, library_id)
		SELECT s.id || '/' || $2, $2, s.uri || '/' || $2, $2, 'comic', s.id, s.library_id FROM content s WHERE s.id = $1`,
		series, fileURI)
}

// sources sets the library's sources and its settings.
func (e *env) sources(lib, settings, sources string) {
	e.t.Helper()
	e.exec(`UPDATE libraries SET settings = $2, sources = $3 WHERE id = $1`, lib, settings, sources)
}

func (e *env) tx(fn func(tx pgx.Tx) error) {
	e.t.Helper()
	if err := db.WithTx(context.Background(), e.pool, fn); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) view(contentID string) MetadataView {
	e.t.Helper()
	v, err := e.svc.View(context.Background(), e.pool, contentID)
	if err != nil {
		e.t.Fatal(err)
	}
	return v
}

func (e *env) link(contentID string) Link {
	e.t.Helper()
	l, err := readLink(context.Background(), e.pool, metadata.Target{ContentID: contentID, LibraryID: "l1"}, "fake")
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func isValidation(err error) bool {
	_, ok := errors.AsType[*metadata.ValidationError](err)
	return ok
}

func TestLinkAndIgnore(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if v := e.view("s"); len(v.Links) != 1 || v.Links[0].State != StateNone || v.Links[0].Rev != nil {
		t.Fatalf("unlinked view = %+v", v.Links)
	}

	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	v := e.view("s")
	l := v.Links[0]
	if l.State != StateLinked || *l.ExternalID != "1" || *l.Origin != OriginManual || l.Entry.Title != "Remote One" || *l.Rev != 1 {
		t.Fatalf("linked = %+v", l)
	}
	if v.Merged.Title.V != "Remote One" || !reflect.DeepEqual(v.Sources["title"], []string{"fake"}) {
		t.Fatalf("merged = %+v, sources %v", v.Merged, v.Sources)
	}
	if kinds := []string{v.Layers[0].Kind, v.Layers[1].Kind, v.Layers[2].Kind}; !slices.Equal(kinds, []string{"file", "provider", "overrides"}) ||
		len(v.Layers[1].Raw) == 0 {
		t.Fatalf("layers = %+v", v.Layers)
	}

	for _, c := range []struct {
		name  string
		input string
		rev   *int64
		check func(error) bool
	}{
		{"stale rev", "2", nil, func(err error) bool { return errors.Is(err, metadata.ErrConflict) }},
		{"not an id", "one", l.Rev, isValidation},
		{"missing upstream", "99", l.Rev, isValidation},
		{"another kind", "3", l.Rev, isValidation},
	} {
		if err := e.svc.Link(ctx, "s", "fake", c.input, c.rev); !c.check(err) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
	e.fake.Fail(errors.New("down"))
	if err := e.svc.Link(ctx, "s", "fake", "2", l.Rev); !errors.As(err, new(*ProviderError)) {
		t.Fatalf("provider down: err = %v", err)
	}
	e.fake.Fail(nil)

	if err := e.svc.Link(ctx, "s", "fake", "2", l.Rev); err != nil {
		t.Fatal(err)
	}
	if got := e.link("s"); *got.ExternalID != "2" || !slices.Equal(got.Rejected, []string{"1"}) {
		t.Fatalf("relinked = %+v", got)
	}
	if _, err := e.svc.Ignore(ctx, "s", "fake", new(int64(2))); err != nil {
		t.Fatal(err)
	}
	if got := e.link("s"); got.State != StateIgnored || !slices.Equal(got.Rejected, []string{"1", "2"}) {
		t.Fatalf("ignored = %+v", got)
	}
	if v := e.view("s"); v.Merged.Title.V != "Local" || len(v.Layers) != 2 {
		t.Fatalf("ignored view = %+v", v)
	}
	if !slices.Contains(e.notified, "l1") {
		t.Fatal("no notification")
	}
}

func TestCandidates(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Remote Two")
	e.exec("INSERT INTO content (id, uri_part, uri, type, library_id) VALUES ('leaf', 'ch1', 'comic/s/ch1', 'comic', 'l1')")
	e.fake.Put("86", providertest.Series("Eighty-Six", metadata.Manga))
	e.fake.Put("5", providertest.Series("86", metadata.Manga))
	e.fake.Put("4", providertest.Series("7 Seeds", metadata.Manga))
	e.fake.Put("7", providertest.Series("Studio 7", metadata.Manga))
	// Hits whose merges cannot be followed are left out of the results.
	for id, into := range map[string]string{"8": "9", "9": "8"} {
		p := providertest.Series("Remote Loop", metadata.Manga)
		p.MergedInto = into
		e.fake.Put(id, p)
	}

	keys := func(cs []Candidate) []string { return fp.Map(cs, func(c Candidate) string { return c.Key.ID }) }
	for _, c := range []struct {
		input string
		want  []string
	}{
		{"", []string{"2"}},            // the series' own title
		{"remote", []string{"2", "1"}}, // best first, the novel left out
		{"1", []string{"1"}},           // by id
		{"86", []string{"86", "5"}},    // by id, then by a title that is a number
		{"7", []string{"7", "4"}},      // the entry with the id once, though the search finds it too
		{"https://nope/x", []string{}}, // nothing matches
	} {
		got, err := e.svc.Candidates(ctx, "s", "fake", c.input)
		if err != nil || !slices.Equal(keys(got), c.want) {
			t.Errorf("%q: got %v (%v), want %v", c.input, keys(got), err, c.want)
		}
	}
	if got, _ := e.svc.Candidates(ctx, "s", "fake", ""); !got[0].Evaluation.Exact || got[0].URL != "https://fake/2" {
		t.Fatalf("candidate = %+v", got[0])
	}
	for _, id := range []string{"leaf", "s"} {
		provider := map[string]string{"leaf": "fake", "s": "nope"}[id]
		if _, err := e.svc.Candidates(ctx, id, provider, ""); !isValidation(err) {
			t.Errorf("%s with %s: err = %v", id, provider, err)
		}
	}
}

func TestRefreshFollowsAMerge(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	e.series("l2", "t", "Other")
	for _, id := range []string{"s", "t"} {
		if err := e.svc.Link(ctx, id, "fake", "1", nil); err != nil {
			t.Fatal(err)
		}
	}

	e.fake.Put("1", providertest.Series("Renamed", metadata.Manga))
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	if a, b := e.view("s").Merged.Title.V, e.view("t").Merged.Title.V; a != "Renamed" || b != "Renamed" {
		t.Fatalf("titles after refresh = %q, %q", a, b)
	}

	e.fake.Put("1", providertest.Payload{MergedInto: "2"})
	e.notified = nil
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"s", "t"} {
		if v := e.view(id); v.Links[0].Entry.Key.ID != "2" || *v.Links[0].Rev != 1 || v.Merged.Title.V != "Remote Two" {
			t.Fatalf("%s after the merge = %+v", id, v)
		}
	}
	if !slices.Equal(slices.Sorted(slices.Values(e.notified)), []string{"l1", "l2"}) {
		t.Fatalf("notified = %v", e.notified)
	}
	if into, err := db.SelectScalar[*string](ctx, e.pool,
		"SELECT merged_into FROM provider_entries WHERE external_id = '1'"); err != nil || into == nil || *into != "2" {
		t.Fatalf("tombstone = %v (%v)", into, err)
	}
}

// A refresh fetches the entry the link resolves to, as its own id may no longer be served.
func TestRefreshFetchesTheCanonicalEntry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.fake.Put("1", providertest.Payload{MergedInto: "2"})
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	e.fake.Remove("1")
	e.fake.Put("2", providertest.Series("Updated Two", metadata.Manga))
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	stored, err := db.SelectScalar[string](ctx, e.pool, "SELECT data->>'title' FROM content WHERE id = 's'")
	if title := e.view("s").Merged.Title.V; title != "Updated Two" || stored != "Updated Two" || err != nil {
		t.Fatalf("title = %q, stored %q (%v)", title, stored, err)
	}
}

func TestRefreshFollowsAMergeIntoADeletedEntry(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	e.series("l1", "s", "Local")
	if err := e.svc.Link(ctx, "s", "fake", "1", nil); err != nil {
		t.Fatal(err)
	}
	e.fake.Put("1", providertest.Payload{MergedInto: "9"})
	e.fake.Put("9", providertest.Payload{Deleted: true})
	if err := e.svc.Refresh(ctx, "s", "fake"); err != nil {
		t.Fatal(err)
	}
	if l := e.view("s").Links[0]; l.Entry.Key.ID != "9" || !l.Deleted {
		t.Fatalf("link = %+v", l)
	}
}

func found(id, title string, at time.Time) Fetched {
	key := metadata.EntryKey{Provider: "fake", ID: id}
	raw, _ := json.Marshal(providertest.Series(title, metadata.Manga))
	return Fetched{Requested: key, Hops: []string{id}, ObservedAt: at, Entry: providers.Entry{Key: key, Raw: raw}}
}

func merged(from, into string, at time.Time) Fetched {
	f := found(into, "Title "+into, at)
	f.Requested.ID, f.Hops = from, []string{from, into}
	return f
}

// publish publishes res and returns the canonical id of its first requested entry.
func (e *env) publish(res ...Fetched) string {
	e.t.Helper()
	var finals map[metadata.EntryKey]metadata.EntryKey
	err := e.svc.run(context.Background(), func(o *op) (err error) {
		finals, err = e.svc.publish(context.Background(), o, res)
		return err
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return finals[res[0].Requested].ID
}

func (e *env) entry(id string) (title string, mergedInto *string) {
	e.t.Helper()
	var raw json.RawMessage
	err := e.pool.QueryRow(context.Background(),
		"SELECT raw, merged_into FROM provider_entries WHERE external_id = $1", id).Scan(&raw, &mergedInto)
	if err != nil {
		e.t.Fatal(err)
	}
	var p providertest.Payload
	_ = json.Unmarshal(raw, &p)
	return p.Fields.Title.V, mergedInto
}

func TestPublishKeepsTheNewestObservation(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.publish(found("1", "Newer", now))
	e.publish(found("1", "Older", now.Add(-time.Minute)))
	if title, _ := e.entry("1"); title != "Newer" {
		t.Fatalf("title = %q", title)
	}
}

// Links keep the ids they recorded; readers resolve them through merges.
func TestMergesResolveOnRead(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.series("l1", "s", "Local")
	e.publish(found("A", "Old", now.Add(-time.Hour)))
	cands, _ := json.Marshal([]Candidate{{EntrySummary: EntrySummary{Key: metadata.EntryKey{Provider: "fake", ID: "A"}}}})
	e.exec(`INSERT INTO metadata_links (library_id, content_id, provider, state, candidates, rejected)
		VALUES ('l1', 's', 'fake', 'review', $1, '{A,B}')`, cands)

	e.publish(merged("A", "C", now))
	if l := e.link("s"); !slices.Equal(l.Rejected, []string{"A", "B"}) || l.Candidates[0].Key.ID != "A" || l.Rev != 1 {
		t.Fatalf("link = %+v", l)
	}
	if c := e.view("s").Links[0].Candidates; len(c) != 1 || c[0].Key.ID != "C" {
		t.Fatalf("candidates = %+v", c)
	}

	// A late fetch that still sees A cannot undo the merge, and resolves to C.
	if final := e.publish(found("A", "Revived", now.Add(time.Minute))); final != "C" {
		t.Fatalf("final = %s", final)
	}
	if _, into := e.entry("A"); into == nil || *into != "C" {
		t.Fatalf("A is no longer merged: %v", into)
	}
}

// Aliases sharing a chain resolve as one, also when the chain is redirected.
func TestAliasesResolveAlike(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.series("l1", "s", "Local")
	e.publish(merged("A", "B", now.Add(-2*time.Hour)), merged("E", "B", now.Add(-2*time.Hour)))
	e.publish(merged("B", "C", now.Add(-time.Hour)))
	e.publish(merged("F", "D", now.Add(-time.Hour)), merged("X", "D", now.Add(-time.Hour)))
	e.exec(`INSERT INTO metadata_links (library_id, content_id, provider, state, candidates) VALUES ('l1', 's', 'fake', 'review',
		'[{"key": {"provider": "fake", "id": "A"}}, {"key": {"provider": "fake", "id": "E"}}]')`)

	e.publish(merged("B", "X", now))
	keys := fp.Map([]string{"A", "E", "B", "F"}, func(id string) metadata.EntryKey { return metadata.EntryKey{Provider: "fake", ID: id} })
	finals, err := canonical(context.Background(), e.pool, keys)
	if err != nil || slices.ContainsFunc(keys, func(k metadata.EntryKey) bool { return finals[k].ID != "D" }) {
		t.Fatalf("canonical = %v (%v)", finals, err)
	}
	if c := e.view("s").Links[0].Candidates; len(c) != 1 || c[0].Key.ID != "D" {
		t.Fatalf("candidates = %+v", c)
	}
}

func TestPublishIgnoresAMergeReversal(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.publish(merged("B", "A", now))
	for range 2 {
		now = now.Add(time.Minute)
		if final := e.publish(merged("A", "B", now)); final != "A" {
			t.Fatalf("final = %s", final)
		}
	}
	if _, into := e.entry("A"); into != nil {
		t.Fatalf("A merged into %s, making a cycle", *into)
	}
}

func TestPublishResolvesAnOlderMergeThroughTheNewer(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.series("l1", "s", "Local")
	e.publish(found("A", "Old", now.Add(-time.Hour)))
	e.exec(`INSERT INTO metadata_links (library_id, content_id, provider, state, external_id, origin)
		VALUES ('l1', 's', 'fake', 'linked', 'A', 'manual')`)

	e.publish(merged("A", "C", now))
	if final := e.publish(merged("A", "B", now.Add(-time.Minute))); final != "C" {
		t.Fatalf("final = %s", final)
	}
	if l := e.view("s").Links[0]; l.Entry.Key.ID != "C" {
		t.Fatalf("link = %+v", l)
	}
}

// A publish that sees older news of a merged entry still reaches the consumers of the snapshot it
// writes.
func TestPublishRecomputesAnOlderMergesSnapshot(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.series("l1", "s", "Local")
	e.publish(found("B", "Old B", now.Add(-time.Hour)))
	e.exec(`INSERT INTO metadata_links (library_id, content_id, provider, state, external_id, origin)
		VALUES ('l1', 's', 'fake', 'linked', 'B', 'manual')`)
	e.publish(merged("A", "C", now))
	e.publish(merged("A", "B", now.Add(-time.Minute)))
	stored, err := db.SelectScalar[string](context.Background(), e.pool,
		"SELECT data->>'title' FROM content WHERE id = 's'")
	if err != nil || stored != "Title B" {
		t.Fatalf("stored title = %q (%v)", stored, err)
	}
}

func TestBuildQuery(t *testing.T) {
	e := setup(t)
	e.series("l1", "s", "Foo")
	e.exec("UPDATE content SET file_uri = '/lib/Foo (2019)' WHERE id = 's'")
	e.tx(func(tx pgx.Tx) error {
		return e.svc.store.WriteFileLayers(context.Background(), tx, []metadata.FileLayer{{ContentID: "s",
			Fields: metadata.Fields{Title: metadata.Val("Foo Alt"), AltTitles: metadata.Val([]string{"Foo"})}}}, time.Now())
	})
	for i, child := range []metadata.Fields{
		{Series: metadata.Val("Foo Alt"), PublicationDate: metadata.Val("2018-03"), Volume: metadata.Val("3"),
			Staff: metadata.Val([]metadata.Staff{{Name: "Ann Author", Role: "writer"}})},
		{Series: metadata.Val("foo"), PublicationDate: metadata.Val("2017"), Volume: metadata.Val("1.5"),
			Staff: metadata.Val([]metadata.Staff{{Name: "Author Ann", Role: "writer"}})},
	} {
		id := []string{"c1", "c2"}[i]
		e.exec(`INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, "order")
			VALUES ($1, $1, 'comic/s/' || $1, 'comic', 'l1', 's', $2)`, id, i)
		e.tx(func(tx pgx.Tx) error {
			return e.svc.store.WriteFileLayers(context.Background(), tx,
				[]metadata.FileLayer{{ContentID: id, Fields: child}}, time.Now())
		})
	}
	e.tx(func(tx pgx.Tx) error {
		target, err := e.svc.store.Lock(context.Background(), tx, "s")
		if err != nil {
			return err
		}
		return e.svc.store.SetOverrides(context.Background(), tx, target, 0, metadata.Fields{Title: metadata.Val("Bar")})
	})

	target, _ := metadata.ReadTarget(context.Background(), e.pool, "s")
	q, err := buildQuery(context.Background(), e.pool, target)
	if err != nil {
		t.Fatal(err)
	}
	want := MatchQuery{
		ContentType: "comic_series", Titles: []string{"Bar", "Foo", "Foo Alt"}, Year: new(2019), EditionYear: new(2017),
		Staff: []string{"Ann Author", "Author Ann"}, MaxVolume: new(3),
	}
	if !reflect.DeepEqual(q, want) {
		t.Fatalf("got  %+v\nwant %+v", q, want)
	}
}
