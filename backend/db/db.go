package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	// Postgres allows 100 connections by default. A `pool_max_conns` parameter in the database URL
	// overrides this; pgxpool consumes it, so it is looked for in a plain parse.
	plain, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if _, ok := plain.RuntimeParams["pool_max_conns"]; !ok {
		config.MaxConns = 50
	}
	// Compiling a plan costs more than the short queries here gain from it: hundreds of ms on
	// reading-list queries whose estimated cost crosses the JIT threshold. A `jit` parameter in
	// the database URL, as any unknown one a server setting, overrides this.
	if _, ok := config.ConnConfig.RuntimeParams["jit"]; !ok {
		config.ConnConfig.RuntimeParams["jit"] = "off"
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	return migrate(ctx, pool, "")
}

// MigrateUntil applies the pending migrations up to and including the named one, so a test can
// prepare data for the migration after it.
func MigrateUntil(ctx context.Context, pool *pgxpool.Pool, name string) error {
	return migrate(ctx, pool, name)
}

func migrate(ctx context.Context, pool *pgxpool.Pool, until string) error {
	_, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS _migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	applied := map[string]bool{}
	rows, err := pool.Query(ctx, "SELECT name FROM _migrations")
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		applied[name] = true
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	if until != "" {
		i := slices.IndexFunc(entries, func(e fs.DirEntry) bool { return e.Name() == until+".sql" })
		if i < 0 {
			return fmt.Errorf("unknown migration %s", until)
		}
		entries = entries[:i+1]
	}

	for _, entry := range entries {
		name := entry.Name()
		nameNoExt := name[:len(name)-4]
		if applied[nameNoExt] {
			continue
		}

		slog.Info("running migration", "name", nameNoExt)
		sql, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		tx, err := pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("exec migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO _migrations (name) VALUES ($1)", nameNoExt); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}

	return nil
}
