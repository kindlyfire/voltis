package routes

import (
	"context"
	"errors"
	"net/http"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type AuthRoutes struct {
	pool *pgxpool.Pool
	hub  *WebSocketHub
	st   *settings.Store
}

func (a *AuthRoutes) Register(g *echo.Group) {
	g.POST("/login", a.login)
	g.POST("/register", a.register)
	g.POST("/logout", a.logout)
}

func (a *AuthRoutes) login(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !a.st.Bool(settings.AuthPasswordLoginEnabled) {
		return errPasswordLoginDisabled()
	}

	ctx := reqCtx(c)
	user, err := db.SelectOne[models.User](ctx, a.pool, "SELECT * FROM users WHERE username = $1", req.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}
	if err != nil {
		return err
	}

	if user.PasswordHash == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(req.Password)); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}

	return a.startPasswordSession(c, user.ID)
}

func (a *AuthRoutes) register(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.Username) < 2 {
		return echo.NewHTTPError(http.StatusBadRequest, "username must be at least 2 characters")
	}
	if len(req.Password) < 8 {
		return echo.NewHTTPError(http.StatusBadRequest, "password must be at least 8 characters")
	}

	ctx := reqCtx(c)
	firstUser, err := isFirstUserFlow(ctx, a.pool, a.st)
	if err != nil {
		return err
	}
	if err := registerAllowed(ctx, a.pool, firstUser); err != nil {
		return err
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		return err
	}

	userID := models.MakeUserID()
	var token string
	err = db.WithTx(ctx, a.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}
		if err := settings.LockVersionWrite(ctx, tx); err != nil {
			return err
		}

		first, err := isFirstUserFlow(ctx, tx, a.st)
		if err != nil {
			return err
		}
		if err := registerAllowed(ctx, tx, first); err != nil {
			return err
		}

		permissions := []string{}
		if first {
			permissions = []string{"ADMIN"}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, username, password_hash, permissions) VALUES ($1, $2, $3, $4)`,
			userID, req.Username, hash, permissions,
		); err != nil {
			return err
		}
		if first {
			if err := settings.WriteTx(ctx, tx, settings.BootstrapCompleted, true); err != nil {
				return err
			}
		}
		token, err = createSession(ctx, tx, a.st, userID, models.SessionPassword)
		return err
	})
	if err != nil {
		if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok && pgErr.Code == "23505" {
			return echo.NewHTTPError(http.StatusBadRequest, "username already exists")
		}
		return err
	}

	return a.finishSession(c, token)
}

// The bootstrap admin may register and get a session even with password login
// off: it is the documented recovery path.
func registerAllowed(ctx context.Context, q db.Querier, firstUser bool) error {
	if firstUser {
		return nil
	}
	if err := requirePasswordLoginIn(ctx, q); err != nil {
		return err
	}
	enabled, err := settings.Read(ctx, q, settings.AuthRegistrationEnabled)
	if err != nil {
		return err
	}
	if enabled != true {
		return echo.NewHTTPError(http.StatusForbidden, "registration is disabled")
	}
	return nil
}

func errPasswordLoginDisabled() error {
	return echo.NewHTTPError(http.StatusForbidden, "password login is disabled")
}

// requirePasswordLogin holds off any concurrent disable until the caller's
// transaction, which must not write settings itself, has committed.
func requirePasswordLogin(ctx context.Context, tx pgx.Tx) error {
	if err := settings.LockVersion(ctx, tx); err != nil {
		return err
	}
	return requirePasswordLoginIn(ctx, tx)
}

func requirePasswordLoginIn(ctx context.Context, q db.Querier) error {
	enabled, err := settings.Read(ctx, q, settings.AuthPasswordLoginEnabled)
	if err != nil {
		return err
	}
	if enabled != true {
		return errPasswordLoginDisabled()
	}
	return nil
}

func (a *AuthRoutes) startPasswordSession(c echo.Context, userID string) error {
	ctx := reqCtx(c)
	var token string
	err := db.WithTx(ctx, a.pool, func(tx pgx.Tx) error {
		if err := requirePasswordLogin(ctx, tx); err != nil {
			return err
		}
		var err error
		token, err = createSession(ctx, tx, a.st, userID, models.SessionPassword)
		return err
	})
	if err != nil {
		return err
	}
	return a.finishSession(c, token)
}

func (a *AuthRoutes) finishSession(c echo.Context, token string) error {
	setSessionCookie(c, a.st, token)
	return okResponse(c)
}

func createSession(ctx context.Context, q db.Querier, st *settings.Store, userID, method string) (string, error) {
	token := randomToken()
	var absolute *time.Time
	if method != models.SessionPassword {
		t := time.Now().Add(time.Duration(st.Int(settings.AuthExternalSessionMaxDays)) * 24 * time.Hour)
		absolute = &t
	}
	_, err := q.Exec(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, method, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, token, userID, time.Now().Add(sessionDurationDays*24*time.Hour), method, absolute)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *AuthRoutes) logout(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	// The resolver may have replaced the incoming cookie on this very request.
	session := requestSession(c)
	if session == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	userID, err := db.SelectScalar[string](reqCtx(c), a.pool,
		"DELETE FROM sessions WHERE token = $1 RETURNING user_id", session.Token)
	redirect := ""
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	default:
		a.hub.Drop(userID)
		if session.Method == models.SessionProxy {
			redirect = a.st.String(settings.AuthProxyLogoutURL)
		}
	}

	clearSessionCookie(c, a.st)
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "redirect_url": redirect})
}

func isFirstUserFlow(ctx context.Context, q db.Querier, st *settings.Store) (bool, error) {
	if st.Bool(settings.BootstrapCompleted) {
		return false, nil
	}
	v, err := settings.Read(ctx, q, settings.BootstrapCompleted)
	if err != nil {
		return false, err
	}
	done, _ := v.(bool)
	return !done, nil
}

// Call after the change, with settings.LockVersion held. Lock and check are
// separate statements so the check sees what a racing unlink committed.
func requireLoginMethod(ctx context.Context, tx pgx.Tx, userID string) error {
	passwords, err := settings.Read(ctx, tx, settings.AuthPasswordLoginEnabled)
	if err != nil {
		return err
	}
	_, err = db.SelectScalar[string](ctx, tx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	if err != nil {
		return err
	}

	ok, err := db.SelectScalar[bool](ctx, tx, `
		SELECT ($2 AND password_hash IS NOT NULL)
		    OR EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1)
		FROM users WHERE id = $1
	`, userID, passwords == true)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "this would leave the account with no way to log in")
	}
	return nil
}
