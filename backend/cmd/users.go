package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

func readPassword(password string) (string, error) {
	if password == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", fmt.Errorf("read stdin: %w", err)
		}
		password = strings.TrimSpace(string(data))
		if strings.ContainsAny(password, "\n\r") {
			return "", fmt.Errorf("password must not contain newlines")
		}
	}
	if len(password) < 8 {
		return "", fmt.Errorf("password must be at least 8 characters")
	}
	return password, nil
}

// CreateUser creates a user. A nil password creates a passwordless account, which the first
// external login that matches it claims.
func CreateUser(ctx context.Context, pool *pgxpool.Pool, username string, password *string, admin bool) error {
	var hash *string
	if password != nil {
		pw, err := readPassword(*password)
		if err != nil {
			return err
		}
		raw, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		hash = new(string(raw))
	}

	permissions := []string{}
	if admin {
		permissions = []string{"ADMIN"}
	}

	id := models.MakeUserID()
	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if admin {
			if err := lockForBootstrap(ctx, tx); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, username, password_hash, permissions) VALUES ($1, $2, $3, $4)`,
			id, username, hash, permissions); err != nil {
			return err
		}
		if !admin {
			return nil
		}
		return settings.WriteTx(ctx, tx, settings.BootstrapCompleted, true)
	})
	if err != nil {
		if db.IsDuplicate(err) {
			return fmt.Errorf("user '%s' already exists", username)
		}
		return err
	}

	fmt.Printf("Created user '%s' with id %s\n", username, id)
	return nil
}

func UpdateUser(ctx context.Context, pool *pgxpool.Pool, name string, username, password *string, admin *bool) error {
	// Read and hash before the transaction: stdin must not block the admin lock.
	var hash *string
	if password != nil {
		pw, err := readPassword(*password)
		if err != nil {
			return err
		}
		raw, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		h := string(raw)
		hash = &h
	}

	err := db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		return updateUserTx(ctx, tx, name, username, hash, admin)
	})
	if err != nil {
		if db.IsDuplicate(err) {
			return fmt.Errorf("username '%s' already exists", *username)
		}
		return err
	}

	fmt.Printf("Updated user '%s'\n", name)
	return nil
}

func updateUserTx(ctx context.Context, tx pgx.Tx, name string, username, hash *string, admin *bool) error {
	if admin != nil {
		var err error
		if *admin {
			err = lockForBootstrap(ctx, tx)
		} else {
			err = db.LockAdminMutation(ctx, tx)
		}
		if err != nil {
			return err
		}
	}

	user, err := db.SelectOne[models.User](ctx, tx, "SELECT * FROM users WHERE username = $1", name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("user '%s' not found", name)
	}
	if err != nil {
		return err
	}

	// Only the flags that were passed are written, so a rename cannot put back
	// permissions that changed since the row was read.
	var permissions []string
	if admin != nil {
		permissions = []string{}
		for _, p := range user.Permissions {
			if p != "ADMIN" {
				permissions = append(permissions, p)
			}
		}
		if *admin {
			permissions = append(permissions, "ADMIN")
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE users SET
			username = COALESCE($1::text, username),
			password_hash = COALESCE($2::text, password_hash),
			permissions = COALESCE($3::text[], permissions),
			updated_at = NOW()
		WHERE id = $4
	`, username, hash, permissions, user.ID); err != nil {
		return err
	}

	if admin != nil && *admin {
		return settings.WriteTx(ctx, tx, settings.BootstrapCompleted, true)
	}
	return nil
}

// lockForBootstrap takes the locks a bootstrap write needs, in the order every
// other writer takes them.
func lockForBootstrap(ctx context.Context, tx pgx.Tx) error {
	if err := db.LockAdminMutation(ctx, tx); err != nil {
		return err
	}
	return settings.LockVersionWrite(ctx, tx)
}
