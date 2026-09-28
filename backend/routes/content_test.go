package routes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"voltis/db"
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

	t.Run("progress null", func(t *testing.T) {
		id := newTestContent(t, pool)
		res := post(t, id, map[string]any{"progress": nil}, 200)

		progress, ok := res["progress"].(map[string]any)
		if !ok {
			t.Fatalf("progress not an object: %v", res["progress"])
		}
		assertEq(t, len(progress), 0)
		assertNil(t, "progress_updated_at", res["progress_updated_at"])
	})

	t.Run("progress not an object", func(t *testing.T) {
		id := newTestContent(t, pool)
		for _, invalid := range []any{5, "x", []any{}} {
			post(t, id, map[string]any{"progress": invalid}, 400)
		}
		assertEq(t, utcCount(t, pool, id), 0)
	})

	t.Run("progress object", func(t *testing.T) {
		id := newTestContent(t, pool)
		res := post(t, id, map[string]any{"progress": map[string]any{"current_page": 3}}, 200)
		assertEq(t, s(res["progress"].(map[string]any)["current_page"]), "3")
		assertNotNil(t, "progress_updated_at", res["progress_updated_at"])

		got := c.Get("/api/content/"+id).Assert(t, 200).JSON()
		userData := got["user_data"].(map[string]any)
		assertEq(t, s(userData["progress"].(map[string]any)["current_page"]), "3")
	})

	t.Run("progress reset", func(t *testing.T) {
		id := newTestContent(t, pool)
		for _, reset := range []any{nil, map[string]any{}} {
			post(t, id, map[string]any{"progress": map[string]any{"current_page": 4}}, 200)

			res := post(t, id, map[string]any{"progress": reset}, 200)
			assertEq(t, len(res["progress"].(map[string]any)), 0)
			assertNil(t, "progress_updated_at", res["progress_updated_at"])
		}
	})

	t.Run("empty body", func(t *testing.T) {
		id := newTestContent(t, pool)
		res := post(t, id, map[string]any{}, 200)
		assertEq(t, s(res["starred"]), "false")
		assertEq(t, len(res["progress"].(map[string]any)), 0)
		assertNil(t, "status", res["status"])
		assertEq(t, utcCount(t, pool, id), 1)

		post(t, id, map[string]any{"status": "reading", "rating": 8}, 200)

		res = post(t, id, map[string]any{}, 200)
		assertEq(t, s(res["status"]), "reading")
		assertEq(t, s(res["rating"]), "8")
		assertEq(t, utcCount(t, pool, id), 1)
	})

	t.Run("clear fields", func(t *testing.T) {
		id := newTestContent(t, pool)
		post(t, id, map[string]any{"status": "reading", "notes": "hello", "rating": 8}, 200)

		res := post(t, id, map[string]any{"status": nil, "notes": nil, "rating": nil}, 200)
		assertNil(t, "status", res["status"])
		assertNil(t, "notes", res["notes"])
		assertNil(t, "rating", res["rating"])
	})

	t.Run("partial update", func(t *testing.T) {
		id := newTestContent(t, pool)
		first := post(t, id, map[string]any{
			"status": "reading", "rating": 8, "starred": true,
			"progress": map[string]any{"current_page": 2},
		}, 200)
		assertNotNil(t, "status_updated_at", first["status_updated_at"])
		assertNotNil(t, "progress_updated_at", first["progress_updated_at"])

		res := post(t, id, map[string]any{"notes": "hello"}, 200)
		assertEq(t, s(res["notes"]), "hello")
		assertEq(t, s(res["status"]), "reading")
		assertEq(t, s(res["rating"]), "8")
		assertEq(t, s(res["starred"]), "true")
		assertEq(t, s(res["progress"].(map[string]any)["current_page"]), "2")
		assertEq(t, s(res["status_updated_at"]), s(first["status_updated_at"]))
		assertEq(t, s(res["progress_updated_at"]), s(first["progress_updated_at"]))
	})
}

func TestListSortReleaseDate(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	ctx := context.Background()

	libID := models.MakeLibraryID()
	if _, err := pool.Exec(ctx,
		"INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", libID); err != nil {
		t.Fatalf("insert library: %v", err)
	}

	// A year-only date broke the old `::date` cast.
	dates := []string{"2015-03-01", "2014", "2014-06-15T00:00:00Z", ""}
	ids := make([]string, len(dates))
	for i, date := range dates {
		ids[i] = models.MakeContentID()
		if _, err := pool.Exec(ctx, `
			INSERT INTO content (id, uri_part, uri, type, library_id)
			VALUES ($1, $1, 'file:///lib/' || $1, 'comic', $2)
		`, ids[i], libID); err != nil {
			t.Fatalf("insert content: %v", err)
		}
		if date == "" {
			continue
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO content_metadata (uri, library_id, data)
			VALUES ('file:///lib/' || $1, $2, jsonb_build_object('publication_date', $3::text))
		`, ids[i], libID, date); err != nil {
			t.Fatalf("insert metadata: %v", err)
		}
	}

	for _, tc := range []struct {
		order string
		want  []string
	}{
		{"asc", []string{ids[3], ids[1], ids[2], ids[0]}},
		{"desc", []string{ids[0], ids[2], ids[1], ids[3]}},
	} {
		res := c.Get("/api/content?library_id="+libID+"&sort=release_date&sort_order="+tc.order).
			Assert(t, 200).JSON()
		var got []string
		for _, item := range res["data"].([]any) {
			got = append(got, s(item.(map[string]any)["id"]))
		}
		assertEq(t, strings.Join(got, ","), strings.Join(tc.want, ","))
	}
}

type recentFixture struct {
	t      *testing.T
	pool   *pgxpool.Pool
	libID  string
	userID string
	base   time.Time
}

func (f *recentFixture) exec(sql string, args ...any) {
	f.t.Helper()
	if _, err := f.pool.Exec(context.Background(), sql, args...); err != nil {
		f.t.Fatalf("%s: %v", sql, err)
	}
}

func (f *recentFixture) content(typ string, parentID *string, order int) string {
	f.t.Helper()
	id := models.MakeContentID()
	f.exec(`
		INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, "order")
		VALUES ($1, $1, 'file:///lib/' || $1, $2, $3, $4, $5)
	`, id, typ, f.libID, parentID, order)
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

// at is the fixture time m minutes after the base.
func (f *recentFixture) at(m int) *time.Time {
	t := f.base.Add(time.Duration(m) * time.Minute)
	return &t
}

// set writes a user_to_content row for the fixture user; an empty status is NULL, and a
// progress time sets a non-empty progress.
func (f *recentFixture) set(id, status string, progressAt, statusAt *time.Time) {
	f.t.Helper()
	f.setFor(f.userID, id, status, progressAt, statusAt)
}

func (f *recentFixture) setFor(userID, id, status string, progressAt, statusAt *time.Time) {
	f.t.Helper()
	f.exec(`
		INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at, progress, progress_updated_at)
		SELECT $1, $2, library_id, uri, NULLIF($3, ''), $4,
			CASE WHEN $5::timestamptz IS NULL THEN '{}' ELSE '{"current_page": 1}' END::jsonb, $5
		FROM content WHERE id = $6
		ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status,
			status_updated_at = EXCLUDED.status_updated_at, progress = EXCLUDED.progress,
			progress_updated_at = EXCLUDED.progress_updated_at
	`, models.MakeUserToContentID(), userID, status, statusAt, progressAt, id)
}

func TestRecentlyRead(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	me := c.Get("/api/users/me").Assert(t, 200).JSON()

	f := &recentFixture{t: t, pool: pool, libID: models.MakeLibraryID(), userID: s(me["id"]),
		base: time.Now().Add(-time.Hour).UTC()}
	f.exec("INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", f.libID)

	get := func(t *testing.T, cl *testClient, query string) []map[string]any {
		t.Helper()
		return cl.Get("/api/content/recently-read"+query).Assert(t, 200).JSONArray()
	}
	// expect asserts the entries, as "item" or "item@series" strings.
	expect := func(t *testing.T, want ...string) {
		t.Helper()
		var got []string
		for _, e := range get(t, c, "") {
			id := s(e["item"].(map[string]any)["id"])
			if series, ok := e["series"].(map[string]any); ok {
				id += "@" + s(series["id"])
			}
			got = append(got, id)
		}
		assertEq(t, strings.Join(got, ","), strings.Join(want, ","))
	}
	run := func(name string, fn func(t *testing.T)) {
		t.Run(name, func(t *testing.T) {
			f.t = t
			f.exec("DELETE FROM user_to_content")
			fn(t)
		})
	}

	run("two reading children", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "reading", f.at(1), f.at(0))
		f.set(kids[1], "reading", f.at(2), f.at(0))
		res := get(t, c, "")
		assertLen(t, res, 1)
		item, ser := res[0]["item"].(map[string]any), res[0]["series"].(map[string]any)
		assertEq(t, s(item["id"]), kids[1])
		assertEq(t, s(item["user_data"].(map[string]any)["status"]), "reading")
		assertEq(t, s(ser["id"]), series)
		assertEq(t, s(ser["children_count"]), "3")
		assertEq(t, s(ser["unread_children_count"]), "3")
	})

	run("anchor completed", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", f.at(0), f.at(1))
		expect(t, kids[1]+"@"+series)
		res := get(t, c, "")
		assertNil(t, "user_data", res[0]["item"].(map[string]any)["user_data"])
		assertEq(t, s(res[0]["series"].(map[string]any)["unread_children_count"]), "2")
	})

	run("mark until", func(t *testing.T) {
		series, kids := f.series(4)
		f.set(kids[3], "reading", f.at(0), f.at(0))
		c.Post("/api/content/"+series+"/series-item-statuses", map[string]any{
			"status": "completed", "until_id": kids[1],
		}).Assert(t, 200)
		expect(t, kids[2]+"@"+series)
	})

	run("wraps to an earlier unread child", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[1], "completed", nil, f.at(0))
		f.set(kids[2], "completed", nil, f.at(1))
		expect(t, kids[0]+"@"+series)
	})

	run("all read", func(t *testing.T) {
		_, kids := f.series(2)
		f.set(kids[0], "completed", nil, f.at(0))
		f.set(kids[1], "dropped", nil, f.at(1))
		expect(t)
	})

	run("invalid children", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(1))
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[1])
		expect(t, kids[2]+"@"+series)
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[2])
		expect(t)
	})

	run("on hold children", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(kids[1], "on_hold", nil, f.at(0))
		expect(t, kids[2]+"@"+series)
		f.set(kids[2], "on_hold", nil, f.at(0))
		expect(t)
		got := c.Get("/api/content/"+series).Assert(t, 200).JSON()
		assertEq(t, s(got["unread_children_count"]), "2")
	})

	run("peek with a reading child", func(t *testing.T) {
		series, kids := f.series(6)
		f.set(kids[4], "reading", f.at(1), f.at(0))
		f.set(kids[1], "completed", f.at(3), f.at(-5))
		standalone := f.content("comic", nil, 0)
		f.set(standalone, "reading", f.at(2), f.at(0))
		expect(t, kids[4]+"@"+series, standalone)
	})

	run("peek with no reading child", func(t *testing.T) {
		series, kids := f.series(10)
		f.set(kids[7], "completed", nil, f.at(1))
		f.set(kids[1], "completed", f.at(2), f.at(-5))
		expect(t, kids[8]+"@"+series)
	})

	run("stale reading", func(t *testing.T) {
		series, kids := f.series(10)
		f.set(kids[9], "reading", f.at(0), f.at(0))
		f.set(kids[7], "completed", nil, f.at(1))
		expect(t, kids[8]+"@"+series)
	})

	run("marked completed from the series page", func(t *testing.T) {
		series, kids := f.series(6)
		f.set(kids[4], "reading", f.at(1), f.at(0))
		f.set(kids[2], "completed", nil, f.at(2))
		expect(t, kids[3]+"@"+series)
		f.set(kids[3], "completed", nil, f.at(0))
		expect(t, kids[4]+"@"+series)
	})

	run("two reading children, earlier one newer", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "reading", f.at(2), f.at(0))
		f.set(kids[1], "reading", f.at(1), f.at(0))
		expect(t, kids[0]+"@"+series)
	})

	run("progress without status", func(t *testing.T) {
		series, kids := f.series(4)
		f.set(kids[0], "", f.at(2), nil)
		f.set(kids[2], "completed", nil, f.at(1))
		expect(t, kids[0]+"@"+series)
	})

	run("plan to read being read", func(t *testing.T) {
		series, kids := f.series(5)
		f.set(kids[1], "plan_to_read", f.at(2), f.at(0))
		f.set(kids[3], "completed", nil, f.at(1))
		expect(t, kids[1]+"@"+series)
	})

	run("reading ranks by its newer status time", func(t *testing.T) {
		series, kids := f.series(4)
		f.set(kids[0], "reading", f.at(0), f.at(3))
		f.set(kids[2], "completed", nil, f.at(2))
		expect(t, kids[0]+"@"+series)
	})

	run("next child plan to read", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(kids[1], "plan_to_read", nil, f.at(0))
		expect(t, kids[1]+"@"+series)
	})

	run("anchor on hold", func(t *testing.T) {
		series, kids := f.series(3)
		f.set(kids[0], "completed", nil, f.at(0))
		f.set(kids[1], "on_hold", f.at(2), f.at(1))
		expect(t, kids[2]+"@"+series)
	})

	run("status-only bulk completed", func(t *testing.T) {
		series, kids := f.series(5)
		for _, id := range kids[:3] {
			f.set(id, "completed", nil, f.at(1))
		}
		expect(t, kids[3]+"@"+series)
	})

	run("only plan to read", func(t *testing.T) {
		_, kids := f.series(3)
		f.set(kids[1], "plan_to_read", nil, f.at(0))
		expect(t)
	})

	run("series status", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(kids[0], "completed", nil, f.at(1))
		want := kids[1] + "@" + series
		for _, status := range []string{"dropped", "on_hold"} {
			f.set(series, status, nil, f.at(0))
			expect(t)
			c.Patch("/api/users/me/preferences", map[string]any{"home": map[string]any{"ignoreSeriesStatus": true}}).
				Assert(t, 200)
			expect(t, want)
			c.Patch("/api/users/me/preferences", map[string]any{"home": nil}).Assert(t, 200)
		}
		for _, status := range []string{"plan_to_read", "completed"} {
			f.set(series, status, nil, f.at(0))
			expect(t, want)
		}
		f.exec("DELETE FROM user_to_content WHERE uri = 'file:///lib/' || $1", series)
		expect(t, want)
	})

	run("malformed preferences", func(t *testing.T) {
		series, kids := f.series(2)
		f.set(kids[0], "completed", nil, f.at(1))
		f.set(series, "dropped", nil, f.at(0))
		for _, raw := range []string{`[1,2]`, `{"home": {"ignoreSeriesStatus": "yes"}}`} {
			f.exec("UPDATE users SET preferences = $1 WHERE id = $2", json.RawMessage(raw), f.userID)
			expect(t)
		}
		f.exec("UPDATE users SET preferences = '{}' WHERE id = $1", f.userID)
	})

	run("standalone", func(t *testing.T) {
		reading, completed := f.content("comic", nil, 0), f.content("comic", nil, 0)
		f.set(reading, "reading", nil, f.at(1))
		f.set(completed, "completed", f.at(2), f.at(2))
		expect(t, reading)
	})

	run("groups before the limit", func(t *testing.T) {
		series, kids := f.series(3)
		for i, id := range kids {
			f.set(id, "reading", f.at(i+1), f.at(0))
		}
		standalone := f.content("comic", nil, 0)
		f.set(standalone, "reading", f.at(0), f.at(0))
		assertLen(t, get(t, c, "?limit=2"), 2)
		assertLen(t, get(t, c, "?limit=1"), 1)
		expect(t, kids[2]+"@"+series, standalone)
	})

	run("order", func(t *testing.T) {
		a, aKids := f.series(2)
		b, bKids := f.series(2)
		f.set(aKids[0], "reading", f.at(1), f.at(0))
		f.set(bKids[0], "reading", f.at(3), f.at(0))
		// No progress: sorts by its status time.
		standalone := f.content("comic", nil, 0)
		f.set(standalone, "reading", nil, f.at(2))
		expect(t, bKids[0]+"@"+b, standalone, aKids[0]+"@"+a)
	})

	run("other users", func(t *testing.T) {
		member, memberID := newMemberClient(t, c)
		item := f.content("comic", nil, 0)
		f.setFor(memberID, item, "reading", f.at(0), f.at(0))
		expect(t)
		res := get(t, member, "")
		assertLen(t, res, 1)
		assertEq(t, s(res[0]["item"].(map[string]any)["id"]), item)
	})

	run("limit bounds", func(t *testing.T) {
		c.Get("/api/content/recently-read?limit=0").Assert(t, 400)
		c.Get("/api/content/recently-read?limit=51").Assert(t, 400)
	})
}
