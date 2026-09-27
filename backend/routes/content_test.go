package routes

import (
	"context"
	"strings"
	"testing"

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
