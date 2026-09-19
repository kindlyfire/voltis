package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSeriesScan(t *testing.T, pool *pgxpool.Pool, lib string) (*scanRun, string) {
	t.Helper()
	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)
	id, err := db.SelectScalar[string](context.Background(), pool,
		"SELECT id FROM content WHERE library_id = $1 AND uri = 'comic/S/ch1'", lib)
	if err != nil {
		t.Fatal(err)
	}
	return r, id
}

func TestMetadataScanRaceEditBeforeRename(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r, leafID := seedSeriesScan(t, pool, lib)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err := db.LockMetadata(ctx, tx, lib); err != nil {
		t.Fatal(err)
	}
	var uri string
	if err := tx.QueryRow(ctx, "SELECT uri FROM content WHERE id = $1", leafID).Scan(&uri); err != nil {
		t.Fatal(err)
	}
	if uri != "comic/S/ch1" {
		t.Fatalf("uri = %q", uri)
	}
	raw := metaraw.MetadataRaw{Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "kept"}}}
	merged, _ := json.Marshal(raw.Merge())
	if _, err := tx.Exec(ctx, `
		INSERT INTO content_metadata (uri, library_id, data, data_raw, updated_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (uri, library_id) DO UPDATE SET data = EXCLUDED.data, data_raw = EXCLUDED.data_raw
	`, uri, lib, merged, raw.Dump()); err != nil {
		t.Fatal(err)
	}

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	flushed := make(chan error, 1)
	go func() {
		_, err := r.tryCommit(false)
		flushed <- err
	}()

	dbtest.WaitForBlockedLock(t, pool)
	select {
	case err := <-flushed:
		t.Fatalf("flush committed while the editor held the lock: %v", err)
	default:
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-flushed; err != nil {
		t.Fatalf("flush: %v", err)
	}

	if got := contentURIs(t, pool, lib); !slices.Equal(got, []string{"comic/S_2019", "comic/S_2019/ch1"}) {
		t.Fatalf("uris = %v", got)
	}
	moved := readMeta(t, pool, lib, "comic/S_2019/ch1")
	if moved.Overrides == nil || moved.Overrides.Raw.Title != "kept" {
		t.Fatalf("override = %+v, want it carried through the rename", moved.Overrides)
	}
	if moved.File == nil {
		t.Fatal("file layer was not written after the rename")
	}
}

func TestScanConcurrencyAnnotationDestinationKept(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r, _ := seedSeriesScan(t, pool, lib)

	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('src', 'u1', $1, 'comic/S/ch1', true)", lib)
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, notes) VALUES ('dst', 'u1', $1, 'comic/S_2019/ch1', 'destination')", lib)
	seedMetadata(t, pool, lib, "comic/S_2019/ch1", metaraw.MetadataRaw{
		Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "loser"}},
	})
	seedMetadata(t, pool, lib, "comic/S/ch1", metaraw.MetadataRaw{
		Overrides: &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: "winner"}},
	})

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	r.commit(false)

	rows, err := db.Select[models.UserToContent](context.Background(), pool,
		"SELECT * FROM user_to_content WHERE library_id = $1 ORDER BY id", lib)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("annotations = %+v", rows)
	}
	if rows[0].ID != "dst" || rows[0].URI != "comic/S_2019/ch1" || deref(rows[0].Notes) != "destination" {
		t.Fatalf("destination = %+v, want it kept", rows[0])
	}
	if rows[1].ID != "src" || rows[1].URI != "comic/S/ch1" {
		t.Fatalf("source = %+v, want it left behind", rows[1])
	}

	if got := readMeta(t, pool, lib, "comic/S_2019/ch1").Overrides; got == nil || got.Raw.Title != "winner" {
		t.Fatalf("metadata = %+v, want the moving source to win", got)
	}
}

func failOnInsert(t *testing.T, pool *pgxpool.Pool, condition, sqlstate string) {
	t.Helper()
	exec(t, pool, "CREATE SEQUENCE attempt_counter")
	exec(t, pool, `
		CREATE FUNCTION forced_failure() RETURNS trigger AS $fn$
		BEGIN
			IF `+condition+` THEN
				RAISE EXCEPTION 'forced failure' USING ERRCODE = '`+sqlstate+`';
			END IF;
			RETURN NEW;
		END $fn$ LANGUAGE plpgsql`)
	exec(t, pool, "CREATE TRIGGER forced_failure BEFORE INSERT ON content FOR EACH ROW EXECUTE FUNCTION forced_failure()")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS forced_failure ON content")
	})
}

func runCommitLoop(t *testing.T, pool *pgxpool.Pool, r *scanRun, final bool) committed {
	t.Helper()
	in := make(chan flush, 1)
	out := make(chan committed, 1)
	go commitLoop(context.Background(), pool, r.fs, r.lib, in, out)
	in <- r.w.take(final)
	close(in)
	select {
	case c := <-out:
		return c
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the committer")
		return committed{}
	}
}

func TestScanConcurrencyRetriesSerializationFailure(t *testing.T) {
	for _, code := range []string{"40001", "40P01", "23505"} {
		t.Run(code, func(t *testing.T) {
			pool := newTestPool(t)
			lib := newTestLibrary(t, pool, "comics")
			failOnInsert(t, pool, "nextval('attempt_counter') = 3", code)

			r := newScanRun(t, pool, lib, &ComicsScanner{})
			r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
			r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))

			c := runCommitLoop(t, pool, r, false)
			if c.err != nil {
				t.Fatalf("commit: %v", c.err)
			}
			if c.counts != (Counts{Added: 2}) {
				t.Fatalf("counts = %+v, want only the successful attempt", c.counts)
			}
			want := []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}
			if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
				t.Fatalf("uris = %v, want %v", got, want)
			}
			attempts, err := db.SelectScalar[int64](context.Background(), pool, "SELECT last_value FROM attempt_counter")
			if err != nil {
				t.Fatal(err)
			}
			if attempts != 6 {
				t.Fatalf("trigger fired %d times, want a failure after a counted write then a clean retry", attempts)
			}
		})
	}
}

func TestScanConcurrencyCountersUnchangedAfterFailedAttempts(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	failOnInsert(t, pool, "nextval('attempt_counter') % 3 = 0", "40001")

	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.place(comicResult("/lib/S/ch2.cbz", "ch2", "S", "/lib/S"))

	c := runCommitLoop(t, pool, r, false)
	if c.err == nil {
		t.Fatal("expected the commit to fail")
	}
	if c.counts != (Counts{}) {
		t.Fatalf("counts = %+v, want zero after a failed flush that had already counted a write", c.counts)
	}
	if got := contentURIs(t, pool, lib); len(got) != 0 {
		t.Fatalf("uris = %v, want none", got)
	}
	attempts, err := db.SelectScalar[int64](context.Background(), pool, "SELECT last_value FROM attempt_counter")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 9 {
		t.Fatalf("attempts = %d, want three attempts of three inserts", attempts)
	}
}

func TestScanConcurrencyDoesNotRetryOtherErrors(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	failOnInsert(t, pool, "nextval('attempt_counter') > 0", "22000")

	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))

	c := runCommitLoop(t, pool, r, false)
	if c.err == nil {
		t.Fatal("expected the commit to fail")
	}
	attempts, err := db.SelectScalar[int64](context.Background(), pool, "SELECT last_value FROM attempt_counter")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want a single attempt", attempts)
	}
}

func advisoryHolder(t *testing.T, pool *pgxpool.Pool, key int, exclude ...int) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		pids, err := db.SelectScalars[int](context.Background(), pool, `
			SELECT pid FROM pg_locks
			WHERE locktype = 'advisory' AND objid = $1 AND granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND NOT pid = ANY($2::int[])`, key, exclude)
		if err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if len(pids) > 1 {
			t.Fatalf("advisory key %d held by %v, want a single participant", key, pids)
		}
		if len(pids) == 1 {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for a backend to hold advisory key %d", key)
}

func TestScanConcurrencyRetriesForcedDeadlock(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	ctx, cancel := context.WithCancel(context.Background())

	other, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	crossed := make(chan error, 1)
	crossing := false
	defer func() {
		cancel()
		if crossing {
			<-crossed
		}
		_ = other.Rollback(context.Background())
	}()

	if _, err := other.Exec(ctx, "SET LOCAL deadlock_timeout = '20s'"); err != nil {
		t.Skipf("deadlock_timeout is not settable by this role: %v", err)
	}
	var otherPID int
	if err := other.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&otherPID); err != nil {
		t.Fatal(err)
	}

	exec(t, pool, "CREATE TABLE lockrows (id int PRIMARY KEY)")
	exec(t, pool, "INSERT INTO lockrows VALUES (1), (2)")
	exec(t, pool, "CREATE SEQUENCE attempt_counter")
	exec(t, pool, fmt.Sprintf(`
		CREATE FUNCTION forced_deadlock() RETURNS trigger AS $fn$
		BEGIN
			IF nextval('attempt_counter') = 1 THEN
				PERFORM set_config('deadlock_timeout', '50ms', true);
				PERFORM 1 FROM lockrows WHERE id = 2 FOR UPDATE;
				PERFORM pg_advisory_xact_lock(777);
				FOR i IN 1..2000 LOOP
					EXIT WHEN EXISTS (SELECT 1 FROM pg_locks
						WHERE locktype = 'transactionid' AND NOT granted AND pid = %[1]d);
					PERFORM pg_sleep(0.005);
				END LOOP;
				IF NOT EXISTS (SELECT 1 FROM pg_locks
					WHERE locktype = 'transactionid' AND NOT granted AND pid = %[1]d) THEN
					RAISE EXCEPTION 'forced deadlock barrier expired: pid %[1]d never waited on a transactionid';
				END IF;
				PERFORM 1 FROM lockrows WHERE id = 1 FOR UPDATE;
			END IF;
			RETURN NEW;
		END $fn$ LANGUAGE plpgsql`, otherPID))
	exec(t, pool, "CREATE TRIGGER forced_deadlock BEFORE INSERT ON content FOR EACH ROW EXECUTE FUNCTION forced_deadlock()")
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DROP TRIGGER IF EXISTS forced_deadlock ON content")
	})

	if _, err := other.Exec(ctx, "SELECT 1 FROM lockrows WHERE id = 1 FOR UPDATE"); err != nil {
		t.Fatal(err)
	}

	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	in, out := make(chan flush, 1), make(chan committed, 1)
	go commitLoop(ctx, pool, r.fs, lib, in, out)
	in <- r.w.take(false)
	close(in)

	advisoryHolder(t, pool, 777, otherPID)

	crossing = true
	go func() {
		_, err := other.Exec(ctx, "SELECT 1 FROM lockrows WHERE id = 2 FOR UPDATE")
		crossed <- err
	}()

	var c committed
	select {
	case c = <-out:
	case <-time.After(60 * time.Second):
		t.Fatal("timed out waiting for the committer")
	}
	crossing = false
	if err := <-crossed; err != nil {
		t.Fatalf("other connection: %v", err)
	}
	if err := other.Rollback(ctx); err != nil {
		t.Fatal(err)
	}

	if c.err != nil {
		t.Fatalf("commit: %v, want the deadlock victim to retry", c.err)
	}
	if c.counts != (Counts{Added: 1}) {
		t.Fatalf("counts = %+v", c.counts)
	}
	want := []string{"comic/S", "comic/S/ch1"}
	if got := contentURIs(t, pool, lib); !slices.Equal(got, want) {
		t.Fatalf("uris = %v, want %v", got, want)
	}
	attempts, err := db.SelectScalar[int64](context.Background(), pool, "SELECT last_value FROM attempt_counter")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want a deadlocked attempt then a clean retry", attempts)
	}
}
