package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/config"
	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
)

type opdsFixture struct {
	LibraryID, SeriesID, Vol1, Vol2, Lantern, ListID string
}

// seedOPDS inserts a library holding the series "Moonlit Harbor" (Vol. 1 and Vol. 2, three pages
// each) and the one-page comic "Paper Lantern" at its root, plus userID's private list "Weekend
// Picks" holding the lantern.
func seedOPDS(t *testing.T, pool *pgxpool.Pool, userID string) opdsFixture {
	t.Helper()
	jpg := testJPEG(t)
	img := image.NewNRGBA(image.Rect(0, 0, 4, 2))
	for x := range 4 {
		for y := range 2 {
			img.Set(x, y, color.NRGBA{A: 255})
		}
	}
	img.Set(0, 0, color.NRGBA{}) // transparent, so the JPEG transcode has to flatten it
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	f := opdsFixture{LibraryID: models.MakeLibraryID(), SeriesID: models.MakeContentID(), Vol1: models.MakeContentID(),
		Vol2: models.MakeContentID(), Lantern: models.MakeContentID(), ListID: models.MakeCustomListID()}
	mustExec(t, pool, "INSERT INTO libraries (id, name, type) VALUES ($1, 'Test Shelf', 'comics')", f.LibraryID)

	t0 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	insert := func(id, uri, typ string, parent *string, order *int, created time.Time, pages map[string][]byte, meta string) {
		t.Helper()
		var fileURI *string
		var size *int
		fileData := "{}"
		if pages != nil {
			path := testCBZ(t, t.TempDir(), pages) // testCBZ always writes comic.cbz
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			fileURI, size = &path, new(int(info.Size()))
			var tuples [][]any
			for _, name := range slices.Sorted(maps.Keys(pages)) {
				tuples = append(tuples, []any{name})
			}
			data, _ := json.Marshal(map[string]any{"pages": tuples})
			fileData = string(data)
		}
		mustExec(t, pool, `
			INSERT INTO content (id, created_at, uri_part, uri, type, library_id, parent_id, "order", file_uri,
				file_size, file_data, data, meta_updated_at)
			VALUES ($1, $2, $3, $3, $4, $5, $6, $7, $8, $9, $10, $11, now())
		`, id, created, uri, typ, f.LibraryID, parent, order, fileURI, size, fileData, meta)
	}
	insert(f.SeriesID, "moonlit-harbor", "comic_series", nil, nil, t0, nil,
		`{"title": "Moonlit Harbor", "description": "Boats at night."}`)
	insert(f.Vol1, "moonlit-harbor/vol-1", "comic", &f.SeriesID, new(1), t0.Add(time.Hour),
		map[string][]byte{"001.jpg": jpg, "002.png": pngBuf.Bytes(), "003.jpg": jpg}, `{"title": "Vol. 1"}`)
	insert(f.Vol2, "moonlit-harbor/vol-2", "comic", &f.SeriesID, new(2), t0.Add(2*time.Hour),
		map[string][]byte{"001.jpg": jpg, "002.jpg": jpg, "003.jpg": jpg}, `{"title": "Vol. 2"}`)
	insert(f.Lantern, "paper-lantern.cbz", "comic", nil, nil, t0, map[string][]byte{"001.jpg": jpg},
		`{"title": "Paper Lantern"}`)

	mustExec(t, pool, "INSERT INTO custom_lists (id, name, visibility, user_id) VALUES ($1, 'Weekend Picks', 'private', $2)",
		f.ListID, userID)
	mustExec(t, pool, `INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri)
		VALUES ($1, $2, $3, 'paper-lantern.cbz')`, models.MakeCustomListContentID(), f.ListID, f.LibraryID)
	return f
}

type atomTestLink struct {
	Rel          string `xml:"rel,attr"`
	Href         string `xml:"href,attr"`
	Type         string `xml:"type,attr"`
	Title        string `xml:"title,attr"`
	FacetGroup   string `xml:"http://opds-spec.org/2010/catalog facetGroup,attr"`
	PSECount     string `xml:"http://vaemendis.net/opds-pse/ns count,attr"`
	LastRead     string `xml:"http://vaemendis.net/opds-pse/ns lastRead,attr"`
	LastReadDate string `xml:"http://vaemendis.net/opds-pse/ns lastReadDate,attr"`
}

type atomTestEntry struct {
	ID      string         `xml:"http://www.w3.org/2005/Atom id"`
	Title   string         `xml:"http://www.w3.org/2005/Atom title"`
	Content *string        `xml:"http://www.w3.org/2005/Atom content"`
	Links   []atomTestLink `xml:"http://www.w3.org/2005/Atom link"`
}

type atomTestFeed struct {
	ID      string          `xml:"http://www.w3.org/2005/Atom id"`
	Links   []atomTestLink  `xml:"http://www.w3.org/2005/Atom link"`
	Total   string          `xml:"http://a9.com/-/spec/opensearch/1.1/ totalResults"`
	Entries []atomTestEntry `xml:"http://www.w3.org/2005/Atom entry"`
}

func findLink(links []atomTestLink, rel string) *atomTestLink {
	for i := range links {
		if links[i].Rel == rel {
			return &links[i]
		}
	}
	return nil
}

func (f atomTestFeed) entry(title string) *atomTestEntry {
	for i := range f.Entries {
		if f.Entries[i].Title == title {
			return &f.Entries[i]
		}
	}
	return nil
}

func (f atomTestFeed) titles() []string {
	var out []string
	for _, e := range f.Entries {
		out = append(out, e.Title)
	}
	return out
}

func TestPSEMediaType(t *testing.T) {
	for _, tc := range []struct {
		pages []string
		want  string
	}{
		{[]string{"p1", "p2"}, "image/jpeg"}, // PDF
		{[]string{"A.JPG", "B.JPG"}, "image/jpeg"},
		{[]string{"a.png", "b.png"}, "image/png"},
		{[]string{"a.gif"}, "image/gif"},
		{[]string{"a.jpg", "b.png"}, "image/jpeg"},
		{[]string{"a.png", "b.WEBP"}, "image/jpeg"},
	} {
		if got := pseMediaType(tc.pages); got != tc.want {
			t.Errorf("pseMediaType(%v) = %s, want %s", tc.pages, got, tc.want)
		}
	}
}

func TestAppKeys(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	dir := t.TempDir()
	for name, body := range map[string]string{"index.html": "<!doctype html><title>spa</title>", "build.json": `{"id":"b1"}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	admin := newServerClient(t, pool, config.ProxyAuth{}, dir)
	assertEq(t, s(admin.Get("/api/info").Assert(t, 200).JSON()["web_build"]), "b1")
	assertEq(t, admin.Get("/lists").Assert(t, 200).Headers.Get("Cache-Control"), "no-cache")
	admin.Get("/assets/missing.js").Assert(t, 404)
	admin.Post("/api/auth/register", map[string]any{"username": "admin", "password": "adminpass123"}).Assert(t, 200)
	member, memberID := newMemberClient(t, admin)
	anon := admin.newSession(t)

	k := member.Post("/api/users/me/app-keys", map[string]any{"name": "Reader"}).Assert(t, 201).JSON()
	key := s(k["key"])
	feeds := k["feeds"].(map[string]any)
	assertEq(t, s(feeds["v1"]), admin.server.URL+"/opds/"+key+"/v1.2/catalog")
	assertEq(t, s(feeds["v2"]), admin.server.URL+"/opds/"+key+"/v2/catalog")
	keys := member.Get("/api/users/me/app-keys").Assert(t, 200).JSONArray()
	assertLen(t, keys, 1)
	assertEq(t, s(keys[0]["id"]), s(k["id"]))
	assertEq(t, s(keys[0]["key"]), key)
	assertEq(t, s(keys[0]["name"]), "Reader")
	assertNil(t, "last_used_at", keys[0]["last_used_at"])

	catalog := "/opds/" + key + "/v1.2/catalog"
	for _, tc := range []struct {
		name   string
		client *testClient
		path   string
		want   int
	}{
		{"valid key", anon, catalog, 200},
		{"unknown key with a session", member, "/opds/" + strings.Repeat("a", 32) + "/v1.2/catalog", 401},
		{"malformed key", anon, "/opds/short/v1.2/catalog", 401},
		{"unknown path", anon, "/opds/" + key + "/nope", 404},
		{"no key", anon, "/opds", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.client.Get(tc.path).Assert(t, tc.want)
		})
	}

	used, err := db.SelectScalar[*time.Time](ctx, pool, "SELECT last_used_at FROM app_keys WHERE key = $1", key)
	if err != nil || used == nil {
		t.Fatalf("last_used_at not set: %v", err)
	}
	admin.Delete("/api/users/me/app-keys/"+s(k["id"])).Assert(t, 404)

	member.Post("/api/auth/logout", nil).Assert(t, 200)
	anon.Get(catalog).Assert(t, 200)
	member.Post("/api/auth/login", map[string]any{"username": "member", "password": "memberpass123"}).Assert(t, 200)
	member.Delete("/api/users/me/app-keys/"+s(k["id"])).Assert(t, 200)
	anon.Get(catalog).Assert(t, 401)

	// A create that waited on the user row must not leave a key behind once its session is gone.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", memberID); err != nil {
		t.Fatal(err)
	}
	created := make(chan *response, 1)
	go func() { created <- member.Post("/api/users/me/app-keys", map[string]any{"name": "Late"}) }()
	waitBlockedOn(t, pool, "FROM users WHERE id = $1 FOR UPDATE")
	mustExec(t, pool, "DELETE FROM sessions WHERE user_id = $1", memberID)
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	(<-created).Assert(t, 401)
	late, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM app_keys WHERE user_id = $1 AND name = 'Late'", memberID)
	if err != nil {
		t.Fatal(err)
	}
	assertEq(t, late, 0)
}

func TestOPDSFeeds(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	c := newAdminClient(t, pool)
	prev := opdsPageSize
	opdsPageSize = 1
	t.Cleanup(func() { opdsPageSize = prev })

	userID, err := db.SelectScalar[string](ctx, pool, "SELECT id FROM users WHERE username = 'admin'")
	if err != nil {
		t.Fatal(err)
	}
	fx := seedOPDS(t, pool, userID)
	k := c.Post("/api/users/me/app-keys", map[string]any{"name": "Reader"}).Assert(t, 201).JSON()
	key := s(k["key"])
	prefix := c.server.URL + "/opds/" + key + "/"
	inst := c.st.String(settings.InstallationID)
	if inst == "" {
		t.Fatal("no installation id")
	}
	const (
		navType = "application/atom+xml;profile=opds-catalog;kind=navigation"
		acqType = "application/atom+xml;profile=opds-catalog;kind=acquisition"
	)

	v1 := func(path string) atomTestFeed {
		t.Helper()
		r := c.Get("/opds/"+key+"/v1.2"+path).Assert(t, 200)
		var f atomTestFeed
		if err := xml.Unmarshal(r.Body, &f); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for _, l := range slices.Concat(f.Links, slices.Concat(fp.Map(f.Entries, func(e atomTestEntry) []atomTestLink { return e.Links })...)) {
			if !strings.HasPrefix(l.Href, prefix) {
				t.Fatalf("%s: href %s is not under the key", path, l.Href)
			}
		}
		return f
	}
	v2 := func(path string) map[string]any {
		t.Helper()
		return c.Get("/opds/"+key+"/v2"+path).Assert(t, 200).JSON()
	}
	pubs := func(f map[string]any) []map[string]any {
		t.Helper()
		raw, ok := f["publications"].([]any)
		if !ok {
			t.Fatalf("no publications: %v", f)
		}
		return fp.Map(raw, func(p any) map[string]any { return p.(map[string]any) })
	}
	nav := func(f map[string]any) []map[string]any {
		t.Helper()
		raw, ok := f["navigation"].([]any)
		if !ok || f["publications"] != nil {
			t.Fatalf("not a navigation feed: %v", f)
		}
		return fp.Map(raw, func(p any) map[string]any { return p.(map[string]any) })
	}
	meta := func(v map[string]any) map[string]any { return v["metadata"].(map[string]any) }
	total := func(f map[string]any) float64 { return meta(f)["numberOfItems"].(float64) }
	hasLink := func(f map[string]any, rel string) bool {
		return slices.ContainsFunc(f["links"].([]any), func(l any) bool { return l.(map[string]any)["rel"] == rel })
	}

	// Series feed, page 1 under the default number sort.
	series := "/series/" + fx.SeriesID
	a := v1(series)
	assertEq(t, a.ID, "urn:voltis:"+inst+":series:"+fx.SeriesID)
	assertEq(t, a.Total, "2")
	if len(a.Entries) != 1 || a.Entries[0].Title != "Vol. 1" {
		t.Fatalf("page 1: %v", a.titles())
	}
	e := a.Entries[0]
	assertEq(t, e.ID, "urn:voltis:"+inst+":content:"+fx.Vol1)
	if e.Content == nil {
		t.Fatal("entry without content")
	}
	assertEq(t, findLink(e.Links, "http://opds-spec.org/acquisition").Type, "application/vnd.comicbook+zip")
	assertEq(t, findLink(e.Links, "http://vaemendis.net/opds-pse/stream").Type, "image/jpeg")
	assertEq(t, findLink(a.Links, "up").Type, navType)
	assertEq(t, findLink(a.Links, "next").Type, acqType)
	var searchTypes []string
	for _, l := range a.Links {
		if l.Rel == "search" {
			searchTypes = append(searchTypes, l.Type)
		}
	}
	assertEq(t, strings.Join(searchTypes, ","),
		"application/opensearchdescription+xml,application/atom+xml;profile=opds-catalog")
	assertEq(t, findLink(a.Links, "search").Href, prefix+"v1.2/opensearch.xml")

	j := v2(series)
	assertEq(t, total(j), 2.0)
	if !hasLink(j, "next") {
		t.Fatal("2.0: no next link")
	}
	p := pubs(j)[0]
	assertEq(t, s(meta(p)["identifier"]), e.ID)
	assertEq(t, s(meta(p)["title"]), "Vol. 1")
	assertEq(t, s(p["images"].([]any)[0].(map[string]any)["href"]), prefix+"cover/"+fx.Vol1)
	c.Get("/opds/"+key+"/v1.2"+series+"?page=99").Assert(t, 404)

	// Facets: status and sort.
	setStatus(t, c, fx.Vol1, "completed")
	a = v1(series + "?status=unread")
	assertEq(t, a.Total, "1")
	assertEq(t, a.Entries[0].Title, "Vol. 2")
	assertEq(t, v1(series + "?sort=added").Entries[0].Title, "Vol. 2")

	// Split when mixed, which a status filter must not change.
	lib := "/libraries/" + fx.LibraryID
	libID := "urn:voltis:" + inst + ":library:" + fx.LibraryID
	a = v1(lib + "?status=reading")
	assertEq(t, findLink(a.Links, "self").Type, navType)
	assertEq(t, strings.Join(a.titles(), ","), "Series (1),Comics (1)")
	assertEq(t, a.Entries[0].ID, libID+":series")
	assertEq(t, a.Entries[1].ID, libID+":items")
	if !strings.Contains(findLink(a.Entries[0].Links, "subsection").Href, "kind=series") ||
		!strings.Contains(findLink(a.Entries[1].Links, "subsection").Href, "kind=items") {
		t.Fatalf("split links: %v", a.Entries)
	}
	n := nav(v2(lib + "?status=reading"))
	assertEq(t, len(n), 2)
	assertEq(t, s(n[0]["title"]), "Series (1)")
	assertEq(t, s(n[1]["title"]), "Comics (1)")
	j = v2(lib + "?kind=items&status=reading")
	assertEq(t, total(j), 0.0)
	assertEq(t, len(pubs(j)), 0)

	a = v1(lib + "?kind=items")
	assertEq(t, a.ID, libID+":items")
	assertEq(t, findLink(a.Links, "self").Type, acqType)
	assertEq(t, a.Total, "1")
	assertEq(t, strings.Join(a.titles(), ","), "Paper Lantern")
	groups := map[string]bool{}
	for _, l := range a.Links {
		if l.Rel == "http://opds-spec.org/facet" {
			groups[l.FacetGroup] = true
			assertEq(t, l.Type, acqType)
			if !strings.Contains(l.Href, "kind=items") {
				t.Fatalf("facet link without kind: %s", l.Href)
			}
		}
	}
	if !groups["Read status"] || !groups["Sort"] {
		t.Fatalf("facet groups: %v", groups)
	}
	j = v2(lib + "?kind=items")
	assertEq(t, total(j), 1.0)
	assertEq(t, len(j["facets"].([]any)), 2)
	lantern := meta(pubs(j)[0])
	for _, k := range []string{"language", "publisher", "author", "published"} {
		if _, ok := lantern[k]; ok {
			t.Fatalf("Paper Lantern metadata has %s: %v", k, lantern)
		}
	}

	a = v1(lib + "?kind=series")
	assertEq(t, a.ID, libID+":series")
	assertEq(t, findLink(a.Links, "self").Type, navType)
	assertEq(t, a.Total, "1")
	assertEq(t, strings.Join(a.titles(), ","), "Moonlit Harbor")
	if findLink(a.Links, "http://opds-spec.org/facet") != nil {
		t.Fatal("series form has facets")
	}
	j = v2(lib + "?kind=series")
	assertEq(t, total(j), 1.0)
	assertEq(t, s(nav(j)[0]["title"]), "Moonlit Harbor")
	if _, ok := j["facets"]; ok {
		t.Fatal("2.0 series form has facets")
	}

	// Root link kinds.
	a = v1("/catalog")
	assertEq(t, a.ID, "urn:voltis:"+inst+":user:"+userID+":root")
	if findLink(a.Links, "up") != nil {
		t.Fatal("root has an up link")
	}
	for title, want := range map[string]string{"Test Shelf": navType, "Weekend Picks": acqType, "Starred": acqType} {
		assertEq(t, findLink(a.entry(title).Links, "subsection").Type, want)
	}
	a = v1("/lists/" + fx.ListID)
	assertEq(t, a.Total, "1")
	assertEq(t, strings.Join(a.titles(), ","), "Paper Lantern")
	c.Get("/opds/"+key+"/v1.2/lists/"+models.MakeCustomListID()).Assert(t, 404)
	// Vol. 1 is completed, so the series continues with Vol. 2.
	assertEq(t, strings.Join(v1("/continue").titles(), ","), "Moonlit Harbor – Vol. 2")

	// Search.
	nav(v2("/search?query=moonlit"))
	assertEq(t, findLink(v1("/search?query=moonlit").Links, "self").Type, navType)
	assertEq(t, len(pubs(v2("/search?query=zzzz"))), 0)

	// Recent, newest first.
	for i, want := range []string{"Moonlit Harbor – Vol. 2", "Moonlit Harbor – Vol. 1", "Paper Lantern"} {
		a = v1("/recent?page=" + s(i+1))
		assertEq(t, a.Total, "3")
		assertEq(t, strings.Join(a.titles(), ","), want)
	}
	if findLink(a.Links, "next") != nil {
		t.Fatal("last page has a next link")
	}
	pse := findLink(a.Entries[0].Links, "http://vaemendis.net/opds-pse/stream")
	assertEq(t, pse.PSECount, "1")
	assertEq(t, pse.LastRead+pse.LastReadDate, "")

	// OpenSearch description.
	r := c.Get("/opds/"+key+"/v1.2/opensearch.xml").Assert(t, 200)
	var osd struct {
		URL struct {
			Type     string `xml:"type,attr"`
			Template string `xml:"template,attr"`
		} `xml:"http://a9.com/-/spec/opensearch/1.1/ Url"`
	}
	if err := xml.Unmarshal(r.Body, &osd); err != nil {
		t.Fatal(err)
	}
	assertEq(t, osd.URL.Type, "application/atom+xml;profile=opds-catalog")
	assertEq(t, osd.URL.Template, prefix+"v1.2/search?query={searchTerms}")

	// PSE: a page that is not of the declared type is transcoded, with alpha flattened onto white.
	fetchPage := func(id string, page int) *response {
		return c.Get("/opds/" + key + "/pse/" + id + "/" + s(page))
	}
	r = fetchPage(fx.Vol1, 1).Assert(t, 200)
	assertEq(t, r.Headers.Get("Content-Type"), "image/jpeg")
	img, err := jpeg.Decode(bytes.NewReader(r.Body))
	if err != nil {
		t.Fatal(err)
	}
	if cr, cg, cb, _ := img.At(0, 0).RGBA(); min(cr, cg, cb)>>8 < 240 {
		t.Fatalf("pixel (0,0) is not white: %v", img.At(0, 0))
	}

	// PSE progress.
	mustExec(t, pool, "DELETE FROM user_to_content WHERE uri = 'moonlit-harbor'")
	progress := func(id string) (status string, page int, percent float64, atEnd bool) {
		t.Helper()
		err := pool.QueryRow(ctx, `
			SELECT COALESCE(utc.status, ''), (utc.progress->>'current_page')::int,
				(utc.progress->>'progress_percent')::float8, COALESCE((utc.progress->>'at_end')::bool, false)
			FROM user_to_content utc JOIN content c ON c.library_id = utc.library_id AND c.uri = utc.uri
			WHERE c.id = $1 AND utc.user_id = $2
		`, id, userID).Scan(&status, &page, &percent, &atEnd)
		if err != nil {
			t.Fatal(err)
		}
		return
	}
	assertProgress := func(id, status string, page int, percent float64, atEnd bool) {
		t.Helper()
		st, p, pc, end := progress(id)
		assertEq(t, st, status)
		assertEq(t, p, page)
		assertEq(t, pc, percent)
		assertEq(t, end, atEnd)
	}
	fetchPage(fx.Vol2, 0).Assert(t, 200)
	assertEq(t, utcCount(t, pool, fx.Vol2), 0)
	fetchPage(fx.Vol2, 1).Assert(t, 200)
	assertProgress(fx.Vol2, "reading", 1, 33.3, false) // page / pages, as the web reader shows it
	assertEq(t, readingOf(t, c, fx.SeriesID)["status"], any("reading"))
	rev, err := db.SelectScalar[bool](ctx, pool,
		"SELECT revision LIKE 'opds-' || $1 || ':%' FROM user_to_content WHERE uri = 'moonlit-harbor/vol-2'", s(k["id"]))
	if err != nil || !rev {
		t.Fatalf("PSE revision is not the key's (%v)", err)
	}
	fetchPage(fx.Vol2, 5).Assert(t, 404)
	assertProgress(fx.Vol2, "reading", 1, 33.3, false)
	setStatus(t, c, fx.Vol2, "on_hold")
	fetchPage(fx.Vol2, 2).Assert(t, 200)
	assertProgress(fx.Vol2, "completed", 2, 100, true)
	fetchPage(fx.Vol2, 1).Assert(t, 200) // a lower page is a no-op
	assertProgress(fx.Vol2, "completed", 2, 100, true)

	link := findLink(v1(series + "?page=2").Entries[0].Links, "http://vaemendis.net/opds-pse/stream")
	assertEq(t, link.LastRead, "3")
	assertEq(t, link.PSECount, "3")
	if _, err := time.Parse(time.RFC3339, link.LastReadDate); err != nil {
		t.Fatalf("lastReadDate: %v", err)
	}
	// Progress past the end, as a shorter replacement file leaves it, is clamped.
	mustExec(t, pool, `UPDATE user_to_content SET progress = progress || '{"current_page": 30}' WHERE uri = 'moonlit-harbor/vol-2'`)
	assertEq(t, findLink(v1(series + "?page=2").Entries[0].Links, "http://vaemendis.net/opds-pse/stream").LastRead, "3")

	setStatus(t, c, fx.Vol2, "reading")
	mustExec(t, pool, `UPDATE user_to_content SET progress = '{"current_page": 1}' WHERE uri = 'moonlit-harbor/vol-2'`)
	fetchPage(fx.Vol2, 2).Assert(t, 200)
	assertProgress(fx.Vol2, "completed", 2, 100, true)
	fetchPage(fx.Lantern, 0).Assert(t, 200)
	assertProgress(fx.Lantern, "completed", 0, 100, true)

	// A revoke that lands while a write waits for the library lock drops the write. Last, since it
	// revokes the key.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := db.LockMetadata(ctx, tx, fx.LibraryID); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool, `UPDATE user_to_content SET progress = '{"current_page": 1, "progress_percent": 33.3}'
		WHERE uri = 'moonlit-harbor/vol-1'`)
	fetched := make(chan *response, 1)
	go func() { fetched <- fetchPage(fx.Vol1, 2) }()
	waitBlockedOn(t, pool, "'metadata:'")
	c.Delete("/api/users/me/app-keys/"+s(k["id"])).Assert(t, 200)
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	(<-fetched).Assert(t, 200)
	assertProgress(fx.Vol1, "completed", 1, 33.3, false)
}
