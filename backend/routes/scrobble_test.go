package routes

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"voltis/db"
	"voltis/models"
	"voltis/providers/mangabaka"
)

func TestScrobbling(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	c, memberID := newMemberClient(t, admin)
	f := newRecentFixture(t, pool, c)
	ctx := context.Background()
	query := func(sql string, args ...any) string {
		t.Helper()
		v, err := db.SelectScalar[string](ctx, pool, sql, args...)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return v
	}
	link := func(series, external string) {
		t.Helper()
		f.exec(`INSERT INTO provider_entries (provider, external_id, raw, canonical_id, fetched_at, refresh_at)
			VALUES ('mangabaka', $1, '{}', $1, now(), now())`, external)
		f.exec(`INSERT INTO metadata_links (content_id, provider, library_id, state, external_id, origin)
			SELECT id, 'mangabaka', library_id, 'linked', $2, 'manual' FROM content WHERE id = $1`, series, external)
	}
	// pending reads a series' scrobble: "-" without a row, else whether it's held and pending.
	pending := func(series string) string {
		t.Helper()
		return query(`SELECT COALESCE((SELECT concat_ws(' ', CASE WHEN held THEN 'held' END,
			CASE WHEN pending_since IS NOT NULL THEN 'pending' END) FROM scrobbles
			WHERE user_id = $1 AND content_id = $2), '-')`, memberID, series)
	}
	put := func(path string, body any) *response { return c.do(http.MethodPut, path, body) }

	// Connect.
	c.mb.Token("mb-good")
	const account = "/api/users/me/scrobbling"
	assertEq(t, s(put(account+"/mangabaka", map[string]any{"token": "mb-wrong"}).Assert(t, 400).JSON()["error"]),
		"MangaBaka rejected this token.")
	put(account+"/mangabaka", map[string]any{"token": " mb-good "}).Assert(t, 200)
	res := c.Get(account).Assert(t, 200)
	assertEq(t, asJSON(t, res.JSON()["mangabaka"]),
		`{"connected":true,"private_new":false,"rejected":false,"remote_name":"Paper Moth"}`)
	if strings.Contains(string(res.Body), "mb-good") {
		t.Fatalf("the token is in %s", res.Body)
	}

	// Every write path that changes completion or a series status queues its series.
	series, kids := f.series(5)
	link(series, "500")
	opds := seedOPDS(t, pool, memberID)
	link(opds.SeriesID, "600")
	key := s(c.Post("/api/users/me/app-keys", map[string]any{"name": "Reader"}).Assert(t, 201).JSON()["key"])
	f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status) VALUES ($1, $2, $3, 'gone/ch9', 'completed')`,
		models.MakeUserToContentID(), memberID, f.libID)

	const writer = "tscrobblescrobble"
	seq := 0
	reading := func(id string, body map[string]any) {
		t.Helper()
		if op := body["op"]; op == "position" || op == "finish" || op == "series_status" || op == "restore" {
			seq++
			body["writer_id"], body["seq"] = writer, seq
			body["base_revision"] = readingOf(t, c, id)["revision"]
		}
		c.Post("/api/content/"+id+"/reading", body).Assert(t, 200)
	}
	bulk := func(body map[string]any) { c.Post("/api/content/bulk/user-data", body).Assert(t, 200) }
	for _, tc := range []struct {
		name   string
		do     func()
		series string
		want   bool
	}{
		{"finish", func() { reading(kids[0], map[string]any{"op": "finish"}) }, series, true},
		{"page turn", func() {
			f.set(kids[1], "reading", nil, nil)
			reading(kids[1], map[string]any{"op": "position", "progress": map[string]any{"current_page": 3}})
		}, series, false},
		{"mark completed", func() { reading(kids[2], map[string]any{"op": "mark_completed"}) }, series, true},
		{"series status", func() {
			reading(kids[0], map[string]any{"op": "series_status", "status": "on_hold"})
		}, series, true},
		{"restore", func() {
			st := readingOf(t, c, series)
			reading(kids[0], map[string]any{"op": "restore", "snapshot": map[string]any{"status": "completed"},
				"series": map[string]any{"revision": st["revision"], "status": "reading"}})
		}, series, true},
		{"clear", func() { reading(kids[2], map[string]any{"op": "clear"}) }, series, true},
		{"series clear", func() {
			c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "clear"}).Assert(t, 200)
		}, series, true},
		{"bulk reset", func() {
			f.set(kids[0], "completed", nil, nil)
			bulk(map[string]any{"action": "reset", "ids": []string{series}})
		}, series, true},
		{"mark through", func() {
			c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "mark_through",
				"until_id": kids[1]}).Assert(t, 200)
		}, series, true},
		{"bulk status", func() {
			bulk(map[string]any{"action": "set_status", "status": "on_hold", "ids": []string{kids[0]}})
		}, series, true},
		{"bulk status, unread item", func() {
			bulk(map[string]any{"action": "set_status", "status": "on_hold", "ids": []string{kids[3]}})
		}, series, false},
		{"opds", func() { c.Get("/opds/"+key+"/pse/"+opds.Vol1+"/2").Assert(t, 200) }, opds.SeriesID, true},
		{"opds, standalone", func() { c.Get("/opds/"+key+"/pse/"+opds.Lantern+"/0").Assert(t, 200) },
			opds.Lantern, false},
		{"broken refs", func() {
			c.Post("/api/content/broken-refs/"+f.libID, map[string]any{"moves": []map[string]any{
				{"pairs": map[string]string{"gone/ch9": query("SELECT uri FROM content WHERE id = $1", kids[4])}}}}).
				Assert(t, 200)
		}, series, true},
	} {
		f.exec("DELETE FROM scrobbles")
		tc.do()
		if got := pending(tc.series) == "pending"; got != tc.want {
			t.Fatalf("%s: queued = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Hold and release.
	scrobble := "/api/content/" + series + "/scrobble"
	put(scrobble, map[string]any{"held": true}).Assert(t, 200)
	assertEq(t, pending(series), "held")
	put(scrobble, map[string]any{"held": false}).Assert(t, 200)
	assertEq(t, pending(series), "pending")
	assertEq(t, query("SELECT (due_at - changed_at)::text FROM scrobbles WHERE content_id = $1", series), "00:05:00")
	got := c.Get(scrobble).Assert(t, 200).JSON()
	assertEq(t, s(got["available"])+" "+s(got["state"])+" "+asJSON(t, got["remote"]), "ok pending null")

	// Send now skips a failed row; a retry by id clears its error.
	other, _ := f.series(1)
	link(other, "700")
	f.exec(`INSERT INTO scrobbles (user_id, provider, content_id, pending_since, changed_at, error)
		VALUES ($1, 'mangabaka', $2, now(), now(), 'Bad thing')`, memberID, other)
	c.Post("/api/scrobbles/send", nil).Assert(t, 200)
	due := func(id string) string {
		return query("SELECT concat_ws(' ', (due_at <= now())::text, error) FROM scrobbles WHERE content_id = $1", id)
	}
	assertEq(t, due(series)+", "+due(other), "true, Bad thing")
	var states []string
	for _, r := range c.Get("/api/scrobbles").Assert(t, 200).JSONArray() {
		states = append(states, s(r["state"])+" "+s(r["content_id"] == series))
	}
	assertEq(t, strings.Join(states, ", "), "failed false, pending true")
	c.Post("/api/scrobbles/send", map[string]any{"content_ids": []string{other}}).Assert(t, 200)
	assertEq(t, due(other), "true")

	// Switching scrobbling off for a library drops its pending rows; a malformed switch is on.
	c.Patch("/api/users/me/preferences", map[string]any{"libraries": map[string]any{
		f.libID: map[string]any{"scrobble": false}}}).Assert(t, 200)
	assertEq(t, pending(series)+", "+pending(other), ", ")
	c.Patch("/api/users/me/preferences", map[string]any{"libraries": map[string]any{
		f.libID: map[string]any{"scrobble": "junk"}}}).Assert(t, 200)
	reading(kids[3], map[string]any{"op": "mark_completed"})
	assertEq(t, pending(series), "pending")
	c.Post("/api/content/"+opds.SeriesID+"/scrobble/send", map[string]any{"force": true}).Assert(t, 200)
	assertEq(t, pending(opds.SeriesID), "pending")
	admin.Post("/api/libraries/"+opds.LibraryID, map[string]any{"name": "Test Shelf",
		"sources": []map[string]any{{"path_uri": t.TempDir()}}, "settings": map[string]any{"scrobbling": false}}).
		Assert(t, 200)
	assertEq(t, pending(opds.SeriesID)+", "+pending(series), ", pending")
	assertEq(t, s(c.Get("/api/content/"+opds.SeriesID+"/scrobble").Assert(t, 200).JSON()["available"]), "library_off")

	// Sync all queues the linked series with progress only.
	unlinked, unlinkedKids := f.series(1)
	f.set(unlinkedKids[0], "completed", nil, nil)
	f.exec("DELETE FROM scrobbles")
	sync := c.Post(account+"/mangabaka/sync-all", nil).Assert(t, 200).JSON()
	assertEq(t, s(sync["count"])+" "+pending(series)+" "+pending(other)+" "+pending(unlinked), "1 pending - -")
	assertEq(t, s(c.Get("/api/content/"+unlinked+"/scrobble").Assert(t, 200).JSON()["available"]), "not_linked")

	// Recently sent lists sent series, newest first.
	f.exec(`INSERT INTO scrobbles (user_id, provider, content_id, sent_at) VALUES
		($1, 'mangabaka', $2, now() - interval '1 hour'), ($1, 'mangabaka', $3, now())
		ON CONFLICT (user_id, provider, content_id) DO UPDATE SET sent_at = EXCLUDED.sent_at`, memberID, series, other)
	var recent []string
	for _, r := range c.Get("/api/scrobbles/recent").Assert(t, 200).JSONArray() {
		recent = append(recent, s(r["content_id"] == other))
	}
	assertEq(t, strings.Join(recent, " "), "true false")

	// Series linked to one MangaBaka entry are sent as one: the dialog shows the merged values, what
	// the entry keeps over them, and the other series. Held ones don't count.
	x, _ := f.series(1)
	y, _ := f.series(1)
	z, _ := f.series(1)
	link(x, "800")
	for _, id := range []string{y, z} {
		f.exec(`INSERT INTO metadata_links (content_id, provider, library_id, state, external_id, origin)
			SELECT id, 'mangabaka', library_id, 'linked', '800', 'manual' FROM content WHERE id = $1`, id)
	}
	f.exec(`UPDATE content SET data = jsonb_build_object('title', CASE id WHEN $1 THEN 'Brass Compass' ELSE 'Dusk Ledger' END)
		WHERE id IN ($1, $2)`, y, z)
	f.set(x, "plan_to_read", nil, nil)
	f.set(y, "reading", nil, nil)
	f.set(z, "completed", nil, nil)
	f.exec(`INSERT INTO scrobbles (user_id, provider, content_id, held) VALUES ($1, 'mangabaka', $2, true)`, memberID, z)
	c.mb.Put("mb-good", "800", mangabaka.Entry{State: "rereading"})
	shared := c.Get("/api/content/"+x+"/scrobble").Assert(t, 200).JSON()
	var linked []string
	for _, l := range shared["linked"].([]any) {
		l := l.(map[string]any)
		linked = append(linked, s(l["content_id"])+" "+strconv.FormatBool(l["held"] == true))
	}
	assertEq(t, asJSON(t, shared["ours"])+" "+asJSON(t, shared["ahead"])+" "+strings.Join(linked, ","),
		`{"state":"reading"} ["state"] `+y+" false,"+z+" true")

	// Disconnecting drops pending scrobbles and keeps holds.
	put("/api/content/"+other+"/scrobble", map[string]any{"held": true}).Assert(t, 200)
	c.Delete(account+"/mangabaka").Assert(t, 200)
	assertEq(t, pending(series)+", "+pending(other), ", held")
	assertEq(t, asJSON(t, c.Get(account).Assert(t, 200).JSON()["mangabaka"]),
		`{"connected":false,"private_new":false,"rejected":false,"remote_name":null}`)

	// A rejected token shows to members, as needing input, and counts as unread.
	f.exec(`INSERT INTO notifications (id, user_id, kind, group_key, data, alerted_at)
		VALUES ($1, $2, 'scrobble_auth', 'scrobble_auth', '{"count": 1}', now())`, models.MakeNotificationID(), memberID)
	feed := feedOf(t, c, "")
	items := feedItems(feed, "needs_input")
	assertEq(t, strconv.Itoa(len(items))+" "+s(items[0]["kind"])+" "+s(feed["unread"]), "1 scrobble_auth 1")
}
