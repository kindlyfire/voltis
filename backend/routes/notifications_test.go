package routes

import (
	"context"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/notify"
	"voltis/models"
)

func feedOf(t *testing.T, c *testClient, query string) map[string]any {
	t.Helper()
	return c.Get("/api/notifications"+query).Assert(t, 200).JSON()
}

func feedItems(feed map[string]any, section string) []map[string]any {
	var out []map[string]any
	for _, v := range feed[section].([]any) {
		out = append(out, v.(map[string]any))
	}
	return out
}

func TestNotificationFeed(t *testing.T) {
	// The member is a second admin until demoted.
	admin, member, memberID := adminAndMember(t, []string{"ADMIN"})
	pool := admin.pool()
	ctx := context.Background()

	lib := comicLibrary(t, admin)
	mustExec(t, pool, `INSERT INTO content (id, library_id, uri_part, uri, type)
		VALUES ('c_series', $1, 'Moon Saga', 'comic/Moon Saga', 'comic_series')`, lib)
	series, unit := "c_series", "chapter"
	row := notify.Grouped{UserID: memberID, Kind: models.NotificationNewContent,
		GroupKey: "new_content:c_series", ContentID: &series, Count: 1, Unit: &unit}
	upsert := func(at time.Time) {
		t.Helper()
		if err := notify.UpsertGrouped(ctx, pool, []notify.Grouped{row}, true, at); err != nil {
			t.Fatal(err)
		}
	}
	history := func() []map[string]any { return feedItems(feedOf(t, member, ""), "data") }
	seen := func(id, alertedAt any) float64 {
		return member.Post("/api/notifications/seen", map[string]any{
			"items": []map[string]any{{"id": id, "alerted_at": alertedAt}},
		}).Assert(t, 200).JSON()["unread"].(float64)
	}

	// An unread item adds up; a seen mark of a stale version doesn't hide a newer alert.
	now := time.Now().UTC()
	upsert(now)
	old := history()[0]
	upsert(now.Add(time.Second))
	item := history()[0]
	assertEq(t, item["id"], old["id"])
	assertEq(t, item["data"].(map[string]any)["count"].(float64), 2)
	assertEq(t, item["content"].(map[string]any)["title"], "Moon Saga")
	assertEq(t, item["unread"], true)
	assertEq(t, seen(old["id"], old["alerted_at"]), 1)
	assertEq(t, seen(item["id"], item["alerted_at"]), 0)
	assertEq(t, history()[0]["unread"], false)

	// A seen item restarts its count when more arrives.
	upsert(now.Add(2 * time.Second))
	item = history()[0]
	assertEq(t, item["data"].(map[string]any)["count"].(float64), 1)
	assertEq(t, item["unread"], true)

	// Review: only a pass that adds review work creates the item, and an empty review removes it.
	must := func(users []string, err error) []string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return users
	}
	must(notify.MetadataReview(ctx, pool, 3, 0))
	assertLen(t, feedItems(feedOf(t, member, ""), "needs_input"), 0)
	assertEq(t, len(must(notify.MetadataReview(ctx, pool, 3, 2))), 2)
	review := feedItems(feedOf(t, member, ""), "needs_input")
	assertLen(t, review, 1)
	assertEq(t, review[0]["data"].(map[string]any)["count"].(float64), 3)
	must(notify.MetadataReview(ctx, pool, 0, 0))
	assertLen(t, feedItems(feedOf(t, member, ""), "needs_input"), 0)

	// A failed scan alerts every admin.
	task := models.MakeTaskID()
	mustExec(t, pool, `INSERT INTO tasks (id, name, status) VALUES ($1, 'scan_library', $2)`, task, models.TaskStatusFailed)
	must(notify.CreateScanItems(ctx, pool, task, notify.ScanData{LibraryID: lib, LibraryName: "Comics"}))
	must(notify.FinishScan(ctx, pool, task, notify.ScanSummary{Outcome: "failed"}))
	assertEq(t, feedOf(t, member, "")["unread"].(float64), 2)

	// Paging walks the history once.
	var ids []string
	for cursor := ""; ; {
		page := feedOf(t, member, "?limit=1&cursor="+cursor)
		for _, it := range feedItems(page, "data") {
			ids = append(ids, s(it["id"]))
		}
		if page["next"] == nil {
			break
		}
		cursor = s(page["next"])
	}
	assertEq(t, len(ids), 2)
	assertEq(t, len(slices.Compact(slices.Sorted(slices.Values(ids)))), 2)
	member.Get("/api/notifications?cursor=%25").Assert(t, 400)

	// Admin-only items go with the permission, at read time.
	admin.Post("/api/users/"+memberID, map[string]any{"username": "member", "permissions": []string{}}).Assert(t, 200)
	after := feedOf(t, member, "")
	assertLen(t, feedItems(after, "data"), 1)
	assertEq(t, after["unread"].(float64), 1)

	// Dismiss and clear.
	member.Delete("/api/notifications/"+s(item["id"])).Assert(t, 200)
	member.Delete("/api/notifications/"+s(item["id"])).Assert(t, 404)
	upsert(now.Add(3 * time.Second))
	assertEq(t, member.Delete("/api/notifications").Assert(t, 200).JSON()["unread"].(float64), 0)
	assertLen(t, feedItems(feedOf(t, member, ""), "data"), 0)

	// The sweep keeps unread items.
	other := "new_content:other"
	for _, key := range []string{row.GroupKey, other} {
		row.GroupKey = key
		upsert(now)
	}
	if _, err := pool.Exec(ctx, `UPDATE notifications SET updated_at = now() - interval '31 days',
		seen_at = CASE WHEN group_key = $1 THEN alerted_at END WHERE user_id = $2 AND group_key IS NOT NULL`,
		"new_content:c_series", memberID); err != nil {
		t.Fatal(err)
	}
	if n, err := notify.Sweep(ctx, pool); err != nil || n != 1 {
		t.Fatalf("sweep = %d, %v, want the seen item only", n, err)
	}
	left, err := db.SelectScalars[string](ctx, pool, "SELECT group_key FROM notifications WHERE user_id = $1", memberID)
	if err != nil || !slices.Equal(left, []string{other}) {
		t.Fatalf("left = %v, %v", left, err)
	}
}
