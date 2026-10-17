package scrobble

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/models"
	"voltis/providers/mangabaka"
	"voltis/providers/mangabaka/mbtest"

	"github.com/jackc/pgx/v5"
)

func TestWorker(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	mb := mbtest.New(t)
	mb.Token("mb-a")
	mb.Token("mb-b")
	svc := New(pool, mangabaka.NewLibrary(mb.URL, mb.Client(), mb.Limiter()), func([]string) {}, func([]string) {})

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	exec(`INSERT INTO users (id, username) VALUES ('u1', 'lantern'), ('u2', 'moth')`)
	exec(`INSERT INTO scrobble_accounts (user_id, provider, token, remote_id, private_new)
		VALUES ('u1', 'mangabaka', 'mb-a', 'acct-a', true), ('u2', 'mangabaka', 'mb-b', 'acct-b', false)`)
	exec(`INSERT INTO libraries (id, name, type) VALUES ('lc', 'Comics', 'comics'), ('lb', 'Books', 'books')`)
	content := func(lib, typ string, parent *string, parts string, special bool) string {
		t.Helper()
		id := models.MakeContentID()
		exec(`INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, order_parts, special)
			VALUES ($1, $1, $1, $2, $3, $4, $5::real[], $6)`, id, typ, lib, parent, parts, special)
		return id
	}
	link := func(series, external, canonical string) {
		t.Helper()
		exec(`INSERT INTO provider_entries (provider, external_id, raw, merged_into, canonical_id, fetched_at, refresh_at)
			VALUES ('mangabaka', $1, '{}', NULLIF($2, $1), $2, now(), now()) ON CONFLICT DO NOTHING`, external, canonical)
		exec(`INSERT INTO metadata_links (content_id, provider, library_id, state, external_id, origin)
			SELECT id, 'mangabaka', library_id, 'linked', $2, 'manual' FROM content WHERE id = $1`, series, external)
	}
	status := func(user, id, status string) {
		t.Helper()
		exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status)
			SELECT $1, $2, library_id, uri, $4 FROM content WHERE id = $3
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET status = EXCLUDED.status`,
			models.MakeUserToContentID(), user, id, status)
	}

	// Series A: vol 1 with ch 1-2, vol 2 with ch 3, and a special. B is linked to an entry merged
	// into A's. C is a book series.
	a := content("lc", "comic_series", nil, "{}", false)
	ch1, ch2 := content("lc", "comic", &a, "{1,1}", false), content("lc", "comic", &a, "{1,2}", false)
	ch3, special := content("lc", "comic", &a, "{2,3}", false), content("lc", "comic", &a, "{1,99}", true)
	b := content("lc", "comic_series", nil, "{}", false)
	bItem := content("lc", "comic", &b, "{1,1.5}", false)
	c := content("lb", "book_series", nil, "{}", false)
	book1, book2 := content("lb", "book", &c, "{1}", false), content("lb", "book", &c, "{2}", false)
	content("lb", "book", &c, "{3}", false)
	link(a, "100", "100")
	link(b, "200", "100")
	link(c, "400", "400")

	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	at := func(m float64) time.Time { return t0.Add(time.Duration(m * float64(time.Minute))) }
	queue := func(user string, now time.Time, ids ...string) {
		t.Helper()
		err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
			_, err := Queue(ctx, tx, user, ids, now, quiet)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	// step runs a round and returns the requests it made, "<method> <path> <body>".
	clock := t0
	var clockMu sync.Mutex // the write hook moves the clock from the fake's goroutine
	svc.clock = func() time.Time {
		clockMu.Lock()
		defer clockMu.Unlock()
		return clock
	}
	step := func(now time.Time) []string {
		t.Helper()
		clockMu.Lock()
		clock = now
		clockMu.Unlock()
		if _, err := svc.step(ctx, now); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range mb.Requests() {
			out = append(out, strings.TrimSpace(r.Method+" "+r.Path+" "+r.Body))
		}
		return out
	}
	// row is a scrobble's state: "due <minute>" or "idle", then the error, attempts, conflicts,
	// force and sent time that are set.
	row := func(user, series string) string {
		t.Helper()
		var due *time.Time
		var errMsg *string
		var sent *time.Time
		var attempts, conflicts int
		var force bool
		if err := pool.QueryRow(ctx, `SELECT due_at, error, attempts, conflicts, force, sent_at FROM scrobbles
			WHERE user_id = $1 AND content_id = $2`, user, series).
			Scan(&due, &errMsg, &attempts, &conflicts, &force, &sent); err != nil {
			t.Fatalf("row %s %s: %v", user, series, err)
		}
		parts := []string{"idle"}
		if due != nil {
			parts[0] = fmt.Sprintf("due %g", due.Sub(t0).Minutes())
		}
		if errMsg != nil {
			parts = append(parts, "error "+*errMsg)
		}
		if attempts > 0 || conflicts > 0 {
			parts = append(parts, fmt.Sprintf("attempts %d conflicts %d", attempts, conflicts))
		}
		if force {
			parts = append(parts, "force")
		}
		if sent != nil {
			parts = append(parts, fmt.Sprintf("sent %g", sent.Sub(t0).Minutes()))
		}
		return strings.Join(parts, ", ")
	}
	expect := func(got []string, want ...string) {
		t.Helper()
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Fatalf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
		}
	}
	assertRow := func(user, series, want string) {
		t.Helper()
		if got := row(user, series); got != want {
			t.Fatalf("row = %q, want %q", got, want)
		}
	}
	const batchA = "GET /v1/my/library/batch?series_id=100"

	// 1-2. Debounce: 5 quiet minutes, at most 15 after the first change.
	for _, id := range []string{ch1, ch2, special, bItem} {
		status("u1", id, "completed")
	}
	status("u1", a, "reading")
	status("u1", b, "plan_to_read")
	queue("u1", at(0), ch1, ch2, special, bItem)
	expect(step(at(0)))
	assertRow("u1", a, "due 5")
	queue("u1", at(4), ch2)
	assertRow("u1", a, "due 9")
	queue("u1", at(8), ch2)
	queue("u1", at(12), ch2)
	assertRow("u1", a, "due 15")

	// 3. A and B go out as one create, without the special.
	expect(step(at(15)), batchA,
		`POST /v1/my/library/100 {"state":"reading","progress_chapter":2,"progress_volume":1,"is_private":true}`)
	const sent1 = "sent 15"
	assertRow("u1", a, "idle, "+sent1)
	assertRow("u1", b, "idle, "+sent1)

	// 4. A change while the write is in flight stays pending, with its force; a failure of the old
	// version doesn't back the new one off.
	status("u1", ch3, "completed")
	queue("u1", at(20), a)
	exec("UPDATE scrobbles SET force = true, due_at = $1 WHERE content_id = $2", at(20), a)
	mb.OnWrite(func() { queue("u1", at(21), a) })
	expect(step(at(20)), batchA, `PATCH /v1/my/library/100 {"progress_chapter":3,"progress_volume":2}`)
	assertRow("u1", a, `due 26, force, sent 20`)
	mb.OnWrite(func() { queue("u1", at(27), a) })
	mb.FailNext(http.StatusInternalServerError, "")
	expect(step(at(26)), batchA, `PATCH /v1/my/library/100 {"progress_chapter":3,"progress_volume":2}`)
	assertRow("u1", a, `due 32, force, sent 20`)
	mb.OnWrite(nil)
	exec("UPDATE scrobbles SET force = false WHERE content_id = $1", a)

	// 5. MangaBaka ahead: only the volume goes; then nothing does, and no write is made.
	mb.Put("mb-a", "100", mangabaka.Entry{State: "reading", Chapter: num(52), Volume: num(1)})
	expect(step(at(32)), batchA, `PATCH /v1/my/library/100 {"progress_volume":2}`)
	assertRow("u1", a, `idle, sent 32`)
	queue("u1", at(40), a)
	expect(step(at(45)), batchA)
	assertRow("u1", a, `idle, sent 32`)

	// 6. A held row is never sent.
	queue("u1", at(50), a)
	exec("UPDATE scrobbles SET held = true WHERE content_id = $1", a)
	expect(step(at(55)))
	exec("UPDATE scrobbles SET held = false WHERE content_id = $1", a)

	// 7. A refused write fails that series only: another user's goes out in the same round.
	status("u1", a, "completed")
	status("u2", ch1, "completed")
	queue("u1", at(60), a)
	queue("u2", at(60), a)
	mb.FailNext(http.StatusBadRequest, `{"status":400,"message":"Bad thing","code":null}`)
	expect(step(at(65)), batchA, `PATCH /v1/my/library/100 {"state":"completed"}`,
		batchA, `POST /v1/my/library/100 {"state":"reading","progress_chapter":1}`)
	assertRow("u1", a, `idle, error Bad thing, sent 32`)
	assertRow("u2", a, `idle, sent 65`)

	// 8. Transient failures back off, doubling up to an hour.
	queue("u1", at(70), a)
	mb.FailNext(http.StatusInternalServerError, "")
	step(at(75))
	assertRow("u1", a, `due 76, attempts 1 conflicts 0, sent 32`)
	exec("UPDATE scrobbles SET attempts = 9 WHERE content_id = $1", a)
	mb.FailNext(http.StatusInternalServerError, "")
	step(at(76))
	assertRow("u1", a, `due 140, attempts 10 conflicts 0, sent 32`)

	// 9. The entry changing between the read and the write, three times in a row, fails.
	mb.Delete("mb-a", "100")
	queue("u1", at(150), a)
	writes := 0
	mb.OnWrite(func() {
		if writes++; writes%2 == 1 {
			mb.Put("mb-a", "100", mangabaka.Entry{State: "plan_to_read"})
		} else {
			mb.Delete("mb-a", "100")
		}
	})
	step(at(155))
	assertRow("u1", a, `due 156, attempts 1 conflicts 1, sent 32`)
	expect(step(at(156)), batchA, `PATCH /v1/my/library/100 {"state":"completed","progress_chapter":3,"progress_volume":2}`)
	assertRow("u1", a, `due 158, attempts 2 conflicts 2, sent 32`)
	step(at(158))
	assertRow("u1", a, `idle, error MangaBaka entry changed while sending, attempts 3 conflicts 3, `+
		`sent 32`)
	mb.OnWrite(nil)

	// 10. A rejected token pauses the account and tells the user; a reject of a replaced token
	// changes nothing.
	queue("u1", at(160), a)
	mb.FailNext(http.StatusUnauthorized, "")
	step(at(165))
	assertRow("u1", a, `due 165, sent 32`)
	var rejected, notified bool
	if err := pool.QueryRow(ctx, `SELECT rejected_at IS NOT NULL, EXISTS (SELECT 1 FROM notifications
		WHERE user_id = 'u1' AND kind = 'scrobble_auth') FROM scrobble_accounts WHERE user_id = 'u1'`).
		Scan(&rejected, &notified); err != nil || !rejected || !notified {
		t.Fatalf("rejected %v, notified %v (%v)", rejected, notified, err)
	}
	expect(step(at(166)))
	exec("UPDATE scrobble_accounts SET token = 'mb-a2', rejected_at = NULL WHERE user_id = 'u1'")
	if err := svc.Reject(ctx, "u1", "mb-a", at(166)); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, "SELECT rejected_at IS NOT NULL FROM scrobble_accounts WHERE user_id = 'u1'").
		Scan(&rejected); err != nil || rejected {
		t.Fatalf("rejected %v (%v)", rejected, err)
	}
	exec("UPDATE scrobble_accounts SET token = 'mb-a' WHERE user_id = 'u1'")

	// 11. A misread number fails the series before anything is written.
	exec("UPDATE content SET order_parts = '{3,20190}' WHERE id = $1", ch3)
	queue("u1", at(170), a)
	expect(step(at(175)), batchA)
	assertRow("u1", a, `idle, error Chapter 20190 is out of range, sent 32`)

	// 12. A book series sends its highest finished index as the volume, and no chapter.
	status("u1", book1, "completed")
	status("u1", book2, "completed")
	queue("u1", at(180), book2)
	expect(step(at(185)), "GET /v1/my/library/batch?series_id=400",
		`POST /v1/my/library/400 {"state":"reading","progress_volume":2,"is_private":true}`)

	// 13. Each write is stamped with the time after it, not the round's start.
	status("u1", c, "completed")
	status("u2", a, "completed")
	queue("u1", at(190), book2)
	queue("u2", at(190), a)
	mb.OnWrite(func() {
		clockMu.Lock()
		defer clockMu.Unlock()
		clock = clock.Add(time.Minute)
	})
	step(at(195))
	mb.OnWrite(nil)
	assertRow("u1", c, "idle, sent 196")
	assertRow("u2", a, "idle, sent 197")
}
