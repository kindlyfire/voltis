package db_test

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

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
