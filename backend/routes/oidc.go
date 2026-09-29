package routes

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
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

var (
	errSessionChanged  = reject("your session changed, sign in again and retry", nil)
	errNoSignInWaiting = echo.NewHTTPError(http.StatusNotFound, "there is no sign-in waiting")
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
	if err != nil {
		return ""
	}
	// The frontend router matches case-insensitively.
	if p := strings.ToLower(u.Path); u.Scheme != "" || u.Host != "" || p == "/auth" || strings.HasPrefix(p, "/auth/") {
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

	row, err := consumeFlow(ctx, o.res.pool, raw, c.QueryParam("state"))
	if err != nil {
		return o.fail(c, false, err)
	}
	if row == nil {
		// A live flow with another state stays, cookie included.
		waiting, err := readPending(ctx, o.res.pool, raw, pendingFlow)
		if err != nil {
			return o.fail(c, false, err)
		}
		if waiting != nil {
			flow, _ := pendingData[oidcFlow](waiting)
			return o.fail(c, flow.Link, reject("the sign-in state did not match", nil))
		}
		clearPendingCookie(c, o.res.st)
		return o.fail(c, false, reject("the sign-in request expired, try again", nil))
	}
	clearPendingCookie(c, o.res.st)
	flow, err := pendingData[oidcFlow](row)
	if err != nil {
		return o.fail(c, false, err)
	}

	id, clientID, err := o.verify(ctx, c, flow)
	if err != nil {
		return o.fail(c, flow.Link, err)
	}
	if flow.Link {
		return o.finishLink(c, row, id, clientID)
	}
	return o.finishLogin(c, id, clientID, flow.Redirect)
}

// verify returns the identity and the client ID its token was verified for.
func (o *OIDCRoutes) verify(ctx context.Context, c echo.Context, flow oidcFlow) (ExternalIdentity, string, error) {
	if denied := c.QueryParam("error"); denied != "" {
		return ExternalIdentity{}, "", reject("the identity provider refused the sign-in", errors.New(denied))
	}
	code := c.QueryParam("code")
	if code == "" {
		return ExternalIdentity{}, "", reject("the identity provider returned no authorization code", nil)
	}

	conf, verifier, provider, err := o.oidc.oauth(ctx)
	if err != nil {
		return ExternalIdentity{}, "", err
	}
	ctx = oidc.ClientContext(ctx, o.oidc.http)

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
	return id, conf.ClientID, err
}

// The verifier checks membership only: a token issued to another client can
// list ours among several audiences, so those must name us as the azp.
func checkAudience(idToken *oidc.IDToken, claims map[string]any, clientID string) error {
	if !slices.Contains(idToken.Audience, clientID) {
		return reject("the ID token had an unexpected audience", nil)
	}
	// A present azp of any other type or value is rejected.
	azp, hasAZP := claims["azp"]
	if (hasAZP || len(idToken.Audience) > 1) && azp != any(clientID) {
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

	// Missing groups only mean "none" when userinfo answered or does not exist.
	groupsKnown := true
	if (id.Username == "" || !hasEmail || !hasGroups) && provider.UserInfoEndpoint() != "" {
		info, err := provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		switch {
		case err != nil:
			// Without groups from either source there is no way to tell a
			// demotion from an outage, so refuse rather than guess.
			if !hasGroups && o.res.st.String(settings.AuthAdminGroup) != "" {
				return ExternalIdentity{}, reject("group membership could not be checked", err)
			}
			slog.Warn("[oidc] the userinfo fallback failed", "err", err)
			groupsKnown = false
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
	if hasEmail && verified {
		id.Email = email
	}
	if !hasGroups && groupsKnown {
		groups, hasGroups = []string{}, true
	}
	id.Groups, id.HasGroups = groups, hasGroups
	return id, nil
}

func (o *OIDCRoutes) finishLink(c echo.Context, row *models.AuthPending, id ExternalIdentity, clientID string) error {
	if row.UserID == nil || row.SessionToken == nil {
		return o.fail(c, true, errSessionChanged)
	}

	// The browser holding the callback must still be the one that asked.
	user, _ := c.Get(contextKeyUser).(*models.User)
	session := requestSession(c)
	if user == nil || session == nil || user.ID != *row.UserID || session.Token != *row.SessionToken {
		return o.fail(c, true, errSessionChanged)
	}

	_, err := o.finalize(reqCtx(c), finalizeRequest{
		Op: finalizeLink, ID: id, ClientID: clientID, UserID: *row.UserID, SessionToken: *row.SessionToken,
	})
	if err != nil {
		return o.fail(c, true, err)
	}
	return c.Redirect(http.StatusFound, accountPath)
}

func (o *OIDCRoutes) finishLogin(c echo.Context, id ExternalIdentity, clientID, redirect string) error {
	ctx := reqCtx(c)
	out, err := o.finalize(ctx, finalizeRequest{Op: finalizeLogin, ID: id, ClientID: clientID, Redirect: redirect})
	if err != nil {
		return o.fail(c, false, err)
	}
	if out.Completion != "" {
		setPendingCookie(c, o.res.st, out.Completion)
		return c.Redirect(http.StatusFound, "/auth/oidc/complete")
	}
	o.signIn(c, out, id)
	return c.Redirect(http.StatusFound, cmp.Or(redirect, "/"))
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
		"can_create":     o.res.st.Bool(settings.AuthExternalAutoCreate),
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

	ctx := reqCtx(c)
	raw := pendingID(c)
	row, err := reserveAttempt(ctx, o.res.pool, raw)
	if err != nil {
		return err
	}
	if row == nil {
		return errNoSignInWaiting
	}
	data, err := pendingData[oidcComplete](row)
	if err != nil {
		return err
	}
	if data.MatchID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "there is no account to confirm")
	}

	match, err := getUser(ctx, o.res.pool, data.MatchID)
	if err != nil {
		return err
	}
	if match.PasswordHash == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "that account has no password")
	}
	if bcrypt.CompareHashAndPassword([]byte(*match.PasswordHash), []byte(req.Password)) != nil {
		if row.Attempts < maxConfirmAttempts {
			// Keep the pending row: a typo must not end the sign-in.
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid password")
		}
		if _, err := o.res.pool.Exec(ctx, "DELETE FROM auth_pending WHERE id = $1", row.ID); err != nil {
			return err
		}
		clearPendingCookie(c, o.res.st)
		return echo.NewHTTPError(http.StatusUnauthorized, "too many wrong passwords, sign in again")
	}

	id := data.identity()
	out, err := o.finalize(ctx, finalizeRequest{
		Op: finalizeConfirm, ID: id, UserID: match.ID, MatchBy: data.MatchBy, Pending: raw,
	})
	if err != nil {
		return err
	}
	clearPendingCookie(c, o.res.st)
	o.signIn(c, out, id)
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

	id := data.identity()
	out, err := o.finalize(reqCtx(c), finalizeRequest{Op: finalizeCreate, ID: id, Username: name, Pending: raw})
	if db.IsDuplicate(err) {
		return echo.NewHTTPError(http.StatusBadRequest, "that username is already taken")
	}
	if err != nil {
		return err
	}
	clearPendingCookie(c, o.res.st)
	o.signIn(c, out, id)
	return okResponse(c)
}

func consumeComplete(ctx context.Context, tx pgx.Tx, raw string) error {
	// Deletes as it reads: a completion is single use.
	row, err := selectPending(ctx, tx, `
		DELETE FROM auth_pending WHERE id = $1 AND kind = $2 AND expires_at > NOW() RETURNING *
	`, pendingKey(raw), pendingComplete)
	if err != nil {
		return err
	}
	if row == nil {
		return echo.NewHTTPError(http.StatusBadRequest, "the sign-in request expired, try again")
	}
	return nil
}

func (o *OIDCRoutes) completeRow(c echo.Context) (*models.AuthPending, string, error) {
	raw := pendingID(c)
	row, err := readPending(reqCtx(c), o.res.pool, raw, pendingComplete)
	if err != nil {
		return nil, "", err
	}
	if row == nil {
		return nil, "", errNoSignInWaiting
	}
	return row, raw, nil
}

func (o *OIDCRoutes) fail(c echo.Context, link bool, err error) error {
	slog.Warn("[oidc] sign-in failed", "err", err, "link", link)
	target := "/auth/login"
	if link {
		target = accountPath
	}
	return c.Redirect(http.StatusFound, target+"?error="+url.QueryEscape(appMessage(err)))
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

type finalizeOp int

const (
	finalizeLogin   finalizeOp = iota // callback: resolve the identity, then sign in
	finalizeLink                      // callback: attach to the initiating user, no session
	finalizeConfirm                   // /confirm: attach to the password-proven match, sign in
	finalizeCreate                    // /choose-username: create the named account, sign in
)

type finalizeRequest struct {
	Op           finalizeOp
	ID           ExternalIdentity
	ClientID     string // login, link: the client the ID token was verified for
	UserID       string // link: the initiating user; confirm: the matched account
	SessionToken string // link: the initiating session
	Username     string // create
	MatchBy      string // confirm
	Pending      string // confirm, create: the raw completion ID, consumed here
	Redirect     string // login: kept in the completion row
}

type finalized struct {
	User       *models.User
	Token      string // empty for link and for a completion
	Completion string // login: the raw ID of the completion row it inserted
	Demoted    bool   // drop the user's sockets after the commit
}

type oidcPolicy struct {
	linkPolicy
	adminGroup string
}

// finalize is the one transaction that links an OIDC identity or signs it in,
// against the settings as they are now rather than when the flow started.
func (o *OIDCRoutes) finalize(ctx context.Context, req finalizeRequest) (finalized, error) {
	var out finalized
	err := db.WithTx(ctx, o.res.pool, func(tx pgx.Tx) error {
		if err := db.LockAdminMutation(ctx, tx); err != nil {
			return err
		}
		// Write mode: the admin step may close bootstrap, and upgrading from
		// share deadlocks against a waiting writer.
		if err := settings.LockVersionWrite(ctx, tx); err != nil {
			return err
		}
		policy, err := readOIDCPolicy(ctx, tx, req)
		if err != nil {
			return err
		}
		if req.Pending != "" {
			if err := consumeComplete(ctx, tx, req.Pending); err != nil {
				return err
			}
		}
		id := req.ID
		if err := db.LockIdentity(ctx, tx, id.Provider, id.Issuer, id.Subject); err != nil {
			return err
		}

		var user *models.User
		switch req.Op {
		case finalizeLogin:
			login, err := provision(ctx, tx, id, policy.linkPolicy)
			if err != nil {
				return err
			}
			if login.Needs != "" {
				// Under the settings lock, so a config change cannot slip in
				// between and leave a completion for the old client.
				out.Completion, err = insertCompletion(ctx, tx, id, login, req.Redirect)
				return err
			}
			user = login.User
		case finalizeLink:
			// The row lock makes a concurrent logout wait for this commit.
			owner, err := db.SelectScalar[string](ctx, tx,
				"SELECT user_id FROM sessions WHERE token = $1 AND "+liveSession+" FOR KEY SHARE", req.SessionToken)
			if errors.Is(err, pgx.ErrNoRows) || (err == nil && owner != req.UserID) {
				return errSessionChanged
			}
			if err != nil {
				return err
			}
			return attachIdentity(ctx, tx, id, req.UserID)
		case finalizeConfirm:
			if err := attachIdentity(ctx, tx, id, req.UserID); err != nil {
				return err
			}
			match, err := getUser(ctx, tx, req.UserID)
			if err != nil {
				return err
			}
			user = &match
		case finalizeCreate:
			if user, err = identityUser(ctx, tx, id); err != nil {
				return err
			}
			if user == nil {
				// A taken name fails the insert; chooseUsername maps that to 400.
				created, err := create(ctx, tx, id, req.Username)
				if err != nil {
					return err
				}
				user = created.User
			}
		}

		if policy.adminGroup != "" {
			// identity() read the group setting before this transaction did.
			want, ok := adminWanted(policy.adminGroup, id)
			if !ok {
				return echo.NewHTTPError(http.StatusForbidden, "group membership could not be checked")
			}
			if want != slices.Contains(user.Permissions, "ADMIN") {
				changed, err := setAdminTx(ctx, tx, user.ID, want)
				if err != nil {
					return err
				}
				out.Demoted = changed && !want
			}
		}

		out.User = user
		out.Token, err = createSession(ctx, tx, o.res.st, user.ID, models.SessionOIDC)
		return err
	})
	return out, err
}

func insertCompletion(ctx context.Context, tx pgx.Tx, id ExternalIdentity, login externalLogin, redirect string) (string, error) {
	complete := oidcComplete{
		Needs: login.Needs, Issuer: id.Issuer, Subject: id.Subject, Username: id.Username,
		Email: id.Email, Groups: id.Groups, HasGroups: id.HasGroups,
		MatchBy: login.MatchBy, Redirect: redirect,
	}
	if login.Match != nil {
		complete.MatchID, complete.MatchUsername = login.Match.ID, login.Match.Username
	}
	return insertPending(ctx, tx, pendingComplete, complete, nil, nil)
}

func readOIDCPolicy(ctx context.Context, tx pgx.Tx, req finalizeRequest) (oidcPolicy, error) {
	values, err := readSettings(ctx, tx, append([]string{
		settings.OIDCEnabled, settings.OIDCIssuer, settings.OIDCClientID, settings.AuthAdminGroup,
	}, linkPolicyKeys...)...)
	if err != nil {
		return oidcPolicy{}, err
	}
	policy := oidcPolicy{linkPolicy: linkPolicyOf(values)}
	policy.adminGroup, _ = values[settings.AuthAdminGroup].(string)

	if values[settings.OIDCEnabled] != true {
		return policy, echo.NewHTTPError(http.StatusForbidden, "single sign-on is no longer available")
	}
	callback := req.Op == finalizeLogin || req.Op == finalizeLink
	if values[settings.OIDCIssuer] != req.ID.Issuer || (callback && values[settings.OIDCClientID] != req.ClientID) {
		return policy, echo.NewHTTPError(http.StatusForbidden, "the single sign-on configuration changed, start again")
	}
	allowed := true
	switch req.Op {
	case finalizeConfirm:
		allowed = (req.MatchBy == "email" && policy.matchEmail) || (req.MatchBy == "username" && policy.matchUsername)
	case finalizeCreate:
		allowed = policy.autoCreate
	}
	if !allowed {
		return policy, echo.NewHTTPError(http.StatusForbidden, "this sign-in option is no longer available")
	}
	return policy, nil
}

// signIn runs after the commit. The email syncs are best effort, so a
// collision cannot fail the login.
func (o *OIDCRoutes) signIn(c echo.Context, out finalized, id ExternalIdentity) {
	ctx := reqCtx(c)
	if out.Demoted {
		o.res.hub.Drop(out.User.ID)
	}
	syncEmail(ctx, o.res.pool, out.User, id.Email)
	syncIdentityEmail(ctx, o.res.pool, id)
	setSessionCookie(c, o.res.st, out.Token)
}
