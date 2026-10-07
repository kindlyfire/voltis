package routes

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/fp"
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

// TestReadingTransition runs the transition table that the Android app's port runs too.
func TestReadingTransition(t *testing.T) {
	type state struct {
		Status   *string         `json:"status"`
		Progress json.RawMessage `json:"progress"`
		LastRead *string         `json:"last_read"`
	}
	var cases []struct {
		Name string `json:"name"`
		Cur  state  `json:"cur"`
		Op   struct {
			Op       string          `json:"op"`
			Status   *string         `json:"status"`
			Progress json.RawMessage `json:"progress"`
			Snapshot *state          `json:"snapshot"`
		} `json:"op"`
		End  json.RawMessage `json:"end"`
		Want struct {
			state
			Outcome  string `json:"outcome"`
			Previous bool   `json:"previous"`
			Write    bool   `json:"write"`
		} `json:"want"`
	}
	raw, err := os.ReadFile("testdata/reading_transitions.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cases); err != nil || len(cases) == 0 {
		t.Fatal("no cases", err)
	}
	now := time.Now()
	earlier := now.Add(-time.Hour)
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	at := func(label *string) *time.Time {
		switch str(label) {
		case "now":
			return &now
		case "earlier":
			return &earlier
		}
		return nil
	}
	snapshot := func(s state) readingSnapshot {
		return readingSnapshot{Status: s.Status, Progress: s.Progress, LastReadAt: at(s.LastRead)}
	}
	sameJSON := func(a, b json.RawMessage) bool {
		var x, y any
		return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && reflect.DeepEqual(x, y)
	}
	for _, tc := range cases {
		op := readingOp{Op: tc.Op.Op, Status: tc.Op.Status, Progress: tc.Op.Progress}
		if tc.Op.Snapshot != nil {
			op.Snapshot = new(snapshot(*tc.Op.Snapshot))
		}
		next, outcome, previous, write := transition(snapshot(tc.Cur), op, tc.End, now)
		w := tc.Want
		if !fp.PtrEq(next.Status, w.Status) || outcome != w.Outcome || (previous != nil) != w.Previous ||
			write != w.Write || !sameJSON(next.Progress, w.Progress) {
			t.Errorf("%s: got %v %s %v %v %s", tc.Name, str(next.Status), outcome, previous != nil, write,
				next.Progress)
		}
		if wantAt := at(w.LastRead); (next.LastReadAt == nil) != (wantAt == nil) ||
			wantAt != nil && !next.LastReadAt.Equal(*wantAt) {
			t.Errorf("%s: last read %v", tc.Name, next.LastReadAt)
		}
		if previous != nil && !fp.PtrEq(previous.Status, tc.Cur.Status) {
			t.Errorf("%s: previous %v", tc.Name, str(previous.Status))
		}
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

// TestReadingSeq checks that every state a row reaches gets a greater reading_seq, that the
// endpoints report the row's current one, and that a row leaving live content keeps a versioned
// empty state.
func TestReadingSeq(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)
	ctx := context.Background()

	const w = "taaaaaaaaaaaaaaaa"
	wseq := 0
	seqOf := func(id string) string { // "0" without a row
		t.Helper()
		v, err := db.SelectScalar[string](ctx, pool, `
			SELECT COALESCE((SELECT utc.reading_seq FROM content c JOIN user_to_content utc
				ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $2 WHERE c.id = $1), 0)::text`,
			id, f.userID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	rowID := func(id string) string {
		t.Helper()
		v, err := db.SelectScalar[string](ctx, pool, `SELECT utc.id FROM content c JOIN user_to_content utc
			ON utc.library_id = c.library_id AND utc.uri = c.uri AND utc.user_id = $2 WHERE c.id = $1`, id, f.userID)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	post := func(id string, body map[string]any) map[string]any {
		t.Helper()
		return c.Post("/api/content/"+id+"/reading", body).Assert(t, 200).JSON()
	}
	position := func(id string) {
		wseq++
		cur := c.Get("/api/content/"+id+"/reading").Assert(t, 200).JSON()["state"].(map[string]any)["revision"]
		post(id, map[string]any{"op": "position", "writer_id": w, "seq": wseq, "base_revision": cur,
			"progress": map[string]any{"current_page": 1}})
	}
	seriesReading := func(id string, body map[string]any) map[string]any {
		t.Helper()
		return c.Post("/api/content/"+id+"/series-reading", body).Assert(t, 200).JSON()
	}

	x := f.content("comic", nil, 0)
	f.exec("UPDATE content SET page_count = 10 WHERE id = $1", x)
	series, kids := f.series(3)
	moved, target, dropped := f.content("comic", nil, 0), f.content("comic", nil, 0), f.content("comic", nil, 0)
	f.set(moved, "reading", f.at(1), f.at(1))
	f.set(target, "completed", f.at(2), f.at(2))
	f.set(dropped, "reading", f.at(1), f.at(1))
	all := append([]string{series}, kids...)

	steps := []struct {
		name    string
		touched []string
		do      func()
	}{
		{"position", []string{x}, func() { position(x) }},
		{"position again", []string{x}, func() { position(x) }},
		{"status cleared", []string{x}, func() { setStatus(t, c, x, nil) }},
		{"restore", []string{x}, func() {
			wseq++
			post(x, map[string]any{"op": "restore", "writer_id": w, "seq": wseq, "base_revision": readingOf(t, c, x)["revision"],
				"snapshot": map[string]any{"status": "on_hold", "progress": map[string]any{}}})
		}},
		{"clear", []string{x}, func() { post(x, map[string]any{"op": "clear"}) }},
		{"start series", []string{kids[0], series}, func() { position(kids[0]) }},
		{"bulk completion", all, func() {
			seriesReading(series, map[string]any{"action": "mark_series_completed", "include_unread": true})
		}},
		{"series clear", all, func() {
			res := post(series, map[string]any{"op": "clear"})
			items := res["items"].([]any)
			assertEq(t, len(items), 3)
			for _, it := range items {
				m := it.(map[string]any)
				assertEq(t, s(m["reading_seq"]), seqOf(s(m["id"])))
			}
		}},
		{"move onto a live row", []string{moved, target}, func() {
			c.Post("/api/content/broken-refs/"+f.libID, map[string]any{
				"update": map[string]string{rowID(moved): "file:///lib/" + target},
				"keep":   map[string]string{rowID(moved): "source"},
			}).Assert(t, 200)
			// The source identity stays, empty.
			assertEq(t, f.userData(moved), "- {}")
		}},
		{"delete", []string{dropped}, func() {
			c.Post("/api/content/broken-refs/"+f.libID, map[string]any{"delete": []string{rowID(dropped)}}).
				Assert(t, 200)
			assertEq(t, f.userData(dropped), "- {}")
		}},
	}
	for _, st := range steps {
		before := map[string]int64{}
		for _, id := range st.touched {
			before[id], _ = strconv.ParseInt(seqOf(id), 10, 64)
		}
		st.do()
		for _, id := range st.touched {
			now := seqOf(id)
			if n, _ := strconv.ParseInt(now, 10, 64); n <= before[id] {
				t.Errorf("%s: seq %d, was %d", st.name, n, before[id])
			}
			res := c.Get("/api/content/"+id+"/reading").Assert(t, 200).JSON()
			assertEq(t, s(res["state"].(map[string]any)["reading_seq"]), now)
			detail := c.Get("/api/content/"+id).Assert(t, 200).JSON()["user_data"].(map[string]any)
			assertEq(t, s(detail["reading_seq"]), now)
		}
	}

	t.Run("nested series info and conflicts", func(t *testing.T) {
		got := c.Get("/api/content/"+kids[1]+"/reading").Assert(t, 200).JSON()["series"].(map[string]any)
		assertEq(t, s(got["reading_seq"]), seqOf(series))
		wseq++
		res := c.Post("/api/content/"+x+"/reading", map[string]any{"op": "position", "writer_id": w, "seq": wseq,
			"base_revision": "stale", "progress": map[string]any{"current_page": 1}}).Assert(t, 409).JSON()
		assertEq(t, s(res["state"].(map[string]any)["reading_seq"]), seqOf(x))
		assertEq(t, c.Get("/api/content/"+f.content("comic", nil, 0)+"/reading").Assert(t, 200).
			JSON()["state"].(map[string]any)["reading_seq"], any("0"))
	})

	t.Run("a series response covers its scope on replay", func(t *testing.T) {
		wseq++
		body := map[string]any{"action": "mark_through", "until_id": kids[1], "writer_id": w, "seq": wseq}
		first := seriesReading(series, body)
		assertEq(t, s(first["count"]), "2")
		before := seqOf(kids[1])
		again := seriesReading(series, body)
		assertEq(t, s(again["count"]), "0")
		assertEq(t, seqOf(kids[1]), before)
		assertEq(t, s(again["series"].(map[string]any)["reading_seq"]), seqOf(series))
		items := again["items"].([]any)
		assertEq(t, len(items), 2) // the volumes up to the target, not the last
		for _, it := range items {
			m := it.(map[string]any)
			assertEq(t, s(m["reading_seq"]), seqOf(s(m["id"])))
		}

		// A catalog change since drops volumes from the current scope; the guarded ids keep them.
		ids := func(res map[string]any) string {
			var got []string
			for _, it := range res["items"].([]any) {
				got = append(got, s(it.(map[string]any)["id"]))
			}
			slices.Sort(got)
			return strings.Join(got, ",")
		}
		both := []string{kids[0], kids[1]}
		slices.Sort(both)
		f.exec(`UPDATE content SET "order" = 10 WHERE id = $1`, kids[0])
		body["ids"] = both
		again = seriesReading(series, body)
		assertEq(t, s(again["count"]), "0")
		assertEq(t, ids(again), strings.Join(both, ","))
		// A cutoff that is no longer valid still gets its receipt.
		f.exec("UPDATE content SET valid = false WHERE id = $1", kids[1])
		again = seriesReading(series, body)
		assertEq(t, s(again["count"]), "0")
		assertEq(t, ids(again), strings.Join(both, ","))
		for _, it := range again["items"].([]any) {
			m := it.(map[string]any)
			assertEq(t, s(m["reading_seq"]), seqOf(s(m["id"])))
		}
	})

	t.Run("a replay is recognized by the series row after its volume left", func(t *testing.T) {
		reading, rkids := f.series(2)
		other, _ := f.series(1)
		f.set(reading, "reading", nil, f.at(1)) // startSeries leaves its revision alone
		wseq++
		body := map[string]any{"action": "mark_through", "until_id": rkids[0], "writer_id": w, "seq": wseq,
			"ids": []string{rkids[0]}}
		assertEq(t, s(seriesReading(reading, body)["count"]), "1")
		f.exec("UPDATE content SET parent_id = $2 WHERE id = $1", rkids[0], other)
		again := seriesReading(reading, body)
		assertEq(t, s(again["count"]), "0")
		items := again["items"].([]any)
		assertEq(t, len(items), 1)
		assertEq(t, s(items[0].(map[string]any)["id"]), rkids[0])
		assertEq(t, s(items[0].(map[string]any)["status"]), "completed")
		assertEq(t, s(items[0].(map[string]any)["reading_seq"]), seqOf(rkids[0]))
	})
}
