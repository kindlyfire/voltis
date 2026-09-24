package db_test

import (
	"context"
	"testing"

	"voltis/db"
	"voltis/db/dbtest"
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
