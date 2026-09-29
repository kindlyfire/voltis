package settings

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Run inside the write transaction, so a CLI write applies them too.
var transitions = map[string]func(ctx context.Context, tx pgx.Tx, value any) error{
	AuthPasswordLoginEnabled: func(ctx context.Context, tx pgx.Tx, value any) error {
		if value.(bool) {
			return nil
		}
		_, err := tx.Exec(ctx, "DELETE FROM sessions WHERE method = 'password'")
		return err
	},
	OIDCEnabled: func(ctx context.Context, tx pgx.Tx, value any) error {
		if value.(bool) {
			return nil
		}
		return revokeOIDC(ctx, tx, value)
	},
	OIDCIssuer:   revokeOIDC,
	OIDCClientID: revokeOIDC,
}

// Signs out OIDC sessions and drops the flows and completions started under the
// old configuration.
func revokeOIDC(ctx context.Context, tx pgx.Tx, _ any) error {
	if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE method = 'oidc'"); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, "DELETE FROM auth_pending")
	return err
}
