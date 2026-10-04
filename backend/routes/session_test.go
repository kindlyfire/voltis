package routes

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"voltis/cmd"
	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"
)

func setSetting(t *testing.T, st *settings.Store, key string, value any) {
	t.Helper()
	if err := st.Set(context.Background(), key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func meID(t *testing.T, c *testClient) string {
	t.Helper()
	return s(c.Get("/api/users/me").Assert(t, 200).JSON()["id"])
}

func insertSession(t *testing.T, pool *pgxpool.Pool, userID, method string, expires time.Time, absolute *time.Time) string {
	t.Helper()
	token := randomToken()
	mustExec(t, pool, `
		INSERT INTO sessions (token, user_id, expires_at, method, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, token, userID, expires, method, absolute)
	return token
}

func sessionExpiry(t *testing.T, pool *pgxpool.Pool, token string) time.Time {
	t.Helper()
	at, err := db.SelectScalar[time.Time](context.Background(), pool,
		"SELECT expires_at FROM sessions WHERE token = $1", token)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	return at
}

func TestLoginRejectsUsersWithoutAPassword(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	mustExec(t, pool, "UPDATE users SET password_hash = NULL WHERE username = 'admin'")

	c.newSession(t).Post("/api/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 401)
}

func TestPasswordLoginDisabled(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	setSetting(t, c.st, settings.AuthRegistrationEnabled, true)
	setSetting(t, c.st, settings.AuthPasswordLoginEnabled, false)

	fresh := c.newSession(t)
	fresh.Post("/api/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 403)
	fresh.Post("/api/auth/register", map[string]any{
		"username": "someone", "password": "somepass123",
	}).Assert(t, 403)
}

func TestDisablingPasswordLoginDropsPasswordSessions(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	member := c.Post("/api/users/new", map[string]any{
		"username": "member", "password": "memberpass123", "permissions": []string{},
	}).Assert(t, 200).JSON()
	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, s(member["id"]), models.SessionOIDC, time.Now().Add(time.Hour), nil))

	conn := c.dialWS(t)
	externalConn := external.dialWS(t)
	waitConns(t, c.hub, 2)

	setSetting(t, c.st, settings.AuthPasswordLoginEnabled, false)

	c.Get("/api/users/me").Assert(t, 401)
	expectWSClosed(t, conn)

	external.Get("/api/users/me").Assert(t, 200)
	c.toUser(s(member["id"]), "still-live")
	expectWSMessage(t, externalConn, `"still-live"`)
}

func TestBootstrapRegisterWorksWithPasswordLoginDisabled(t *testing.T) {
	pool := newTestPool(t)
	c := newClient(t, pool)
	setSetting(t, c.st, settings.AuthPasswordLoginEnabled, false)

	c.Post("/api/auth/register", map[string]any{
		"username": "first", "password": "firstpass123",
	}).Assert(t, 200)

	c.newSession(t).Post("/api/auth/register", map[string]any{
		"username": "second", "password": "secondpass123",
	}).Assert(t, 403)
}

func TestRegisterBlockedAfterBootstrap(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	other := c.newSession(t)
	other.Post("/api/auth/register", map[string]any{
		"username": "second", "password": "secondpass123",
	}).Assert(t, 403)

	setSetting(t, c.st, settings.AuthRegistrationEnabled, true)
	other.Post("/api/auth/register", map[string]any{
		"username": "second", "password": "secondpass123",
	}).Assert(t, 200)
}

func TestExternalSessionAbsoluteCap(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)

	absolute := time.Now().Add(48 * time.Hour)
	token := insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), &absolute)

	capped := c.newSession(t)
	capped.SetCookie("voltis_session", token)
	capped.Get("/api/users/me").Assert(t, 200)

	got := sessionExpiry(t, pool, token)
	if got.Sub(absolute).Abs() > time.Second {
		t.Fatalf("session expiry %v, want the absolute cap %v", got, absolute)
	}

	past := time.Now().Add(-time.Minute)
	expired := insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(24*time.Hour), &past)
	done := c.newSession(t)
	done.SetCookie("voltis_session", expired)
	done.Get("/api/users/me").Assert(t, 401)
}

func TestExternalSessionCapComesFromSettings(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	setSetting(t, c.st, settings.AuthExternalSessionMaxDays, 2)

	token, err := createSession(context.Background(), pool, c.st, meID(t, c), models.SessionProxy)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	absolute, err := db.SelectScalar[*time.Time](context.Background(), pool,
		"SELECT absolute_expires_at FROM sessions WHERE token = $1", token)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	want := time.Now().Add(48 * time.Hour)
	if absolute == nil || absolute.Sub(want).Abs() > time.Minute {
		t.Fatalf("got absolute expiry %v, want about %v", absolute, want)
	}
}

func TestCookieSecureFollowsPublicURL(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	login := func(cl *testClient) *http.Cookie {
		t.Helper()
		resp := cl.Post("/api/auth/login", map[string]any{
			"username": "admin", "password": "adminpass123",
		}).Assert(t, 200)
		cookie := resp.Cookie("voltis_session")
		if cookie == nil {
			t.Fatal("no session cookie")
		}
		return cookie
	}

	assertEq(t, login(c.newSession(t)).Secure, false)

	setSetting(t, c.st, settings.AppPublicURL, "https://voltis.example")
	assertEq(t, login(c.newSession(t)).Secure, true)

	// The jar will not send a Secure cookie over http, so carry it by hand.
	logout := c.newSession(t)
	logout.SetCookie("voltis_session", login(logout).Value)
	cleared := logout.Post("/api/auth/logout", nil).Assert(t, 200)
	assertEq(t, cleared.Cookie("voltis_session").Secure, true)
	assertEq(t, cleared.JSON()["redirect_url"], "")
}

func TestWebSocketOriginCheck(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	dial := func(origin string) (*http.Response, error) {
		header := http.Header{}
		for _, ck := range c.http.Jar.Cookies(c.url()) {
			header.Add("Cookie", ck.Name+"="+ck.Value)
		}
		if origin != "" {
			header.Set("Origin", origin)
		}
		conn, resp, err := websocket.DefaultDialer.Dial(
			"ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/ws", header)
		if conn != nil {
			_ = conn.Close()
		}
		return resp, err
	}

	if _, err := dial(c.server.URL); err != nil {
		t.Fatalf("same-origin dial failed: %v", err)
	}
	if _, err := dial("http://localhost:5173"); err != nil {
		t.Fatalf("dev origin dial failed: %v", err)
	}
	resp, err := dial("http://evil.example")
	if err == nil {
		t.Fatal("expected a foreign origin to be rejected")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("got %v", resp)
	}

	setSetting(t, c.st, settings.AppPublicURL, "https://voltis.example")
	if _, err := dial("https://voltis.example"); err != nil {
		t.Fatalf("public url dial failed: %v", err)
	}
}

func TestWebSocketCarriesRefreshedSessionCookie(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	token := insertSession(t, pool, meID(t, c), models.SessionPassword, time.Now().Add(24*time.Hour), nil)

	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/ws",
		http.Header{"Cookie": []string{"voltis_session=" + token}})
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if len(resp.Header.Values("Set-Cookie")) == 0 {
		t.Fatal("the upgrade response dropped the session cookie")
	}
}

func TestCookieSecureSources(t *testing.T) {
	st := newStore(t, newTestPool(t))
	ctx := func(forwarded string, tlsOn bool) echo.Context {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if forwarded != "" {
			req.Header.Set("X-Forwarded-Proto", forwarded)
		}
		if tlsOn {
			req.TLS = &tls.ConnectionState{}
		}
		return echo.New().NewContext(req, httptest.NewRecorder())
	}

	assertEq(t, cookieSecure(ctx("https", false), st), false)
	assertEq(t, cookieSecure(ctx("", true), st), true)

	setSetting(t, st, settings.AppPublicURL, "HTTPS://Voltis.Example")
	assertEq(t, cookieSecure(ctx("", false), st), true)
}

// A login or registration that passed the early check must not leave a
// password session behind a disable that commits while it is in flight.
func TestPasswordLoginDisabledMidRequest(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	setSetting(t, c.st, settings.AuthRegistrationEnabled, true)
	ctx := context.Background()

	for _, req := range []struct {
		path string
		body map[string]any
		lock string
	}{
		{"/api/auth/login", map[string]any{"username": "admin", "password": "adminpass123"},
			"FROM settings_version FOR SHARE"},
		{"/api/auth/register", map[string]any{"username": "ghost", "password": "ghostpass123"},
			"FROM settings_version FOR UPDATE"},
	} {
		setSetting(t, c.st, settings.AuthPasswordLoginEnabled, true)
		disable, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = disable.Rollback(ctx) }()
		if err := settings.WriteTx(ctx, disable, settings.AuthPasswordLoginEnabled, false); err != nil {
			t.Fatalf("disable: %v", err)
		}

		done := make(chan *response, 1)
		go func() { done <- c.newSession(t).Post(req.path, req.body) }()

		waitBlockedOn(t, pool, req.lock)
		if err := disable.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		(<-done).Assert(t, 403)
	}

	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions WHERE method = 'password'"), 0)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users WHERE username = 'ghost'"), 0)
}

// waitBlockedOn waits until a backend in this database is waiting on a lock
// while running a statement that contains fragment.
func waitBlockedOn(t *testing.T, pool *pgxpool.Pool, fragment string) {
	t.Helper()
	const blocked = `
		SELECT query FROM pg_stat_activity
		WHERE datname = current_database() AND wait_event_type = 'Lock'`
	for range 1000 {
		n, err := db.SelectScalar[int](context.Background(), pool,
			"SELECT count(*) FROM ("+blocked+") b WHERE strpos(query, $1) > 0", fragment)
		if err != nil {
			t.Fatalf("read pg_stat_activity: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	others, _ := db.SelectScalars[string](context.Background(), pool, blocked)
	t.Fatalf("timed out waiting for a lock wait in %q; blocked statements: %q", fragment, others)
}

func TestCLIRenameKeepsACommittedDemotion(t *testing.T) {
	pool := newTestPool(t)
	newAdminClient(t, pool)
	ctx := context.Background()

	if err := cmd.CreateUser(ctx, pool, "bob", new("bobpass12345"), true); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Demote bob but hold the transaction open, so the rename reads the row as
	// it was before and only then blocks on the row lock.
	demote, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = demote.Rollback(ctx) }()
	if err := db.LockAdminMutation(ctx, demote); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := demote.Exec(ctx, "UPDATE users SET permissions = '{}' WHERE username = 'bob'"); err != nil {
		t.Fatalf("demote: %v", err)
	}

	renamed := make(chan error, 1)
	go func() {
		newName := "bobby"
		renamed <- cmd.UpdateUser(ctx, pool, "bob", &newName, nil, nil)
	}()

	waitBlockedOn(t, pool, "username = COALESCE($1::text, username)")
	if err := demote.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-renamed; err != nil {
		t.Fatalf("rename: %v", err)
	}

	perms, err := db.SelectScalar[[]string](ctx, pool,
		"SELECT permissions FROM users WHERE username = 'bobby'")
	if err != nil {
		t.Fatalf("read permissions: %v", err)
	}
	assertEq(t, len(perms), 0)
}

func TestBearerAuth(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	member, memberID := newMemberClient(t, admin)

	tokenReq := map[string]any{"username": "member", "password": "memberpass123", "client_name": " Test Phone "}
	device := s(admin.newSession(t).Post("/api/auth/token", tokenReq).Assert(t, 200).JSON()["token"])
	assertEq(t, countRows(t, admin, `SELECT count(*) FROM sessions WHERE token = '`+device+`'
		AND method = 'password' AND client_name = 'Test Phone' AND absolute_expires_at IS NULL`), 1)
	admin.newSession(t).Post("/api/auth/token", map[string]any{
		"username": "member", "password": "wrong", "client_name": "Test Phone",
	}).Assert(t, 401)

	adminBearer := insertSession(t, pool, meID(t, admin), models.SessionPassword, time.Now().Add(time.Hour), nil)
	browserProxy := insertSession(t, pool, memberID, models.SessionProxy, time.Now().Add(time.Hour), nil)
	deviceProxy := insertSession(t, pool, memberID, models.SessionProxy, time.Now().Add(time.Hour), nil)
	stale := insertSession(t, pool, memberID, models.SessionPassword, time.Now().Add(24*time.Hour), nil)
	mustExec(t, pool, `UPDATE sessions SET client_name = 'Test Phone',
		last_used_at = NOW() - interval '2 hours' WHERE token IN ($1, $2)`, deviceProxy, stale)

	for _, tc := range []struct {
		auth []string
		want string // username, or "" for anonymous
	}{
		{[]string{"Bearer " + device}, "member"},
		{[]string{"bearer " + adminBearer}, "admin"},
		{[]string{"Bearer " + randomToken()}, ""},
		{[]string{"Bearer a.b.c"}, "member"},
		{[]string{"Basic dXNlcjpwYXNz"}, "member"},
		{[]string{"Basic dXNlcjpwYXNz", "Bearer " + adminBearer}, "admin"},
		{[]string{"Bearer " + browserProxy}, ""},
		// Forwarded auth is off on this server.
		{[]string{"Bearer " + deviceProxy}, ""},
	} {
		resp := member.WithRawHeader("Authorization", tc.auth...).Get("/api/users/me")
		if tc.want == "" {
			resp.Assert(t, 401)
		} else {
			assertEq(t, s(resp.Assert(t, 200).JSON()["username"]), tc.want)
		}
	}

	// A device session slides to the full window and never gets a cookie.
	bearer := admin.newSession(t).WithHeader("Authorization", "Bearer "+stale)
	resp := bearer.Get("/api/users/me").Assert(t, 200)
	assertEq(t, len(resp.Cookies), 0)
	assertEq(t, countRows(t, admin, `SELECT count(*) FROM sessions WHERE token = '`+stale+`'
		AND abs(extract(epoch FROM expires_at - (NOW() + interval '30 days'))) < 60
		AND absolute_expires_at IS NULL AND last_used_at > NOW() - interval '1 minute'`), 1)

	// A browser session due for a refresh, sent as a bearer over the socket.
	due := insertSession(t, pool, memberID, models.SessionPassword, time.Now().Add(24*time.Hour), nil)
	conn, wsResp, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(admin.server.URL, "http")+"/api/ws",
		http.Header{"Authorization": []string{"Bearer " + due}})
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	_ = conn.Close()
	assertEq(t, len(wsResp.Header.Values("Set-Cookie")), 0)

	// Session list and revoke.
	sessionID := func(token string) string {
		t.Helper()
		id, err := db.SelectScalar[string](context.Background(), pool, "SELECT id FROM sessions WHERE token = $1", token)
		if err != nil {
			t.Fatalf("read session id: %v", err)
		}
		return id
	}
	listResp := member.Get("/api/users/me/sessions").Assert(t, 200)
	if body := string(listResp.Body); strings.Contains(body, device) || strings.Contains(body, `"token"`) {
		t.Fatalf("session list leaks a token: %s", listResp.Body)
	}
	list := listResp.JSONArray()
	assertEq(t, s(list[0]["id"]), sessionID(member.cookie("voltis_session")))
	assertEq[any](t, list[0]["current"], true)
	i := slices.IndexFunc(list, func(row map[string]any) bool { return s(row["id"]) == sessionID(device) })
	if i < 1 || s(list[i]["client_name"]) != "Test Phone" || list[i]["current"] != false {
		t.Fatalf("device session not listed: %v", list)
	}
	member.Delete("/api/users/me/sessions/"+sessionID(device)).Assert(t, 200)
	admin.newSession(t).WithHeader("Authorization", "Bearer "+device).Get("/api/users/me").Assert(t, 401)
	member.Delete("/api/users/me/sessions/"+s(list[0]["id"])).Assert(t, 404)
	member.Delete("/api/users/me/sessions/"+sessionID(adminBearer)).Assert(t, 404)
	member.Get("/api/users/me").Assert(t, 200)

	bearer.Post("/api/auth/logout", nil).Assert(t, 200)
	bearer.Get("/api/users/me").Assert(t, 401)

	// Web-session handoff.
	verifier := oauth2.GenerateVerifier()
	codeReq := map[string]any{"code_challenge": oauth2.S256ChallengeFromVerifier(verifier), "client_name": "Test Tablet"}
	mint := func() string {
		t.Helper()
		redirect := s(member.Post("/api/auth/app-code", codeReq).Assert(t, 200).JSON()["redirect_url"])
		if !strings.HasPrefix(redirect, "voltis://auth/callback?code=") {
			t.Fatalf("redirect_url = %q", redirect)
		}
		u, _ := url.Parse(redirect)
		return u.Query().Get("code")
	}
	anon := admin.newSession(t)
	exchange := func(code, v string) *response {
		return anon.Post("/api/auth/token/exchange", map[string]any{"code": code, "code_verifier": v})
	}
	member.WithHeader("Content-Type", "text/plain").Post("/api/auth/app-code", codeReq).Assert(t, 415)
	member.Post("/api/auth/app-code", map[string]any{"code_challenge": "short", "client_name": "Test Tablet"}).
		Assert(t, 400)

	code := mint()
	handed := s(exchange(code, verifier).Assert(t, 200).JSON()["token"])
	assertEq(t, s(anon.WithHeader("Authorization", "Bearer "+handed).Get("/api/users/me").Assert(t, 200).
		JSON()["username"]), "member")
	assertEq(t, countRows(t, admin, `SELECT count(*) FROM sessions WHERE token = '`+handed+`'
		AND method = 'password' AND client_name = 'Test Tablet' AND absolute_expires_at IS NULL`), 1)
	exchange(code, verifier).Assert(t, 400)

	// A wrong verifier spends the code.
	code = mint()
	exchange(code, oauth2.GenerateVerifier()).Assert(t, 400)
	exchange(code, verifier).Assert(t, 400)

	// Last: this deletes every password session, so a code dies with the one that minted it.
	code = mint()
	setSetting(t, admin.st, settings.AuthPasswordLoginEnabled, false)
	exchange(code, verifier).Assert(t, 400)
	admin.newSession(t).Post("/api/auth/token", tokenReq).Assert(t, 403)
}
