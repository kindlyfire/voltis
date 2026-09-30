package db_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/db/dbtest"
	"voltis/scanner"
)

// An instance upgrading from 007 may already hold addresses that differ only in
// case, which the case-insensitive index would refuse.
func TestEmailIndexMigrationClearsAmbiguousAddresses(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	for _, stmt := range []string{
		"DROP INDEX users_email_lower_key",
		"DELETE FROM _migrations WHERE name = '008_email_ci'",
		"ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email)",
		`INSERT INTO users (id, username, email) VALUES
			('u_a', 'a', 'Person@Example.com'),
			('u_b', 'b', 'person@example.com'),
			('u_c', 'c', 'other@example.com')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare (%s): %v", stmt, err)
		}
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	emails, err := db.SelectScalars[*string](ctx, pool, "SELECT email FROM users ORDER BY id")
	if err != nil {
		t.Fatalf("read emails: %v", err)
	}
	if len(emails) != 3 || emails[0] != nil || emails[1] != nil || emails[2] == nil {
		t.Fatalf("got %v, want the ambiguous pair cleared and the unique one kept", emails)
	}

	if _, err := pool.Exec(ctx,
		"UPDATE users SET email = 'OTHER@example.com' WHERE id = 'u_a'"); err == nil {
		t.Fatal("the case-insensitive index was not created")
	}
}

// auto_match was a bool before it went per provider, and a pending scan stores the settings in
// its input.
func TestAutoMatchMigrationConvertsTheBool(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	for _, stmt := range []string{
		"DROP INDEX idx_content_file_uri, idx_content_roots, idx_metadata_links_retry",
		"DELETE FROM _migrations WHERE name = '010_auto_match_providers'",
		`INSERT INTO libraries (id, name, type, sources, settings) VALUES
			('l_on', 'on', 'comics', 'null', '{"auto_match": true, "book_series_inference": "off"}'),
			('l_off', 'off', 'comics', '[]', '{"auto_match": false}'),
			('l_none', 'none', 'comics', '[{"path_uri": "/a"}]', '{}')`,
		`INSERT INTO tasks (id, name, status, input) VALUES
			('t_pending', 'scan_library', 0, '{"library_id": "l_on", "settings": {"auto_match": true, "book_series_inference": "off"}}')`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare (%s): %v", stmt, err)
		}
	}

	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	got, err := db.SelectScalars[string](ctx, pool,
		"SELECT id || ' ' || sources::text || ' ' || settings::text FROM libraries ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`l_none [{"path_uri": "/a"}] {}`,
		`l_off [] {"auto_match": {}}`,
		`l_on [] {"auto_match": {"mangabaka": true}, "book_series_inference": "off"}`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("libraries = %q, want %q", got, want)
	}

	input, err := db.SelectScalar[string](ctx, pool, "SELECT input::text FROM tasks WHERE id = 't_pending'")
	if err != nil {
		t.Fatal(err)
	}
	var si scanner.ScanInput
	if err := json.Unmarshal([]byte(input), &si); err != nil || si.Settings.BookSeriesInference != "off" {
		t.Fatalf("task input %s = %+v (%v)", input, si, err)
	}
}

// 015 moves metadata onto content and links onto series content, dropping everything else.
func TestMergeContentMetadataMigration(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Unmigrated(t)
	if err := db.MigrateUntil(ctx, pool, "014_sort_title"); err != nil {
		t.Fatalf("migrate to 014: %v", err)
	}
	for _, stmt := range []string{
		"INSERT INTO libraries (id, name, type) VALUES ('l', 'lib', 'comics')",
		`INSERT INTO content (id, uri_part, uri, type, library_id, parent_id) VALUES
			('c_s', 'S', 'comic/S', 'comic_series', 'l', NULL), ('c_1', '1', 'comic/S/1', 'comic', 'l', 'c_s'),
			('c_e', 'E', 'comic/E', 'comic_series', 'l', NULL), ('c_bare', 'B', 'comic/B', 'comic_series', 'l', NULL)`,
		`INSERT INTO content_metadata (uri, library_id, data_raw, data, data_version, updated_at) VALUES
			('comic/S', 'l', '{"v": 2, "rev": 1, "overrides": {"title": "The Sun"}}',
				'{"title": "The Sun", "alt_titles": ["Sol"], "rating": 4.5, "publication_date": "2001-02-03"}', 2, '2020-01-02Z'),
			('comic/S/1', 'l', '{"v": 2, "file": {"volume": "1"}}', '{"volume": "1"}', 2, '2020-01-02Z'),
			('comic/E', 'l', '{"v": 2}', '{"title": "Empty"}', 2, '2020-01-02Z'),
			('comic/gone', 'l', '{"v": 2, "overrides": {"title": "Kept"}}', '{"title": "Kept"}', 2, '2020-01-02Z')`,
		`INSERT INTO provider_entries (provider, external_id, canonical_id, raw, fetched_at, refresh_at)
			VALUES ('a', '1', '1', '{}', now(), now())`,
		`INSERT INTO metadata_links (library_id, uri, provider, state, external_id, origin) VALUES
			('l', 'comic/S', 'a', 'linked', '1', 'manual'), ('l', 'comic/E', 'a', 'review', NULL, NULL),
			('l', 'comic/S/1', 'a', 'ignored', NULL, NULL), ('l', 'comic/gone', 'a', 'ignored', NULL, NULL)`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("prepare (%s): %v", stmt, err)
		}
	}
	constraints := func() []string {
		t.Helper()
		names, err := db.SelectScalars[string](ctx, pool,
			"SELECT conname FROM pg_constraint WHERE conrelid = 'content'::regclass ORDER BY conname")
		if err != nil {
			t.Fatal(err)
		}
		return names
	}
	before := constraints()

	if err := db.MigrateUntil(ctx, pool, "015_merge_content_metadata"); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	type row struct {
		ID      string     `db:"id"`
		Raw     string     `db:"data_raw"`
		Data    string     `db:"data"`
		Version int        `db:"data_version"`
		Updated *time.Time `db:"meta_updated_at"`
		Release *string    `db:"release_date"`
		Rating  *float64   `db:"rating"`
		Search  string     `db:"search_text"`
		SortKey bool       `db:"sort_key"`
	}
	rows, err := db.Select[row](ctx, pool, `
		SELECT id, data_raw::text, data::text, data_version, meta_updated_at, release_date, rating, search_text,
			sort_title IS NOT DISTINCT FROM public.sort_key(data->>'title') AS sort_key
		FROM content ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]row{}
	for _, r := range rows {
		if !r.SortKey {
			t.Errorf("%s: sort_title differs from its expression", r.ID)
		}
		byID[r.ID] = r
	}
	s, leaf, bare := byID["c_s"], byID["c_1"], byID["c_bare"]
	if s.Raw != `{"v": 2, "rev": 1, "overrides": {"title": "The Sun"}}` || s.Version != 2 ||
		s.Updated == nil || !s.Updated.Equal(time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC)) ||
		s.Release == nil || *s.Release != "2001-02-03" || s.Rating == nil || *s.Rating != 4.5 ||
		s.Search != "The Sun\n[\"Sol\"]" {
		t.Errorf("series = %+v", s)
	}
	if leaf.Raw != `{"v": 2, "file": {"volume": "1"}}` || leaf.Data != `{"volume": "1"}` {
		t.Errorf("leaf = %+v", leaf)
	}
	if bare.Raw != "{}" || bare.Data != "{}" || bare.Version != 0 || bare.Updated != nil || bare.Release != nil ||
		bare.Rating != nil {
		t.Errorf("row without metadata = %+v", bare)
	}

	if gone, err := db.SelectScalar[bool](ctx, pool, "SELECT to_regclass('content_metadata') IS NULL"); err != nil || !gone {
		t.Errorf("content_metadata dropped = %v (%v)", gone, err)
	}
	links := func() []string {
		t.Helper()
		got, err := db.SelectScalars[string](ctx, pool,
			"SELECT content_id || ':' || state FROM metadata_links ORDER BY content_id")
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := links(); !slices.Equal(got, []string{"c_e:review", "c_s:linked"}) {
		t.Errorf("links = %v", got)
	}
	if _, err := pool.Exec(ctx, "DELETE FROM content WHERE id = 'c_e'"); err != nil {
		t.Fatal(err)
	}
	if got := links(); !slices.Equal(got, []string{"c_s:linked"}) {
		t.Errorf("links after deleting the series = %v", got)
	}

	after := constraints()
	for _, name := range before {
		if !slices.Contains(after, name) {
			t.Errorf("constraint %s is gone: %v", name, after)
		}
	}
	if len(before) != 14 {
		t.Errorf("constraints before 015 = %v", before)
	}

	found, err := db.SelectScalars[string](ctx, pool,
		"SELECT id FROM content WHERE id @@@ paradedb.match('search_text', 'sun')")
	if err != nil || !slices.Equal(found, []string{"c_s"}) {
		t.Errorf("search = %v (%v)", found, err)
	}
}
