package routes

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"voltis/config"
	"voltis/db"
	"voltis/models"
	"voltis/settings"

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
	sessionTouchInterval        = time.Hour
	contextKeyUser              = "user"
	contextKeySession           = "session"
)

// sessionInfo is the session the request ended up on, not the cookie it
// arrived with: forwarded auth may have replaced it.
type sessionInfo struct {
	Token  string
	Method string
}

func requestSession(c echo.Context) *sessionInfo {
	session, _ := c.Get(contextKeySession).(*sessionInfo)
	return session
}

// cookieSecure never trusts X-Forwarded-Proto, which any client can set.
func cookieSecure(c echo.Context, st *settings.Store) bool {
	if u, err := url.Parse(st.String(settings.AppPublicURL)); err == nil && strings.EqualFold(u.Scheme, "https") {
		return true
	}
	return c.Request().TLS != nil
}

func setSessionCookie(c echo.Context, st *settings.Store, token string) {
	c.SetCookie(&http.Cookie{
		Name:     "voltis_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(c, st),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   sessionMaxAge,
	})
}

func clearSessionCookie(c echo.Context, st *settings.Store) {
	c.SetCookie(&http.Cookie{
		Name:     "voltis_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   cookieSecure(c, st),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// Cross-site requests cannot set this header without a preflight CORS rejects.
func requireJSON(c echo.Context) error {
	ct := c.Request().Header.Get(echo.HeaderContentType)
	if !strings.HasPrefix(ct, echo.MIMEApplicationJSON) {
		return echo.NewHTTPError(http.StatusUnsupportedMediaType, "expected a JSON request body")
	}
	return nil
}

type resolver struct {
	pool  *pgxpool.Pool
	st    *settings.Store
	hub   *WebSocketHub
	proxy config.ProxyAuth
}

func authMiddleware(r *resolver) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			user, err := r.resolve(c)
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
	SessionToken      string     `db:"session_token"`
	SessionExpiresAt  time.Time  `db:"session_expires_at"`
	SessionMethod     string     `db:"session_method"`
	SessionAbsolute   *time.Time `db:"session_absolute_expires_at"`
	SessionClientName *string    `db:"session_client_name"`
	SessionLastUsed   *time.Time `db:"session_last_used_at"`
}

func (r *resolver) resolve(c echo.Context) (*models.User, error) {
	// A well-formed bearer wins over the cookie and never falls back to it.
	if token, ok := bearerToken(c); ok {
		row, err := sessionByToken(reqCtx(c), r.pool, token)
		if err != nil || row == nil {
			return nil, err
		}
		// A browser proxy session needs its header; a proxy device session needs proxy auth enabled.
		if row.SessionMethod == models.SessionProxy && (row.SessionClientName == nil || !r.proxy.Enabled()) {
			return nil, nil
		}
		r.refresh(c, row, false)
		c.Set(contextKeySession, &sessionInfo{Token: row.SessionToken, Method: row.SessionMethod})
		return &row.User, nil
	}

	id, forwarded, err := r.proxyIdentity(c)
	if err != nil {
		return nil, err
	}

	var row *userWithSession
	if token := cookieToken(c); token != "" {
		if row, err = sessionByToken(reqCtx(c), r.pool, token); err != nil {
			return nil, err
		}
	}
	if forwarded {
		return r.resolveForwarded(c, id, row)
	}
	// A proxy session without its header means the proxy was bypassed.
	if row == nil || row.SessionMethod == models.SessionProxy {
		return nil, nil
	}
	r.refresh(c, row, true)
	c.Set(contextKeySession, &sessionInfo{Token: row.SessionToken, Method: row.SessionMethod})
	return &row.User, nil
}

func (r *resolver) resolveForwarded(c echo.Context, id ExternalIdentity, row *userWithSession) (*models.User, error) {
	ctx := reqCtx(c)
	login, err := r.resolveExternalLogin(ctx, id)
	if err == nil && login.User == nil {
		err = echo.NewHTTPError(http.StatusForbidden, "no account for this login; ask an administrator")
	}
	if err != nil {
		// The session this would have replaced must not outlive the rejection.
		if _, rejected := errors.AsType[*echo.HTTPError](err); rejected && row != nil {
			if _, delErr := r.pool.Exec(ctx, "DELETE FROM sessions WHERE token = $1", row.SessionToken); delErr != nil {
				return nil, delErr
			}
			r.hub.Drop(row.ID)
		}
		return nil, err
	}
	user := login.User

	var token string
	if row != nil && row.SessionMethod == models.SessionProxy && row.ID == user.ID {
		r.refresh(c, row, true)
		token = row.SessionToken
	} else {
		err := db.WithTx(ctx, r.pool, func(tx pgx.Tx) error {
			if err := db.LockUserSessions(ctx, tx, user.ID); err != nil {
				return err
			}
			// User first: a concurrent delete cascades in that same order.
			if _, err := db.SelectScalar[string](ctx, tx,
				"SELECT id FROM users WHERE id = $1 FOR KEY SHARE", user.ID); err != nil {
				return err
			}
			if row != nil {
				if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE token = $1", row.SessionToken); err != nil {
					return err
				}
			}

			// Per-request proxy authorization makes session reuse safe. A device
			// session's token must never become a browser cookie.
			live, err := db.SelectScalar[string](ctx, tx, `
				SELECT token FROM sessions
				WHERE user_id = $1 AND method = $2 AND client_name IS NULL AND `+liveSession+`
				ORDER BY expires_at DESC LIMIT 1
			`, user.ID, models.SessionProxy)
			if err == nil {
				token = live
				return nil
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			token, err = createSession(ctx, tx, r.st, user.ID, models.SessionProxy)
			return err
		})
		if err != nil {
			return nil, err
		}
		if row != nil && row.ID != user.ID {
			r.hub.Drop(row.ID)
		}
		setSessionCookie(c, r.st, token)
		syncIdentityEmail(ctx, r.pool, id)
	}
	c.Set(contextKeySession, &sessionInfo{Token: token, Method: models.SessionProxy})

	syncEmail(ctx, r.pool, user, id.Email)
	if err := r.syncAdmin(ctx, user, id); err != nil {
		return nil, err
	}
	return user, nil
}

// refresh slides the expiry and bumps last_used_at, writing at most once per
// touch interval unless a browser session needs extending.
func (r *resolver) refresh(c echo.Context, row *userWithSession, setCookie bool) {
	now := time.Now()
	touch := row.SessionLastUsed == nil || now.Sub(*row.SessionLastUsed) >= sessionTouchInterval
	expiry := row.SessionExpiresAt
	if row.SessionClientName != nil {
		// Device sessions have no absolute cap: 30 days of inactivity ends them.
		if touch {
			expiry = now.Add(sessionDurationDays * 24 * time.Hour)
		}
	} else if expiry.Sub(now) < sessionRefreshThresholdDays*24*time.Hour {
		expiry = now.Add(sessionDurationDays * 24 * time.Hour)
		if row.SessionAbsolute != nil && expiry.After(*row.SessionAbsolute) {
			expiry = *row.SessionAbsolute
		}
	}
	extend := expiry.After(row.SessionExpiresAt)
	if !touch && !extend {
		return
	}
	_, _ = r.pool.Exec(reqCtx(c),
		"UPDATE sessions SET expires_at = GREATEST(expires_at, $1), last_used_at = $2 WHERE token = $3",
		expiry, now, row.SessionToken)
	if extend && setCookie {
		setSessionCookie(c, r.st, row.SessionToken)
	}
}

var bearerPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// bearerToken accepts only the shape randomToken produces, so a proxy's own
// Authorization header (Basic, a JWT) leaves the cookie in charge. It checks
// every Authorization header, as a proxy's bypass matcher does: reading only
// the first would put a request that bypassed the proxy on the cookie path.
func bearerToken(c echo.Context) (string, bool) {
	for _, value := range c.Request().Header.Values(echo.HeaderAuthorization) {
		scheme, token, ok := strings.Cut(value, " ")
		if ok && strings.EqualFold(scheme, "Bearer") && bearerPattern.MatchString(token) {
			return token, true
		}
	}
	return "", false
}

func cookieToken(c echo.Context) string {
	cookie, err := c.Cookie("voltis_session")
	if err != nil {
		return ""
	}
	return cookie.Value
}

func sessionByToken(ctx context.Context, q db.Querier, token string) (*userWithSession, error) {
	row, err := db.SelectOne[userWithSession](ctx, q, `
		SELECT u.*, s.token AS session_token, s.expires_at AS session_expires_at,
		       s.method AS session_method, s.absolute_expires_at AS session_absolute_expires_at,
		       s.client_name AS session_client_name, s.last_used_at AS session_last_used_at
		FROM users u
		JOIN sessions s ON s.user_id = u.id
		WHERE s.token = $1 AND s.expires_at > NOW()
		  AND (s.absolute_expires_at IS NULL OR s.absolute_expires_at > NOW())
	`, token)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func requireUser(c echo.Context) (*models.User, error) {
	user, _ := c.Get(contextKeyUser).(*models.User)
	if user == nil {
		return nil, echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}
	return user, nil
}

func requireSession(c echo.Context) (*models.User, *sessionInfo, error) {
	user, err := requireUser(c)
	if err != nil {
		return nil, nil, err
	}
	session := requestSession(c)
	if session == nil {
		return nil, nil, echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}
	return user, session, nil
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
		case reflect.Slice:
			if fv.Type().Elem().Kind() == reflect.String {
				vals := c.QueryParams()[name]
				if len(vals) > 0 {
					fv.Set(reflect.ValueOf(vals))
				}
			}
			continue
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
		if ve, ok := errors.AsType[validator.ValidationErrors](err); ok {
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
