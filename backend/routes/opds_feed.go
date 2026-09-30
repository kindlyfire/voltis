package routes

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"voltis/covers"
	"voltis/db"
	"voltis/metadata"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

var opdsPageSize = 50 // a var so tests can page by one

// opdsFeed is one page of a catalog, rendered by renderV1 (Atom) and renderV2 (JSON).
type opdsFeed struct {
	ID, Title string
	Updated   time.Time
	Kind      opdsKind
	Self, Up  string // Up is omitted when empty
	Entries   []opdsEntry
	Facets    []opdsFacetGroup
	Page      *opdsPage // nil: unpaged
}

type opdsKind int

const (
	kindNavigation opdsKind = iota
	kindAcquisition
)

type opdsPage struct {
	Number, Size, Total     int
	First, Prev, Next, Last string
}

type opdsFacetGroup struct {
	Title  string
	Facets []opdsFacet
}

type opdsFacet struct {
	Title, Href string
	Active      bool
}

type opdsEntry struct {
	ID, Title                   string
	Updated                     time.Time
	Summary                     string
	Content                     string // Atom <content>: the description, else a count or the title
	Staff                       []metadata.Staff
	Publisher, Language, Issued string
	Genres                      []string // slugs
	ContentID                   string   // "" for entries without a content row
	Cover                       string   // own or inherited cover URL, "" when none
	Nav                         *opdsNav
	Acq                         *opdsAcq
	PSE                         *opdsPSE
}

type opdsNav struct {
	Href  string
	Count *int
	Kind  opdsKind
}

type opdsAcq struct {
	Href, Type string
	Size       *int
}

type opdsPSE struct {
	Template, Type string
	Count          int
	LastRead       int // 1-based; 0 when there is no progress
	LastReadAt     *time.Time
}

type opdsLinks struct{ Prefix, V string }

// feed links to a feed of this version.
func (l opdsLinks) feed(path string, params url.Values) string {
	u := l.Prefix + "/" + l.V + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	return u
}

// bin links to a version-free route (covers, files).
func (l opdsLinks) bin(path string) string { return l.Prefix + path }

type opdsQuery struct {
	Kind   string `query:"kind"   validate:"omitempty,oneof=series items"`
	Page   int    `query:"page"   validate:"min=1" default:"1"`
	Status string `query:"status" validate:"oneof=all unread reading completed" default:"all"`
	Sort   string `query:"sort"   validate:"omitempty,oneof=title added released number"`
	Query  string `query:"query"  validate:"max=200"`
}

// bindOPDSQuery binds the params, allowing only the given sorts when there are any.
func bindOPDSQuery(c echo.Context, sorts ...string) (opdsQuery, error) {
	q, err := BindQuery[opdsQuery](c)
	if err != nil {
		return q, err
	}
	if len(sorts) > 0 {
		if q.Sort != "" && !slices.Contains(sorts, q.Sort) {
			return q, echo.NewHTTPError(http.StatusBadRequest, "invalid sort")
		}
	}
	return q, nil
}

// values holds the params that shape a feed; links add the page.
func (q opdsQuery) values() url.Values {
	v := url.Values{}
	for k, s := range map[string]string{"query": q.Query, "kind": q.Kind, "sort": q.Sort} {
		if s != "" {
			v.Set(k, s)
		}
	}
	if q.Status != "all" {
		v.Set("status", q.Status)
	}
	return v
}

func withPage(params url.Values, page int) url.Values {
	p := maps.Clone(params)
	if p == nil {
		p = url.Values{}
	}
	if page > 1 {
		p.Set("page", strconv.Itoa(page))
	}
	return p
}

var opdsSorts = map[string]struct{ sort, order, title string }{
	"title":    {"title", "asc", "Title"},
	"added":    {"created_at", "desc", "Recently added"},
	"released": {"release_date", "desc", "Release date"},
	"number":   {"order", "asc", "Number"},
}

var opdsStatuses = []struct{ value, title string }{
	{"all", "All"}, {"unread", "Unread"}, {"reading", "Reading"}, {"completed", "Completed"},
}

var itemTypes = []string{"comic", "book"}

// applySort sets both Sort and SortOrder: the `default:"desc"` tag only applies to bound queries.
// An empty sort keeps the query's own order (list order, or relevance for a search).
func applySort(f *contentListQuery, sort string) {
	if s, ok := opdsSorts[sort]; ok {
		f.Sort, f.SortOrder = s.sort, s.order
	}
}

func applyStatus(f *contentListQuery, status string) {
	switch status {
	case "unread":
		f.HasStatus = "false"
	case "reading", "completed":
		f.ReadingStatus = status
	}
}

// opdsFacets builds the read-status and sort facets of an item feed. A facet link keeps the other
// facet and the kind in params, and drops the page.
func opdsFacets(l opdsLinks, path string, params url.Values, status, sort string, sorts []string) []opdsFacetGroup {
	link := func(key, value string) string {
		p := maps.Clone(params)
		p.Set(key, value)
		if key == "status" && value == "all" {
			p.Del("status")
		}
		return l.feed(path, p)
	}
	statuses := opdsFacetGroup{Title: "Read status"}
	for _, s := range opdsStatuses {
		statuses.Facets = append(statuses.Facets, opdsFacet{Title: s.title, Href: link("status", s.value), Active: s.value == status})
	}
	sortGroup := opdsFacetGroup{Title: "Sort"}
	for _, s := range sorts {
		sortGroup.Facets = append(sortGroup.Facets, opdsFacet{Title: opdsSorts[s].title, Href: link("sort", s), Active: s == sort})
	}
	return []opdsFacetGroup{statuses, sortGroup}
}

// opdsID builds a globally unique Atom ID. IDs never contain keys or page numbers.
func (o *OPDSRoutes) id(parts ...string) string {
	return "urn:voltis:" + o.st.String(settings.InstallationID) + ":" + strings.Join(parts, ":")
}

// Feed handlers

func (o *OPDSRoutes) root(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	ctx := reqCtx(c)
	kinds, err := rootKinds(ctx, o.pool, user.ID)
	if err != nil {
		return opdsFeed{}, err
	}
	type named struct{ ID, Name string }
	libs, err := db.Select[named](ctx, o.pool, "SELECT id, name FROM libraries ORDER BY name, id")
	if err != nil {
		return opdsFeed{}, err
	}
	lists, err := db.Select[named](ctx, o.pool,
		"SELECT id, name FROM custom_lists WHERE user_id = $1 ORDER BY created_at DESC, id", user.ID)
	if err != nil {
		return opdsFeed{}, err
	}

	now := time.Now()
	nav := func(id, title, path string, kind opdsKind, count *int) opdsEntry {
		content := title
		if count != nil {
			content = itemCount(*count)
		}
		return opdsEntry{ID: id, Title: title, Updated: now, Content: content,
			Nav: &opdsNav{Href: l.feed(path, nil), Kind: kind, Count: count}}
	}
	counted := func(key string) (opdsKind, *int) {
		k := kinds[key]
		return targetKind(k), new(k.Series + k.items())
	}
	starredKind, _ := counted("starred")
	entries := []opdsEntry{
		nav(o.id("user", user.ID, "continue"), "Continue reading", "/continue", kindAcquisition, nil),
		nav(o.id("recent"), "Recently added", "/recent", kindAcquisition, nil),
		nav(o.id("user", user.ID, "starred"), "Starred", "/starred", starredKind, nil),
	}
	for _, lib := range libs {
		kind, n := counted("library:" + lib.ID)
		entries = append(entries, nav(o.id("library", lib.ID), lib.Name, "/libraries/"+lib.ID, kind, n))
	}
	for _, list := range lists {
		kind, n := counted("list:" + list.ID)
		entries = append(entries, nav(o.id("user", user.ID, "list", list.ID), list.Name, "/lists/"+list.ID, kind, n))
	}
	return opdsFeed{ID: o.id("user", user.ID, "root"), Title: "Voltis", Updated: now, Kind: kindNavigation,
		Self: l.feed("/catalog", nil), Entries: entries}, nil
}

func (o *OPDSRoutes) library(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	sorts := []string{"title", "added", "released"}
	q, err := bindOPDSQuery(c, sorts...)
	if err != nil {
		return opdsFeed{}, err
	}
	var name string
	err = o.pool.QueryRow(reqCtx(c), "SELECT name FROM libraries WHERE id = $1", c.Param("library_id")).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return opdsFeed{}, echo.NewHTTPError(http.StatusNotFound, "Library not found")
	}
	if err != nil {
		return opdsFeed{}, err
	}
	id := c.Param("library_id")
	sort := cmp.Or(q.Sort, "title")
	return o.mixable(c, user, l, q, contentListQuery{LibraryID: id, ParentID: "null"}, itemFilters{q.Status, sort}, feedMeta{
		path: "/libraries/" + id, id: o.id("library", id), title: name, up: l.feed("/catalog", nil), seriesSort: "title",
		facetSorts: sorts,
	})
}

func (o *OPDSRoutes) series(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	sorts := []string{"number", "added"}
	q, err := bindOPDSQuery(c, sorts...)
	if err != nil {
		return opdsFeed{}, err
	}
	ctx := reqCtx(c)
	rows, err := selectContentRows(ctx, o.pool, user.ID, []string{c.Param("content_id")})
	if err != nil {
		return opdsFeed{}, err
	}
	r, ok := rows[c.Param("content_id")]
	if !ok || !r.Valid || !slices.Contains(metadata.SeriesTypes, r.Type) {
		return opdsFeed{}, echo.NewHTTPError(http.StatusNotFound, "Series not found")
	}
	var m metadata.Fields
	_ = json.Unmarshal(r.MetaData, &m)

	sort := cmp.Or(q.Sort, "number")
	f := contentListQuery{ParentID: r.ID}
	applyStatus(&f, q.Status)
	applySort(&f, sort)
	path := "/series/" + r.ID
	params := q.values()
	feed := opdsFeed{ID: o.id("series", r.ID), Title: cmp.Or(m.Title.V, r.URIPart), Kind: kindAcquisition,
		Self: l.feed(path, withPage(params, q.Page)), Up: l.feed("/libraries/"+r.LibraryID, nil),
		Facets: opdsFacets(l, path, params, q.Status, sort, sorts)}
	err = o.fillPage(ctx, user.ID, l, &feed, path, params, q.Page, f, nil, false)
	return feed, err
}

func (o *OPDSRoutes) continueFeed(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	picks, err := continueReading(reqCtx(c), o.pool, user, 50)
	if err != nil {
		return opdsFeed{}, err
	}
	var entries []opdsEntry
	for _, p := range picks {
		if e, ok := o.entryFor(p.Item, p.Series, l, true); ok {
			entries = append(entries, e)
		}
	}
	return opdsFeed{ID: o.id("user", user.ID, "continue"), Title: "Continue reading", Kind: kindAcquisition,
		Self: l.feed("/continue", nil), Up: l.feed("/catalog", nil), Entries: entries, Updated: latest(entries)}, nil
}

func (o *OPDSRoutes) recent(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	q, err := bindOPDSQuery(c)
	if err != nil {
		return opdsFeed{}, err
	}
	f := contentListQuery{Type: itemTypes}
	applySort(&f, "added")
	feed := opdsFeed{ID: o.id("recent"), Title: "Recently added", Kind: kindAcquisition,
		Self: l.feed("/recent", withPage(nil, q.Page)), Up: l.feed("/catalog", nil)}
	err = o.fillPage(reqCtx(c), user.ID, l, &feed, "/recent", nil, q.Page, f, nil, true)
	return feed, err
}

func (o *OPDSRoutes) starred(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	q, err := bindOPDSQuery(c)
	if err != nil {
		return opdsFeed{}, err
	}
	return o.mixable(c, user, l, q, contentListQuery{Starred: "true"}, itemFilters{"all", "title"}, feedMeta{
		path: "/starred", id: o.id("user", user.ID, "starred"), title: "Starred", up: l.feed("/catalog", nil),
		crossSeries: true, seriesSort: "title",
	})
}

func (o *OPDSRoutes) list(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	q, err := bindOPDSQuery(c)
	if err != nil {
		return opdsFeed{}, err
	}
	id := c.Param("list_id")
	var name string
	err = o.pool.QueryRow(reqCtx(c), "SELECT name FROM custom_lists WHERE id = $1 AND user_id = $2", id, user.ID).
		Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) {
		return opdsFeed{}, echo.NewHTTPError(http.StatusNotFound, "List not found")
	}
	if err != nil {
		return opdsFeed{}, err
	}
	return o.mixable(c, user, l, q, contentListQuery{ListID: id}, itemFilters{"all", ""}, feedMeta{
		path: "/lists/" + id, id: o.id("user", user.ID, "list", id), title: name, up: l.feed("/catalog", nil),
		crossSeries: true,
	})
}

func (o *OPDSRoutes) search(c echo.Context, user *models.User, l opdsLinks) (opdsFeed, error) {
	q, err := bindOPDSQuery(c)
	if err != nil {
		return opdsFeed{}, err
	}
	if strings.TrimSpace(q.Query) == "" {
		return opdsFeed{}, echo.NewHTTPError(http.StatusBadRequest, "query is required")
	}
	return o.mixable(c, user, l, q, contentListQuery{Search: q.Query}, itemFilters{"all", ""}, feedMeta{
		path: "/search", id: o.id("user", user.ID, "search", url.QueryEscape(q.Query)),
		title: "Search: " + q.Query, up: l.feed("/catalog", nil), crossSeries: true,
	})
}

// Split when mixed

// feedMeta is the identity of a feed that may hold both series and items.
type feedMeta struct {
	path, id, title, up string
	crossSeries         bool     // prefix item titles with their series title
	facetSorts          []string // the item form's sort facets; nil for no facets
	seriesSort          string   // fixed order of the series form
}

// itemFilters apply only once the kind resolves to items.
type itemFilters struct{ Status, Sort string }

// mixable renders a feed whose rows may be series, items or both; a mixed result without a
// requested kind becomes a navigation feed linking to the two pure sub-feeds. Resolve kind before
// applying item filters, so facets can't change the split.
func (o *OPDSRoutes) mixable(c echo.Context, user *models.User, l opdsLinks, q opdsQuery, base contentListQuery, items itemFilters, meta feedMeta) (opdsFeed, error) {
	ctx := reqCtx(c)
	params := q.values()
	self := l.feed(meta.path, withPage(params, q.Page))

	kind := q.Kind
	var counts *kindCounts
	if kind == "" {
		k, err := countContentKinds(ctx, o.pool, user.ID, base.filter())
		if err != nil {
			return opdsFeed{}, err
		}
		switch {
		case k.Series > 0 && k.items() > 0:
			return splitFeed(l, meta, params, self, k), nil
		case k.Series > 0:
			kind = "series"
		case k.items() > 0:
			kind = "items"
		}
		counts = &k // one kind or none: the sum is also the total of the pure feed
	}

	id := meta.id
	if q.Kind != "" {
		id += ":" + q.Kind
	}
	feed := opdsFeed{ID: id, Title: meta.title, Self: self, Up: meta.up, Kind: kindAcquisition}
	f := base
	if kind != "" {
		params.Set("kind", kind) // facet and paging links keep the resolved kind
	}
	switch kind {
	case "series":
		feed.Kind = kindNavigation
		f.Type = metadata.SeriesTypes
		applySort(&f, meta.seriesSort)
		params.Del("status")
		params.Del("sort")
	case "items":
		f.Type = itemTypes
		applyStatus(&f, items.Status)
		applySort(&f, items.Sort)
		if items.Status != "all" {
			counts = nil // the status narrows the rows: count again
		}
		if meta.facetSorts != nil {
			feed.Facets = opdsFacets(l, meta.path, params, items.Status, items.Sort, meta.facetSorts)
		}
	}
	err := o.fillPage(ctx, user.ID, l, &feed, meta.path, params, q.Page, f, counts, meta.crossSeries)
	return feed, err
}

func splitFeed(l opdsLinks, meta feedMeta, params url.Values, self string, k kindCounts) opdsFeed {
	now := time.Now()
	label := "Items"
	if k.Books == 0 {
		label = "Comics"
	} else if k.Comics == 0 {
		label = "Books"
	}
	entry := func(kind, title string, n int, navKind opdsKind, p url.Values) opdsEntry {
		p.Set("kind", kind)
		return opdsEntry{ID: meta.id + ":" + kind, Title: title, Updated: now, Content: title,
			Nav: &opdsNav{Href: l.feed(meta.path, p), Count: &n, Kind: navKind}}
	}
	seriesParams := maps.Clone(params) // the series form ignores item facets
	seriesParams.Del("status")
	seriesParams.Del("sort")
	return opdsFeed{ID: meta.id, Title: meta.title, Self: self, Up: meta.up, Updated: now, Kind: kindNavigation,
		Entries: []opdsEntry{
			entry("series", fmt.Sprintf("Series (%d)", k.Series), k.Series, kindNavigation, seriesParams),
			entry("items", fmt.Sprintf("%s (%d)", label, k.items()), k.items(), kindAcquisition, maps.Clone(params)),
		}}
}

// fillPage runs one page of f into feed, with paging links under path and params. A page past the
// last is a 404, and an empty result renders as acquisition, so a navigation feed is never empty.
func (o *OPDSRoutes) fillPage(ctx context.Context, userID string, l opdsLinks, feed *opdsFeed, path string,
	params url.Values, page int, f contentListQuery, counts *kindCounts, crossSeries bool,
) error {
	size := opdsPageSize
	f.Limit, f.Offset = &size, (page-1)*size
	rows, total, err := queryContent(ctx, o.pool, userID, f, counts)
	if err != nil {
		return err
	}
	last := max(1, (total+size-1)/size)
	if page > last {
		return echo.NewHTTPError(http.StatusNotFound, "Page not found")
	}
	if total == 0 {
		feed.Kind = kindAcquisition // 2.0 then emits "publications": []
	}
	if feed.Entries, err = o.entries(ctx, userID, l, rows, crossSeries); err != nil {
		return err
	}
	feed.Updated = latest(feed.Entries)
	link := func(n int) string { return l.feed(path, withPage(params, n)) }
	feed.Page = &opdsPage{Number: page, Size: size, Total: total, First: link(1), Last: link(last)}
	if page > 1 {
		feed.Page.Prev = link(page - 1)
	}
	if page < last {
		feed.Page.Next = link(page + 1)
	}
	return nil
}

func latest(entries []opdsEntry) time.Time {
	var t time.Time
	for _, e := range entries {
		if e.Updated.After(t) {
			t = e.Updated
		}
	}
	if t.IsZero() {
		return time.Now()
	}
	return t
}

// rootKinds counts the series, comics and books each root target holds, with the default filters
// of its feed, keyed "library:<id>", "list:<id>" and "starred".
func rootKinds(ctx context.Context, q db.Querier, userID string) (map[string]kindCounts, error) {
	rows, err := db.Select[struct {
		Key string
		kindCounts
	}](ctx, q, `
		SELECT 'library:' || c.library_id AS key, `+kindCountColumns+` FROM content c
		WHERE c.valid AND c.parent_id IS NULL
		GROUP BY c.library_id
		UNION ALL
		SELECT 'list:' || clc.custom_list_id, `+kindCountColumns+` FROM custom_list_to_content clc
		JOIN custom_lists cl ON cl.id = clc.custom_list_id AND cl.user_id = $1
		JOIN content c ON c.library_id = clc.library_id AND c.uri = clc.uri
		WHERE c.valid
		GROUP BY clc.custom_list_id
		UNION ALL
		SELECT 'starred', `+kindCountColumns+` FROM content c
		JOIN user_to_content utc ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $1
		WHERE c.valid AND utc.starred
	`, userID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]kindCounts, len(rows))
	for _, r := range rows {
		out[r.Key] = r.kindCounts
	}
	return out, nil
}

// targetKind is the kind of feed a mixable target renders with these counts: a split feed or a
// series form are navigation; items or nothing are acquisition.
func targetKind(k kindCounts) opdsKind {
	if k.Series > 0 {
		return kindNavigation
	}
	return kindAcquisition
}

// Entries

func (o *OPDSRoutes) entries(ctx context.Context, userID string, l opdsLinks, rows []contentListRow, crossSeries bool) ([]opdsEntry, error) {
	var parentIDs []string
	for _, r := range rows {
		if r.ParentID != nil {
			parentIDs = append(parentIDs, *r.ParentID)
		}
	}
	parents := map[string]contentListRow{}
	if len(parentIDs) > 0 {
		var err error
		if parents, err = selectContentRows(ctx, o.pool, userID, parentIDs); err != nil {
			return nil, err
		}
	}
	out := make([]opdsEntry, 0, len(rows))
	for _, r := range rows {
		var parent *contentListRow
		if r.ParentID != nil {
			if p, ok := parents[*r.ParentID]; ok {
				parent = &p
			}
		}
		if e, ok := o.entryFor(r, parent, l, crossSeries); ok {
			out = append(out, e)
		}
	}
	return out, nil
}

// entryFor maps a content row to an entry; items without a file have none. Volumes often carry
// little metadata of their own, so an item falls back to its series field by field.
func (o *OPDSRoutes) entryFor(r contentListRow, parent *contentListRow, l opdsLinks, crossSeries bool) (opdsEntry, bool) {
	var m, pm metadata.Fields
	_ = json.Unmarshal(r.MetaData, &m)
	if parent != nil {
		_ = json.Unmarshal(parent.MetaData, &pm)
	}
	inherited := false
	staff := orParent(m.Staff, pm.Staff, &inherited)
	publishers := orParent(m.Publishers, pm.Publishers, &inherited)
	desc := orParent(m.Description, pm.Description, &inherited)

	e := opdsEntry{
		ID:        o.id("content", r.ID),
		ContentID: r.ID,
		Title:     cmp.Or(m.Title.V, r.URIPart),
		Summary:   desc,
		Staff:     staff,
		Language:  orParent(m.Language, pm.Language, &inherited),
		Genres:    orParent(m.Genres, pm.Genres, &inherited),
		Issued:    m.PublicationDate.V,
		Cover:     opdsCoverURL(l, r.Content, m),
	}
	if crossSeries && parent != nil {
		e.Title = cmp.Or(pm.Title.V, parent.URIPart) + " – " + e.Title
		inherited = true
	}
	if e.Cover == "" && parent != nil {
		if e.Cover = opdsCoverURL(l, parent.Content, pm); e.Cover != "" {
			inherited = true
		}
	}
	if len(publishers) > 0 {
		e.Publisher = publishers[0]
	}
	e.Updated = r.UpdatedAt
	if t := r.MetaUpdatedAt; t != nil && t.After(e.Updated) {
		e.Updated = *t
	}
	if inherited && parent != nil && parent.MetaUpdatedAt != nil && parent.MetaUpdatedAt.After(e.Updated) {
		e.Updated = *parent.MetaUpdatedAt
	}

	isSeries := slices.Contains(metadata.SeriesTypes, r.Type)
	e.Content = desc
	if e.Content == "" {
		e.Content = e.Title
		if isSeries && r.ChildrenCount != nil {
			e.Content = itemCount(*r.ChildrenCount)
		}
	}

	if isSeries {
		e.Nav = &opdsNav{Href: l.feed("/series/"+r.ID, nil), Count: r.ChildrenCount, Kind: kindAcquisition}
		return e, true
	}
	if r.FileURI == nil {
		return e, false
	}
	e.Acq = &opdsAcq{Href: l.bin("/file/" + r.ID + "/" + url.PathEscape(filepath.Base(*r.FileURI))),
		Type: opdsMediaType(*r.FileURI), Size: r.FileSize}
	if r.Type != "comic" || l.V != "v1.2" { // PSE is a 1.2 extension
		return e, true
	}
	if names, err := comicPageNames(r.FileData); err == nil && len(names) > 0 {
		e.PSE = &opdsPSE{Template: l.bin("/pse/"+r.ID) + "/{pageNumber}", Type: pseMediaType(names),
			Count: len(names)}
		// Read directly: r.utc() is nil without a user row.
		if e.PSE.LastRead = opdsLastRead(r.UTCProgress, len(names)); e.PSE.LastRead > 0 {
			e.PSE.LastReadAt = r.UTCProgressUpdatedAt
		}
	}
	return e, true
}

func orParent[T any](own, parent metadata.Opt[T], inherited *bool) T {
	if v, ok := own.Get(); ok {
		return v
	}
	if v, ok := parent.Get(); ok {
		*inherited = true
		return v
	}
	var zero T
	return zero
}

// opdsCoverURL is the versioned cover URL of content, as contentToDTO versions it, or "".
func opdsCoverURL(l opdsLinks, c models.Content, m metadata.Fields) string {
	var ref *metadata.CoverRef
	if v, ok := m.Cover.Get(); ok {
		ref = &v
	}
	v := covers.Version(ref, c.CoverURI != nil, c.FileMtime)
	if v == nil {
		return ""
	}
	return l.bin("/cover/"+c.ID) + "?v=" + *v
}

// opdsLastRead is PSE's 1-based lastRead from the 0-based current_page, or 0 without one. It is
// at most pages, since a rescan can replace the file with a shorter one.
func opdsLastRead(progress []byte, pages int) int {
	var p struct {
		CurrentPage *float64 `json:"current_page"`
	}
	if json.Unmarshal(progress, &p) != nil || p.CurrentPage == nil || *p.CurrentPage < 0 {
		return 0
	}
	if *p.CurrentPage >= float64(pages) {
		return pages
	}
	return int(*p.CurrentPage) + 1
}

func itemCount(n int) string {
	if n == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", n)
}

// slugTitle labels a genre slug: "slice_of_life" → "Slice Of Life".
func slugTitle(slug string) string {
	words := strings.Split(slug, "_")
	for i, w := range words {
		if r, n := utf8.DecodeRuneInString(w); n > 0 {
			words[i] = string(unicode.ToUpper(r)) + w[n:]
		}
	}
	return strings.Join(words, " ")
}
