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
}
