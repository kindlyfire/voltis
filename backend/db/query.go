package db

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"

	"voltis/models"

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

// LockMetadata takes a library's metadata lock exclusively, for writes to its catalog: scans move
// refs under it, and it excludes every user's LockUserData.
func LockMetadata(ctx context.Context, tx pgx.Tx, libraryID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('metadata:' || $1))", libraryID)
	return err
}

// LockUserData takes a library's metadata lock shared, so its refs stay put, then the user's lock
// for the library, which serialises the user's writes there. Callers locking several libraries
// take both for each, in library order. The user lock's two-key form never collides with a
// metadata lock.
func LockUserData(ctx context.Context, tx pgx.Tx, userID, libraryID string) error {
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock_shared(hashtext('metadata:' || $1))", libraryID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('user-data:' || $1), hashtext($2))", userID, libraryID)
	return err
}

// IsDuplicate reports a unique violation.
func IsDuplicate(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == "23505"
}

var ErrIdentityTaken = errors.New("that identity is already linked to another account")

// AttachIdentity links an identity to userID.
func AttachIdentity(ctx context.Context, tx pgx.Tx, provider, issuer, subject, userID, email string) error {
	// Reentrant: callers that already hold it lose nothing.
	if err := LockIdentity(ctx, tx, provider, issuer, subject); err != nil {
		return err
	}
	owner, err := SelectScalar[string](ctx, tx,
		"SELECT user_id FROM user_identities WHERE provider = $1 AND issuer = $2 AND subject = $3",
		provider, issuer, subject)
	switch {
	case err == nil && owner == userID:
		return nil
	case err == nil:
		return ErrIdentityTaken
	case !errors.Is(err, pgx.ErrNoRows):
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id, email)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''))
	`, models.MakeIdentityID(), provider, issuer, subject, userID, email)
	return err
}

// ServerRevision is a fresh user_to_content revision token for a write outside the readers.
func ServerRevision() string { return "srv:" + strings.ToLower(rand.Text()) }

// MergeUserToContentSQL merges the user rows at source URIs into the same users' rows at their
// destinations in library $1, then deletes those sources; sources without a destination row stay.
// $2/$3/$4 pair sources, destinations and which side's reading state to keep ('source', 'target',
// or ” for the more recently touched, the source on a tie). Stars combine, notes and rating fall
// back to the other side. $5 is the merged rows' revision; $6, when not NULL, limits to one user.
const MergeUserToContentSQL = `
	WITH m AS (
		SELECT s.id AS src_id, d.id AS dst_id,
			CASE WHEN r.keep <> '' THEN r.keep
				WHEN COALESCE(GREATEST(d.last_read_at, d.status_updated_at, d.progress_updated_at), '-infinity')
					> COALESCE(GREATEST(s.last_read_at, s.status_updated_at, s.progress_updated_at), '-infinity')
				THEN 'target' ELSE 'source' END AS keep
		FROM unnest($2::text[], $3::text[], $4::text[]) AS r(src, dst, keep)
		JOIN user_to_content s ON s.library_id = $1 AND s.uri = r.src
		JOIN user_to_content d ON d.library_id = $1 AND d.uri = r.dst AND d.user_id = s.user_id
		WHERE s.id <> d.id AND ($6::text IS NULL OR s.user_id = $6)
	), merged AS (
		UPDATE user_to_content d SET status = k.status, status_updated_at = k.status_updated_at,
			progress = k.progress, progress_updated_at = k.progress_updated_at, last_read_at = k.last_read_at,
			starred = k.starred OR o.starred, notes = COALESCE(k.notes, o.notes),
			rating = COALESCE(k.rating, o.rating), revision = $5
		FROM m
		JOIN user_to_content k ON k.id = CASE WHEN m.keep = 'source' THEN m.src_id ELSE m.dst_id END
		JOIN user_to_content o ON o.id = CASE WHEN m.keep = 'source' THEN m.dst_id ELSE m.src_id END
		WHERE d.id = m.dst_id
	)
	DELETE FROM user_to_content WHERE id IN (SELECT src_id FROM m)`
