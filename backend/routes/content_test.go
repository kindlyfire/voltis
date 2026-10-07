package routes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/lib/comic"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestContent(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	libID := models.MakeLibraryID()
	if _, err := pool.Exec(ctx,
		"INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", libID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	contentID := models.MakeContentID()
	if _, err := pool.Exec(ctx, `
		INSERT INTO content (id, uri_part, uri, type, library_id)
		VALUES ($1, $1, 'file:///lib/' || $1, 'comic', $2)
	`, contentID, libID); err != nil {
		t.Fatalf("insert content: %v", err)
	}

	return contentID
}

func utcCount(t *testing.T, pool *pgxpool.Pool, contentID string) int {
	t.Helper()
	n, err := db.SelectScalar[int](context.Background(), pool, `
		SELECT COUNT(*) FROM user_to_content
		WHERE uri = (SELECT uri FROM content WHERE id = $1)
	`, contentID)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func assertNil(t *testing.T, name string, v any) {
	t.Helper()
	if v != nil {
		t.Fatalf("expected %s to be null, got %v", name, v)
	}
}

func assertNotNil(t *testing.T, name string, v any) {
	t.Helper()
	if v == nil {
		t.Fatalf("expected %s to be set", name)
	}
}

func TestUserData(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	post := func(t *testing.T, id string, body map[string]any, code int) map[string]any {
		t.Helper()
		res := c.Post("/api/content/"+id+"/user-data", body).Assert(t, code)
		if code != 200 {
			return nil
		}
		return res.JSON()
	}

	t.Run("status and progress go through reading", func(t *testing.T) {
		id := newTestContent(t, pool)
		for _, body := range []map[string]any{
			{"status": "reading"}, {"status": nil}, {"progress": map[string]any{"current_page": 3}}, {"progress": nil},
		} {
			post(t, id, body, 400)
		}
		assertEq(t, utcCount(t, pool, id), 0)
	})

	t.Run("empty body", func(t *testing.T) {
		id := newTestContent(t, pool)
		res := post(t, id, map[string]any{}, 200)
		assertEq(t, s(res["starred"]), "false")
		assertEq(t, len(res["progress"].(map[string]any)), 0)
		assertNil(t, "status", res["status"])
		assertEq(t, utcCount(t, pool, id), 1)

		post(t, id, map[string]any{"rating": 8}, 200)

		res = post(t, id, map[string]any{}, 200)
		assertEq(t, s(res["rating"]), "8")
		assertEq(t, utcCount(t, pool, id), 1)
	})

	t.Run("clear fields", func(t *testing.T) {
		id := newTestContent(t, pool)
		post(t, id, map[string]any{"notes": "hello", "rating": 8}, 200)

		res := post(t, id, map[string]any{"notes": nil, "rating": nil}, 200)
		assertNil(t, "notes", res["notes"])
		assertNil(t, "rating", res["rating"])
	})

	t.Run("partial update keeps the reading state", func(t *testing.T) {
		id := newTestContent(t, pool)
		setStatus(t, c, id, "reading")
		first := post(t, id, map[string]any{"rating": 8, "starred": true}, 200)
		assertNotNil(t, "revision", first["revision"])

		res := post(t, id, map[string]any{"notes": "hello"}, 200)
		assertEq(t, s(res["notes"]), "hello")
		assertEq(t, s(res["status"]), "reading")
		assertEq(t, s(res["rating"]), "8")
		assertEq(t, s(res["starred"]), "true")
		assertEq(t, s(res["status_updated_at"]), s(first["status_updated_at"]))
		assertEq(t, s(res["revision"]), s(first["revision"]))
	})

	t.Run("length", func(t *testing.T) {
		ctx := context.Background()
		libID := models.MakeLibraryID()
		if _, err := pool.Exec(ctx,
			"INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", libID); err != nil {
			t.Fatalf("insert library: %v", err)
		}
		insert := func(typ string, parentID *string, words, pages *int) string {
			id := models.MakeContentID()
			if _, err := pool.Exec(ctx, `
				INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, word_count, page_count)
				VALUES ($1, $1, 'file:///lib/' || $1, $2, $3, $4, $5, $6)
			`, id, typ, libID, parentID, words, pages); err != nil {
				t.Fatalf("insert content: %v", err)
			}
			return id
		}
		length := func(id string) map[string]any {
			l, _ := c.Get("/api/content/"+id).Assert(t, 200).JSON()["length"].(map[string]any)
			return l
		}

		comics := insert("comic_series", nil, nil, nil)
		comic1 := insert("comic", &comics, nil, new(10))
		comic2 := insert("comic", &comics, nil, new(20))
		comic3 := insert("comic", &comics, nil, new(30))
		books := insert("book_series", nil, nil, nil)
		insert("book", &books, new(100), nil)
		insert("book", &books, nil, nil)
		book := insert("book", nil, new(200), nil)

		setStatus(t, c, comic1, "completed")
		for id, progress := range map[string]string{
			comic2: `{"current_page": 5}`, comic3: `{"current_page": 0, "progress_percent": 10}`,
			book: `{"progress_percent": 50}`,
		} {
			setStatus(t, c, id, "reading")
			mustExec(t, pool, `UPDATE user_to_content SET progress = $1
				WHERE uri = (SELECT uri FROM content WHERE id = $2)`, progress, id)
		}

		want := func(l map[string]any, unit string, total, remaining int) {
			t.Helper()
			assertEq(t, s(l), s(map[string]any{"unit": unit, "total": total, "remaining": remaining}))
		}
		want(length(comics), "pages", 60, 45)
		want(length(comic3), "pages", 30, 30)
		want(length(book), "words", 200, 100)
		if l := length(books); l != nil {
			t.Errorf("book series length = %v, want none with an uncounted child", l)
		}
		for _, status := range []string{"completed", "dropped"} {
			setStatus(t, c, comics, status)
			want(length(comics), "pages", 60, 0)
		}
	})
}

func TestContentListWindow(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	libID := models.MakeLibraryID()
	mustExec(t, pool, "INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", libID)

	// An empty title is a row without metadata. A year-only date broke the old `::date` cast.
	var echoID string
	for _, it := range []struct {
		title, date, rating string
		year                int
	}{
		{"Vol 10", "2015-03-01", "4.5", 2020},
		{"Émile and the Kite", "2014", "3", 2021},
		{"", "", "", 2022},
		{"Echo Park", "2014-06-15T00:00:00Z", "", 2023},
		{"Amber Road", "", "4.5", 2020},
		{"3 Tales", "1999-12", "10", 2021},
		{"Vol 2", "2015", "3", 2022},
		{"かたな", "", "", 2023},
		{`"Quiet Hours"`, "2014", "0.5", 2020},
		{"Amber Road", "1999", "4.5", 2021},
		{"Zephyr", "", "", 2022},
		{"Ezra Vale", "2015-01", "7.25", 2023},
		{"Moss", "", "3", 2020},
		{"Amber Road", "2014", "", 2021},
	} {
		id := models.MakeContentID()
		mustExec(t, pool, `
			INSERT INTO content (id, uri_part, uri, type, library_id, created_at)
			VALUES ($1, $1, 'file:///lib/' || $1, 'comic_series', $2, make_timestamptz($3, 6, 1, 0, 0, 0, 'UTC'))
		`, id, libID, it.year)
		if it.title != "" {
			mustExec(t, pool, `
				UPDATE content SET data = jsonb_strip_nulls(jsonb_build_object('title', $2::text,
					'publication_date', NULLIF($3, ''), 'rating', NULLIF($4, '')::numeric))
				WHERE id = $1
			`, id, it.title, it.date, it.rating)
		}
		if it.title == "Echo Park" {
			echoID = id
		}
	}

	dataIDs := func(res map[string]any) []string {
		var ids []string
		for _, item := range res["data"].([]any) {
			ids = append(ids, s(item.(map[string]any)["id"]))
		}
		return ids
	}

	// Search matches alternative titles too.
	mustExec(t, pool, `UPDATE content SET data = data || '{"alt_titles": ["Lantern Quay"]}' WHERE id = $1`, echoID)
	for _, q := range []string{"lantern", "quay"} {
		res := c.Get("/api/content?library_id="+libID+"&search="+q).Assert(t, 200).JSON()
		assertEq(t, s(dataIDs(res)), s([]string{echoID}))
		assertEq(t, res["data"].([]any)[0].(map[string]any)["uri"], any("file:///lib/"+echoID))
	}

	// Keys and values are listed ascending; "" is the null key. value reads an item's sort value.
	for _, tc := range []struct {
		sort   string
		value  func(item map[string]any) string
		keys   []string
		values []string
	}{
		{"title", func(item map[string]any) string { return s(item["title"]) },
			[]string{"#", "a", "e", "m", "q", "v", "z"}, []string{
				"", "3 Tales", "かたな", "Amber Road", "Amber Road", "Amber Road", "Echo Park",
				"Émile and the Kite", "Ezra Vale", "Moss", `"Quiet Hours"`, "Vol 2", "Vol 10", "Zephyr",
			}},
		{"created_at", func(item map[string]any) string { return s(item["created_at"]) },
			[]string{"2020", "2021", "2022", "2023"}, nil},
		{"release_date", func(item map[string]any) string {
			date, _ := item["meta"].(map[string]any)["publication_date"].(string)
			return date
		}, []string{"", "1999", "2014", "2015"}, []string{
			"", "", "", "", "", "1999", "1999-12", "2014", "2014", "2014", "2014-06-15T00:00:00Z",
			"2015", "2015-01", "2015-03-01",
		}},
		{"rating", func(item map[string]any) string {
			if rating, ok := item["meta"].(map[string]any)["rating"]; ok {
				return s(rating)
			}
			return ""
		}, nil, []string{"", "", "", "", "", "0.5", "3", "3", "3", "4.5", "4.5", "4.5", "7.25", "10"}},
	} {
		for _, dir := range []string{"asc", "desc"} {
			t.Run(tc.sort+" "+dir, func(t *testing.T) {
				keys, values := slices.Clone(tc.keys), slices.Clone(tc.values)
				if dir == "desc" {
					slices.Reverse(keys)
					slices.Reverse(values)
				}
				query := "?library_id=" + libID + "&sort=" + tc.sort + "&sort_order=" + dir

				full := c.Get("/api/content"+query+"&include=meta").Assert(t, 200).JSON()
				all := dataIDs(full)
				assertEq(t, s(full["total"]), s(len(all)))
				var got []string
				for i, item := range full["data"].([]any) {
					got = append(got, tc.value(item.(map[string]any)))
					// Equal sort values are ordered by id (under the database collation), in the
					// list's direction.
					if i > 0 && got[i] == got[i-1] {
						less, err := db.SelectScalar[bool](context.Background(), pool, "SELECT $1::text < $2", all[i-1], all[i])
						if err != nil {
							t.Fatal(err)
						}
						assertEq(t, less, dir == "asc")
					}
				}
				if values != nil {
					assertEq(t, strings.Join(got, "|"), strings.Join(values, "|"))
				}

				var paged []string
				for offset := 0; offset < len(all); offset += 4 {
					page := c.Get(fmt.Sprintf("/api/content%s&count=false&limit=4&offset=%d", query, offset)).
						Assert(t, 200).JSON()
					assertNil(t, "total", page["total"])
					paged = append(paged, dataIDs(page)...)
				}
				assertEq(t, strings.Join(paged, ","), strings.Join(all, ","))
				assertEq(t, len(fp.Dedup(all)), len(all))

				ids := c.Get("/api/content/ids"+query+"&limit=1000").Assert(t, 200).JSON()["ids"]
				assertEq(t, s(ids), s(all))

				// The run-length encoding of the ordered ids' keys is the buckets.
				type run struct {
					key   string
					count int
				}
				var runs []run
				if key, _ := bucketKey(tc.sort); key != "" {
					rows, err := db.Select[struct {
						ID  string `db:"id"`
						Key string `db:"key"`
					}](context.Background(), pool, "SELECT c.id, COALESCE("+key+", '') AS key FROM content c"+
						" WHERE c.id = ANY($1)", all)
					if err != nil {
						t.Fatal(err)
					}
					keyOf := map[string]string{}
					for _, r := range rows {
						keyOf[r.ID] = r.Key
					}
					for _, id := range all {
						if n := len(runs); n > 0 && runs[n-1].key == keyOf[id] {
							runs[n-1].count++
						} else {
							runs = append(runs, run{keyOf[id], 1})
						}
					}
				}

				res := c.Get("/api/content/buckets"+query).Assert(t, 200).JSON()
				assertEq(t, s(res["total"]), s(len(all)))
				var buckets []run
				var bucketKeys []string
				for _, b := range res["buckets"].([]any) {
					b := b.(map[string]any)
					k, _ := b["key"].(string)
					buckets = append(buckets, run{k, int(b["count"].(float64))})
					bucketKeys = append(bucketKeys, k)
				}
				assertEq(t, s(buckets), s(runs))
				assertEq(t, s(bucketKeys), s(keys))
			})
		}
	}

	// Both start with "lantern" and score alike. Sort title and id would put c_lantern_a first, so
	// c_lantern_b first shows the shorter-title tiebreak.
	mustExec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, data) VALUES
		('c_lantern_a', 'a', 'file:///lib/a', 'comic_series', $1, '{"title": "Lantern Alpha Two"}'),
		('c_lantern_b', 'b', 'file:///lib/b', 'comic_series', $1, '{"title": "Lantern Beta"}')`, libID)
	list := func(query string) string {
		return s(dataIDs(c.Get("/api/content?library_id="+libID+query).Assert(t, 200).JSON()))
	}
	relevance := s([]string{"c_lantern_b", "c_lantern_a", echoID}) // titles above the alt, shorter first
	assertEq(t, list("&search=lantern"), relevance)
	assertEq(t, list("&search=+lantern+"), relevance)
	assertEq(t, list("&search=lantern&sort=title&sort_order=asc"), s([]string{echoID, "c_lantern_a", "c_lantern_b"}))
	assertEq(t, list("&sort=relevance"), list(""))

	// Kind counts of a search stay right under generic plans, which pgx reaches from a statement's
	// sixth run.
	k, err := countContentKinds(context.Background(), dbtest.GenericPlans(t, pool), "", contentFilter{LibraryID: libID, Search: "lantern"})
	if err != nil || k != (kindCounts{Series: 3}) {
		t.Fatalf("kinds = %+v (%v)", k, err)
	}
}

type recentFixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	libID  string
	userID string
	base   time.Time
}

func newRecentFixture(t *testing.T, pool *pgxpool.Pool, c *testClient) *recentFixture {
	t.Helper()
	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	f := &recentFixture{t: t, pool: pool, libID: models.MakeLibraryID(), userID: s(me["id"]),
		base: time.Now().Add(-time.Hour).UTC()}
	f.exec("INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", f.libID)
	return f
}

func (f *recentFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

// content inserts content added a day before the fixture's base.
func (f *recentFixture) content(typ string, parentID *string, order int) string {
	f.t.Helper()
	id := models.MakeContentID()
	f.exec(`
		INSERT INTO content (id, created_at, uri_part, uri, type, library_id, parent_id, "order")
		VALUES ($1, $2, $1, 'file:///lib/' || $1, $3, $4, $5, $6)
	`, id, f.base.Add(-24*time.Hour), typ, f.libID, parentID, order)
	return id
}

// series creates a series whose kids[i] is volume i+1.
func (f *recentFixture) series(n int) (string, []string) {
	f.t.Helper()
	id := f.content("comic_series", nil, 0)
	kids := make([]string, n)
	for i := range kids {
		kids[i] = f.content("comic", &id, i)
	}
	return id, kids
}

// added makes content added at fixture minute m.
func (f *recentFixture) added(id string, m int) {
	f.t.Helper()
	f.exec("UPDATE content SET created_at = $2 WHERE id = $1", id, f.at(m))
}

// at is the fixture time m minutes after the base.
func (f *recentFixture) at(m int) *time.Time {
	t := f.base.Add(time.Duration(m) * time.Minute)
	return &t
}

// set writes a user_to_content row for the fixture user; an empty status is NULL, and a read
// time sets a non-empty progress.
func (f *recentFixture) set(id, status string, readAt, statusAt *time.Time) {
	f.t.Helper()
	f.setFor(f.userID, id, status, readAt, statusAt)
}

func (f *recentFixture) setFor(userID, id, status string, readAt, statusAt *time.Time) {
	f.t.Helper()
	f.exec(`
		INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at, progress,
			progress_updated_at, last_read_at)
		SELECT $1, $2, library_id, uri, NULLIF($3, ''), $4,
			CASE WHEN $5::timestamptz IS NULL THEN '{}' ELSE '{"current_page": 1}' END::jsonb, $5, $5
		FROM content WHERE id = $6
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status,
			status_updated_at = EXCLUDED.status_updated_at, progress = EXCLUDED.progress,
			progress_updated_at = EXCLUDED.progress_updated_at, last_read_at = EXCLUDED.last_read_at
	`, models.MakeUserToContentID(), userID, status, statusAt, readAt, id)
}

// userData is "<status or -> <progress>", plus " t" when progress_updated_at is set, or "-"
// without a row.
func (f *recentFixture) userData(id string) string {
	f.t.Helper()
	v, err := db.SelectScalar[string](context.Background(), f.pool, `
		SELECT concat_ws(' ', COALESCE(utc.status, '-'), utc.progress::text,
			NULLIF(utc.progress_updated_at IS NOT NULL, false))
		FROM content c LEFT JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $2
		WHERE c.id = $1
	`, id, f.userID)
	if err != nil {
		f.t.Fatal(err)
	}
	return v
}

func (f *recentFixture) revision(id string) string {
	f.t.Helper()
	v, err := db.SelectScalar[*string](context.Background(), f.pool, `
		SELECT utc.revision FROM content c JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $2
		WHERE c.id = $1
	`, id, f.userID)
	if err != nil || v == nil {
		f.t.Fatalf("revision of %s: %v", id, err)
	}
	return *v
}

func TestBulkActions(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)

	a, b, other := f.content("comic", nil, 0), f.content("comic", nil, 1), f.content("comic", nil, 2)
	f.exec("UPDATE content SET page_count = 5 WHERE id = $1", a)
	series, kids := f.series(2)
	f.set(a, "reading", f.at(0), f.at(0))
	f.set(series, "reading", f.at(0), f.at(0))
	f.set(kids[0], "completed", f.at(0), f.at(0))

	bulk := func(path string, body map[string]any) string {
		t.Helper()
		return s(c.Post(path, body).Assert(t, 200).JSON()["count"])
	}

	// Completing ends the items, as mark_completed does, under one revision.
	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{
		"ids": []string{a, b, a, "c_unknown"}, "action": "set_status", "status": "completed",
	}), "2")
	assertEq(t, f.userData(a), `completed {"at_end": true, "current_page": 4, "progress_percent": 100} t`)
	assertEq(t, f.userData(b), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)
	assertEq(t, f.userData(other), "-")
	assertEq(t, f.revision(a), f.revision(b))
	c.Post("/api/content/bulk/user-data", map[string]any{"ids": []string{}, "action": "set_status"}).Assert(t, 400)

	// Other statuses keep the position.
	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{
		"ids": []string{a}, "action": "set_status", "status": "on_hold",
	}), "1")
	assertEq(t, f.userData(a), `on_hold {"at_end": true, "current_page": 4, "progress_percent": 100} t`)

	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{"ids": []string{series}, "action": "reset"}), "1")
	assertEq(t, f.userData(series), "- {}")
	assertEq(t, f.userData(kids[0]), "- {}")
	// Completing every child starts the series, and never completes it.
	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{
		"ids": kids, "action": "set_status", "status": "completed",
	}), "2")
	assertEq(t, f.userData(series), "reading {}")

	// A series with its unfinished volumes; dropped ones stay dropped.
	s2, kids2 := f.series(3)
	f.set(kids2[0], "dropped", nil, f.at(0))
	f.set(kids2[1], "reading", f.at(0), f.at(0))
	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{
		"ids": []string{s2}, "action": "set_status", "status": "completed",
	}), "1")
	assertEq(t, f.userData(s2), "completed {}")
	assertEq(t, f.userData(kids2[1]), `reading {"current_page": 1} t`)
	bulk("/api/content/bulk/user-data", map[string]any{
		"ids": []string{s2}, "action": "set_status", "status": "completed", "include_children": true,
	})
	assertEq(t, f.userData(kids2[0]), "dropped {}")
	assertEq(t, f.userData(kids2[1]), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)
	assertEq(t, f.userData(kids2[2]), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)

	// A series selected with one of its unfinished children.
	s4, kids4 := f.series(2)
	assertEq(t, bulk("/api/content/bulk/user-data", map[string]any{
		"ids": []string{s4, kids4[0]}, "action": "set_status", "status": "completed", "include_children": true,
	}), "2")
	assertEq(t, f.userData(kids4[1]), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)

	s3, kids3 := f.series(1)
	bulk("/api/content/bulk/user-data", map[string]any{"ids": kids3, "action": "set_status", "status": "reading"})
	assertEq(t, f.userData(s3), "reading {}")

	lists := make([]string, 2)
	for i := range lists {
		lists[i] = s(c.Post("/api/custom-lists", map[string]any{"name": "l", "visibility": "private"}).Assert(t, 200).JSON()["id"])
	}
	entries := map[string]any{"list_ids": lists, "ids": []string{a, b, a}}
	assertEq(t, bulk("/api/custom-lists/entries", entries), "4")
	assertEq(t, bulk("/api/custom-lists/entries", entries), "0")
}

func TestSeriesReading(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)

	seriesReading := func(id string, body map[string]any) string {
		t.Helper()
		return s(c.Post("/api/content/"+id+"/series-reading", body).Assert(t, 200).JSON()["count"])
	}
	counts := func(id string) string {
		t.Helper()
		got := c.Get("/api/content/"+id).Assert(t, 200).JSON()
		return fmt.Sprintf("%v/%v read, %v dropped, %v unread, %v new", got["completed_children_count"],
			got["children_count"], got["dropped_children_count"], got["unread_children_count"], got["new_children_count"])
	}

	t.Run("mark through", func(t *testing.T) {
		series, kids := f.series(5)
		f.set(kids[1], "dropped", nil, f.at(0))
		f.set(kids[4], "reading", f.at(0), f.at(0))
		f.exec(`UPDATE user_to_content SET progress = '{"current_page": 12}' WHERE uri = 'file:///lib/' || $1`, kids[4])
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[0])
		assertEq(t, seriesReading(series, map[string]any{"action": "mark_through", "until_id": kids[2]}), "1")
		assertEq(t, f.userData(kids[0]), "-")
		assertEq(t, f.userData(kids[1]), "dropped {}")
		assertEq(t, f.userData(kids[2]), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)
		assertEq(t, f.userData(kids[3]), "-")
		assertEq(t, f.userData(kids[4]), `reading {"current_page": 12} t`)
		assertEq(t, f.userData(series), "reading {}")
		assertEq(t, counts(series), "1/4 read, 1 dropped, 2 unread, 0 new")
		c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "mark_through", "until_id": "c_x"}).
			Assert(t, 404)
		c.Post("/api/content/"+kids[1]+"/series-reading", map[string]any{"action": "clear"}).Assert(t, 400)
	})

	t.Run("mark series completed", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(0))
		assertEq(t, seriesReading(series, map[string]any{"action": "mark_series_completed"}), "1")
		assertEq(t, f.userData(series), "completed {}")
		assertEq(t, f.userData(kids[1]), "-")
		assertEq(t, seriesReading(series, map[string]any{"action": "mark_series_completed", "include_unread": true}), "3")
		assertEq(t, f.userData(kids[2]), `completed {"at_end": true, "current_page": 0, "progress_percent": 100} t`)
		assertEq(t, counts(series), "3/3 read, 0 dropped, 0 unread, 0 new")
	})

	t.Run("clear is the same everywhere", func(t *testing.T) {
		for _, clear := range []func(series string){
			func(series string) { seriesReading(series, map[string]any{"action": "clear"}) },
			func(series string) {
				c.Post("/api/content/"+series+"/reading", map[string]any{"op": "clear"}).Assert(t, 200)
			},
			func(series string) {
				c.Post("/api/content/bulk/user-data", map[string]any{"ids": []string{series}, "action": "reset"}).
					Assert(t, 200)
			},
		} {
			series, kids := f.series(2)
			f.set(series, "reading", nil, f.at(0))
			f.set(kids[0], "completed", f.at(1), f.at(1))
			f.set(kids[1], "reading", f.at(2), f.at(2))
			clear(series)
			for _, id := range []string{series, kids[0], kids[1]} {
				var state string
				if err := pool.QueryRow(context.Background(), `
					SELECT concat_ws(' ', utc.status, utc.progress::text, utc.progress_updated_at, utc.last_read_at,
						utc.status_updated_at, utc.revision LIKE 'srv:%')
					FROM content c JOIN user_to_content utc ON utc.library_id = c.library_id AND utc.uri = c.uri
					WHERE c.id = $1`, id).Scan(&state); err != nil {
					t.Fatal(err)
				}
				assertEq(t, state, "{} t")
			}
		}
	})

	t.Run("new volumes of a completed series", func(t *testing.T) {
		series, kids := f.series(4)
		for _, id := range kids {
			f.set(id, "completed", nil, f.at(0))
		}
		f.set(series, "completed", nil, f.at(1))
		for i := range 2 {
			f.added(f.content("comic", &series, 10+i), 2)
		}
		assertEq(t, counts(series), "4/6 read, 0 dropped, 2 unread, 2 new")
		// An earlier volume added before the completion is not new.
		f.content("comic", &series, -1)
		assertEq(t, counts(series), "4/7 read, 0 dropped, 3 unread, 2 new")
		// Past max(3, half), they are a re-added series, not new volumes.
		for i := range 4 {
			f.added(f.content("comic", &series, 20+i), 2)
		}
		assertEq(t, counts(series), "4/11 read, 0 dropped, 7 unread, 0 new")
	})
}

func TestContinueReading(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)

	get := func(t *testing.T, cl *testClient, query string) []map[string]any {
		t.Helper()
		return cl.Get("/api/content/continue-reading"+query).Assert(t, 200).JSONArray()
	}
	// entries are "item" or "item@series" strings, with "*" when new.
	entries := func(t *testing.T, res []map[string]any) []string {
		var got []string
		for _, e := range res {
			id := s(e["item"].(map[string]any)["id"])
			if series, ok := e["series"].(map[string]any); ok {
				id += "@" + s(series["id"])
			}
			if e["is_new"] == true {
				id += "*"
			}
			got = append(got, id)
		}
		return got
	}
	// list is the same, from the grid's sort.
	list := func(t *testing.T, sort string) []string {
		t.Helper()
		var got []string
		for _, e := range c.Get("/api/content?sort="+sort+"&sort_order=desc").Assert(t, 200).JSON()["data"].([]any) {
			item := e.(map[string]any)
			id := s(item["id"])
			cont := item["continue"].(map[string]any)
			if series, ok := cont["series"].(map[string]any); ok {
				id += "@" + s(series["id"])
			}
			if cont["is_new"] == true {
				id += "*"
			}
			got = append(got, id)
		}
		return got
	}
	// expect asserts Home's entries, which the continue sort lists too.
	expect := func(t *testing.T, want ...string) {
		t.Helper()
		assertEq(t, strings.Join(entries(t, get(t, c, "")), ","), strings.Join(want, ","))
		assertEq(t, strings.Join(list(t, "continue"), ","), strings.Join(want, ","))
	}
	expectUpdated := func(t *testing.T, want ...string) {
		t.Helper()
		assertEq(t, strings.Join(list(t, "recently_updated"), ","), strings.Join(want, ","))
	}
	// target resolves content's continue button as "target action reason new earlier".
	target := func(t *testing.T, id string) string {
		t.Helper()
		r := c.Get("/api/content/"+id+"/continue").Assert(t, 200).JSON()
		tid := "-"
		if tg, ok := r["target"].(map[string]any); ok {
			tid = s(tg["id"])
		}
		return fmt.Sprintf("%s %v %v %v %v", tid, r["action"], r["reason"], r["is_new"], r["earlier_unread_id"])
	}
	run := func(name string, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			f.t = t
			f.exec("DELETE FROM user_to_content")
			fn(t)
		})
	}

	run("reading children", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "reading", f.at(1), f.at(0))
		f.set(kids[1], "reading", f.at(2), f.at(0))
		res := get(t, c, "")
		assertLen(t, res, 1)
		assertEq(t, s(res[0]["item"].(map[string]any)["id"]), kids[1])
		assertEq(t, res[0]["action"], any("resume"))
		assertEq(t, s(res[0]["series"].(map[string]any)["unread_children_count"]), "3")
		assertEq(t, target(t, series), kids[1]+" resume <nil> false "+kids[0])
	})

	run("completed anchor continues with the next volume", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(1))
		expect(t, kids[1]+"@"+series)
		assertEq(t, target(t, series), kids[1]+" next <nil> false <nil>")
	})

	run("held and dropped volumes are skipped", func(t *testing.T) {
		series, kids := f.series(4)
		f.set(kids[0], "completed", nil, f.at(2))
		f.set(kids[1], "on_hold", nil, f.at(0))
		f.set(kids[2], "dropped", nil, f.at(0))
		expect(t, kids[3]+"@"+series)
	})

	run("no wrap, earlier unread", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[1], "completed", nil, f.at(0))
		f.set(kids[2], "completed", nil, f.at(1))
		expect(t)
		assertEq(t, target(t, series), "- <nil> earlier_unread false "+kids[0])
	})

	run("caught up", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(series, "reading", nil, f.at(0))
		f.set(kids[0], "completed", nil, f.at(0))
		f.set(kids[1], "dropped", nil, f.at(1))
		expect(t)
		assertEq(t, target(t, series), "- <nil> caught_up false <nil>")
		empty, _ := f.series(0)
		assertEq(t, target(t, empty), "- <nil> empty false <nil>")
	})

	run("held target", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "on_hold", nil, f.at(0))
		f.set(kids[1], "completed", nil, f.at(1))
		f.set(kids[2], "on_hold", f.at(0), f.at(0))
		expect(t)
		assertEq(t, target(t, series), kids[2]+" resume held false <nil>")
	})

	run("later new volume", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(series, "reading", nil, f.at(0))
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(kids[1], "completed", nil, f.at(2))
		expect(t)
		expectUpdated(t)
		vol3 := f.content("comic", &series, 2)
		f.added(vol3, 3)
		expect(t, vol3+"@"+series+"*")
		expectUpdated(t, vol3+"@"+series+"*")
		assertEq(t, target(t, series), vol3+" next <nil> true <nil>")
		// A newer earlier insertion does not move the new volume's date.
		early := f.content("comic", &series, -1)
		f.added(early, 4)
		assertEq(t, target(t, series), vol3+" next <nil> true "+early)
		// Only a reading series is recently updated.
		f.set(series, "", nil, nil)
		expect(t, vol3+"@"+series+"*")
		expectUpdated(t)
		f.set(series, "completed", nil, f.at(3))
		expect(t)
		expectUpdated(t)
	})

	run("earlier insertion only", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(series, "reading", nil, f.at(0))
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(kids[1], "completed", nil, f.at(2))
		f.added(f.content("comic", &series, -1), 3)
		expect(t)
		expectUpdated(t)
	})

	run("mass re-add guard", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(series, "reading", nil, f.at(0))
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(kids[1], "completed", nil, f.at(2))
		var added []string
		for i := range 4 {
			id := f.content("comic", &series, 10+i)
			f.added(id, 3)
			added = append(added, id)
		}
		expect(t, added[0]+"@"+series)
		expectUpdated(t)
	})

	run("standalone", func(t *testing.T) {
		reading, completed, atEnd := f.content("comic", nil, 0), f.content("comic", nil, 0), f.content("comic", nil, 0)
		f.set(reading, "reading", nil, f.at(1))
		f.set(completed, "completed", f.at(2), f.at(2))
		f.set(atEnd, "completed", f.at(3), f.at(3))
		f.exec(`UPDATE user_to_content SET progress = '{"current_page": 3, "at_end": true}' WHERE uri = 'file:///lib/' || $1`, atEnd)
		expect(t, reading)
		assertEq(t, target(t, reading), reading+" start <nil> false <nil>")
		assertEq(t, target(t, completed), completed+" resume <nil> false <nil>")
		assertEq(t, target(t, atEnd), "- <nil> completed false <nil>")
	})

	run("order", func(t *testing.T) {
		a, aKids := f.series(2)
		b, bKids := f.series(2)
		f.set(aKids[0], "reading", f.at(1), f.at(0))
		f.set(bKids[0], "reading", f.at(3), f.at(0))
		standalone := f.content("comic", nil, 0)
		f.set(standalone, "reading", nil, f.at(2))
		expect(t, bKids[0]+"@"+b, standalone, aKids[0]+"@"+a)
		assertLen(t, get(t, c, "?limit=2"), 2)
		c.Get("/api/content/continue-reading?limit=0").Assert(t, 400)
		c.Get("/api/content/continue-reading?limit=51").Assert(t, 400)

		// Equal recency: target id descending.
		x, y := f.content("comic", nil, 0), f.content("comic", nil, 0)
		f.exec("DELETE FROM user_to_content")
		f.set(x, "reading", nil, f.at(5))
		f.set(y, "reading", nil, f.at(5))
		// The database's collation orders the ids, not Go's byte order.
		want, err := db.SelectScalars[string](context.Background(), pool,
			"SELECT id FROM content WHERE id = ANY($1) ORDER BY id DESC", []string{x, y})
		if err != nil {
			t.Fatal(err)
		}
		expect(t, want...)
	})

	run("preference adds held items and series", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(series, "on_hold", nil, f.at(0))
		item := f.content("comic", nil, 0)
		f.set(item, "dropped", f.at(2), f.at(2))
		expect(t)
		c.Patch("/api/users/me/preferences", map[string]any{"home": map[string]any{"ignoreSeriesStatus": true}}).
			Assert(t, 200)
		expect(t, item, kids[1]+"@"+series)
		c.Patch("/api/users/me/preferences", map[string]any{"home": nil}).Assert(t, 200)
		for _, status := range []string{"plan_to_read", "completed"} {
			f.set(series, status, nil, f.at(0))
			expect(t)
		}
	})

	run("preference adds held series without child activity", func(t *testing.T) {
		for _, status := range []string{"on_hold", "dropped"} {
			series, kids := f.series(2)
			f.set(series, status, nil, f.at(0))
			expect(t)
			c.Patch("/api/users/me/preferences", map[string]any{"home": map[string]any{"ignoreSeriesStatus": true}}).
				Assert(t, 200)
			expect(t, kids[0]+"@"+series)
			c.Patch("/api/users/me/preferences", map[string]any{"home": nil}).Assert(t, 200)
			f.set(series, "completed", nil, f.at(0))
		}
	})

	run("invalid anchor and target are skipped", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "reading", f.at(5), f.at(5))
		f.set(kids[1], "completed", nil, f.at(1))
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[0])
		expect(t, kids[2]+"@"+series)
		assertEq(t, target(t, series), kids[2]+" next <nil> false <nil>")
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[2])
		expect(t)
		assertEq(t, target(t, series), "- <nil> caught_up false <nil>")
	})

	run("other users", func(t *testing.T) {
		member, memberID := newMemberClient(t, c)
		item := f.content("comic", nil, 0)
		f.setFor(memberID, item, "reading", f.at(0), f.at(0))
		expect(t)
		assertEq(t, strings.Join(entries(t, get(t, member, "")), ","), item)
	})
}

func TestComicPageSizes(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	ctx := context.Background()
	const unsized = `{"pages": [["01.jpg"], ["02.jpg"]]}`
	const sized = `{"pages": [["01.jpg", 4, 2], ["02.jpg", 0, 0]]}`

	newComic := func(t *testing.T, path, fileData string) string {
		t.Helper()
		id := newTestContent(t, pool)
		mustExec(t, pool, "UPDATE content SET file_uri = $2, file_mtime = now(), file_size = 1, file_data = $3 WHERE id = $1",
			id, path, fileData)
		return id
	}
	cbz := func(t *testing.T) string {
		return testCBZ(t, t.TempDir(), map[string][]byte{"01.jpg": testJPEG(t), "02.jpg": []byte("not an image")})
	}
	stored := func(t *testing.T, id string) string {
		t.Helper()
		fd, err := db.SelectScalar[string](ctx, pool, "SELECT file_data::text FROM content WHERE id = $1", id)
		if err != nil {
			t.Fatal(err)
		}
		return fd
	}
	served := func(t *testing.T, path string) string {
		t.Helper()
		fd, _ := json.Marshal(c.Get(path).Assert(t, 200).JSON()["file_data"])
		return string(fd)
	}
	normalize := func(s string) string {
		var v any
		_ = json.Unmarshal([]byte(s), &v)
		out, _ := json.Marshal(v)
		return string(out)
	}
	// counting swaps pageSizes for one that counts calls and waits for release, which cleanup calls.
	counting := func(t *testing.T) (calls *atomic.Int32, release func()) {
		calls, released := new(atomic.Int32), make(chan struct{})
		release = sync.OnceFunc(func() { close(released) })
		t.Cleanup(func() { pageSizes = comic.PageSizes })
		t.Cleanup(release)
		pageSizes = func(ctx context.Context, path string, names []string) ([]comic.PageInfo, error) {
			calls.Add(1)
			<-released
			return comic.PageSizes(ctx, path, names)
		}
		return calls, release
	}
	waitUntil := func(t *testing.T, cond func() bool) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatal("timed out")
			}
		}
	}

	t.Run("without the flag", func(t *testing.T) {
		id := newComic(t, cbz(t), unsized)
		assertEq(t, served(t, "/api/content/"+id), normalize(unsized))
		assertEq(t, stored(t, id), unsized)
	})

	t.Run("a JSON null is served as an object", func(t *testing.T) {
		id := newComic(t, cbz(t), "null")
		assertEq(t, served(t, "/api/content/"+id), "{}")
	})

	t.Run("with the flag", func(t *testing.T) {
		id := newComic(t, cbz(t), unsized)
		assertEq(t, served(t, "/api/content/"+id+"?page_sizes=1"), normalize(sized))
		assertEq(t, stored(t, id), sized)
	})

	t.Run("already sized", func(t *testing.T) {
		calls, release := counting(t)
		release()
		id := newComic(t, cbz(t), `{"pages": [["01.jpg", 9, 9]]}`)
		assertEq(t, served(t, "/api/content/"+id+"?page_sizes=1"), `{"pages":[["01.jpg",9,9]]}`)
		assertEq(t, calls.Load(), int32(0))
	})

	t.Run("unreadable archive", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bad.cbz")
		if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
			t.Fatal(err)
		}
		id := newComic(t, path, unsized)
		assertEq(t, served(t, "/api/content/"+id+"?page_sizes=1"), `{"pages":[["01.jpg",0,0],["02.jpg",0,0]]}`)
		assertEq(t, stored(t, id), unsized)
	})

	t.Run("flight deadline", func(t *testing.T) {
		t.Cleanup(func() { pageSizes, pageSizesTimeout = comic.PageSizes, 5*time.Minute })
		pageSizesTimeout = 50 * time.Millisecond
		observed := make(chan error, 1)
		pageSizes = func(ctx context.Context, path string, names []string) ([]comic.PageInfo, error) {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			observed <- ctx.Err()
			return nil, errors.New("stub")
		}
		id := newComic(t, cbz(t), unsized)
		assertEq(t, served(t, "/api/content/"+id+"?page_sizes=1"), `{"pages":[["01.jpg",0,0],["02.jpg",0,0]]}`)
		assertEq(t, stored(t, id), unsized)
		select {
		case err := <-observed:
			assertEq(t, err, context.DeadlineExceeded)
		default:
			t.Fatal("pageSizes was not called")
		}
	})

	t.Run("row changed since read", func(t *testing.T) {
		id := newComic(t, cbz(t), unsized)
		row, err := db.SelectOne[pageSizesRow](ctx, pool,
			"SELECT file_uri, file_mtime, file_size, file_data FROM content WHERE id = $1", id)
		if err != nil {
			t.Fatal(err)
		}
		mustExec(t, pool, "UPDATE content SET file_mtime = file_mtime + interval '1 second' WHERE id = $1", id)
		if _, err := savePageSizes(ctx, pool, id, row); err != nil {
			t.Fatal(err)
		}
		assertEq(t, stored(t, id), unsized)

		row.FileMtime = nil
		row.FileData = models.JSONB(`{"pages": [["other.jpg"]]}`)
		mustExec(t, pool, "UPDATE content SET file_mtime = NULL WHERE id = $1", id)
		if _, err := savePageSizes(ctx, pool, id, row); err != nil {
			t.Fatal(err)
		}
		assertEq(t, stored(t, id), unsized)
	})

	t.Run("concurrent and sequential callers compute once", func(t *testing.T) {
		calls, release := counting(t)
		id := newComic(t, cbz(t), unsized)
		// The deadline bounds every call, so a stuck flight fails the test instead of hanging it.
		wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		const n = 8
		results := make(chan error, n+1)
		call := func() {
			fd, err := pageSizesFor(wctx, pool, id)
			if err == nil && normalize(string(fd)) != normalize(sized) {
				err = fmt.Errorf("got %s", fd)
			}
			results <- err
		}
		go call()
		waitUntil(t, func() bool { return calls.Load() == 1 })
		for range n {
			go call()
		}
		time.Sleep(50 * time.Millisecond)
		assertEq(t, calls.Load(), int32(1))
		assertEq(t, len(results), 0)
		release()
		for range n + 1 {
			if err := <-results; err != nil {
				t.Error(err)
			}
		}
		if _, err := pageSizesFor(wctx, pool, id); err != nil {
			t.Fatal(err)
		}
		assertEq(t, calls.Load(), int32(1))
		assertEq(t, stored(t, id), sized)
	})

	t.Run("cancelled caller", func(t *testing.T) {
		calls, release := counting(t)
		id := newComic(t, cbz(t), unsized)
		cctx, cancel := context.WithCancel(ctx)
		done := make(chan error, 1)
		go func() {
			_, err := pageSizesFor(cctx, pool, id)
			done <- err
		}()
		waitUntil(t, func() bool { return calls.Load() == 1 })
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cancelled caller did not return")
		}
		release()
		waitUntil(t, func() bool { return stored(t, id) == sized })
	})
}

func TestContinueListSorts(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)

	ids := func(query string) []string {
		t.Helper()
		var out []string
		for _, e := range c.Get("/api/content?"+query).Assert(t, 200).JSON()["data"].([]any) {
			out = append(out, s(e.(map[string]any)["id"]))
		}
		return out
	}
	idsOf := func(query string) []string {
		t.Helper()
		var out []string
		for _, id := range c.Get("/api/content/ids?limit=100&"+query).Assert(t, 200).JSON()["ids"].([]any) {
			out = append(out, s(id))
		}
		return out
	}

	// Series a read last at minute 5 by its newest child, b at minute 2, the standalone at 3.
	a, aKids := f.series(2)
	b, bKids := f.series(2)
	standalone, unread := f.content("comic", nil, 0), f.content("comic", nil, 0)
	f.set(aKids[0], "completed", f.at(5), f.at(5))
	f.set(aKids[1], "reading", f.at(1), f.at(1))
	f.set(bKids[0], "reading", f.at(2), f.at(2))
	f.set(standalone, "reading", f.at(3), f.at(3))
	f.set(unread, "plan_to_read", nil, f.at(9))
	newVol := f.content("comic", &b, 5)
	f.added(newVol, 6)
	f.set(b, "reading", nil, f.at(0))
	f.set(bKids[0], "completed", f.at(2), f.at(2))
	f.set(bKids[1], "completed", nil, f.at(1))

	root := "parent_id=null&library_id=" + f.libID
	assertEq(t, s(ids(root+"&sort=last_read_at&sort_order=desc")), s([]string{a, standalone, b, unread}))
	assertEq(t, s(ids(root+"&sort=progress_updated_at&sort_order=desc")), s([]string{a, standalone, b, unread}))
	assertEq(t, s(ids(root+"&sort=history&sort_order=desc")), s([]string{a, standalone, b}))

	cont := []string{aKids[1], standalone, newVol}
	assertEq(t, s(ids("sort=continue&sort_order=desc&parent_id=null")), s(cont))
	assertEq(t, s(ids("sort=recently_updated&sort_order=desc")), s([]string{newVol}))
	slices.Reverse(cont)
	assertEq(t, s(ids("sort=continue&sort_order=asc")), s(cont))
	assertEq(t, s(idsOf("sort=continue&sort_order=asc")), s(cont))

	for _, q := range []string{"sort=history&sort_order=desc&parent_id=null", "sort=continue&sort_order=desc",
		"sort=continue&sort_order=asc", "sort=recently_updated&sort_order=desc"} {
		page := c.Get("/api/content?"+q).Assert(t, 200).JSON()
		buckets := c.Get("/api/content/buckets?"+q).Assert(t, 200).JSON()
		n := len(idsOf(q))
		assertEq(t, s(page["total"]), s(n))
		assertEq(t, s(buckets["total"]), s(n))
		keys := buckets["buckets"].([]any)
		if len(keys) == 0 || keys[0].(map[string]any)["key"] == nil {
			t.Fatalf("%s: no year buckets: %v", q, keys)
		}
	}

	// The continue rows carry their series.
	data := c.Get("/api/content?sort=continue&sort_order=desc").Assert(t, 200).JSON()["data"].([]any)
	first := data[0].(map[string]any)["continue"].(map[string]any)
	assertEq(t, first["action"], any("resume"))
	assertEq(t, s(first["series"].(map[string]any)["id"]), a)
	assertNil(t, "continue", c.Get("/api/content/"+a).Assert(t, 200).JSON()["continue"])

	// Recently updated orders by when the new volume came, read longer ago or not.
	d, dKids := f.series(1)
	f.set(d, "reading", nil, f.at(-20))
	f.set(dKids[0], "completed", f.at(-10), f.at(-10))
	dNew := f.content("comic", &d, 5)
	f.added(dNew, 8)
	updated := []string{dNew, newVol}
	assertEq(t, s(ids("sort=recently_updated&sort_order=desc")), s(updated))
	assertEq(t, s(idsOf("sort=recently_updated&sort_order=desc")), s(updated))
	slices.Reverse(updated)
	assertEq(t, s(ids("sort=recently_updated&sort_order=asc")), s(updated))
	assertEq(t, s(idsOf("sort=recently_updated&sort_order=asc")), s(updated))
	buckets := c.Get("/api/content/buckets?sort=recently_updated&sort_order=asc").Assert(t, 200).JSON()
	assertEq(t, s(buckets["total"]), "2")
}

// TestContinueListMatrix checks the continue and recently updated sorts together across the list,
// its ids and its year buckets: one library at a time, both directions, invalid volumes left out.
func TestContinueListMatrix(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)
	other := newRecentFixture(t, pool, c)
	lastYear := -400 * 24 * 60

	ids := func(query string) []string {
		t.Helper()
		var out []string
		for _, e := range c.Get("/api/content?"+query).Assert(t, 200).JSON()["data"].([]any) {
			out = append(out, s(e.(map[string]any)["id"]))
		}
		return out
	}
	idsOf := func(query string) []string {
		t.Helper()
		var out []string
		for _, id := range c.Get("/api/content/ids?limit=100&"+query).Assert(t, 200).JSON()["ids"].([]any) {
			out = append(out, s(id))
		}
		return out
	}
	years := func(query string) []string {
		t.Helper()
		res := c.Get("/api/content/buckets?"+query).Assert(t, 200).JSON()
		var out []string
		for _, b := range res["buckets"].([]any) {
			out = append(out, s(b.(map[string]any)["key"]))
		}
		assertEq(t, s(res["total"]), s(len(idsOf(query))))
		return out
	}
	year := func(m int) string { return s(f.at(m).Year()) }

	recent, old := f.content("comic", nil, 0), f.content("comic", nil, 0)
	f.set(recent, "reading", f.at(0), f.at(0))
	f.set(old, "reading", f.at(lastYear+3), f.at(lastYear+3))
	// A series whose only read volume is no longer valid continues nowhere.
	_, goneKids := f.series(2)
	f.set(goneKids[0], "reading", f.at(1), f.at(1))
	f.exec("UPDATE content SET valid = false WHERE id = $1", goneKids[0])
	// Caught up with a new volume each: this year, and last year, with an invalid newer one.
	newer, newerKids := f.series(1)
	f.set(newer, "reading", nil, f.at(lastYear))
	f.set(newerKids[0], "completed", f.at(lastYear+2), f.at(lastYear+2))
	newVol := f.content("comic", &newer, 5)
	f.added(newVol, -10)
	older, olderKids := f.series(1)
	f.set(older, "reading", nil, f.at(lastYear))
	f.set(olderKids[0], "completed", f.at(lastYear+1), f.at(lastYear+1))
	oldVol, invalidVol := f.content("comic", &older, 5), f.content("comic", &older, 6)
	f.added(oldVol, lastYear+10)
	f.added(invalidVol, 0)
	f.exec("UPDATE content SET valid = false WHERE id = $1", invalidVol)
	// Another library's reading stays out of this one's lists.
	elsewhere := other.content("comic", nil, 0)
	other.set(elsewhere, "reading", other.at(2), other.at(2))

	lib := "&library_id=" + f.libID
	for _, tc := range []struct {
		sort  string
		want  []string
		years []string
	}{
		{"continue", []string{recent, old, newVol, oldVol}, []string{year(0), year(lastYear)}},
		{"recently_updated", []string{newVol, oldVol}, []string{year(-10), year(lastYear)}},
	} {
		for _, dir := range []string{"desc", "asc"} {
			q := "sort=" + tc.sort + "&sort_order=" + dir + lib
			want, wantYears := slices.Clone(tc.want), slices.Clone(tc.years)
			if dir == "asc" {
				slices.Reverse(want)
				slices.Reverse(wantYears)
			}
			assertEq(t, s(ids(q)), s(want))
			assertEq(t, s(idsOf(q)), s(want))
			assertEq(t, s(years(q)), s(wantYears))
			assertEq(t, s(c.Get("/api/content?"+q).Assert(t, 200).JSON()["total"]), s(len(want)))
		}
	}
}
