package cmd

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LinkIdentity links an external identity to an existing user. The issuer is empty for the
// proxy.
func LinkIdentity(ctx context.Context, pool *pgxpool.Pool, username, provider, issuer, subject string) error {
	switch {
	case provider != models.SessionOIDC && provider != models.SessionProxy:
		return fmt.Errorf("provider must be %s or %s", models.SessionOIDC, models.SessionProxy)
	case provider == models.SessionOIDC && issuer == "":
		return errors.New("--issuer is required for oidc")
	case provider == models.SessionProxy && issuer != "":
		return errors.New("--issuer only applies to oidc")
	case subject == "":
		return errors.New("--subject is required")
	}
	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		userID, err := db.SelectScalar[string](ctx, tx, "SELECT id FROM users WHERE username = $1", username)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("user '%s' not found", username)
		}
		if err != nil {
			return err
		}
		return db.AttachIdentity(ctx, tx, provider, issuer, subject, userID, "")
	})
	if err != nil {
		return err
	}
	fmt.Printf("Linked %s identity '%s' to '%s'\n", provider, subject, username)
	return nil
}

// SetIssuer moves every OIDC identity from one issuer to another. It changes nothing when a
// subject already exists under the new issuer.
func SetIssuer(ctx context.Context, pool *pgxpool.Pool, from, to string) error {
	var moved int64
	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		// Serializes with OIDC sign-ins and unlinks, which rely on identity keys not changing.
		if err := settings.LockVersionWrite(ctx, tx); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT u.username, o.subject, nu.username FROM user_identities o
			JOIN user_identities n ON n.provider = 'oidc' AND n.issuer = $2 AND n.subject = o.subject
			JOIN users u ON u.id = o.user_id
			JOIN users nu ON nu.id = n.user_id
			WHERE o.provider = 'oidc' AND o.issuer = $1
			ORDER BY u.username`, from, to)
		if err != nil {
			return err
		}
		conflicts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (string, error) {
			var name, subject, newOwner string
			err := row.Scan(&name, &subject, &newOwner)
			return fmt.Sprintf("%s (%s): new identity on %s", name, subject, newOwner), err
		})
		if err != nil {
			return err
		}
		if len(conflicts) > 0 {
			return fmt.Errorf("%d identities already exist under %s:\n  %s",
				len(conflicts), to, strings.Join(conflicts, "\n  "))
		}
		tag, err := tx.Exec(ctx,
			"UPDATE user_identities SET issuer = $2 WHERE provider = 'oidc' AND issuer = $1", from, to)
		moved = tag.RowsAffected()
		return err
	})
	if db.IsDuplicate(err) {
		return errors.New("a user signed in under the new issuer meanwhile, retry")
	}
	if err != nil {
		return err
	}
	fmt.Printf("Moved %d identities to %s\n", moved, to)
	return nil
}
