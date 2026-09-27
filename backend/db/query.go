package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

func Select[T any](ctx context.Context, q Querier, query string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[T])
}

func SelectOne[T any](ctx context.Context, q Querier, query string, args ...any) (T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[T])
}

func SelectScalars[T any](ctx context.Context, q Querier, query string, args ...any) ([]T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[T])
}

func SelectScalar[T any](ctx context.Context, q Querier, query string, args ...any) (T, error) {
	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		var zero T
		return zero, err
	}
	return pgx.CollectExactlyOneRow(rows, pgx.RowTo[T])
}

func WithTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

const adminMutationLockKey int64 = 7263845190

func LockAdminMutation(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminMutationLockKey)
	return err
}

func LockUserSessions(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('sessions:' || $1))", userID)
	return err
}

func LockIdentity(ctx context.Context, tx pgx.Tx, provider, issuer, subject string) error {
	_, err := tx.Exec(ctx,
		"SELECT pg_advisory_xact_lock(hashtext('identity:' || $1 || ':' || $2 || ':' || $3))",
		provider, issuer, subject)
	return err
}

// LockProvider serialises the writes of a provider's entries. It comes before any LockMetadata.
func LockProvider(ctx context.Context, tx pgx.Tx, provider string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('provider:' || $1))", provider)
	return err
}

func LockMetadata(ctx context.Context, tx pgx.Tx, libraryID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('metadata:' || $1))", libraryID)
	return err
}
