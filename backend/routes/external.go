package routes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

type ExternalIdentity struct {
	Provider      string
	Issuer        string
	Subject       string
	Username      string
	Email         string
	EmailVerified bool
	Groups        []string
	HasGroups     bool
}

const (
	needsConfirm      = "confirm"
	needsPickUsername = "pick_username"
)

// externalLogin is a finished login, or the interaction OIDC needs to finish
// one. Match is the account a confirm or decline applies to.
type externalLogin struct {
	User  *models.User
	Needs string
	Match *models.User
}

func (r *resolver) resolveExternalLogin(ctx context.Context, id ExternalIdentity) (externalLogin, error) {
	user, err := identityUser(ctx, r.pool, id)
	if err != nil || user != nil {
		return externalLogin{User: user}, err
	}

	// The second pass sees the row whose unique violation lost the first.
	out, err := r.provisionTx(ctx, id)
	if isDuplicate(err) {
		if out, err = r.provisionTx(ctx, id); isDuplicate(err) {
			return pickUsername(id, "the username "+id.Username+" is already taken")
		}
	}
	return out, err
}

func (r *resolver) provisionTx(ctx context.Context, id ExternalIdentity) (externalLogin, error) {
	var out externalLogin
	err := db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := db.LockIdentity(ctx, tx, id.Provider, id.Issuer, id.Subject); err != nil {
			return err
		}
		var err error
		out, err = r.provision(ctx, tx, id)
		return err
	})
	if err != nil {
		return externalLogin{}, err
	}
	return out, nil
}

func isDuplicate(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && pgErr.Code == "23505"
}

func (r *resolver) provision(ctx context.Context, tx pgx.Tx, id ExternalIdentity) (externalLogin, error) {
	user, err := identityUser(ctx, tx, id)
	if err != nil || user != nil {
		return externalLogin{User: user}, err
	}

	if r.st.Bool(settings.AuthLinkMatchEmail) && id.EmailVerified && normalizeEmail(id.Email) != "" {
		match, err := userByEmail(ctx, tx, normalizeEmail(id.Email))
		if err != nil {
			return externalLogin{}, err
		}
		if match != nil {
			return link(ctx, tx, id, match)
		}
	}

	if r.st.Bool(settings.AuthLinkMatchUsername) && id.Username != "" {
		match, err := userByName(ctx, tx, id.Username)
		if err != nil {
			return externalLogin{}, err
		}
		if match != nil {
			return r.matchedUsername(ctx, tx, id, match)
		}
	}

	if !r.st.Bool(settings.AuthExternalAutoCreate) {
		return externalLogin{}, echo.NewHTTPError(http.StatusForbidden, "no account for this login; ask an administrator")
	}

	name := strings.TrimSpace(id.Username)
	if len(name) < 2 {
		return pickUsername(id, "the login provided no usable username")
	}
	taken, err := userByName(ctx, tx, name)
	if err != nil {
		return externalLogin{}, err
	}
	if taken != nil {
		return pickUsername(id, "the username "+name+" is already taken")
	}
	return create(ctx, tx, id, name)
}

// userByName locked match, so these checks cannot be invalidated mid-flight.
func (r *resolver) matchedUsername(ctx context.Context, tx pgx.Tx, id ExternalIdentity, match *models.User) (externalLogin, error) {
	if id.Provider == models.SessionProxy {
		return link(ctx, tx, id, match)
	}
	if match.PasswordHash != nil {
		return externalLogin{Needs: needsConfirm, Match: match}, nil
	}

	used, err := db.SelectScalar[bool](ctx, tx,
		"SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1)", match.ID)
	if err != nil {
		return externalLogin{}, err
	}
	if used {
		return externalLogin{Needs: needsPickUsername, Match: match}, nil
	}
	return link(ctx, tx, id, match)
}

func pickUsername(id ExternalIdentity, reason string) (externalLogin, error) {
	if id.Provider == models.SessionProxy {
		return externalLogin{}, echo.NewHTTPError(http.StatusForbidden, reason)
	}
	return externalLogin{Needs: needsPickUsername}, nil
}

func link(ctx context.Context, tx pgx.Tx, id ExternalIdentity, user *models.User) (externalLogin, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id, email)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, models.MakeIdentityID(), id.Provider, id.Issuer, id.Subject, user.ID, nullable(normalizeEmail(id.Email)))
	if err != nil {
		return externalLogin{}, err
	}
	return externalLogin{User: user}, nil
}

func create(ctx context.Context, tx pgx.Tx, id ExternalIdentity, name string) (externalLogin, error) {
	userID := models.MakeUserID()
	if _, err := tx.Exec(ctx, "INSERT INTO users (id, username) VALUES ($1, $2)", userID, name); err != nil {
		return externalLogin{}, err
	}

	user, err := getUser(ctx, tx, userID)
	if err != nil {
		return externalLogin{}, err
	}
	return link(ctx, tx, id, &user)
}

func identityUser(ctx context.Context, q db.Querier, id ExternalIdentity) (*models.User, error) {
	return selectUser(ctx, q, `
		SELECT u.* FROM users u
		JOIN user_identities i ON i.user_id = u.id
		WHERE i.provider = $1 AND i.issuer = $2 AND i.subject = $3
	`, id.Provider, id.Issuer, id.Subject)
}

// Locks the row: callers branch on fields a concurrent login could change.
func userByName(ctx context.Context, tx pgx.Tx, username string) (*models.User, error) {
	return selectUser(ctx, tx, "SELECT * FROM users WHERE username = $1 FOR UPDATE", username)
}

// An address held by two rows differing only in case matches nothing.
func userByEmail(ctx context.Context, tx pgx.Tx, email string) (*models.User, error) {
	users, err := db.Select[models.User](ctx, tx,
		"SELECT * FROM users WHERE lower(email) = $1 LIMIT 2 FOR UPDATE", email)
	if err != nil || len(users) != 1 {
		return nil, err
	}
	return &users[0], nil
}

func selectUser(ctx context.Context, q db.Querier, query string, args ...any) (*models.User, error) {
	user, err := db.SelectOne[models.User](ctx, q, query, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func syncEmail(ctx context.Context, pool *pgxpool.Pool, user *models.User, email string) {
	email = normalizeEmail(email)
	if email == "" || (user.Email != nil && *user.Email == email) {
		return
	}
	tag, err := pool.Exec(ctx, `
		UPDATE users SET email = $1, updated_at = NOW()
		WHERE id = $2 AND NOT EXISTS (SELECT 1 FROM users WHERE lower(email) = $1 AND id <> $2)
	`, email, user.ID)
	if err != nil {
		slog.Warn("[auth] failed to sync the email", "user", user.ID, "err", err)
		return
	}
	if tag.RowsAffected() == 0 {
		slog.Warn("[auth] skipping the email sync, another account already uses it",
			"user", user.ID, "email", email)
		return
	}
	user.Email = &email
}

// Keeps the identity's email current. An empty email keeps the last known one.
func syncIdentityEmail(ctx context.Context, pool *pgxpool.Pool, id ExternalIdentity) {
	email := normalizeEmail(id.Email)
	if email == "" {
		return
	}
	if _, err := pool.Exec(ctx, `
		UPDATE user_identities SET email = $4
		WHERE provider = $1 AND issuer = $2 AND subject = $3 AND email IS DISTINCT FROM $4
	`, id.Provider, id.Issuer, id.Subject, email); err != nil {
		slog.Warn("[auth] failed to refresh the identity email", "provider", id.Provider, "subject", id.Subject, "err", err)
	}
}

func (r *resolver) syncAdmin(ctx context.Context, user *models.User, id ExternalIdentity) error {
	group := r.st.String(settings.AuthAdminGroup)
	if group == "" || !id.HasGroups {
		return nil
	}
	want := slices.Contains(id.Groups, group)
	if want == slices.Contains(user.Permissions, "ADMIN") {
		return nil
	}

	changed, err := r.setAdmin(ctx, user.ID, want)
	if err != nil {
		return err
	}

	// Reconcile even when the row already matched: a request that lost a
	// concurrent demotion must not stay authorized.
	user.Permissions = slices.DeleteFunc(slices.Clone(user.Permissions),
		func(p string) bool { return p == "ADMIN" })
	if want {
		user.Permissions = append(user.Permissions, "ADMIN")
	} else if changed {
		r.hub.Drop(user.ID)
	}
	return nil
}

func (r *resolver) setAdmin(ctx context.Context, userID string, admin bool) (bool, error) {
	query := "UPDATE users SET permissions = array_remove(permissions, 'ADMIN'), updated_at = NOW() WHERE id = $1 AND permissions @> ARRAY['ADMIN']"
	if admin {
		query = "UPDATE users SET permissions = array_append(permissions, 'ADMIN'), updated_at = NOW() WHERE id = $1 AND NOT permissions @> ARRAY['ADMIN']"
	}

	var changed bool
	err := db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}
		if admin {
			if err := settings.LockVersionWrite(ctx, tx); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, query, userID)
		if err != nil {
			return err
		}
		changed = tag.RowsAffected() > 0
		if !admin {
			return nil
		}
		// An admin now exists, so public bootstrap registration must close.
		return settings.WriteTx(ctx, tx, settings.BootstrapCompleted, true)
	})
	return changed, err
}

func parseGroups(v any) ([]string, bool) {
	switch groups := v.(type) {
	case string:
		out := []string{}
		for _, g := range strings.Split(groups, ",") {
			if g = strings.TrimSpace(g); g != "" {
				out = append(out, g)
			}
		}
		return out, true
	case []string:
		return groups, true
	case []any:
		out := []string{}
		for _, g := range groups {
			if s, ok := g.(string); ok {
				out = append(out, s)
			}
		}
		return out, true
	}
	return nil, false
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
