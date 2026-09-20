package routes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/db"
	"voltis/lib/tasks"
	"voltis/models"
	"voltis/scanner"

	"github.com/jackc/pgx/v5/pgxpool"
)

func fastFlushes(t *testing.T) {
	t.Helper()
	old := scanner.FlushSpacing
	scanner.FlushSpacing = 50 * time.Millisecond
	t.Cleanup(func() { scanner.FlushSpacing = old })
}

func publishEveryTick(t *testing.T) {
	t.Helper()
	old := tasks.PublishInterval
	tasks.PublishInterval = time.Millisecond
	t.Cleanup(func() { tasks.PublishInterval = old })
}

func snapshotOf(t *testing.T, c *testClient, id string) map[string]any {
	t.Helper()
	for _, s := range c.Get("/api/tasks/snapshot?ids="+id).Assert(t, 200).JSONArray() {
		if s["id"] == id {
			return s
		}
	}
	return nil
}

func waitUntil(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func awaitTask(t *testing.T, c *testClient, id, what string, fn func(map[string]any) bool) map[string]any {
	t.Helper()
	var found map[string]any
	waitUntil(t, what, func() bool {
		found = snapshotOf(t, c, id)
		return found != nil && fn(found)
	})
	return found
}

func awaitRetired(t *testing.T, c *testClient) {
	t.Helper()
	waitUntil(t, "the finished task to be retired from the live map", func() bool {
		return len(c.Get("/api/tasks/snapshot").Assert(t, 200).JSONArray()) == 0
	})
}

func blockedScanLibrary(t *testing.T, pool *pgxpool.Pool, c *testClient, mode string) (string, func()) {
	t.Helper()

	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	libID := libraryAt(t, c, "comics", dir)

	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		conn.Release()
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE content IN "+mode+" MODE"); err != nil {
		conn.Release()
		t.Fatalf("lock content: %v", err)
	}

	return libID, sync.OnceFunc(func() {
		_ = tx.Rollback(ctx)
		conn.Release()
	})
}

func TestTaskSnapshotRoute(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	libID, release := blockedScanLibrary(t, pool, c, "ACCESS EXCLUSIVE")
	defer release()

	scan := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	ids, _ := scan["task_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("task_ids = %v, want one ID", scan["task_ids"])
	}
	id := s(ids[0])

	second := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	dupes, _ := second["task_ids"].([]any)
	if len(dupes) != 1 || s(dupes[0]) != id {
		t.Fatalf("duplicate scan returned %v, want the existing ID %s", second["task_ids"], id)
	}

	live := awaitTask(t, c, id, "the scan to report progress", func(snap map[string]any) bool {
		return snap["progress"] != nil
	})
	assertEq(t, s(live["name"]), "scan_library")
	assertEq(t, live["status"].(float64), float64(1))
	if live["output"] == nil {
		t.Fatal("output = null, want {}")
	}

	if _, err := pool.Exec(context.Background(),
		"UPDATE tasks SET status = 3, updated_at = now() - interval '1 hour' WHERE id = $1", id); err != nil {
		t.Fatalf("stale write: %v", err)
	}
	stale := snapshotOf(t, c, id)
	assertEq(t, stale["status"].(float64), float64(1))
	if stale["progress"] == nil {
		t.Fatal("live progress was dropped in favour of a stale stored row")
	}

	if len(c.Get("/api/tasks/snapshot").Assert(t, 200).JSONArray()) != 1 {
		t.Fatal("snapshot without ids did not return the live entry")
	}

	release()
	awaitTask(t, c, id, "the scan to finish", func(snap map[string]any) bool {
		return snap["status"].(float64) >= 2
	})
	awaitRetired(t, c)

	done := snapshotOf(t, c, id)
	assertEq(t, done["status"].(float64), float64(2))
	if done["progress"] != nil {
		t.Fatal("a retired task served from the database carried progress")
	}
	output, _ := done["output"].(map[string]any)
	if output == nil || output["added"] == nil {
		t.Fatalf("output = %v, want the terminal row from the database", done["output"])
	}

	if got := c.Get("/api/tasks/snapshot?ids=t_missing").Assert(t, 200).JSONArray(); len(got) != 0 {
		t.Fatalf("unknown IDs returned %v, want an empty array", got)
	}
}

func TestTaskLogRoute(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	libID, release := blockedScanLibrary(t, pool, c, "EXCLUSIVE")
	defer release()

	scan := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	id := s(scan["task_ids"].([]any)[0])

	awaitTask(t, c, id, "the walk failure to be logged", func(snap map[string]any) bool {
		return snap["log_len"].(float64) > 0
	})

	full := c.Get("/api/tasks/"+id+"/logs?offset=0").Assert(t, 200).JSON()
	text := s(full["text"])
	length := int(full["len"].(float64))
	assertEq(t, full["offset"].(float64), float64(0))
	if length != len(text) || length == 0 {
		t.Fatalf("len = %d, text is %d bytes", length, len(text))
	}
	if !strings.Contains(text, "Failed to read") {
		t.Fatalf("logs = %q, want the walk failure", text)
	}

	tail := c.Get("/api/tasks/"+id+"/logs?offset="+s(length)).Assert(t, 200).JSON()
	assertEq(t, s(tail["text"]), "")
	assertEq(t, tail["len"].(float64), float64(length))

	half := length / 2
	part := c.Get("/api/tasks/"+id+"/logs?offset="+s(half)).Assert(t, 200).JSON()
	assertEq(t, s(part["text"]), text[half:])

	c.Get("/api/tasks/"+id+"/logs?offset="+s(length+1)).Assert(t, 400)
	c.Get("/api/tasks/"+id+"/logs?offset=-1").Assert(t, 400)
	c.Get("/api/tasks/"+id+"/logs?offset=abc").Assert(t, 400)
	c.Get("/api/tasks/t_missing/logs").Assert(t, 404)

	release()
	awaitTask(t, c, id, "the scan to finish", func(snap map[string]any) bool {
		return snap["status"].(float64) >= 2
	})

	awaitRetired(t, c)

	stored := c.Get("/api/tasks/"+id+"/logs?offset=0").Assert(t, 200).JSON()
	if !strings.HasPrefix(s(stored["text"]), text) {
		t.Fatalf("stored logs = %q, want them to start with the live tail %q", stored["text"], text)
	}
	c.Get("/api/tasks/"+id+"/logs?offset="+s(int(stored["len"].(float64))+1)).Assert(t, 400)
}

func TestTaskRoutesRequireAdmin(t *testing.T) {
	c, plain, _ := adminAndMember(t, []string{})

	plain.Get("/api/tasks/snapshot").Assert(t, 403)
	plain.Get("/api/tasks/t_1/logs").Assert(t, 403)
	c.newSession(t).Get("/api/tasks/snapshot").Assert(t, 401)
}

func writeCBZ(t *testing.T, path, comicInfo string) {
	t.Helper()
	entries := map[string]string{"001.jpg": "not really a jpeg"}
	if comicInfo != "" {
		entries["ComicInfo.xml"] = comicInfo
	}
	writeZip(t, path, entries)
}

func taskLogs(t *testing.T, c *testClient, id string) string {
	t.Helper()
	return s(c.Get("/api/tasks/"+id+"/logs?offset=0").Assert(t, 200).JSON()["text"])
}

func libraryAt(t *testing.T, c *testClient, kind, dir string) string {
	t.Helper()
	lib := c.Post("/api/libraries/new", map[string]any{
		"name": kind, "type": kind,
		"sources": []map[string]any{{"path_uri": dir}},
	}).Assert(t, 200).JSON()
	return s(lib["id"])
}

func assertContentURIs(t *testing.T, pool *pgxpool.Pool, libraryID string, want []string) {
	t.Helper()
	got, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri FROM content WHERE library_id = $1 ORDER BY uri", libraryID)
	if err != nil {
		t.Fatalf("read uris: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
}

func comicLibrary(t *testing.T, c *testClient) string {
	t.Helper()

	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "Series")
	if err := os.Mkdir(seriesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, name := range []string{"ch1.cbz", "ch2.cbz"} {
		writeCBZ(t, filepath.Join(seriesDir, name), "")
	}
	return libraryAt(t, c, "comics", dir)
}

func messagesUntilTerminal(t *testing.T, f *fakeSocket, id string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for {
		msg := nextMessage(t, f)
		out = append(out, msg)
		task, _ := msg["task"].(map[string]any)
		if s(msg["type"]) == "task_update" && s(task["id"]) == id && task["status"].(float64) >= 2 {
			return out
		}
	}
}

func progressOf(msg map[string]any) map[string]any {
	task, _ := msg["task"].(map[string]any)
	progress, _ := task["progress"].(map[string]any)
	return progress
}

func TestScanProgressAdvancesAndCommitsNotify(t *testing.T) {
	noPings(t)
	publishEveryTick(t)
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	sock := newFakeSocket(false)
	serveFake(t, c.hub, sock, "u1", true)

	libID := comicLibrary(t, c)
	scan := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	id := s(scan["task_ids"].([]any)[0])

	var final map[string]any
	var seqs []float64
	phases := map[string]bool{}
	saved, seq, ticks := 0.0, 0.0, 0
	for _, msg := range messagesUntilTerminal(t, sock, id) {
		switch s(msg["type"]) {
		case "task_update":
			progress := progressOf(msg)
			if progress == nil {
				continue
			}
			phases[s(progress["phase"])] = true
			added := progress["saved"].(map[string]any)["added"].(float64)
			if added < saved || progress["commit_seq"].(float64) < seq {
				t.Fatalf("progress went backwards: saved %v after %v, commit_seq %v after %v",
					added, saved, progress["commit_seq"], seq)
			}
			saved, seq, ticks, final = added, progress["commit_seq"].(float64), ticks+1, progress
		case "catalog_changed":
			assertEq(t, s(msg["library_id"]), libID)
			assertEq(t, s(msg["task_id"]), id)
			seqs = append(seqs, msg["commit_seq"].(float64))
		}
	}

	if ticks == 0 {
		t.Fatal("the scan published no progress at all")
	}
	if !phases["parsing"] {
		t.Fatalf("no intermediate parsing progress was published, phases = %v", phases)
	}
	assertEq(t, s(final["phase"]), "done")
	assertEq(t, final["total"].(float64), float64(2))
	assertEq(t, final["processed"].(float64), float64(2))
	assertEq(t, final["saved"].(map[string]any)["added"].(float64), float64(2))

	if len(seqs) < 2 || final["commit_seq"].(float64) != float64(len(seqs)) {
		t.Fatalf("catalog_changed commit_seqs = %v, want one per commit up to the final %v", seqs, final["commit_seq"])
	}
	for i, got := range seqs {
		if got != float64(i+1) {
			t.Fatalf("catalog_changed commit_seqs = %v, want them contiguous from 1", seqs)
		}
	}

	done := snapshotOf(t, c, id)
	assertEq(t, done["status"].(float64), float64(2))
	assertEq(t, done["output"].(map[string]any)["added"].(float64), float64(2))
}

func TestScanCommitFailureSavesNothingAndNotifiesNobody(t *testing.T) {
	noPings(t)
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	sock := newFakeSocket(false)
	serveFake(t, c.hub, sock, "u1", true)

	libID := comicLibrary(t, c)
	if _, err := pool.Exec(context.Background(), `
		CREATE FUNCTION reject_content() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'content is read only'; END $$ LANGUAGE plpgsql;
		CREATE TRIGGER reject_content BEFORE INSERT ON content
		FOR EACH ROW EXECUTE FUNCTION reject_content();
	`); err != nil {
		t.Fatalf("install trigger: %v", err)
	}

	scan := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	id := s(scan["task_ids"].([]any)[0])

	ticks := 0
	for _, msg := range messagesUntilTerminal(t, sock, id) {
		if s(msg["type"]) == "catalog_changed" {
			t.Fatalf("a failed commit notified the catalog: %v", msg)
		}
		progress := progressOf(msg)
		if s(msg["type"]) != "task_update" || progress == nil {
			continue
		}
		ticks++
		counts := progress["saved"].(map[string]any)
		if counts["added"].(float64) != 0 || counts["updated"].(float64) != 0 ||
			counts["removed"].(float64) != 0 || progress["commit_seq"].(float64) != 0 {
			t.Fatalf("a failed commit published saved counts: %v", progress)
		}
	}
	if ticks == 0 {
		t.Fatal("the failed scan published no progress at all")
	}

	done := snapshotOf(t, c, id)
	assertEq(t, done["status"].(float64), float64(3))

	logs := taskLogs(t, c, id)
	if !strings.Contains(logs, "content is read only") {
		t.Fatalf("logs = %q, want the rejected commit", logs)
	}

	var rows int
	if err := pool.QueryRow(context.Background(), "SELECT COUNT(*) FROM content").Scan(&rows); err != nil {
		t.Fatalf("count content: %v", err)
	}
	assertEq(t, rows, 0)
}

func unparsableComicLibrary(t *testing.T, c *testClient) (string, string) {
	t.Helper()

	dir := t.TempDir()
	bad := filepath.Join(dir, "broken.cbz")
	if err := os.WriteFile(bad, []byte("not an archive"), 0o644); err != nil {
		t.Fatalf("write archive: %v", err)
	}

	return libraryAt(t, c, "comics", dir), bad
}

func TestScanRouteRunsTheWriterPipeline(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	libID, bad := unparsableComicLibrary(t, c)

	scan := c.Post("/api/libraries/scan", map[string]any{"ids": []string{libID}}).Assert(t, 200).JSON()
	id := s(scan["task_ids"].([]any)[0])
	awaitTask(t, c, id, "the scan to finish", func(snap map[string]any) bool {
		return snap["status"].(float64) >= 2
	})

	logs := taskLogs(t, c, id)
	if !strings.Contains(logs, "Failed to parse "+bad+"\n") {
		t.Fatalf("logs = %q, want the writer's parse failure for %s", logs, bad)
	}
}

func TestPendingScanLibraryIsRequeuedIntoTheWriterPipeline(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	libID, bad := unparsableComicLibrary(t, c)

	ctx := context.Background()
	id := models.MakeTaskID()
	input, err := json.Marshal(scanner.ScanInput{
		LibraryID:   libID,
		LibraryType: "comics",
		Sources:     []string{filepath.Dir(bad)},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	if _, err := pool.Exec(ctx,
		"INSERT INTO tasks (id, name, status, input) VALUES ($1, 'scan_library', $2, $3)",
		id, models.TaskStatusPending, input); err != nil {
		t.Fatalf("insert pending task: %v", err)
	}

	newClient(t, pool)

	var status int
	var logs string
	waitUntil(t, "the pending scan to be requeued and run at startup", func() bool {
		if err := pool.QueryRow(ctx,
			"SELECT status, COALESCE(logs, '') FROM tasks WHERE id = $1", id).Scan(&status, &logs); err != nil {
			t.Fatalf("read task: %v", err)
		}
		return status >= models.TaskStatusCompleted
	})

	assertEq(t, status, models.TaskStatusCompleted)
	if !strings.Contains(logs, "Failed to parse "+bad+"\n") {
		t.Fatalf("logs = %q, want the writer's parse failure for %s", logs, bad)
	}
}

func TestScanLogsTheLeafKeyItRejects(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dir := t.TempDir()
	seriesDir := filepath.Join(dir, "Series")
	if err := os.Mkdir(seriesDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeCBZ(t, filepath.Join(seriesDir, "ch1.cbz"), "")
	libID := libraryAt(t, c, "comics", dir)
	runScans(t, pool, c, map[string]any{"ids": []string{libID}})

	dupe := filepath.Join(seriesDir, "dupe.cbz")
	writeCBZ(t, dupe, `<?xml version="1.0"?><ComicInfo><Series>Series</Series><Number>1</Number></ComicInfo>`)
	ids := runScans(t, pool, c, map[string]any{"ids": []string{libID}})

	var seriesID string
	err := pool.QueryRow(context.Background(),
		"SELECT id FROM content WHERE library_id = $1 AND uri = 'comic/Series'", libID).Scan(&seriesID)
	if err != nil {
		t.Fatalf("read series: %v", err)
	}

	logs := taskLogs(t, c, ids[0])
	want := "URI conflict for file " + dupe + ", skipping (uri_part: ch1, parent_id: " + seriesID + ")\n"
	if strings.Count(logs, "URI conflict") != 1 || !strings.Contains(logs, want) {
		t.Fatalf("logs = %q, want one entry naming the rejected file and the key it wanted", logs)
	}

	status, out := scanOutcome(t, pool, ids[0])
	if status != models.TaskStatusCompleted || out.Failed != 1 || out.Added != 0 {
		t.Fatalf("scan = %d, %+v, want the rejected file counted as the only failure", status, out)
	}
	assertContentURIs(t, pool, libID, []string{"comic/Series", "comic/Series/ch1"})
}

func TestScanLogsTheSeriesKeyItRejects(t *testing.T) {
	fastFlushes(t)
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dir := t.TempDir()
	writeEPUB(t, filepath.Join(dir, "Foo_bar.epub"), "Foo_bar", "")
	libID := libraryAt(t, c, "books", dir)
	runScans(t, pool, c, map[string]any{"ids": []string{libID}})

	member := filepath.Join(dir, "x.epub")
	writeEPUB(t, member, "X", "Foo/bar")
	ids := runScans(t, pool, c, map[string]any{"ids": []string{libID}})

	logs := taskLogs(t, c, ids[0])
	want := "Series key conflict for file " + member + ", skipping (uri: book/Foo_bar)\n"
	if strings.Count(logs, "Series key conflict") != 1 || !strings.Contains(logs, want) {
		t.Fatalf("logs = %q, want one entry naming the rejected file and series key", logs)
	}

	status, out := scanOutcome(t, pool, ids[0])
	if status != models.TaskStatusCompleted || out.Failed != 1 || out.Added != 0 {
		t.Fatalf("scan = %d, %+v, want the rejected file counted as the only failure", status, out)
	}
	assertContentURIs(t, pool, libID, []string{"book/Foo_bar"})
}
