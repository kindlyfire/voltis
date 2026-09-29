package scanner

import (
	"context"
	"fmt"
	"testing"
	"time"

	"voltis/db"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

func seedSeriesScan(t *testing.T, pool *pgxpool.Pool, lib string) (*scanRun, string) {
	t.Helper()
	r := newScanRun(t, pool, lib, &ComicsScanner{})
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S", "/lib/S"))
	r.commit(false)
	return r, contentIDByURI(t, pool, lib, "comic/S/ch1")
}

func TestScanConcurrencyRenameSourceWins(t *testing.T) {
	pool := newTestPool(t)
	lib := newTestLibrary(t, pool, "comics")
	r, _ := seedSeriesScan(t, pool, lib)

	exec(t, pool, "INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u', 'x')")
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, starred) VALUES ('src', 'u1', $1, 'comic/S/ch1', true)", lib)
	exec(t, pool, "INSERT INTO user_to_content (id, user_id, library_id, uri, notes) VALUES ('dst', 'u1', $1, 'comic/S_2019/ch1', 'destination')", lib)
	seedMetadata(t, pool, lib, "comic/S_2019/ch1", metadata.Doc{Overrides: metadata.Fields{Title: metadata.Val("loser")}})
	seedMetadata(t, pool, lib, "comic/S/ch1", metadata.Doc{Overrides: metadata.Fields{Title: metadata.Val("winner")}})
	exec(t, pool, `INSERT INTO metadata_links (library_id, uri, provider, state) VALUES
		($1, 'comic/S', 'p', 'review'), ($1, 'comic/S_2019', 'p', 'ignored')`, lib)

	r.reload()
	r.place(comicResult("/lib/S/ch1.cbz", "ch1", "S_2019", "/lib/S"))
	r.commit(false)

	rows, err := db.Select[models.UserToContent](context.Background(), pool,
		"SELECT * FROM user_to_content WHERE library_id = $1 ORDER BY id", lib)
	must(t, err)
	if len(rows) != 1 || rows[0].ID != "src" || rows[0].URI != "comic/S_2019/ch1" {
		t.Fatalf("annotations = %+v, want the source moved over the orphaned destination", rows)
	}

	if got := readMeta(t, pool, lib, "comic/S_2019/ch1").Overrides; got.Title.V != "winner" {
		t.Fatalf("metadata = %+v, want the moving source to win", got)
	}

	links, err := db.SelectScalars[string](context.Background(), pool,
		"SELECT uri || ':' || state FROM metadata_links WHERE library_id = $1", lib)
	must(t, err)
	if len(links) != 1 || links[0] != "comic/S_2019:review" {
		t.Fatalf("links = %v, want the source moved over the orphaned destination", links)
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
	go commitLoop(context.Background(), pool, testStore, r.fs, r.lib, in, out)
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

func TestScanConcurrencyRetryPolicy(t *testing.T) {
	for _, c := range []struct {
		name      string
		sqlstate  string
		condition string
		leaves    int
		attempts  int64
		wantErr   bool
		counts    Counts
		uris      []string
	}{
		{"serialization retry", "40001", "nextval('attempt_counter') = 3", 2, 6, false,
			Counts{Added: 2}, []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}},
		{"deadlock code retry", "40P01", "nextval('attempt_counter') = 3", 2, 6, false,
			Counts{Added: 2}, []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}},
		{"uniqueness retry", "23505", "nextval('attempt_counter') = 3", 2, 6, false,
			Counts{Added: 2}, []string{"comic/S", "comic/S/ch1", "comic/S/ch2"}},
		{"exhausted retries", "40001", "nextval('attempt_counter') % 3 = 0", 2, 9, true, Counts{}, nil},
		{"non-retryable", "22000", "nextval('attempt_counter') > 0", 1, 1, true, Counts{}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newTestScan(t, "comics")
			failOnInsert(t, r.pool, c.condition, c.sqlstate)

			for i := range c.leaves {
				part := fmt.Sprintf("ch%d", i+1)
				r.place(comicResult("/lib/S/"+part+".cbz", part, "S", "/lib/S"))
			}

			got := runCommitLoop(t, r.pool, r, false)
			if (got.err != nil) != c.wantErr {
				t.Fatalf("commit err = %v, want an error: %v", got.err, c.wantErr)
			}
			if got.counts != c.counts {
				t.Fatalf("counts = %+v, want %+v", got.counts, c.counts)
			}
			assertCatalog(t, r.pool, r.lib, c.uris)
			attempts, err := db.SelectScalar[int64](context.Background(), r.pool, "SELECT last_value FROM attempt_counter")
			must(t, err)
			if attempts != c.attempts {
				t.Fatalf("trigger fired %d times, want %d", attempts, c.attempts)
			}
		})
	}
}
