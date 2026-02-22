package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/go-playground/validator/v10"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	_ = v.RegisterValidation("notblank", func(fl validator.FieldLevel) bool {
		return strings.TrimSpace(fl.Field().String()) != ""
	})
	return v
}

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

// QueryToStruct binds query parameters into a struct using `query` tags and
// applies defaults from `default` tags.
// Supported field types: string, bool, int, *int.
func QueryToStruct[T any](c echo.Context) (T, error) {
	var result T
	v := reflect.ValueOf(&result).Elem()
	t := v.Type()

	for i := range t.NumField() {
		field := t.Field(i)
		fv := v.Field(i)

		name := field.Tag.Get("query")
		if name == "" {
			continue
		}

		raw := c.QueryParam(name)
		if raw == "" {
			if def, ok := field.Tag.Lookup("default"); ok {
				raw = def
			}
		}
		if raw == "" {
			continue
		}

		switch fv.Kind() {
		case reflect.String:
			fv.SetString(raw)
		case reflect.Bool:
			switch raw {
			case "true":
				fv.SetBool(true)
			case "false":
				fv.SetBool(false)
			default:
				return result, echo.NewHTTPError(http.StatusBadRequest,
					fmt.Sprintf("Invalid value for %s: must be 'true' or 'false'", name))
			}
		case reflect.Int:
			n, err := strconv.Atoi(raw)
			if err != nil {
				return result, echo.NewHTTPError(http.StatusBadRequest,
					fmt.Sprintf("Invalid value for %s: must be an integer", name))
			}
			fv.SetInt(int64(n))
		case reflect.Pointer:
			if fv.Type().Elem().Kind() == reflect.Int {
				n, err := strconv.Atoi(raw)
				if err != nil {
					return result, echo.NewHTTPError(http.StatusBadRequest,
						fmt.Sprintf("Invalid value for %s: must be an integer", name))
				}
				fv.Set(reflect.ValueOf(&n))
			}
		}
	}

	return result, nil
}

// ValidateStruct validates a struct using `validate` tags. Field names in error
// messages are resolved from `query` or `json` tags when available.
func ValidateStruct[T any](s T) error {
	if err := validate.Struct(s); err != nil {
		var ve validator.ValidationErrors
		if errors.As(err, &ve) {
			t := reflect.TypeOf(s)
			fields := make([]string, len(ve))
			for i, fe := range ve {
				name := fe.Field()
				if sf, ok := t.FieldByName(fe.StructField()); ok {
					if q := sf.Tag.Get("query"); q != "" {
						name = q
					} else if j := sf.Tag.Get("json"); j != "" {
						name = strings.SplitN(j, ",", 2)[0]
					}
				}
				fields[i] = fmt.Sprintf("%s: failed on '%s'", name, fe.Tag())
			}
			return echo.NewHTTPError(http.StatusBadRequest,
				fmt.Sprintf("Validation failed: %s", strings.Join(fields, "; ")))
		}
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	return nil
}

// BindQuery binds query parameters into a struct and validates it.
func BindQuery[T any](c echo.Context) (T, error) {
	result, err := QueryToStruct[T](c)
	if err != nil {
		return result, err
	}
	if err := ValidateStruct(result); err != nil {
		return result, err
	}
	return result, nil
}
