package routes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"time"

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

	rows, err := ur.pool.Query(reqCtx(c), "SELECT * FROM users")
	if err != nil {
		return err
	}
	users, err := pgx.CollectRows(rows, pgx.RowToStructByName[models.User])
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

	if idOrNew == "new" {
		if req.Password == nil || *req.Password == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "Password is required for new users")
		}
		if len(*req.Password) > 72 {
			return echo.NewHTTPError(http.StatusBadRequest, "password must be at most 72 characters")
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}

		id := models.MakeUserID()
		_, err = ur.pool.Exec(ctx, `
			INSERT INTO users (id, created_at, updated_at, username, password_hash, permissions)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, id, now, now, req.Username, string(hash), req.Permissions)
		if err != nil {
			return err
		}

		user, err := getUser(ctx, ur.pool, id)
		if err != nil {
			return err
		}
		return c.JSON(http.StatusOK, userToDTO(user))
	}

	existing, err := getUser(ctx, ur.pool, idOrNew)
	if err != nil {
		return err
	}

	passwordHash := existing.PasswordHash
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

	_, err = ur.pool.Exec(ctx, `
		UPDATE users SET username = $1, password_hash = $2, permissions = $3, updated_at = $4
		WHERE id = $5
	`, req.Username, passwordHash, req.Permissions, now, idOrNew)
	if err != nil {
		return err
	}

	user, err := getUser(ctx, ur.pool, idOrNew)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, userToDTO(user))
}

func (ur *UserRoutes) delete(c echo.Context) error {
	if _, err := requireUser(c); err != nil {
		return err
	}

	ctx := reqCtx(c)
	userID := c.Param("user_id")
	result, err := ur.pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	return okResponse(c)
}

func getUser(ctx context.Context, pool *pgxpool.Pool, id string) (models.User, error) {
	rows, err := pool.Query(ctx, "SELECT * FROM users WHERE id = $1", id)
	if err != nil {
		return models.User{}, err
	}
	user, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[models.User])
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	return user, err
}
