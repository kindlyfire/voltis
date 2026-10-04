package routes

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

const maxAppKeys = 20

type AppKeyRoutes struct {
	pool *pgxpool.Pool
	st   *settings.Store
}

func (r *AppKeyRoutes) Register(g *echo.Group) {
	g.Use(func(next echo.HandlerFunc) echo.HandlerFunc { // responses carry raw keys
		return func(c echo.Context) error {
			c.Response().Header().Set("Cache-Control", "private, no-store")
			return next(c)
		}
	})
	g.GET("", r.list)
	g.POST("", r.create)
	g.DELETE("/:key_id", r.revoke)
}

type AppKeyDTO struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Key        string     `json:"key"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	Feeds      struct {
		V1 string `json:"v1"`
		V2 string `json:"v2"`
	} `json:"feeds"`
}

func (r *AppKeyRoutes) toDTO(c echo.Context, k models.AppKey) AppKeyDTO {
	dto := AppKeyDTO{ID: k.ID, Name: k.Name, Key: k.Key, CreatedAt: k.CreatedAt, LastUsedAt: k.LastUsedAt}
	base := opdsBase(c, r.st) + "/opds/" + k.Key
	dto.Feeds.V1 = base + "/v1.2/catalog"
	dto.Feeds.V2 = base + "/v2/catalog"
	return dto
}

// newAppKey returns 160 random bits as 32 characters of [a-z2-7], typeable on e-reader keyboards
// and safe in a URL path.
func newAppKey() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

func (r *AppKeyRoutes) list(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	keys, err := db.Select[models.AppKey](reqCtx(c), r.pool,
		"SELECT * FROM app_keys WHERE user_id = $1 ORDER BY created_at DESC, id", user.ID)
	if err != nil {
		return err
	}
	dtos := make([]AppKeyDTO, len(keys))
	for i, k := range keys {
		dtos[i] = r.toDTO(c, k)
	}
	return c.JSON(http.StatusOK, dtos)
}

func (r *AppKeyRoutes) create(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	user, sess, err := requireSession(c)
	if err != nil {
		return err
	}
	var req struct {
		Name string `json:"name" validate:"notblank,max=100"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid JSON")
	}
	if err := ValidateStruct(req); err != nil {
		return err
	}

	ctx := reqCtx(c)
	var key models.AppKey
	err = db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
		// Lock order users → sessions, as a user delete's cascade. The user row serializes the cap.
		if _, err := tx.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", user.ID); err != nil {
			return err
		}
		// A logout that deleted the calling session while this waited must not leave a new key
		// behind; KEY SHARE makes a later logout's DELETE wait for this commit.
		// clock_timestamp, not NOW(): the transaction may have started long before the lock wait
		// ended, and the session may have expired meanwhile.
		err := tx.QueryRow(ctx, `
			SELECT 1 FROM sessions WHERE token = $1 AND user_id = $2
				AND expires_at > clock_timestamp()
				AND (absolute_expires_at IS NULL OR absolute_expires_at > clock_timestamp())
			FOR KEY SHARE
		`, sess.Token, user.ID).Scan(new(int))
		if errors.Is(err, pgx.ErrNoRows) {
			return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
		}
		if err != nil {
			return err
		}
		n, err := db.SelectScalar[int](ctx, tx, "SELECT COUNT(*) FROM app_keys WHERE user_id = $1", user.ID)
		if err != nil {
			return err
		}
		if n >= maxAppKeys {
			return echo.NewHTTPError(http.StatusBadRequest, "At most 20 keys per user")
		}
		key, err = db.SelectOne[models.AppKey](ctx, tx, `
			INSERT INTO app_keys (id, user_id, name, key) VALUES ($1, $2, $3, $4) RETURNING *
		`, models.MakeAppKeyID(), user.ID, strings.TrimSpace(req.Name), newAppKey())
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(http.StatusCreated, r.toDTO(c, key))
}

func (r *AppKeyRoutes) revoke(c echo.Context) error {
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(reqCtx(c),
		"DELETE FROM app_keys WHERE id = $1 AND user_id = $2", c.Param("key_id"), user.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "Key not found")
	}
	return okResponse(c)
}

// Key authentication, for any protocol that carries an app key in its URL path (OPDS today).

const contextKeyAppKey = "app_key"

var appKeyRe = regexp.MustCompile(`^[a-z2-7]{32}$`)

type appKeyUser struct {
	User  *models.User
	KeyID string
	Key   string
}

// userForAppKey returns the key's user, or nil for an unknown key. It bumps last_used_at at most
// once every 5 minutes, best effort.
func userForAppKey(ctx context.Context, pool *pgxpool.Pool, key string) (*appKeyUser, error) {
	if !appKeyRe.MatchString(key) {
		return nil, nil
	}
	row, err := db.SelectOne[struct {
		models.User
		KeyID      string     `db:"key_id"`
		LastUsedAt *time.Time `db:"last_used_at"`
	}](ctx, pool, `
		SELECT u.*, k.id AS key_id, k.last_used_at
		FROM app_keys k JOIN users u ON u.id = k.user_id
		WHERE k.key = $1
	`, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.LastUsedAt == nil || time.Since(*row.LastUsedAt) > 5*time.Minute {
		if _, err := pool.Exec(ctx, `
			UPDATE app_keys SET last_used_at = NOW()
			WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < NOW() - interval '5 minutes')
		`, row.KeyID); err != nil {
			slog.Warn("update app key last_used_at", "key_id", row.KeyID, "err", err)
		}
	}
	return &appKeyUser{User: &row.User, KeyID: row.KeyID, Key: key}, nil
}

// appKeyAuth authenticates by the :key path param alone, never by cookies or headers. It sets the
// user, so handlers using requireUser work unchanged, but no session.
func appKeyAuth(pool *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			a, err := userForAppKey(reqCtx(c), pool, c.Param("key"))
			if err != nil {
				return err
			}
			if a == nil {
				slog.Debug("app key auth failed", "path", c.Path())
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid key")
			}
			c.Set(contextKeyUser, a.User)
			c.Set(contextKeyAppKey, a)
			return next(c)
		}
	}
}
