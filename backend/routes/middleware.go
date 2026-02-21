package routes

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"voltis/models"

	"github.com/jmoiron/sqlx"
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

func authMiddleware(db *sqlx.DB) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := resolveUser(c, db)
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

func resolveUser(c echo.Context, db *sqlx.DB) (*models.User, error) {
	cookie, err := c.Cookie("voltis_session")
	if err != nil || cookie.Value == "" {
		return nil, nil
	}

	var row struct {
		models.User
		SessionToken     string    `db:"session_token"`
		SessionExpiresAt time.Time `db:"session_expires_at"`
	}
	err = db.Get(&row, `
		SELECT u.*, s.token AS session_token, s.expires_at AS session_expires_at
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > NOW()
	`, cookie.Value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Refresh session if expiring within threshold
	timeUntilExpiry := time.Until(row.SessionExpiresAt)
	if timeUntilExpiry < sessionRefreshThresholdDays*24*time.Hour {
		newExpiry := time.Now().Add(sessionDurationDays * 24 * time.Hour)
		_, _ = db.Exec("UPDATE sessions SET expires_at = $1 WHERE token = $2", newExpiry, row.SessionToken)
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
	for _, p := range user.Permissions {
		if p == "ADMIN" {
			return user, nil
		}
	}
	return nil, echo.NewHTTPError(http.StatusForbidden)
}

func okResponse(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{"ok": true})
}
