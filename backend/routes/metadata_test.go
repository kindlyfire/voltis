package routes

import (
	"context"
	"errors"
	"testing"

	"voltis/db"
	"voltis/metadata"
	"voltis/providers/providertest"

	"github.com/jackc/pgx/v5/pgxpool"
)

// newTestSeries adds a comic series whose file layer has a title and a description.
func newTestSeries(t *testing.T, c *testClient) string {
	t.Helper()
	id := newTestContent(t, c.pool())
	mustExec(t, c.pool(), `UPDATE content SET type = 'comic_series',
		data_raw = '{"v": 2, "file": {"title": "Local", "description": "From the file"}}',
		data = '{"title": "Local", "description": "From the file"}', data_version = $2
		WHERE id = $1`, id, metadata.DataVersion)
	return id
}

func TestMetadataOverrides(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestSeries(t, c)
	path := "/api/metadata/content/" + id + "/overrides"

	c.Post(path, map[string]any{"fields": map[string]any{}}).Assert(t, 400)
	for _, fields := range []map[string]any{{"count": "one"}, {"nope": 1}, {"alt_titles": []string{"x"}}, {"rating": 101}} {
		c.Post(path, map[string]any{"rev": 0, "fields": fields}).Assert(t, 400)
	}

	sock := newFakeSocket(false)
	serveFake(t, c.hub, sock, meID(t, c), true)

	// series_index is hidden for a series, yet saved with the rest, as the editor sends it back.
	v := c.Post(path, map[string]any{"rev": 0, "fields": map[string]any{
		"title": "Mine", "description": nil, "count": 0, "series_index": 1.5,
	}}).Assert(t, 200).JSON()
	merged := v["merged"].(map[string]any)
	if merged["title"] != "Mine" || merged["count"] != 0.0 || merged["description"] != nil || merged["series_index"] != 1.5 {
		t.Fatalf("merged = %v", merged)
	}
	layers := v["layers"].([]any)
	overrides := layers[len(layers)-1].(map[string]any)["fields"].(map[string]any)
	if d, ok := overrides["description"]; !ok || d != nil {
		t.Fatalf("overrides = %v, want the description cleared", overrides)
	}
	assertEq(t, v["overrides_rev"], any(1.0))
	assertEq(t, v["type"], any("comic_series"))
	msg := nextMessage(t, sock)
	assertEq(t, s(msg["type"]), "catalog_changed")
	assertEq(t, s(msg["library_id"]), contentLibrary(t, pool, id))

	c.Post(path, map[string]any{"rev": 0, "fields": map[string]any{}}).Assert(t, 409)
	v = c.Post(path, map[string]any{"rev": 1, "fields": map[string]any{}}).Assert(t, 200).JSON()
	assertEq(t, v["merged"].(map[string]any)["description"], any("From the file"))

	member, _ := newMemberClient(t, c)
	member.Get("/api/metadata/content/"+id).Assert(t, 403)
	c.Get("/api/metadata/content/c_missing").Assert(t, 404)
}

func TestMetadataLinking(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestSeries(t, c)
	c.fake.Put("1", providertest.Series("Remote", metadata.Manga))
	c.fake.Put("2", providertest.Series("Remote Novel", metadata.Novel))
	base := "/api/metadata/content/" + id

	config := c.Get("/api/metadata/config").Assert(t, 200).JSON()
	if len(config["fields"].([]any)) != len(metadata.Defs()) || len(config["providers"].([]any)) != 1 {
		t.Fatalf("config = %v", config)
	}
	cands := c.Get(base+"/candidates?provider=fake&q=remote").Assert(t, 200).JSON()["data"].([]any)
	if len(cands) != 1 || cands[0].(map[string]any)["title"] != "Remote" {
		t.Fatalf("candidates = %v", cands)
	}

	link := func(externalID string, rev any) *response {
		return c.Post(base+"/link", map[string]any{"provider": "fake", "external_id": externalID, "expect_rev": rev})
	}
	link("2", nil).Assert(t, 400)
	c.Post(base+"/link", map[string]any{"provider": "nope", "external_id": "1"}).Assert(t, 400)
	c.fake.Fail(errors.New("down"))
	link("1", nil).Assert(t, 502)
	c.fake.Fail(nil)

	v := link("1", nil).Assert(t, 200).JSON()
	l := v["links"].([]any)[0].(map[string]any)
	if l["state"] != "linked" || l["external_id"] != "1" || l["origin"] != "manual" || v["merged"].(map[string]any)["title"] != "Remote" {
		t.Fatalf("view = %v", v)
	}
	link("1", nil).Assert(t, 409)
	c.Post(base+"/refresh", map[string]any{"provider": "fake"}).Assert(t, 200)
	c.Post(base+"/ignore", map[string]any{"provider": "fake", "expect_rev": 0}).Assert(t, 409)
	v = c.Post(base+"/ignore", map[string]any{"provider": "fake", "expect_rev": l["rev"]}).Assert(t, 200).JSON()
	if l := v["links"].([]any)[0].(map[string]any); l["state"] != "ignored" || l["rejected"].([]any)[0] != "1" {
		t.Fatalf("ignored = %v", l)
	}
	c.Post(base+"/refresh", map[string]any{"provider": "fake"}).Assert(t, 400)

	rev := v["links"].([]any)[0].(map[string]any)["rev"]
	c.Post(base+"/undo", map[string]any{"provider": "fake"}).Assert(t, 400)
	v = c.Post(base+"/undo", map[string]any{"provider": "fake", "expect_rev": rev}).Assert(t, 200).JSON()
	if l := v["links"].([]any)[0].(map[string]any); l["state"] != "linked" || l["external_id"] != "1" {
		t.Fatalf("undone = %v", l)
	}
	c.Post(base+"/undo", map[string]any{"provider": "fake", "expect_rev": rev}).Assert(t, 409)
}

func TestMetadataRefreshNow(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestSeries(t, c)
	c.fake.Put("1", providertest.Series("Remote", metadata.Manga))
	c.Post("/api/metadata/content/"+id+"/link", map[string]any{"provider": "fake", "external_id": "1"}).Assert(t, 200)
	c.fake.Put("1", providertest.Series("Renamed", metadata.Manga))

	member, _ := newMemberClient(t, c)
	member.Post("/api/metadata/refresh", nil).Assert(t, 403)
	c.Post("/api/metadata/refresh", nil).Assert(t, 200)
	waitUntil(t, "the refresh", func() bool {
		return c.Get("/api/metadata/content/"+id).Assert(t, 200).JSON()["merged"].(map[string]any)["title"] == "Renamed"
	})
}

func TestMetadataReview(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	id := newTestSeries(t, c)
	lib := contentLibrary(t, pool, id)
	c.fake.Put("1", providertest.Series("Local", metadata.Manga))
	c.fake.Put("2", providertest.Series("Local", metadata.Manga))

	c.Get("/api/metadata/review?tab=nope").Assert(t, 400)
	c.Post("/api/metadata/review/resolve", map[string]any{"items": make([]map[string]any, 201)}).Assert(t, 400)
	mustExec(t, pool, `UPDATE libraries SET settings = '{"auto_match": {"fake": true}}' WHERE id = $1`, lib)
	c.Post("/api/metadata/match", map[string]any{"library_ids": []string{lib}}).Assert(t, 200)
	var item map[string]any
	waitUntil(t, "the match", func() bool {
		page := c.Get("/api/metadata/review?tab=review&library_id="+lib).Assert(t, 200).JSON()
		if page["total"] != 1.0 {
			return false
		}
		item = page["data"].([]any)[0].(map[string]any)
		return true
	})
	link := item["link"].(map[string]any)
	if item["content"].(map[string]any)["id"] != id || item["local_title"] != "Local" || len(link["candidates"].([]any)) != 2 {
		t.Fatalf("item = %v", item)
	}
	summary := c.Get("/api/metadata/summary").Assert(t, 200).JSON()
	if libs := summary["libraries"].([]any); len(libs) != 1 || s(libs[0].(map[string]any)["library_id"]) != lib ||
		libs[0].(map[string]any)["review"] != 1.0 {
		t.Fatalf("summary = %v", summary)
	}
	waitUntil(t, "the worker's status", func() bool {
		worker := c.Get("/api/metadata/summary").Assert(t, 200).JSON()["worker"].(map[string]any)
		return worker["match_pass"].(map[string]any)["counts"].(map[string]any)["review"] == 1.0
	})

	res := c.Post("/api/metadata/review/resolve", map[string]any{"items": []map[string]any{
		{"content_id": id, "provider": "fake", "action": "link", "external_id": "1", "expect_rev": link["rev"]},
		{"content_id": id, "provider": "fake", "action": "ignore", "expect_rev": link["rev"]},
	}}).Assert(t, 200).JSON()["results"].([]any)
	if res[0].(map[string]any)["ok"] != true || res[1].(map[string]any)["ok"] != false || res[1].(map[string]any)["error"] == nil {
		t.Fatalf("results = %v", res)
	}
	assertEq(t, c.Get("/api/metadata/review?tab=review").Assert(t, 200).JSON()["total"], any(0.0))

	base := "/api/metadata/content/" + id
	v := c.Get(base).Assert(t, 200).JSON()
	rev := v["links"].([]any)[0].(map[string]any)["rev"]
	c.Post(base+"/rematch", map[string]any{"provider": "fake", "expect_rev": rev}).Assert(t, 409)
	v = c.Post(base+"/ignore", map[string]any{"provider": "fake", "expect_rev": rev}).Assert(t, 200).JSON()
	rev = v["links"].([]any)[0].(map[string]any)["rev"]
	// The rejected entry is left out, so the other one is the only match.
	v = c.Post(base+"/rematch", map[string]any{"provider": "fake", "expect_rev": rev}).Assert(t, 200).JSON()
	if l := v["links"].([]any)[0].(map[string]any); l["state"] != "linked" || l["external_id"] != "2" || l["origin"] != "auto" {
		t.Fatalf("rematched = %v", l)
	}
	auto := c.Get("/api/metadata/review?tab=auto").Assert(t, 200).JSON()
	assertEq(t, auto["total"], any(1.0))

	// Rejecting a wrong match rejects the linked entry too, and leaves the series to matching.
	rev = v["links"].([]any)[0].(map[string]any)["rev"]
	v = c.Post(base+"/reject", map[string]any{"provider": "fake", "external_ids": []string{"3"}, "expect_rev": rev}).Assert(t, 200).JSON()
	if l := v["links"].([]any)[0].(map[string]any); l["state"] != "unmatched" || s(l["rejected"]) != "[1 3 2]" {
		t.Fatalf("rejected = %v", l)
	}
	assertEq(t, c.Get("/api/metadata/review?tab=unmatched&q=loc").Assert(t, 200).JSON()["total"], any(1.0))
}

func mustExec(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("exec %s: %v", sql, err)
	}
}

func contentLibrary(t *testing.T, pool *pgxpool.Pool, contentID string) string {
	t.Helper()
	lib, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT library_id FROM content WHERE id = $1", contentID)
	if err != nil {
		t.Fatalf("read library: %v", err)
	}
	return lib
}
