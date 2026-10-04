package routes

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

type AuthRoutes struct {
	pool         *pgxpool.Pool
	hub          *WebSocketHub
	st           *settings.Store
	proxyEnabled bool
}

func (a *AuthRoutes) Register(g *echo.Group) {
	g.POST("/login", a.login)
	g.POST("/register", a.register)
	g.POST("/logout", a.logout)
	g.POST("/token", a.token)
	g.POST("/app-code", a.appCode)
	g.POST("/token/exchange", a.exchange)
}

func (a *AuthRoutes) login(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !a.st.Bool(settings.AuthPasswordLoginEnabled) {
		return errPasswordLoginDisabled()
	}

	user, err := a.checkPassword(reqCtx(c), req.Username, req.Password)
	if err != nil {
		return err
	}
	token, err := a.passwordSession(reqCtx(c), user.ID, nil)
	if err != nil {
		return err
	}
	return a.finishSession(c, token)
}

// token signs a native client in with a password and returns a device session
// token. It never sets cookies.
func (a *AuthRoutes) token(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		ClientName string `json:"client_name"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	clientName, err := validClientName(req.ClientName)
	if err != nil {
		return err
	}
	if !a.st.Bool(settings.AuthPasswordLoginEnabled) {
		return errPasswordLoginDisabled()
	}

	ctx := reqCtx(c)
	user, err := a.checkPassword(ctx, req.Username, req.Password)
	if err != nil {
		return err
	}
	token, err := a.passwordSession(ctx, user.ID, &clientName)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"token": token})
}

// appCode mints a one-time code that hands the calling web session's user to a
// native client, bound to the client's PKCE challenge.
func (a *AuthRoutes) appCode(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	user, session, err := requireSession(c)
	if err != nil {
		return err
	}
	var req struct {
		CodeChallenge string `json:"code_challenge"`
		ClientName    string `json:"client_name"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if !challengePattern.MatchString(req.CodeChallenge) {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid code_challenge")
	}
	clientName, err := validClientName(req.ClientName)
	if err != nil {
		return err
	}

	code := nativeCode{Challenge: req.CodeChallenge, ClientName: clientName, Method: session.Method}
	raw, err := insertPending(reqCtx(c), a.pool, pendingNativeCode, code, &user.ID, &session.Token, nativeCodeTTL)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{
		"redirect_url": nativeCallbackURI + "?code=" + raw, // 64 hex, nothing to escape
	})
}

var (
	// BASE64URL-NOPAD(SHA256(verifier)) and the verifier itself, RFC 7636.
	challengePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	verifierPattern  = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
)

// exchange trades a code from appCode and its PKCE verifier for a device
// session. The code is spent by any attempt, so a wrong verifier can't be retried.
func (a *AuthRoutes) exchange(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	var req struct {
		Code         string `json:"code"`
		CodeVerifier string `json:"code_verifier"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	invalid := echo.NewHTTPError(http.StatusBadRequest, "the sign-in code is invalid or expired")
	if !verifierPattern.MatchString(req.CodeVerifier) {
		return invalid
	}

	ctx := reqCtx(c)
	var token string
	err := db.WithTx(ctx, a.pool, func(tx pgx.Tx) error {
		// Orders this against the setting transitions, which delete sessions.
		if err := settings.LockVersion(ctx, tx); err != nil {
			return err
		}
		p, err := readPending(ctx, tx, req.Code, pendingNativeCode)
		if err != nil || p == nil {
			return err
		}
		code, err := pendingData[nativeCode](p)
		if err != nil {
			return err
		}
		// Lock order user → session → pending row, as unlink (user, then its
		// rows); the session INSERT's FK check needs the user's KEY SHARE anyway.
		err = tx.QueryRow(ctx, "SELECT 1 FROM users WHERE id = $1 FOR KEY SHARE", *p.UserID).Scan(new(int))
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// The minting session must still be live: every path that ends web
		// sessions thereby voids their codes. KEY SHARE makes a racing DELETE
		// wait for this commit; clock_timestamp because the locks may have waited.
		live, err := db.SelectScalar[bool](ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM sessions WHERE token = $1 AND user_id = $2
				AND expires_at > clock_timestamp()
				AND (absolute_expires_at IS NULL OR absolute_expires_at > clock_timestamp())
				FOR KEY SHARE)
		`, *p.SessionToken, *p.UserID)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, "DELETE FROM auth_pending WHERE id = $1 AND expires_at > clock_timestamp()", p.ID)
		if err != nil {
			return err
		}
		challenge := oauth2.S256ChallengeFromVerifier(req.CodeVerifier)
		ok := tag.RowsAffected() == 1 && live && (code.Method != models.SessionProxy || a.proxyEnabled) &&
			subtle.ConstantTimeCompare([]byte(challenge), []byte(code.Challenge)) == 1
		if !ok {
			return nil
		}
		token, err = addSession(ctx, tx, *p.UserID, code.Method, nil, &code.ClientName)
		return err
	})
	if err != nil {
		return err
	}
	// Failures still commit, so the DELETE above spends the code.
	if token == "" {
		return invalid
	}
	return c.JSON(http.StatusOK, map[string]string{"token": token})
}

func (a *AuthRoutes) checkPassword(ctx context.Context, username, password string) (models.User, error) {
	user, err := db.SelectOne[models.User](ctx, a.pool, "SELECT * FROM users WHERE username = $1", username)
	if errors.Is(err, pgx.ErrNoRows) {
		return models.User{}, errInvalidCredentials()
	}
	if err != nil {
		return models.User{}, err
	}
	if user.PasswordHash == nil {
		return models.User{}, errInvalidCredentials()
	}
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PasswordHash), []byte(password)); err != nil {
		return models.User{}, errInvalidCredentials()
	}
	return user, nil
}

func validClientName(s string) (string, error) {
	s = strings.TrimSpace(s)
	n := utf8.RuneCountInString(s)
	if n < 1 || n > 64 || strings.ContainsFunc(s, unicode.IsControl) {
		return "", echo.NewHTTPError(http.StatusBadRequest, "client_name must be 1 to 64 characters")
	}
	return s, nil
}

func (a *AuthRoutes) register(c echo.Context) error {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}
	if len(req.Username) < 2 {
		return echo.NewHTTPError(http.StatusBadRequest, "username must be at least 2 characters")
	}
	if len(req.Password) < 8 {
		return echo.NewHTTPError(http.StatusBadRequest, "password must be at least 8 characters")
	}

	ctx := reqCtx(c)
	firstUser, err := isFirstUserFlow(ctx, a.pool, a.st)
	if err != nil {
		return err
	}
	if err := registerAllowed(ctx, a.pool, firstUser); err != nil {
		return err
	}

	hash, err := hashPassword(req.Password)
	if err != nil {
		return err
	}

	userID := models.MakeUserID()
	var token string
	err = db.WithTx(ctx, a.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}
		if err := settings.LockVersionWrite(ctx, tx); err != nil {
			return err
		}

		first, err := isFirstUserFlow(ctx, tx, a.st)
		if err != nil {
			return err
		}
		if err := registerAllowed(ctx, tx, first); err != nil {
			return err
		}

		permissions := []string{}
		if first {
			permissions = []string{"ADMIN"}
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO users (id, username, password_hash, permissions) VALUES ($1, $2, $3, $4)`,
			userID, req.Username, hash, permissions,
		); err != nil {
			return err
		}
		if first {
			if err := settings.WriteTx(ctx, tx, settings.BootstrapCompleted, true); err != nil {
				return err
			}
		}
		token, err = createSession(ctx, tx, a.st, userID, models.SessionPassword)
		return err
	})
	if err != nil {
		if db.IsDuplicate(err) {
			return echo.NewHTTPError(http.StatusBadRequest, "username already exists")
		}
		return err
	}

	return a.finishSession(c, token)
}

// The bootstrap admin may register and get a session even with password login
// off: it is the documented recovery path.
func registerAllowed(ctx context.Context, q db.Querier, firstUser bool) error {
	if firstUser {
		return nil
	}
	if err := requirePasswordLoginIn(ctx, q); err != nil {
		return err
	}
	enabled, err := settings.Read(ctx, q, settings.AuthRegistrationEnabled)
	if err != nil {
		return err
	}
	if enabled != true {
		return echo.NewHTTPError(http.StatusForbidden, "registration is disabled")
	}
	return nil
}

func errInvalidCredentials() error {
	return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
}

func errPasswordLoginDisabled() error {
	return echo.NewHTTPError(http.StatusForbidden, "password login is disabled")
}

// requirePasswordLogin holds off any concurrent disable until the caller's
// transaction, which must not write settings itself, has committed.
func requirePasswordLogin(ctx context.Context, tx pgx.Tx) error {
	if err := settings.LockVersion(ctx, tx); err != nil {
		return err
	}
	return requirePasswordLoginIn(ctx, tx)
}

func requirePasswordLoginIn(ctx context.Context, q db.Querier) error {
	enabled, err := settings.Read(ctx, q, settings.AuthPasswordLoginEnabled)
	if err != nil {
		return err
	}
	if enabled != true {
		return errPasswordLoginDisabled()
	}
	return nil
}

// passwordSession starts a browser session, or a device session when clientName is set.
func (a *AuthRoutes) passwordSession(ctx context.Context, userID string, clientName *string) (token string, err error) {
	err = db.WithTx(ctx, a.pool, func(tx pgx.Tx) error {
		if err := requirePasswordLogin(ctx, tx); err != nil {
			return err
		}
		token, err = addSession(ctx, tx, userID, models.SessionPassword, nil, clientName)
		return err
	})
	return token, err
}

func (a *AuthRoutes) finishSession(c echo.Context, token string) error {
	setSessionCookie(c, a.st, token)
	return okResponse(c)
}

func createSession(ctx context.Context, q db.Querier, st *settings.Store, userID, method string) (string, error) {
	var absolute *time.Time
	if method != models.SessionPassword {
		t := time.Now().Add(time.Duration(st.Int(settings.AuthExternalSessionMaxDays)) * 24 * time.Hour)
		absolute = &t
	}
	return addSession(ctx, q, userID, method, absolute, nil)
}

// Device sessions (clientName set) slide forever (no absolute expiry), whatever the login method.
func addSession(ctx context.Context, q db.Querier, userID, method string, absolute *time.Time, clientName *string) (string, error) {
	token := randomToken()
	_, err := q.Exec(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, method, absolute_expires_at, client_name, last_used_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW())
	`, token, userID, time.Now().Add(sessionDurationDays*24*time.Hour), method, absolute, clientName)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (a *AuthRoutes) logout(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	// The resolver may have replaced the incoming cookie on this very request.
	session := requestSession(c)
	if session == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	userID, err := db.SelectScalar[string](reqCtx(c), a.pool,
		"DELETE FROM sessions WHERE token = $1 RETURNING user_id", session.Token)
	redirect := ""
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return err
	default:
		a.hub.Drop(userID)
		if session.Method == models.SessionProxy {
			redirect = a.st.String(settings.AuthProxyLogoutURL)
		}
	}

	clearSessionCookie(c, a.st)
	return c.JSON(http.StatusOK, map[string]any{"ok": true, "redirect_url": redirect})
}

func isFirstUserFlow(ctx context.Context, q db.Querier, st *settings.Store) (bool, error) {
	if st.Bool(settings.BootstrapCompleted) {
		return false, nil
	}
	v, err := settings.Read(ctx, q, settings.BootstrapCompleted)
	if err != nil {
		return false, err
	}
	done, _ := v.(bool)
	return !done, nil
}

// lockUser takes the row lock that serializes changes to an account's login
// methods. It comes after any identity lock.
func lockUser(ctx context.Context, tx pgx.Tx, userID string) error {
	_, err := db.SelectScalar[string](ctx, tx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return echo.NewHTTPError(http.StatusNotFound, "User not found")
	}
	return err
}

// Call after the change, with settings.LockVersion and lockUser held. The check
// is a later statement than the lock, so it sees what a racing unlink committed.
// Only methods that can log in today count: an identity for a disabled provider,
// an incomplete OIDC configuration or an old issuer does not.
func requireLoginMethod(ctx context.Context, tx pgx.Tx, userID string, proxyEnabled bool) error {
	vals, err := readSettings(ctx, tx, settings.AuthPasswordLoginEnabled,
		settings.OIDCEnabled, settings.OIDCIssuer, settings.OIDCClientID, settings.AppPublicURL)
	if err != nil {
		return err
	}
	str := func(key string) string { v, _ := vals[key].(string); return v }
	oidcCfg := oidcConfig{
		enabled: vals[settings.OIDCEnabled] == true, issuer: str(settings.OIDCIssuer),
		clientID: str(settings.OIDCClientID), publicURL: str(settings.AppPublicURL),
	}

	ok, err := db.SelectScalar[bool](ctx, tx, `
		SELECT ($2 AND password_hash IS NOT NULL)
		    OR EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1 AND (
		         (provider = 'oidc' AND $3 AND issuer = $4) OR (provider = 'proxy' AND $5)))
		FROM users WHERE id = $1
	`, userID, vals[settings.AuthPasswordLoginEnabled] == true, oidcCfg.usable(), oidcCfg.issuer, proxyEnabled)
	if err != nil {
		return err
	}
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "this would leave the account with no way to log in")
	}
	return nil
}
