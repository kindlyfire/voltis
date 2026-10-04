package routes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
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

type UserRoutes struct {
	pool *pgxpool.Pool
	hub  *WebSocketHub
	st   *settings.Store

	proxyEnabled bool
}

func (ur *UserRoutes) Register(g *echo.Group) {
	g.GET("", adminOnly(ur.list))
	g.GET("/me", ur.me)
	g.POST("/me", ur.updateMe)
	g.PATCH("/me/preferences", ur.patchPreferences)
	g.GET("/me/identities", ur.myIdentities)
	g.DELETE("/me/identities/:identity_id", ur.unlinkMine)
	g.GET("/me/sessions", ur.sessions)
	g.DELETE("/me/sessions/:session_id", ur.revokeSession)
	g.GET("/:user_id/identities", adminOnly(ur.identities))
	g.POST("/:user_id/identities", adminOnly(ur.linkIdentity))
	g.DELETE("/:user_id/identities/:identity_id", adminOnly(ur.unlinkIdentity))
	g.POST("/:id_or_new", ur.upsert)
	g.DELETE("/:user_id", adminOnly(ur.delete))
}

type UserDTO struct {
	ID          string          `json:"id"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Username    string          `json:"username"`
	Email       *string         `json:"email"`
	Permissions []string        `json:"permissions"`
	Preferences json.RawMessage `json:"preferences"`
	HasPassword bool            `json:"has_password"`
}

type MeDTO struct {
	UserDTO
	SessionMethod string `json:"session_method"`
	CanLogout     bool   `json:"can_logout"`
}

func userToDTO(u models.User) UserDTO {
	perms := u.Permissions
	if perms == nil {
		perms = []string{}
	}
	prefs := u.Preferences
	if prefs == nil {
		prefs = json.RawMessage("{}")
	}
	return UserDTO{
		ID:          u.ID,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		Username:    u.Username,
		Email:       u.Email,
		Permissions: perms,
		Preferences: prefs,
		HasPassword: u.PasswordHash != nil,
	}
}

func (ur *UserRoutes) list(c echo.Context) error {
	users, err := db.Select[models.User](reqCtx(c), ur.pool, "SELECT * FROM users")
	if err != nil {
		return err
	}

	result := make([]UserDTO, len(users))
	for i, u := range users {
		result[i] = userToDTO(u)
	}
	return c.JSON(http.StatusOK, result)
}

func (ur *UserRoutes) me(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	method := models.SessionPassword
	if session := requestSession(c); session != nil {
		method = session.Method
	}
	// A proxy session can only end upstream, so without that URL there is
	// nothing a logout button could do.
	canLogout := method != models.SessionProxy || ur.st.String(settings.AuthProxyLogoutURL) != ""
	return c.JSON(http.StatusOK, MeDTO{UserDTO: userToDTO(*user), SessionMethod: method, CanLogout: canLogout})
}

type SessionDTO struct {
	ID         string     `json:"id" db:"id"`
	Method     string     `json:"method" db:"method"`
	ClientName *string    `json:"client_name" db:"client_name"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at" db:"last_used_at"`
	ExpiresAt  time.Time  `json:"expires_at" db:"expires_at"`
	Current    bool       `json:"current" db:"current"`
}

func (ur *UserRoutes) sessions(c echo.Context) error {
	user, session, err := requireSession(c)
	if err != nil {
		return err
	}
	rows, err := db.Select[SessionDTO](reqCtx(c), ur.pool, `
		SELECT id, method, client_name, created_at, last_used_at, expires_at, token = $2 AS current
		FROM sessions WHERE user_id = $1 AND `+liveSession+`
		ORDER BY current DESC, last_used_at DESC NULLS LAST, created_at DESC
	`, user.ID, session.Token)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, rows)
}

func (ur *UserRoutes) revokeSession(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	user, session, err := requireSession(c)
	if err != nil {
		return err
	}
	// The current session ends through logout, which also clears its cookie.
	tag, err := ur.pool.Exec(reqCtx(c),
		"DELETE FROM sessions WHERE id = $1 AND user_id = $2 AND token <> $3",
		c.Param("session_id"), user.ID, session.Token)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Session not found")
	}
	ur.hub.Drop(user.ID)
	return okResponse(c)
}

type updateMeRequest struct {
	Username string  `json:"username"`
	Email    *string `json:"email"`
	Password *string `json:"password"`
}

func (ur *UserRoutes) updateMe(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	var req updateMeRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	var passwordHash *string
	setsPassword := req.Password != nil && *req.Password != ""
	if setsPassword {
		if !ur.st.Bool(settings.AuthPasswordLoginEnabled) {
			return errPasswordLoginDisabled()
		}
		hash, err := hashPassword(*req.Password)
		if err != nil {
			return err
		}
		passwordHash = hash
	}

	setsEmail, email, err := parseEmail(req.Email)
	if err != nil {
		return err
	}

	err = db.WithTx(reqCtx(c), ur.pool, func(tx pgx.Tx) error {
		if setsPassword {
			if err := requirePasswordLogin(reqCtx(c), tx); err != nil {
				return err
			}
		}
		// Only the supplied columns are written: values read at authentication
		// are stale by now, and writing them back can undo another request.
		_, err := tx.Exec(reqCtx(c), `
			UPDATE users SET
				username = COALESCE($1::text, username),
				password_hash = COALESCE($2::text, password_hash),
				email = CASE WHEN $3 THEN $4::text ELSE email END,
				updated_at = $5
			WHERE id = $6
		`, nullable(req.Username), passwordHash, setsEmail, email, time.Now().UTC(), user.ID)
		return err
	})
	if msg := uniqueViolation(err); msg != "" {
		return echo.NewHTTPError(http.StatusBadRequest, msg)
	}
	if err != nil {
		return err
	}

	updated, err := getUser(reqCtx(c), ur.pool, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userToDTO(updated))
}

func (ur *UserRoutes) patchPreferences(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}

	// Not c.Bind: it accepts an empty body without touching the target.
	dec := json.NewDecoder(c.Request().Body)
	dec.UseNumber()
	var patch map[string]any
	if err := dec.Decode(&patch); err != nil || patch == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "body must be a JSON object")
	}
	// A single Decode stops at the end of the first value, so `{} {}` and
	// `{} garbage` would otherwise pass.
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return echo.NewHTTPError(http.StatusBadRequest, "body must be a JSON object")
	}

	ctx := reqCtx(c)
	var updated models.User
	err = db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		raw, err := db.SelectScalar[json.RawMessage](ctx, tx,
			"SELECT preferences FROM users WHERE id = $1 FOR UPDATE", user.ID)
		if err != nil {
			return err
		}

		// Into `any`, not a map: the old POST /users/me accepted arbitrary
		// JSON here, and mergePatch already replaces a non-object target.
		var current any
		d := json.NewDecoder(bytes.NewReader(raw))
		d.UseNumber()
		if err := d.Decode(&current); err != nil && !errors.Is(err, io.EOF) {
			return err
		}

		updated, err = db.SelectOne[models.User](ctx, tx, `
			UPDATE users SET preferences = $1, updated_at = $2
			WHERE id = $3 RETURNING *
		`, mergePatch(current, patch), time.Now().UTC(), user.ID)
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userToDTO(updated))
}

type upsertUserRequest struct {
	Username    string   `json:"username"`
	Email       *string  `json:"email"`
	Password    *string  `json:"password"`
	Permissions []string `json:"permissions"`
}

func (ur *UserRoutes) upsert(c echo.Context) error {
	admin, err := requireAdmin(c)
	if err != nil {
		return err
	}

	idOrNew := c.Param("id_or_new")

	var req upsertUserRequest
	if err := c.Bind(&req); err != nil {
		return err
	}

	if admin.ID == idOrNew && !slices.Contains(req.Permissions, "ADMIN") {
		return echo.NewHTTPError(http.StatusForbidden, "Cannot remove admin permission from yourself")
	}

	ctx := reqCtx(c)
	now := time.Now().UTC()

	var newHash *string
	if req.Password != nil && *req.Password != "" {
		if !ur.st.Bool(settings.AuthPasswordLoginEnabled) {
			return errPasswordLoginDisabled()
		}
		newHash, err = hashPassword(*req.Password)
		if err != nil {
			return err
		}
	}

	if idOrNew == "new" {
		id := models.MakeUserID()
		_, email, err := parseEmail(req.Email)
		if err != nil {
			return err
		}
		err = db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
			if err := db.LockAdminMutation(ctx, tx); err != nil {
				return err
			}
			if newHash != nil {
				if err := requirePasswordLogin(ctx, tx); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO users (id, created_at, updated_at, username, email, password_hash, permissions)
				VALUES ($1, $2, $3, $4, $5, $6, $7)
			`, id, now, now, req.Username, email, newHash, req.Permissions)
			return err
		})
		if msg := uniqueViolation(err); msg != "" {
			return echo.NewHTTPError(http.StatusBadRequest, msg)
		}
		if err != nil {
			return err
		}

		user, err := getUser(ctx, ur.pool, id)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, userToDTO(user))
	}

	var user models.User
	err = db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}
		if newHash != nil {
			if err := requirePasswordLogin(ctx, tx); err != nil {
				return err
			}
		}

		existing, err := getUser(ctx, tx, idOrNew)
		if err != nil {
			return err
		}

		if slices.Contains(existing.Permissions, "ADMIN") && !slices.Contains(req.Permissions, "ADMIN") {
			otherAdmin, err := db.SelectScalar[bool](ctx, tx,
				"SELECT EXISTS (SELECT 1 FROM users WHERE permissions @> ARRAY['ADMIN'] AND id <> $1)", idOrNew)
			if err != nil {
				return err
			}
			if !otherAdmin {
				return echo.NewHTTPError(http.StatusForbidden, "Cannot remove the last admin")
			}
		}

		setsEmail, email, err := parseEmail(req.Email)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE users SET
				username = COALESCE($1::text, username),
				password_hash = COALESCE($2::text, password_hash),
				permissions = $3,
				email = CASE WHEN $4 THEN $5::text ELSE email END,
				updated_at = $6
			WHERE id = $7
		`, nullable(req.Username), newHash, req.Permissions, setsEmail, email, now, idOrNew); err != nil {
			return err
		}

		user, err = getUser(ctx, tx, idOrNew)
		return err
	})
	if msg := uniqueViolation(err); msg != "" {
		return echo.NewHTTPError(http.StatusBadRequest, msg)
	}
	if err != nil {
		return err
	}
	ur.hub.Drop(idOrNew)
	return c.JSON(http.StatusOK, userToDTO(user))
}

func (ur *UserRoutes) delete(c echo.Context) error {
	ctx := reqCtx(c)
	userID := c.Param("user_id")

	err := db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}

		wasAdmin, err := db.SelectScalar[bool](ctx, tx, `
			DELETE FROM users WHERE id = $1
			RETURNING permissions @> ARRAY['ADMIN']
		`, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusNotFound, "User not found")
		}
		if err != nil {
			return err
		}
		if !wasAdmin {
			return nil
		}

		adminLeft, err := db.SelectScalar[bool](ctx, tx,
			"SELECT EXISTS (SELECT 1 FROM users WHERE permissions @> ARRAY['ADMIN'])")
		if err != nil {
			return err
		}
		if !adminLeft {
			return echo.NewHTTPError(http.StatusForbidden, "Cannot delete the last admin")
		}
		return nil
	})
	if err != nil {
		return err
	}
	ur.hub.Drop(userID)
	return okResponse(c)
}

// Stored lowercase, as external logins write them. The first result reports
// whether the caller asked for a change at all.
func parseEmail(requested *string) (bool, *string, error) {
	if requested == nil {
		return false, nil, nil
	}
	email := normalizeEmail(*requested)
	if email == "" {
		return true, nil, nil
	}
	if validate.Var(email, "email") != nil {
		return false, nil, echo.NewHTTPError(http.StatusBadRequest, "that is not a valid email address")
	}
	return true, &email, nil
}

func uniqueViolation(err error) string {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23505" {
		return ""
	}
	switch pgErr.ConstraintName {
	case "users_email_lower_key":
		return "that email address is already in use"
	case "users_username_key":
		return "that username is already taken"
	}
	return "that value is already in use"
}

func hashPassword(password string) (*string, error) {
	if len(password) > 72 {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "password must be at most 72 characters")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	s := string(hash)
	return &s, nil
}

func getUser(ctx context.Context, q db.Querier, id string) (models.User, error) {
	user, err := db.SelectOne[models.User](ctx, q, "SELECT * FROM users WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	return user, err
}
