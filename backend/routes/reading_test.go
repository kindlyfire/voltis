package routes

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
)

// setStatus sets a status through the reading service, as the status dropdowns do.
func setStatus(t *testing.T, c *testClient, id string, status any) map[string]any {
	t.Helper()
	return c.Post("/api/content/"+id+"/reading", map[string]any{"op": "set_status", "status": status}).
		Assert(t, 200).JSON()
}

// readingOf is the user's current reading state of content.
func readingOf(t *testing.T, c *testClient, id string) map[string]any {
	t.Helper()
	return c.Get("/api/content/"+id+"/reading").Assert(t, 200).JSON()["state"].(map[string]any)
}

func TestReadingTransition(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	pos := json.RawMessage(`{"current_page":3}`)
	end := json.RawMessage(`{"at_end":true}`)
	type want struct {
		status, outcome string
		previous, write bool
	}
	events := map[string]map[string]want{
		"": {
			"position": {"reading", "started", false, true},
			"finish":   {"completed", "completed", false, true},
		},
		"plan_to_read": {
			"position": {"reading", "moved_to_reading", true, true},
			"finish":   {"completed", "completed", true, true},
		},
		"on_hold": {
			"position": {"reading", "moved_to_reading", true, true},
			"finish":   {"completed", "completed", true, true},
		},
		"dropped": {
			"position": {"reading", "moved_to_reading", true, true},
			"finish":   {"completed", "completed", true, true},
		},
		"reading": {
			"position": {"reading", "saved", false, true},
			"finish":   {"completed", "completed", false, true},
		},
		"completed": {
			"position": {"completed", "saved", false, true},
			"finish":   {"completed", "none", false, false},
		},
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	for status, ops := range events {
		cur := readingSnapshot{Progress: json.RawMessage(`{"current_page":1}`), LastReadAt: &earlier}
		if status != "" {
			cur.Status = &status
		}
		for op, w := range ops {
			next, outcome, previous, write := transition(cur, readingOp{Op: op, Progress: pos}, end, now)
			if str(next.Status) != w.status || outcome != w.outcome || (previous != nil) != w.previous || write != w.write {
				t.Errorf("%s %s: got %s %s %v %v, want %+v", status, op, str(next.Status), outcome, previous != nil, write, w)
			}
			if previous != nil && str(previous.Status) != status {
				t.Errorf("%s %s: previous %s", status, op, str(previous.Status))
			}
			wantProgress, wantLastRead := pos, &now
			if op == "finish" {
				wantProgress = end
			}
			if !write {
				wantProgress, wantLastRead = cur.Progress, &earlier
			}
			if string(next.Progress) != string(wantProgress) || !next.LastReadAt.Equal(*wantLastRead) {
				t.Errorf("%s %s: progress %s at %v", status, op, next.Progress, next.LastReadAt)
			}
		}
	}

	cur := readingSnapshot{Status: new("on_hold"), Progress: pos, LastReadAt: &earlier}
	for _, status := range []string{"reading", "on_hold", "dropped", "plan_to_read", ""} {
		op := readingOp{Op: "set_status"}
		if status != "" {
			op.Status = &status
		}
		next, outcome, previous, _ := transition(cur, op, end, now)
		if str(next.Status) != status || outcome != "status_set" || previous != nil ||
			string(next.Progress) != string(pos) || !next.LastReadAt.Equal(earlier) {
			t.Errorf("set_status %s: %+v %s", status, next, outcome)
		}
	}
	for _, op := range []readingOp{{Op: "mark_completed"}, {Op: "set_status", Status: new("completed")}} {
		next, outcome, _, _ := transition(cur, op, end, now)
		if str(next.Status) != "completed" || outcome != "completed" || string(next.Progress) != string(end) ||
			!next.LastReadAt.Equal(earlier) {
			t.Errorf("%s: %+v %s", op.Op, next, outcome)
		}
	}
	next, outcome, _, _ := transition(cur, readingOp{Op: "clear"}, end, now)
	if next.Status != nil || string(next.Progress) != "{}" || next.LastReadAt != nil || outcome != "cleared" {
		t.Errorf("clear: %+v %s", next, outcome)
	}
	snap := readingSnapshot{Status: new("dropped"), Progress: pos, LastReadAt: &earlier}
	next, outcome, _, _ = transition(cur, readingOp{Op: "restore", Snapshot: &snap}, end, now)
	if str(next.Status) != "dropped" || string(next.Progress) != string(pos) || !next.LastReadAt.Equal(earlier) ||
		outcome != "restored" {
		t.Errorf("restore: %+v %s", next, outcome)
	}
}

func TestParseRevision(t *testing.T) {
	for token, want := range map[string]struct {
		writer string
		seq    int64
		ok     bool
	}{
		"tabcdefghijklmnop:10": {"tabcdefghijklmnop", 10, true},
		"srv:abcdef":           {"srv", 0, false},
		"opds-ok_1:abc":        {"opds-ok_1", 0, false},
		"nocolon":              {"", 0, false},
	} {
		writer, seq, ok := parseRevision(&token)
		if writer != want.writer || seq != want.seq || ok != want.ok {
			t.Errorf("%s: %s %d %v", token, writer, seq, ok)
		}
	}
	// Numeric, not lexical: 9 was delivered before 10.
	apply, conflict := checkRevision(new("tabcdefghijklmnop:10"), readingRequest{WriterID: "tabcdefghijklmnop", Seq: 9})
	if apply || conflict {
		t.Fatal("seq 9 after 10 was not dropped")
	}
}

func TestReading(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	f := &recentFixture{t: t, pool: pool, libID: models.MakeLibraryID(), userID: s(me["id"]),
		base: time.Now().Add(-time.Hour).UTC()}
	f.exec("INSERT INTO libraries (id, name, type) VALUES ($1, 'lib', 'comics')", f.libID)

	const a, b = "taaaaaaaaaaaaaaaa", "tbbbbbbbbbbbbbbbb"
	post := func(id string, body map[string]any, code int) map[string]any {
		t.Helper()
		return c.Post("/api/content/"+id+"/reading", body).Assert(t, code).JSON()
	}
	seqs := map[string]int{}
	// event sends op as writer w with the next seq of that writer and base revision base.
	event := func(id, w, op string, base any, progress map[string]any, code int) map[string]any {
		t.Helper()
		seqs[w]++
		return post(id, map[string]any{"op": op, "writer_id": w, "seq": seqs[w], "base_revision": base,
			"progress": progress}, code)
	}
	page := func(n int) map[string]any { return map[string]any{"current_page": n} }
	state := func(res map[string]any) map[string]any { return res["state"].(map[string]any) }
	comic := func(parent *string) string {
		t.Helper()
		id := f.content("comic", parent, 0)
		f.exec("UPDATE content SET page_count = 10 WHERE id = $1", id)
		return id
	}

	t.Run("revisions", func(t *testing.T) {
		id := comic(nil)
		res := event(id, a, "position", nil, page(1), 200)
		assertEq(t, res["outcome"], any("started"))
		revA := s(state(res)["revision"])
		assertEq(t, revA, a+":1")
		assertEq(t, res["writer"], any(a))

		// Another writer with a stale base conflicts, and gets the current state and its writer.
		conflict := event(id, b, "position", nil, page(2), 409)
		assertEq(t, s(conflict["state"].(map[string]any)["revision"]), revA)
		assertEq(t, conflict["writer"], any(a))
		res = event(id, b, "position", revA, page(2), 200)
		revB := s(state(res)["revision"])

		// After a page command, a reader must have seen it.
		srv := s(state(setStatus(t, c, id, "on_hold"))["revision"])
		if srv[:4] != "srv:" {
			t.Fatalf("command revision %s", srv)
		}
		assertNil(t, "server writer", c.Get("/api/content/"+id+"/reading").Assert(t, 200).JSON()["writer"])
		event(id, a, "position", revA, page(3), 409)
		event(id, b, "position", revB, page(3), 409)
		res = event(id, b, "position", srv, page(3), 200)

		// A writer's own later write needs its own last revision as base too.
		event(id, b, "position", "x", page(4), 409)
		res = event(id, b, "position", state(res)["revision"], page(4), 200)

		// A request delivered again, or overtaken by the writer's later one, changes nothing.
		body := map[string]any{"op": "position", "writer_id": b, "seq": seqs[b], "base_revision": srv,
			"progress": page(9)}
		assertEq(t, post(id, body, 200)["outcome"], any("none"))
		body["seq"] = 1
		res = post(id, body, 200)
		assertEq(t, res["outcome"], any("none"))
		assertEq(t, s(state(res)["revision"]), b+":"+s(seqs[b]))
		assertEq(t, s(readingOf(t, c, id)["progress"].(map[string]any)["current_page"]), "4")
	})

	t.Run("series actions carry a reader's revision", func(t *testing.T) {
		series, kids := f.series(1)
		c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "mark_series_completed",
			"include_unread": true, "writer_id": a, "seq": 500}).Assert(t, 200)
		res := c.Get("/api/content/"+kids[0]+"/reading").Assert(t, 200).JSON()
		assertEq(t, s(state(res)["revision"]), a+":500")
		assertEq(t, res["writer"], any(a))
		c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "clear", "writer_id": a}).
			Assert(t, 400)

		// Delivered again, or after the writer's later clear: nothing changes.
		seriesAction := func(body map[string]any) int {
			t.Helper()
			res := c.Post("/api/content/"+series+"/series-reading", body).Assert(t, 200).JSON()
			return int(res["count"].(float64))
		}
		complete := map[string]any{"action": "mark_series_completed", "include_unread": true, "writer_id": a, "seq": 500}
		assertEq(t, seriesAction(complete), 0)
		assertEq(t, seriesAction(map[string]any{"action": "clear", "writer_id": a, "seq": 501}), 2)
		assertEq(t, seriesAction(complete), 0)
		assertNil(t, "status", readingOf(t, c, kids[0])["status"])
		assertEq(t, s(readingOf(t, c, series)["revision"]), a+":501")
	})

	t.Run("mark through a volume no longer valid", func(t *testing.T) {
		series, kids := f.series(2)
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[1])
		c.Post("/api/content/"+series+"/series-reading", map[string]any{"action": "mark_through",
			"until_id": kids[1]}).Assert(t, 404)
		assertNil(t, "status", readingOf(t, c, kids[0])["status"])
	})

	t.Run("series status", func(t *testing.T) {
		series, kids := f.series(2)
		setStatus(t, c, series, "on_hold")
		seqs[a]++
		body := map[string]any{"op": "series_status", "status": "reading", "writer_id": a, "seq": seqs[a],
			"base_revision": "stale"}
		res := post(kids[0], body, 200)
		assertEq(t, res["outcome"], any("series_status"))
		info := res["series"].(map[string]any)
		assertEq(t, info["status"], any("reading"))
		assertEq(t, s(info["revision"]), a+":"+s(seqs[a]))
		assertNil(t, "item status", state(res)["status"])

		// Delivered again: applied once.
		assertEq(t, post(kids[0], body, 200)["outcome"], any("none"))
		post(series, map[string]any{"op": "series_status", "status": "reading", "writer_id": a, "seq": 999}, 400)
	})

	t.Run("validation", func(t *testing.T) {
		id := comic(nil)
		for _, body := range []map[string]any{
			{"op": "position", "writer_id": "srv", "seq": 1, "progress": page(1)},
			{"op": "position", "writer_id": "taaaaaaaaaaaaaa:1", "seq": 1, "progress": page(1)},
			{"op": "position", "progress": page(1)},
			{"op": "position", "writer_id": a, "seq": 1, "progress": 5},
			{"op": "restore", "writer_id": a, "seq": 1},
			{"op": "restore", "writer_id": a, "seq": 1, "snapshot": map[string]any{}, "series": map[string]any{"status": "x"}},
			{"op": "set_status", "status": "finished"},
			{"op": "series_status", "status": "reading"},
			{"op": "jump"},
		} {
			post(id, body, 400)
		}
		assertEq(t, utcCount(t, pool, id), 0)
	})

	t.Run("completed items", func(t *testing.T) {
		id := comic(nil)
		res := post(id, map[string]any{"op": "mark_completed"}, 200)
		assertEq(t, s(state(res)["progress"]), s(map[string]any{"current_page": 9, "progress_percent": 100, "at_end": true}))
		res = event(id, a, "position", state(res)["revision"], map[string]any{"current_page": 4, "at_end": true}, 200)
		assertEq(t, res["outcome"], any("saved"))
		st := state(res)
		assertEq(t, st["status"], any("completed"))
		assertEq(t, s(st["progress"]), s(page(4)))
		assertNotNil(t, "last_read_at", st["last_read_at"])
		res = event(id, a, "finish", st["revision"], nil, 200)
		assertEq(t, res["outcome"], any("none"))
		assertEq(t, s(state(res)["revision"]), s(st["revision"]))
	})

	t.Run("held items move to reading", func(t *testing.T) {
		for _, status := range []string{"plan_to_read", "on_hold", "dropped"} {
			id := comic(nil)
			setStatus(t, c, id, status)
			res := event(id, a, "position", readingOf(t, c, id)["revision"], page(2), 200)
			assertEq(t, res["outcome"], any("moved_to_reading"))
			assertEq(t, state(res)["status"], any("reading"))
			assertEq(t, res["previous"].(map[string]any)["status"], any(status))

			id = comic(nil)
			setStatus(t, c, id, status)
			res = event(id, a, "finish", readingOf(t, c, id)["revision"], nil, 200)
			assertEq(t, res["outcome"], any("completed"))
			assertEq(t, state(res)["status"], any("completed"))
			assertEq(t, res["previous"].(map[string]any)["status"], any(status))
		}
		id := comic(nil)
		res := event(id, a, "finish", nil, nil, 200)
		assertNil(t, "previous", res["previous"])
	})

	t.Run("series start", func(t *testing.T) {
		series, kids := f.series(3)
		res := event(kids[0], a, "position", nil, page(1), 200)
		assertNil(t, "previous status", res["series_previous"].(map[string]any)["status"])
		// The response carries the series as the write left it.
		info := res["series"].(map[string]any)
		assertEq(t, info["status"], any("reading"))
		assertEq(t, s(info["revision"]), s(state(res)["revision"]))
		assertEq(t, s(c.Get("/api/content/"+kids[0]+"/reading").Assert(t, 200).JSON()["series"]), s(info))
		res = event(kids[1], a, "position", nil, page(1), 200)
		assertNil(t, "series_previous", res["series_previous"])

		// Never completes on its own, and leaves a held series alone.
		for _, kid := range kids {
			event(kid, a, "finish", readingOf(t, c, kid)["revision"], nil, 200)
		}
		info = c.Get("/api/content/"+kids[0]+"/reading").Assert(t, 200).JSON()["series"].(map[string]any)
		assertEq(t, info["status"], any("reading"))
		assertEq(t, info["caught_up"], any(true))
		setStatus(t, c, series, "on_hold")
		res = event(kids[2], a, "position", readingOf(t, c, kids[2])["revision"], page(1), 200)
		assertNil(t, "series_previous", res["series_previous"])
		assertEq(t, readingOf(t, c, series)["status"], any("on_hold"))

		// A saved position on a reading item starts nothing; a planned series starts.
		series2, kids2 := f.series(1)
		setStatus(t, c, series2, "plan_to_read")
		f.set(kids2[0], "reading", nil, f.at(0))
		res = event(kids2[0], a, "position", nil, page(1), 200)
		assertEq(t, res["outcome"], any("saved"))
		assertEq(t, readingOf(t, c, series2)["status"], any("plan_to_read"))
		setStatus(t, c, kids2[0], "reading")
		assertEq(t, readingOf(t, c, series2)["status"], any("reading"))

		// Starting a volume reopens a completed series, finishing one doesn't. Undoing the reopen
		// puts its status time back, which the series' new volumes count from.
		series3, kids3 := f.series(2)
		done := state(setStatus(t, c, series3, "completed"))
		res = event(kids3[0], a, "finish", nil, nil, 200)
		assertNil(t, "series_previous", res["series_previous"])
		res = event(kids3[1], a, "position", nil, page(1), 200)
		prev := res["series_previous"].(map[string]any)
		assertEq(t, prev["status"], any("completed"))
		assertEq(t, readingOf(t, c, series3)["status"], any("reading"))
		seqs[a]++
		res = post(kids3[1], map[string]any{"op": "series_status", "writer_id": a, "seq": seqs[a],
			"series": prev}, 200)
		assertEq(t, res["series"].(map[string]any)["status"], any("completed"))
		assertEq(t, readingOf(t, c, series3)["status_updated_at"], done["status_updated_at"])
		assertEq(t, readingOf(t, c, kids3[1])["status"], any("reading"))
	})

	t.Run("restore", func(t *testing.T) {
		undo := func(id string, res map[string]any) map[string]any {
			t.Helper()
			seqs[a]++
			return post(id, map[string]any{"op": "restore", "writer_id": a, "seq": seqs[a],
				"base_revision": state(res)["revision"], "snapshot": res["previous"], "series": res["series_previous"]}, 200)
		}
		series, kids := f.series(2)
		setStatus(t, c, kids[0], "on_hold")
		res := event(kids[0], a, "finish", readingOf(t, c, kids[0])["revision"], nil, 200)
		assertNotNil(t, "series_previous", res["series_previous"])
		back := undo(kids[0], res)
		assertEq(t, state(back)["status"], any("on_hold"))
		assertNil(t, "series status", back["series"].(map[string]any)["status"])
		assertNil(t, "series status", readingOf(t, c, series)["status"])

		// The series changed since the start: the restore leaves it.
		res = event(kids[0], a, "position", state(back)["revision"], page(2), 200)
		assertNotNil(t, "series_previous", res["series_previous"])
		setStatus(t, c, series, "reading")
		back = undo(kids[0], res)
		assertEq(t, back["series"].(map[string]any)["status"], any("reading"))
	})

	t.Run("end progress", func(t *testing.T) {
		book := f.content("book", nil, 0)
		locator := map[string]any{"href": "c2.xhtml", "textOffset": 10}
		res := event(book, a, "position", nil, map[string]any{"book": locator, "progress_percent": 40}, 200)
		res = event(book, a, "finish", state(res)["revision"], map[string]any{"book": locator}, 200)
		assertEq(t, s(state(res)["progress"]), s(map[string]any{"book": locator, "progress_percent": 100, "at_end": true}))
		setStatus(t, c, book, "reading")
		res = post(book, map[string]any{"op": "mark_completed"}, 200)
		assertEq(t, s(state(res)["progress"]), s(map[string]any{"progress_percent": 100, "at_end": true}))

		series, _ := f.series(1)
		st := state(setStatus(t, c, series, "completed"))
		assertEq(t, st["status"], any("completed"))
		assertEq(t, s(st["progress"]), s(map[string]any{}))
		post(series, map[string]any{"op": "position", "writer_id": a, "seq": 999, "progress": page(1)}, 400)
	})

	t.Run("clear", func(t *testing.T) {
		series, kids := f.series(2)
		event(kids[0], a, "position", nil, page(3), 200)
		res := post(series, map[string]any{"op": "clear"}, 200)
		assertEq(t, res["outcome"], any("cleared"))
		assertEq(t, s(state(res)), s(readingOf(t, c, series)))
		assertNil(t, "status_updated_at", state(post(kids[0], map[string]any{"op": "clear"}, 200))["status_updated_at"])

		// A never-read volume gets the clear's revision too: reading from before it conflicts.
		series2, kids2 := f.series(1)
		c.Post("/api/content/bulk/user-data", map[string]any{"ids": []string{series2}, "action": "reset"}).
			Assert(t, 200)
		event(kids2[0], a, "position", nil, page(2), 409)
		for _, id := range []string{series, kids[0]} {
			st := readingOf(t, c, id)
			assertNil(t, "status", st["status"])
			assertNil(t, "last_read_at", st["last_read_at"])
			assertEq(t, s(st["progress"]), s(map[string]any{}))
		}
	})
}

func TestPagePercent(t *testing.T) {
	for _, tc := range []struct {
		page, pages int
		want        float64
	}{{4, 10, 40}, {1, 3, 33.3}, {1999, 2000, 99.9}, {0, 1, 0}} {
		if got := pagePercent(tc.page, tc.pages); got != tc.want {
			t.Errorf("%d/%d: %v, want %v", tc.page, tc.pages, got, tc.want)
		}
	}
}

// Reading writes of different users in a library don't wait for each other; one user's writes
// there run one at a time, and a catalog write excludes them all.
func TestReadingWriteLocks(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)
	member, _ := newMemberClient(t, c)
	ctx := context.Background()
	item := f.content("comic", nil, 0)
	hold := func(lock func(pgx.Tx) error) pgx.Tx {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		if err := lock(tx); err != nil {
			t.Fatal(err)
		}
		return tx
	}
	post := func(c *testClient, body map[string]any) <-chan *response {
		done := make(chan *response, 1)
		go func() { done <- c.Post("/api/content/"+item+"/reading", body) }()
		return done
	}
	status := map[string]any{"op": "set_status", "status": "reading"}

	tx := hold(func(tx pgx.Tx) error { return db.LockUserData(ctx, tx, f.userID, f.libID) })
	(<-post(member, status)).Assert(t, 200)
	mine := post(c, status)
	waitBlockedOn(t, pool, "'user-data:'")
	_ = tx.Rollback(ctx)
	(<-mine).Assert(t, 200)

	tx = hold(func(tx pgx.Tx) error { return db.LockMetadata(ctx, tx, f.libID) })
	theirs := post(member, status)
	waitBlockedOn(t, pool, "'metadata:'")
	_ = tx.Rollback(ctx)
	(<-theirs).Assert(t, 200)

	// Concurrent writes of one user see each other's revisions: one of two based on the same
	// revision conflicts.
	base := s(readingOf(t, c, item)["revision"])
	writes := []<-chan *response{}
	for _, w := range []string{"taaaaaaaaaaaaaaaa", "tbbbbbbbbbbbbbbbb"} {
		writes = append(writes, post(c, map[string]any{"op": "position", "writer_id": w, "seq": 1,
			"base_revision": base, "progress": map[string]any{"current_page": 1}}))
	}
	codes := []int{(<-writes[0]).StatusCode, (<-writes[1]).StatusCode}
	slices.Sort(codes)
	assertEq(t, s(codes), s([]int{200, 409}))
}
