package routes

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/labstack/echo/v4"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

const (
	accountPath = "/settings/account"
	oidcFailed  = "single sign-on failed"
)

// msg goes to the browser; err is for the log and never reaches a URL.
type oidcRejection struct {
	msg string
	err error
}

func (r oidcRejection) Error() string {
	if r.err == nil {
		return r.msg
	}
	return r.msg + ": " + r.err.Error()
}

// safeRedirect returns s when it is a same-origin app path outside /auth, else "".
func safeRedirect(s string) string {
	if len(s) > 2048 || !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") {
		return ""
	}
	// Browsers strip tabs and newlines and treat `\` as `/`, so "/\t/x" would become "//x".
	if strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return ""
	}
	u, err := url.Parse(s)
	// The frontend router matches case-insensitively.
	if p := strings.ToLower(u.Path); err != nil || u.Scheme != "" || u.Host != "" || p == "/auth" || strings.HasPrefix(p, "/auth/") {
		return ""
	}
	// Browsers resolve dot segments, so "/..//x" would become "//x".
	for seg := range strings.SplitSeq(u.Path, "/") {
		if seg == "." || seg == ".." {
			return ""
		}
	}
	return s
}

func reject(msg string, err error) error { return oidcRejection{msg: msg, err: err} }

func appMessage(err error) string {
	var rejection oidcRejection
	if errors.As(err, &rejection) {
		return rejection.msg
	}
	if he, ok := errors.AsType[*echo.HTTPError](err); ok {
		if msg, ok := he.Message.(string); ok {
			return msg
		}
	}
	return oidcFailed
}

type OIDCRoutes struct {
	res  *resolver
	oidc *oidcClient
}

func (o *OIDCRoutes) Register(g *echo.Group) {
	g.GET("/login", o.login)
	g.POST("/link", o.link)
	g.GET("/callback", o.callback)
	g.GET("/pending", o.pending)
	g.POST("/confirm", o.confirm)
	g.POST("/choose-username", o.chooseUsername)
}

func (o *OIDCRoutes) login(c echo.Context) error {
	target, err := o.start(c, false, nil, nil, safeRedirect(c.QueryParam("redirect")))
	if err != nil {
		return o.fail(c, false, err)
	}
	return c.Redirect(http.StatusFound, target)
}

func (o *OIDCRoutes) link(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	user, err := requireUser(c)
	if err != nil {
		return err
	}
	session := requestSession(c)
	if session == nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "not authenticated")
	}

	target, err := o.start(c, true, &user.ID, &session.Token, "")
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]string{"url": target})
}

func (o *OIDCRoutes) start(c echo.Context, isLink bool, userID, sessionToken *string, redirect string) (string, error) {
	ctx := reqCtx(c)
	conf, _, _, err := o.oidc.oauth(ctx)
	if err != nil {
		return "", err
	}

	flow := oidcFlow{
		State:    randomToken(),
		Nonce:    randomToken(),
		Verifier: oauth2.GenerateVerifier(),
		Link:     isLink,
		Redirect: redirect,
	}
	raw, err := insertPending(ctx, o.res.pool, pendingFlow, flow, userID, sessionToken)
	if err != nil {
		return "", err
	}
	setPendingCookie(c, o.res.st, raw)

	return conf.AuthCodeURL(flow.State, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier)), nil
}

func (o *OIDCRoutes) callback(c echo.Context) error {
	ctx := reqCtx(c)
	raw := pendingID(c)
	clearPendingCookie(c, o.res.st)

	var row *models.AuthPending
	if raw != "" {
		var err error
		if row, err = consumePending(ctx, o.res.pool, raw, pendingFlow); err != nil {
			return err
		}
	}
	if row == nil {
		return o.fail(c, false, reject("the sign-in request expired, try again", nil))
	}
	flow, err := pendingData[oidcFlow](row)
	if err != nil {
		return err
	}

	id, issuer, err := o.verify(ctx, c, flow)
	if err != nil {
		return o.fail(c, flow.Link, err)
	}
	if flow.Link {
		return o.finishLink(c, row, id)
	}
	return o.finishLogin(c, id, issuer, flow.Redirect)
}

func (o *OIDCRoutes) verify(ctx context.Context, c echo.Context, flow oidcFlow) (ExternalIdentity, string, error) {
	if denied := c.QueryParam("error"); denied != "" {
		return ExternalIdentity{}, "", reject("the identity provider refused the sign-in", errors.New(denied))
	}
	if c.QueryParam("state") != flow.State {
		return ExternalIdentity{}, "", reject("the sign-in state did not match", nil)
	}
	code := c.QueryParam("code")
	if code == "" {
		return ExternalIdentity{}, "", reject("the identity provider returned no authorization code", nil)
	}

	conf, verifier, provider, err := o.oidc.oauth(ctx)
	if err != nil {
		return ExternalIdentity{}, "", err
	}

	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return ExternalIdentity{}, "", reject("the authorization code was rejected", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok {
		return ExternalIdentity{}, "", reject("the identity provider returned no ID token", nil)
	}
	idToken, err := verifier.Verify(ctx, rawID)
	if err != nil {
		return ExternalIdentity{}, "", reject("the ID token was rejected", err)
	}

	var claims map[string]any
	if err := idToken.Claims(&claims); err != nil {
		return ExternalIdentity{}, "", reject(oidcFailed, err)
	}
	if err := checkAudience(idToken, claims, conf.ClientID); err != nil {
		return ExternalIdentity{}, "", err
	}
	if idToken.Nonce != flow.Nonce {
		return ExternalIdentity{}, "", reject("the ID token nonce did not match", nil)
	}
	if strings.TrimSpace(idToken.Subject) == "" {
		return ExternalIdentity{}, "", reject("the identity provider did not identify the user", nil)
	}

	id, err := o.identity(ctx, provider, token, idToken, claims)
	return id, idToken.Issuer, err
}

// The verifier checks membership only: a token for another client can list ours.
func checkAudience(idToken *oidc.IDToken, claims map[string]any, clientID string) error {
	if len(idToken.Audience) != 1 || idToken.Audience[0] != clientID {
		return reject("the ID token had an unexpected audience", nil)
	}
	if azp, ok := claims["azp"].(string); ok && azp != clientID {
		return reject("the ID token had an unexpected authorized party", nil)
	}
	return nil
}

func (o *OIDCRoutes) identity(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, idToken *oidc.IDToken, claims map[string]any) (ExternalIdentity, error) {
	usernameClaim := o.res.st.String(settings.OIDCUsernameClaim)
	groupsClaim := o.res.st.String(settings.OIDCGroupsClaim)

	id := ExternalIdentity{Provider: models.SessionOIDC, Issuer: idToken.Issuer, Subject: idToken.Subject}
	id.Username, _ = claims[usernameClaim].(string)
	email, hasEmail := claims["email"].(string)
	verified := claimBool(claims["email_verified"])
	groups, hasGroups := parseGroups(claims[groupsClaim])

	if id.Username == "" || !hasEmail || !hasGroups {
		info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		switch {
		case err != nil:
			// Without groups from either source there is no way to tell a
			// demotion from an outage, so refuse rather than guess.
			if !hasGroups && o.res.st.String(settings.AuthAdminGroup) != "" {
				return ExternalIdentity{}, reject("group membership could not be checked", err)
			}
			slog.Warn("[oidc] the userinfo fallback failed", "err", err)
		case info.Subject != idToken.Subject:
			return ExternalIdentity{}, reject("the userinfo response belongs to a different subject", nil)
		default:
			var extra map[string]any
			if err := info.Claims(&extra); err != nil {
				return ExternalIdentity{}, reject(oidcFailed, err)
			}
			if id.Username == "" {
				id.Username, _ = extra[usernameClaim].(string)
			}
			if !hasEmail {
				// Both always come from the same source.
				email, hasEmail = extra["email"].(string)
				verified = claimBool(extra["email_verified"])
			}
			if !hasGroups {
				groups, hasGroups = parseGroups(extra[groupsClaim])
			}
		}
	}

	id.Username = strings.TrimSpace(id.Username)
	if hasEmail {
		id.Email, id.EmailVerified = email, verified
	}
	id.Groups, id.HasGroups = groups, hasGroups
	return id, nil
}

func (o *OIDCRoutes) finishLink(c echo.Context, row *models.AuthPending, id ExternalIdentity) error {
	changed := reject("your session changed, sign in again and retry", nil)
	if row.UserID == nil || row.SessionToken == nil {
		return o.fail(c, true, changed)
	}

	// The browser holding the callback must still be the one that asked.
	user, _ := c.Get(contextKeyUser).(*models.User)
	session := requestSession(c)
	if user == nil || session == nil || user.ID != *row.UserID || session.Token != *row.SessionToken {
		return o.fail(c, true, changed)
	}

	ctx := reqCtx(c)
	err := db.WithTx(ctx, o.res.pool, func(tx pgx.Tx) error {
		return linkIdentityTx(ctx, tx, id, *row.UserID, *row.SessionToken)
	})
	if err != nil {
		return o.fail(c, true, err)
	}
	return c.Redirect(http.StatusFound, accountPath)
}

func (o *OIDCRoutes) finishLogin(c echo.Context, id ExternalIdentity, issuer, redirect string) error {
	ctx := reqCtx(c)
	login, err := o.res.resolveExternalLogin(ctx, id)
	if err != nil {
		return o.fail(c, false, err)
	}
	if login.User != nil {
		var token string
		err := db.WithTx(ctx, o.res.pool, func(tx pgx.Tx) error {
			// Re-check under the identity lock: an unlink may have revoked the
			// account's sessions since it was resolved.
			if err := db.LockIdentity(ctx, tx, id.Provider, id.Issuer, id.Subject); err != nil {
				return err
			}
			owner, err := identityUser(ctx, tx, id)
			if err != nil {
				return err
			}
			if owner == nil || owner.ID != login.User.ID {
				return reject("the account changed during sign-in, try again", nil)
			}
			token, err = createSession(ctx, tx, o.res.st, login.User.ID, models.SessionOIDC)
			return err
		})
		if err != nil {
			return o.fail(c, false, err)
		}
		if err := o.finishSession(c, login.User, id, token); err != nil {
			return err
		}
		return c.Redirect(http.StatusFound, cmp.Or(redirect, "/"))
	}

	complete := oidcComplete{
		Needs: login.Needs, Issuer: issuer, Subject: id.Subject, Username: id.Username,
		Email: id.Email, EmailVerified: id.EmailVerified, Groups: id.Groups, HasGroups: id.HasGroups,
		Redirect: redirect,
	}
	if login.Match != nil {
		complete.MatchID, complete.MatchUsername = login.Match.ID, login.Match.Username
	}
	raw, err := insertPending(ctx, o.res.pool, pendingComplete, complete, nil, nil)
	if err != nil {
		return err
	}
	setPendingCookie(c, o.res.st, raw)
	return c.Redirect(http.StatusFound, "/auth/oidc/complete")
}

func (o *OIDCRoutes) pending(c echo.Context) error {
	row, _, err := o.completeRow(c)
	if err != nil {
		return err
	}
	data, err := pendingData[oidcComplete](row)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, map[string]any{
		"needs":          data.Needs,
		"username":       data.Username,
		"email":          data.Email,
		"match_username": data.MatchUsername,
		"redirect":       data.Redirect,
	})
}

func (o *OIDCRoutes) confirm(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}

	row, raw, err := o.completeRow(c)
	if err != nil {
		return err
	}
	data, err := pendingData[oidcComplete](row)
	if err != nil {
		return err
	}
	if data.MatchID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "there is no account to confirm")
	}

	ctx := reqCtx(c)
	match, err := getUser(ctx, o.res.pool, data.MatchID)
	if err != nil {
		return err
	}
	if match.PasswordHash == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that account has no password")
	}
	if bcrypt.CompareHashAndPassword([]byte(*match.PasswordHash), []byte(req.Password)) != nil {
		// Keep the pending row: a typo must not end the sign-in.
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid password")
	}

	id := data.identity()
	var token string
	err = db.WithTx(ctx, o.res.pool, func(tx pgx.Tx) error {
		if err := o.recheck(ctx, tx, data.Issuer, settings.AuthLinkMatchUsername); err != nil {
			return err
		}
		if err := consumeComplete(ctx, tx, raw); err != nil {
			return err
		}
		if err := linkIdentityTx(ctx, tx, id, match.ID, ""); err != nil {
			return err
		}
		token, err = createSession(ctx, tx, o.res.st, match.ID, models.SessionOIDC)
		return err
	})
	if err != nil {
		return err
	}

	clearPendingCookie(c, o.res.st)
	if err := o.finishSession(c, &match, id, token); err != nil {
		return err
	}
	return okResponse(c)
}

func (o *OIDCRoutes) chooseUsername(c echo.Context) error {
	if err := requireJSON(c); err != nil {
		return err
	}
	var req struct {
		Username string `json:"username"`
	}
	if err := c.Bind(&req); err != nil {
		return err
	}

	row, raw, err := o.completeRow(c)
	if err != nil {
		return err
	}
	data, err := pendingData[oidcComplete](row)
	if err != nil {
		return err
	}
	name := strings.TrimSpace(req.Username)
	if len(name) < 2 {
		return echo.NewHTTPError(http.StatusBadRequest, "username must be at least 2 characters")
	}

	ctx := reqCtx(c)
	id := data.identity()
	var user *models.User
	var token string
	// One transaction: a rejected name rolls the pending row back, expiry included.
	err = db.WithTx(ctx, o.res.pool, func(tx pgx.Tx) error {
		if err := o.recheck(ctx, tx, data.Issuer, settings.AuthExternalAutoCreate); err != nil {
			return err
		}
		if err := consumeComplete(ctx, tx, raw); err != nil {
			return err
		}
		if user, err = createNamed(ctx, tx, id, name); err != nil {
			return err
		}
		token, err = createSession(ctx, tx, o.res.st, user.ID, models.SessionOIDC)
		return err
	})
	if isDuplicate(err) {
		return echo.NewHTTPError(http.StatusBadRequest, "that username is already taken")
	}
	if err != nil {
		return err
	}

	clearPendingCookie(c, o.res.st)
	if err := o.finishSession(c, user, id, token); err != nil {
		return err
	}
	return okResponse(c)
}

func createNamed(ctx context.Context, tx pgx.Tx, id ExternalIdentity, name string) (*models.User, error) {
	if err := db.LockIdentity(ctx, tx, id.Provider, id.Issuer, id.Subject); err != nil {
		return nil, err
	}
	existing, err := identityUser(ctx, tx, id)
	if err != nil || existing != nil {
		return existing, err
	}
	taken, err := userByName(ctx, tx, name)
	if err != nil {
		return nil, err
	}
	if taken != nil {
		return nil, echo.NewHTTPError(http.StatusBadRequest, "that username is already taken")
	}
	out, err := create(ctx, tx, id, name)
	return out.User, err
}

func consumeComplete(ctx context.Context, tx pgx.Tx, raw string) error {
	row, err := consumePending(ctx, tx, raw, pendingComplete)
	if err != nil {
		return err
	}
	if row == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the sign-in request expired, try again")
	}
	return nil
}

// Runs after the commit: the syncs open their own transactions.
func (o *OIDCRoutes) finishSession(c echo.Context, user *models.User, id ExternalIdentity, token string) error {
	ctx := reqCtx(c)
	syncEmail(ctx, o.res.pool, user, id.Email)
	syncIdentityEmail(ctx, o.res.pool, id)
	if err := o.res.syncAdmin(ctx, user, id); err != nil {
		return err
	}
	setSessionCookie(c, o.res.st, token)
	return nil
}

func (o *OIDCRoutes) completeRow(c echo.Context) (*models.AuthPending, string, error) {
	raw := pendingID(c)
	if raw == "" {
		return nil, "", echo.NewHTTPError(http.StatusNotFound, "there is no sign-in waiting")
	}
	row, err := readPending(reqCtx(c), o.res.pool, raw, pendingComplete)
	if err != nil {
		return nil, "", err
	}
	if row == nil {
		return nil, "", echo.NewHTTPError(http.StatusNotFound, "there is no sign-in waiting")
	}
	return row, raw, nil
}

// Reads the policy in the completing transaction: settings may have moved.
func (o *OIDCRoutes) recheck(ctx context.Context, tx pgx.Tx, issuer, key string) error {
	if err := settings.LockVersion(ctx, tx); err != nil {
		return err
	}
	for _, name := range []string{settings.OIDCEnabled, key} {
		value, err := settings.Read(ctx, tx, name)
		if err != nil {
			return err
		}
		if value != true {
			return echo.NewHTTPError(http.StatusForbidden, "this sign-in option is no longer available")
		}
	}
	current, err := settings.Read(ctx, tx, settings.OIDCIssuer)
	if err != nil {
		return err
	}
	if current != issuer {
		return echo.NewHTTPError(http.StatusForbidden, "the single sign-on configuration changed, start again")
	}
	return nil
}

func (o *OIDCRoutes) fail(c echo.Context, link bool, err error) error {
	slog.Warn("[oidc] sign-in failed", "err", err, "link", link)
	target := "/auth/login"
	if link {
		target = accountPath
	}
	return c.Redirect(http.StatusFound, target+"?error="+url.QueryEscape(appMessage(err)))
}

func linkIdentityTx(ctx context.Context, tx pgx.Tx, id ExternalIdentity, userID, sessionToken string) error {
	if err := db.LockIdentity(ctx, tx, id.Provider, id.Issuer, id.Subject); err != nil {
		return err
	}
	if sessionToken != "" {
		// Revalidate under the lock: a logout must not race the insert.
		owner, err := db.SelectScalar[string](ctx, tx, `
			SELECT user_id FROM sessions
			WHERE token = $1 AND expires_at > NOW()
			  AND (absolute_expires_at IS NULL OR absolute_expires_at > NOW())
		`, sessionToken)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != userID) {
			return reject("your session changed, sign in again and retry", nil)
		}
		if err != nil {
			return err
		}
	}

	owner, err := identityUser(ctx, tx, id)
	if err != nil {
		return err
	}
	if owner != nil {
		if owner.ID != userID {
			return echo.NewHTTPError(http.StatusConflict, "that identity is already linked to another account")
		}
		return nil
	}
	_, err = link(ctx, tx, id, &models.User{ID: userID})
	return err
}

func claimBool(v any) bool {
	switch b := v.(type) {
	case bool:
		return b
	case string:
		return b == "true"
	}
	return false
}
