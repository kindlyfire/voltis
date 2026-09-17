package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
)

type UserRoutes struct {
	pool *pgxpool.Pool
}

func (ur *UserRoutes) Register(g *echo.Group) {
	g.GET("", ur.list)
	g.GET("/me", ur.me)
	g.POST("/me", ur.updateMe)
	g.POST("/:id_or_new", ur.upsert)
	g.DELETE("/:user_id", ur.delete)
}

type UserDTO struct {
	ID          string          `json:"id"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	Username    string          `json:"username"`
	Permissions []string        `json:"permissions"`
	Preferences json.RawMessage `json:"preferences"`
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
		Permissions: perms,
		Preferences: prefs,
	}
}

func (ur *UserRoutes) list(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

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
	return c.JSON(http.StatusOK, userToDTO(*user))
}

type updateMeRequest struct {
	Username    string           `json:"username"`
	Password    *string          `json:"password"`
	Preferences *json.RawMessage `json:"preferences"`
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

	passwordHash := user.PasswordHash
	if req.Password != nil && *req.Password != "" {
		if len(*req.Password) > 72 {
			return echo.NewHTTPError(http.StatusBadRequest, "password must be at most 72 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		passwordHash = string(hash)
	}

	preferences := user.Preferences
	if req.Preferences != nil {
		preferences = *req.Preferences
	}

	_, err = ur.pool.Exec(reqCtx(c), `
		UPDATE users SET username = $1, password_hash = $2, preferences = $3, updated_at = $4
		WHERE id = $5
	`, req.Username, passwordHash, preferences, time.Now().UTC(), user.ID)
	if err != nil {
		return err
	}

	updated, err := getUser(reqCtx(c), ur.pool, user.ID)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userToDTO(updated))
}

type upsertUserRequest struct {
	Username    string   `json:"username"`
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

	if idOrNew == "new" && (req.Password == nil || *req.Password == "") {
		return echo.NewHTTPError(http.StatusBadRequest, "Password is required for new users")
	}

	newHash := ""
	if req.Password != nil && *req.Password != "" {
		if len(*req.Password) > 72 {
			return echo.NewHTTPError(http.StatusBadRequest, "password must be at most 72 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
		newHash = string(hash)
	}

	if idOrNew == "new" {
		id := models.MakeUserID()
		_, err = ur.pool.Exec(ctx, `
			INSERT INTO users (id, created_at, updated_at, username, password_hash, permissions)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, id, now, now, req.Username, newHash, req.Permissions)
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
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminMutationLockKey); err != nil {
			return err
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

		passwordHash := existing.PasswordHash
		if newHash != "" {
			passwordHash = newHash
		}

		if _, err := tx.Exec(ctx, `
			UPDATE users SET username = $1, password_hash = $2, permissions = $3, updated_at = $4
			WHERE id = $5
		`, req.Username, passwordHash, req.Permissions, now, idOrNew); err != nil {
			return err
		}

		user, err = getUser(ctx, tx, idOrNew)
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userToDTO(user))
}

const adminMutationLockKey int64 = 7263845190

func (ur *UserRoutes) delete(c echo.Context) error {
	if _, err := requireAdmin(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	userID := c.Param("user_id")

	err := db.WithTx(ctx, ur.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminMutationLockKey); err != nil {
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
	return okResponse(c)
}

func getUser(ctx context.Context, q db.Querier, id string) (models.User, error) {
	user, err := db.SelectOne[models.User](ctx, q, "SELECT * FROM users WHERE id = $1", id)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	return user, err
}
