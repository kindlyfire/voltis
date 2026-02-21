package routes

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"time"

	"voltis/config"
	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type AuthRoutes struct {
	pool *pgxpool.Pool
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

	ctx := reqCtx(c)
	user, err := db.SelectOne[models.User](ctx, a.pool, "SELECT * FROM users WHERE username = $1", req.Username)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}
	if err != nil {
		return err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}

	token, err := generateToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(sessionDurationDays * 24 * time.Hour)
	_, err = a.pool.Exec(ctx,
		"INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, user.ID, expiresAt,
	)
	if err != nil {
		return err
	}

	setSessionCookie(c, token)
	return okResponse(c)
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
	if len(req.Password) > 72 {
		return echo.NewHTTPError(http.StatusBadRequest, "password must be at most 72 characters")
	}

	ctx := reqCtx(c)
	cfg := config.Get()
	firstUser, err := isFirstUserFlow(ctx, a.pool)
	if err != nil {
		return err
	}
	if !cfg.RegistrationEnabled && !firstUser {
		return echo.NewHTTPError(http.StatusForbidden, "registration is disabled")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	permissions := []string{}
	if firstUser {
		permissions = []string{"ADMIN"}
	}

	userID := models.MakeUserID()
	_, err = a.pool.Exec(ctx,
		`INSERT INTO users (id, username, password_hash, permissions) VALUES ($1, $2, $3, $4)`,
		userID, req.Username, string(hash), permissions,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return echo.NewHTTPError(http.StatusBadRequest, "username already exists")
		}
		return err
	}

	token, err := generateToken()
	if err != nil {
		return err
	}
	expiresAt := time.Now().Add(sessionDurationDays * 24 * time.Hour)
	_, err = a.pool.Exec(ctx,
		"INSERT INTO sessions (token, user_id, expires_at) VALUES ($1, $2, $3)",
		token, userID, expiresAt,
	)
	if err != nil {
		return err
	}

	setSessionCookie(c, token)
	return okResponse(c)
}

func (a *AuthRoutes) logout(c echo.Context) error {
	cookie, err := c.Cookie("voltis_session")
	if err != nil || cookie.Value == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	if _, err := a.pool.Exec(reqCtx(c), "DELETE FROM sessions WHERE token = $1", cookie.Value); err != nil {
		return err
	}

	c.SetCookie(&http.Cookie{
		Name:     "voltis_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   c.Scheme() == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	return okResponse(c)
}

var firstUserFlow = true

func isFirstUserFlow(ctx context.Context, pool *pgxpool.Pool) (bool, error) {
	if firstUserFlow {
		var count int
		err := pool.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE 'ADMIN' = ANY(permissions)").Scan(&count)
		if err != nil {
			return false, err
		}
		if count > 0 {
			firstUserFlow = false
		}
	}
	return firstUserFlow, nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
