package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func CreateUser(ctx context.Context, pool *pgxpool.Pool, username, password string, admin bool) error {
	password, err := readPassword(password)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	permissions := []string{}
	if admin {
		permissions = []string{"ADMIN"}
	}

	id := models.MakeUserID()
	_, err = pool.Exec(ctx,
		`INSERT INTO users (id, username, password_hash, permissions) VALUES ($1, $2, $3, $4)`,
		id, username, string(hash), permissions,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("user '%s' already exists", username)
		}
		return err
	}

	fmt.Printf("Created user '%s' with id %s\n", username, id)
	return nil
}

func UpdateUser(ctx context.Context, pool *pgxpool.Pool, name string, username, password *string, admin *bool) error {
	user, err := db.SelectOne[models.User](ctx, pool, "SELECT * FROM users WHERE username = $1", name)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("user '%s' not found", name)
	}
	if err != nil {
		return err
	}

	if username != nil {
		user.Username = *username
	}
	if password != nil {
		pw, err := readPassword(*password)
		if err != nil {
			return err
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		user.PasswordHash = string(hash)
	}
	if admin != nil {
		if *admin {
			hasAdmin := slices.Contains(user.Permissions, "ADMIN")
			if !hasAdmin {
				user.Permissions = append(user.Permissions, "ADMIN")
			}
		} else {
			filtered := user.Permissions[:0]
			for _, p := range user.Permissions {
				if p != "ADMIN" {
					filtered = append(filtered, p)
				}
			}
			user.Permissions = filtered
		}
	}

	_, err = pool.Exec(ctx,
		`UPDATE users SET username = $1, password_hash = $2, permissions = $3, updated_at = NOW() WHERE id = $4`,
		user.Username, user.PasswordHash, user.Permissions, user.ID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return fmt.Errorf("username '%s' already exists", *username)
		}
		return err
	}

	fmt.Printf("Updated user '%s'\n", name)
	return nil
}
