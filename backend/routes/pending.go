package routes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
)

const (
	pendingCookie   = "voltis_oidc"
	pendingTTL      = 10 * time.Minute
	pendingFlow     = "oidc_flow"
	pendingComplete = "oidc_complete"

	maxConfirmAttempts = 5
)

type oidcFlow struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Link     bool   `json:"link"`
	Redirect string `json:"redirect,omitempty"`
}

// oidcComplete is a verified identity waiting on the user.
type oidcComplete struct {
	Needs         string   `json:"needs"`
	Issuer        string   `json:"issuer"`
	Subject       string   `json:"subject"`
	Username      string   `json:"username"`
	Email         string   `json:"email"`
	Groups        []string `json:"groups"`
	HasGroups     bool     `json:"has_groups"`
	MatchID       string   `json:"match_id"`
	MatchUsername string   `json:"match_username"`
	MatchBy       string   `json:"match_by"`
	Redirect      string   `json:"redirect,omitempty"`
}

func (p oidcComplete) identity() ExternalIdentity {
	return ExternalIdentity{
		Provider:  models.SessionOIDC,
		Issuer:    p.Issuer,
		Subject:   p.Subject,
		Username:  p.Username,
		Email:     p.Email,
		Groups:    p.Groups,
		HasGroups: p.HasGroups,
	}
}

func randomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// The table stores only a hash, so a leaked row cannot be used as the cookie.
func pendingKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func insertPending(ctx context.Context, q db.Querier, kind string, data any, userID, sessionToken *string) (string, error) {
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	if _, err := q.Exec(ctx, "DELETE FROM auth_pending WHERE expires_at < NOW()"); err != nil {
		return "", err
	}

	raw := randomToken()
	_, err = q.Exec(ctx, `
		INSERT INTO auth_pending (id, kind, data, user_id, session_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, pendingKey(raw), kind, encoded, userID, sessionToken, time.Now().Add(pendingTTL))
	if err != nil {
		return "", err
	}
	return raw, nil
}

func readPending(ctx context.Context, q db.Querier, raw, kind string) (*models.AuthPending, error) {
	return selectPending(ctx, q,
		"SELECT * FROM auth_pending WHERE id = $1 AND kind = $2 AND expires_at > NOW()", pendingKey(raw), kind)
}

// consumeFlow deletes the flow only for its own state, so a forged callback
// cannot cancel a sign-in in progress.
func consumeFlow(ctx context.Context, q db.Querier, raw, state string) (*models.AuthPending, error) {
	return selectPending(ctx, q, `
		DELETE FROM auth_pending
		WHERE id = $1 AND kind = $2 AND expires_at > NOW() AND data->>'state' = $3
		RETURNING *
	`, pendingKey(raw), pendingFlow, state)
}

// reserveAttempt counts a password attempt before it is checked, so concurrent
// guesses cannot exceed the cap.
func reserveAttempt(ctx context.Context, q db.Querier, raw string) (*models.AuthPending, error) {
	return selectPending(ctx, q, `
		UPDATE auth_pending SET attempts = attempts + 1
		WHERE id = $1 AND kind = $2 AND expires_at > NOW() AND attempts < $3
		RETURNING *
	`, pendingKey(raw), pendingComplete, maxConfirmAttempts)
}

func selectPending(ctx context.Context, q db.Querier, query string, args ...any) (*models.AuthPending, error) {
	row, err := db.SelectOne[models.AuthPending](ctx, q, query, args...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func pendingData[T any](row *models.AuthPending) (T, error) {
	var out T
	err := json.Unmarshal(row.Data, &out)
	return out, err
}

func setPendingCookie(c echo.Context, st *settings.Store, raw string) {
	c.SetCookie(&http.Cookie{
		Name:     pendingCookie,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(c, st),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(pendingTTL.Seconds()),
	})
}

func clearPendingCookie(c echo.Context, st *settings.Store) {
	c.SetCookie(&http.Cookie{
		Name:     pendingCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(c, st),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

func pendingID(c echo.Context) string {
	cookie, err := c.Cookie(pendingCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}
