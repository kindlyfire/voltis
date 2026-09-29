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
	"github.com/labstack/echo/v4"
)

type IdentityDTO struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	Issuer    string    `json:"issuer"`
	Subject   string    `json:"subject"`
	Email     *string   `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

func identityToDTO(i models.UserIdentity) IdentityDTO {
	return IdentityDTO{
		ID: i.ID, Provider: i.Provider, Issuer: i.Issuer,
		Subject: i.Subject, Email: i.Email, CreatedAt: i.CreatedAt,
	}
}

func (ur *UserRoutes) myIdentities(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	return ur.listIdentities(c, user.ID)
}

func (ur *UserRoutes) identities(c echo.Context) error {
	return ur.listIdentities(c, c.Param("user_id"))
}

func (ur *UserRoutes) listIdentities(c echo.Context, userID string) error {
	rows, err := db.Select[models.UserIdentity](reqCtx(c), ur.pool,
		"SELECT * FROM user_identities WHERE user_id = $1 ORDER BY created_at", userID)
	if err != nil {
		return err
	}
	result := make([]IdentityDTO, len(rows))
	for i, row := range rows {
		result[i] = identityToDTO(row)
	}
	return c.JSON(http.StatusOK, result)
}

func (ur *UserRoutes) linkIdentity(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	var req struct {
		Provider string `json:"provider"`
		Issuer   string `json:"issuer"`
		Subject  string `json:"subject"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if req.Provider != models.SessionOIDC && req.Provider != models.SessionProxy {
		return echo.NewHTTPError(http.StatusBadRequest, "provider must be oidc or proxy")
	}
	if req.Subject == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "subject is required")
	}
	if req.Provider == models.SessionProxy {
		req.Issuer = ""
	}

	ctx := reqCtx(c)
	userID := c.Param("user_id")
	if _, err := getUser(ctx, ur.pool, userID); err != nil {
		return err
	}

	id := ExternalIdentity{Provider: req.Provider, Issuer: req.Issuer, Subject: req.Subject}
	err := db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		return attachIdentity(ctx, tx, id, userID)
	})
	if err != nil {
		return err
	}
	return ur.listIdentities(c, userID)
}

func (ur *UserRoutes) unlinkMine(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	if err := ur.unlink(reqCtx(c), user.ID, c.Param("identity_id")); err != nil {
		return err
	}
	ur.hub.Drop(user.ID)
	clearSessionCookie(c, ur.st)
	return okResponse(c)
}

func (ur *UserRoutes) unlinkIdentity(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	userID := c.Param("user_id")
	if err := ur.unlink(reqCtx(c), userID, c.Param("identity_id")); err != nil {
		return err
	}
	ur.hub.Drop(userID)
	return ur.listIdentities(c, userID)
}

// Revokes every session, the caller's own included. Lock order matches
// finalize: settings version, identity, user row, sessions.
func (ur *UserRoutes) unlink(ctx context.Context, userID, identityID string) error {
	return db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		if err := settings.LockVersion(ctx, tx); err != nil {
			return err
		}
		// Only set-issuer changes the key columns, and it holds the version lock
		// for writing, so reading them before the identity lock is safe.
		key, err := db.SelectOne[models.UserIdentity](ctx, tx,
			"SELECT * FROM user_identities WHERE id = $1 AND user_id = $2", identityID, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "Identity not found")
		}
		if err != nil {
			return err
		}
		if err := db.LockIdentity(ctx, tx, key.Provider, key.Issuer, key.Subject); err != nil {
			return err
		}
		// Before the delete: account deletion locks the user row, then its identities.
		if err := lockUser(ctx, tx, userID); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx,
			"DELETE FROM user_identities WHERE id = $1 AND user_id = $2", identityID, userID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return echo.NewHTTPError(http.StatusNotFound, "Identity not found")
		}
		if err := requireLoginMethod(ctx, tx, userID, ur.proxyEnabled); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id = $1", userID)
		return err
	})
}
