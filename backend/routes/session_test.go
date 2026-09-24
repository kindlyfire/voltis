package routes

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"voltis/cmd"
	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
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
	_, err := pool.Exec(context.Background(), `
		INSERT INTO sessions (token, user_id, expires_at, method, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, token, userID, expires, method, absolute)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
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

	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET password_hash = NULL WHERE username = 'admin'"); err != nil {
		t.Fatalf("clear hash: %v", err)
	}

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

func TestConcurrentUnlinkKeepsALoginMethod(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id)
		VALUES ('ui_a', 'oidc', 'https://idp.example', 'a', $1), ('ui_b', 'proxy', '', 'b', $1)
	`, userID); err != nil {
		t.Fatalf("seed identities: %v", err)
	}

	unlink := func(tx pgx.Tx, id string) error {
		if _, err := tx.Exec(ctx, "DELETE FROM user_identities WHERE id = $1", id); err != nil {
			return err
		}
		return requireLoginMethod(ctx, tx, userID)
	}

	first, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = first.Rollback(ctx) }()
	if err := unlink(first, "ui_a"); err != nil {
		t.Fatalf("first unlink: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		tx, err := pool.Begin(ctx)
		if err != nil {
			second <- err
			return
		}
		defer func() { _ = tx.Rollback(ctx) }()
		second <- unlink(tx, "ui_b")
	}()

	waitBlocked(t, pool)
	if err := first.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := <-second; err == nil {
		t.Fatal("the second unlink left the account with no login method")
	}

	left, err := db.SelectScalar[int](ctx, pool,
		"SELECT count(*) FROM user_identities WHERE user_id = $1", userID)
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	assertEq(t, left, 1)
}

func TestPasswordLoginDisabledWhileLoggingIn(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	ctx := context.Background()

	disable, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = disable.Rollback(ctx) }()
	if err := settings.WriteTx(ctx, disable, settings.AuthPasswordLoginEnabled, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	login := make(chan *response, 1)
	go func() {
		login <- c.newSession(t).Post("/api/auth/login", map[string]any{
			"username": "admin", "password": "adminpass123",
		})
	}()

	waitBlocked(t, pool)
	if err := disable.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	(<-login).Assert(t, 403)
	live, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM sessions WHERE method = 'password'")
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	assertEq(t, live, 0)
}

func waitBlocked(t *testing.T, pool *pgxpool.Pool) { waitBlockedN(t, pool, 1) }

func waitBlockedN(t *testing.T, pool *pgxpool.Pool, want int) {
	t.Helper()
	for range 1000 {
		n, err := db.SelectScalar[int](context.Background(), pool, `
			SELECT count(*) FROM pg_locks l
			JOIN pg_stat_activity a ON a.pid = l.pid
			WHERE NOT l.granted AND a.datname = current_database()`)
		if err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d blocked locks", want)
}

func TestRegisterLeavesNoUserWhenItIsRefused(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	setSetting(t, c.st, settings.AuthRegistrationEnabled, true)
	ctx := context.Background()

	disable, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = disable.Rollback(ctx) }()
	if err := settings.WriteTx(ctx, disable, settings.AuthPasswordLoginEnabled, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	register := make(chan *response, 1)
	go func() {
		register <- c.newSession(t).Post("/api/auth/register", map[string]any{
			"username": "ghost", "password": "ghostpass123",
		})
	}()

	waitBlocked(t, pool)
	if err := disable.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	(<-register).Assert(t, 403)
	ghosts, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM users WHERE username = 'ghost'")
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	assertEq(t, ghosts, 0)
}

func TestCLIRenameKeepsACommittedDemotion(t *testing.T) {
	pool := newTestPool(t)
	newAdminClient(t, pool)
	ctx := context.Background()

	if err := cmd.CreateUser(ctx, pool, "bob", "bobpass12345", true); err != nil {
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

	waitBlocked(t, pool)
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
