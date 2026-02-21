package routes

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

const (
	sessionDurationDays         = 30
	sessionRefreshThresholdDays = 14
	sessionMaxAge               = sessionDurationDays * 24 * 60 * 60
	contextKeyUser              = "user"
)

func setSessionCookie(c echo.Context, token string) {
	secure := c.Scheme() == "https"
	c.SetCookie(&http.Cookie{
		Name:     "voltis_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	})
}

func authMiddleware(pool *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := resolveUser(c, pool)
			if err != nil {
				return err
			}
			if user != nil {
				c.Set(contextKeyUser, user)
			}
			return next(c)
		}
	}
}

type userWithSession struct {
	models.User
	SessionToken     string    `db:"session_token"`
	SessionExpiresAt time.Time `db:"session_expires_at"`
}

func resolveUser(c echo.Context, pool *pgxpool.Pool) (*models.User, error) {
	cookie, err := c.Cookie("voltis_session")
	if err != nil || cookie.Value == "" {
		return nil, nil
	}

	ctx := c.Request().Context()
	row, err := db.SelectOne[userWithSession](ctx, pool, `
		SELECT u.*, s.token AS session_token, s.expires_at AS session_expires_at
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > NOW()
	`, cookie.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Refresh session if expiring within threshold
	timeUntilExpiry := time.Until(row.SessionExpiresAt)
	if timeUntilExpiry < sessionRefreshThresholdDays*24*time.Hour {
		newExpiry := time.Now().Add(sessionDurationDays * 24 * time.Hour)
		_, _ = pool.Exec(ctx, "UPDATE sessions SET expires_at = $1 WHERE token = $2", newExpiry, row.SessionToken)
		setSessionCookie(c, row.SessionToken)
	}

	return &row.User, nil
}

func requireUser(c echo.Context) (*models.User, error) {
	user, _ := c.Get(contextKeyUser).(*models.User)
	if user == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}
	return user, nil
}

func requireAdmin(c echo.Context) (*models.User, error) {
	user, err := requireUser(c)
	if err != nil {
		return nil, err
	}
	if slices.Contains(user.Permissions, "ADMIN") {
		return user, nil
	}
	return nil, echo.NewHTTPError(http.StatusForbidden)
}

func okResponse(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}

func reqCtx(c echo.Context) context.Context {
	return c.Request().Context()
}

type PaginatedResponse[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}
