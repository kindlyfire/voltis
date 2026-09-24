package routes

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/config"
	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
)

const trustedPeerAddr = "10.0.0.7:4711"

var testProxyAuth = config.ProxyAuth{
	TrustedCIDRs: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")},
	UserHeader:   "Remote-User",
	EmailHeader:  "Remote-Email",
	GroupsHeader: "Remote-Groups",
}

func newProxiedClient(t *testing.T, pool *pgxpool.Pool) *testClient {
	t.Helper()
	return newProxyClient(t, pool, testProxyAuth)
}

func (c *testClient) asProxy(user string, headers ...string) *testClient {
	out := c.WithRemoteAddr(trustedPeerAddr).WithHeader("Remote-User", user)
	for i := 0; i+1 < len(headers); i += 2 {
		out = out.WithHeader(headers[i], headers[i+1])
	}
	return out
}

func sessionMethods(t *testing.T, pool *pgxpool.Pool, username string) []string {
	t.Helper()
	methods, err := db.SelectScalars[string](context.Background(), pool, `
		SELECT s.method FROM sessions s JOIN users u ON u.id = s.user_id WHERE u.username = $1
	`, username)
	if err != nil {
		t.Fatalf("read sessions: %v", err)
	}
	return methods
}

func TestProxyHeaderFromAnUntrustedPeerIsIgnored(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	c.WithHeader("Remote-User", "alice").Get("/api/users/me").Assert(t, 401)

	users, err := db.SelectScalar[int](context.Background(), pool, "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	assertEq(t, users, 0)
}

func TestProxyTrustedPeerCreatesTheUserAndASession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	resp := c.asProxy("alice", "Remote-Email", "Alice@Example.com").Get("/api/users/me").Assert(t, 200)
	assertEq(t, s(resp.JSON()["username"]), "alice")
	if resp.Cookie("voltis_session") == nil {
		t.Fatal("the proxy login did not set a session cookie")
	}
	assertEq(t, len(sessionMethods(t, pool, "alice")), 1)
	assertEq(t, sessionMethods(t, pool, "alice")[0], models.SessionProxy)

	email, err := db.SelectScalar[*string](context.Background(), pool,
		"SELECT email FROM users WHERE username = 'alice'")
	if err != nil || email == nil || *email != "alice@example.com" {
		t.Fatalf("got %v (%v), want the synced email", email, err)
	}

	identities, err := db.SelectScalar[int](context.Background(), pool,
		"SELECT count(*) FROM user_identities WHERE provider = 'proxy' AND subject = 'alice'")
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	assertEq(t, identities, 1)
}

func TestProxyReusesItsOwnSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool).asProxy("alice")

	c.Get("/api/users/me").Assert(t, 200)
	c.Get("/api/users/me").Assert(t, 200)
	assertEq(t, len(sessionMethods(t, pool, "alice")), 1)
}

func TestProxyHeaderChangeReplacesTheSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	alice := c.asProxy("alice")
	alice.Get("/api/users/me").Assert(t, 200)
	conn := alice.dialWS(t)
	waitConns(t, c.hub, 1)

	bob := alice.WithHeader("Remote-User", "bob")
	assertEq(t, s(bob.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "bob")

	expectWSClosed(t, conn)
	assertEq(t, len(sessionMethods(t, pool, "alice")), 0)
	assertEq(t, len(sessionMethods(t, pool, "bob")), 1)
}

func TestProxySessionIsRejectedWithoutTheHeader(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	proxied := c.asProxy("alice")
	proxied.Get("/api/users/me").Assert(t, 200)

	bypass := c.newSession(t)
	for _, ck := range proxied.http.Jar.Cookies(proxied.url()) {
		bypass.SetCookie(ck.Name, ck.Value)
	}
	bypass.Get("/api/users/me").Assert(t, 401)
}

func TestProxyLeavesPasswordSessionsAlone(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)

	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "admin")

	external := c.newSession(t)
	external.SetCookie("voltis_session", insertSession(t, pool,
		meID(t, c), models.SessionOIDC, time.Now().Add(time.Hour), nil))
	external.Get("/api/users/me").Assert(t, 200)
}

func TestProxyRejectsUnusableHeaders(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	c.asProxy("  ").Get("/api/users/me").Assert(t, 400)

	c.WithRemoteAddr(trustedPeerAddr).
		WithRawHeader("Remote-User", "alice", "bob").
		Get("/api/users/me").Assert(t, 400)
}

func TestProxySyncsGroupsOnEveryRequest(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")

	admin := c.asProxy("alice", "Remote-Groups", "users, admins")
	assertEq(t, hasAdmin(admin.Get("/api/users/me").Assert(t, 200).JSON()), true)
	admin.Get("/api/users").Assert(t, 200)

	conn := admin.dialWS(t)
	waitConns(t, c.hub, 1)

	assertEq(t, hasAdmin(c.asProxy("alice").Get("/api/users/me").Assert(t, 200).JSON()), true)

	demoted := admin.WithHeader("Remote-Groups", "users")
	assertEq(t, hasAdmin(demoted.Get("/api/users/me").Assert(t, 200).JSON()), false)
	expectWSClosed(t, conn)
	demoted.Get("/api/users").Assert(t, 403)
}

func TestProxyWebSocketAuthenticatesThroughTheHeader(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	conn, resp, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/ws",
		http.Header{
			testRemoteAddrHeader: []string{trustedPeerAddr},
			"Remote-User":        []string{"alice"},
		})
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	waitConns(t, c.hub, 1)

	if len(resp.Header.Values("Set-Cookie")) == 0 {
		t.Fatal("the upgrade response dropped the session minted for the proxy user")
	}
	assertEq(t, len(sessionMethods(t, pool, "alice")), 1)

	_, bad, err := websocket.DefaultDialer.Dial(
		"ws"+strings.TrimPrefix(c.server.URL, "http")+"/api/ws",
		http.Header{
			testRemoteAddrHeader: []string{trustedPeerAddr},
			"Remote-User":        []string{"alice"},
			"Origin":             []string{"http://evil.example"},
		})
	if err == nil {
		t.Fatal("expected a foreign origin to be rejected")
	}
	if bad == nil || bad.StatusCode != http.StatusForbidden {
		t.Fatalf("got %v", bad)
	}
}

func TestProxyRejectsWhenAutoCreateIsOff(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthExternalAutoCreate, false)

	c.asProxy("alice").Get("/api/users/me").Assert(t, 403)
}

func TestProxyRejectsATakenUsername(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	makeUser(t, pool, "alice", "hash", "")

	c.asProxy("alice").Get("/api/users/me").Assert(t, 403)

	setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
	assertEq(t, s(c.asProxy("alice").Get("/api/users/me").Assert(t, 200).JSON()["username"]), "alice")
}

func TestProxyLogoutRedirects(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthProxyLogoutURL, "https://sso.example/logout")

	proxied := c.asProxy("alice")
	proxied.Get("/api/users/me").Assert(t, 200)
	resp := proxied.Post("/api/auth/logout", nil).Assert(t, 200)
	assertEq(t, resp.JSON()["redirect_url"], "https://sso.example/logout")
}

func hasAdmin(user map[string]any) bool {
	perms, _ := user["permissions"].([]any)
	for _, p := range perms {
		if s(p) == "ADMIN" {
			return true
		}
	}
	return false
}

func TestProxyIgnoresAForgedForwardedForHeader(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	// The peer stays 127.0.0.1; only RealIP() would believe these.
	spoofed := c.WithHeader("X-Forwarded-For", "10.0.0.7").
		WithHeader("X-Real-IP", "10.0.0.7").
		WithHeader("Remote-User", "alice")
	spoofed.Get("/api/users/me").Assert(t, 401)

	users, err := db.SelectScalar[int](context.Background(), pool, "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	assertEq(t, users, 0)
}

func TestProxyTrustsMappedAndMixedPeers(t *testing.T) {
	pool := newTestPool(t)
	c := newProxyClient(t, pool, config.ProxyAuth{
		TrustedCIDRs: []netip.Prefix{
			netip.MustParsePrefix("10.0.0.0/8"),
			netip.MustParsePrefix("2001:db8::/32"),
		},
		UserHeader:   "Remote-User",
		GroupsHeader: "Remote-Groups",
	})

	for _, peer := range []string{"[::ffff:10.0.0.7]:4711", "[2001:db8::5]:4711", "10.0.0.7:4711"} {
		resp := c.WithRemoteAddr(peer).WithHeader("Remote-User", "alice").Get("/api/users/me")
		if resp.StatusCode != 200 {
			t.Fatalf("peer %s: got %d, want a trusted peer", peer, resp.StatusCode)
		}
	}
	c.WithRemoteAddr("[2001:dead::5]:4711").WithHeader("Remote-User", "alice").
		Get("/api/users/me").Assert(t, 401)
}

func TestProxyEmptyGroupsHeaderRevokesAdmin(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")

	admin := c.asProxy("alice", "Remote-Groups", "admins")
	assertEq(t, hasAdmin(admin.Get("/api/users/me").Assert(t, 200).JSON()), true)

	empty := admin.WithHeader("Remote-Groups", "")
	assertEq(t, hasAdmin(empty.Get("/api/users/me").Assert(t, 200).JSON()), false)
}

func TestProxyConcurrentDemotionIsNotAuthorized(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")
	ctx := context.Background()

	admin := c.asProxy("alice", "Remote-Groups", "admins")
	assertEq(t, hasAdmin(admin.Get("/api/users/me").Assert(t, 200).JSON()), true)

	demote, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = demote.Rollback(ctx) }()
	if err := db.LockAdminMutation(ctx, demote); err != nil {
		t.Fatalf("lock: %v", err)
	}
	if _, err := demote.Exec(ctx, "UPDATE users SET permissions = '{}' WHERE username = 'alice'"); err != nil {
		t.Fatalf("demote: %v", err)
	}

	// Reads ADMIN, then loses the race to the demotion above.
	result := make(chan *response, 1)
	go func() { result <- admin.WithHeader("Remote-Groups", "users").Get("/api/users") }()

	waitBlocked(t, pool)
	if err := demote.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	(<-result).Assert(t, 403)
}

func TestProxyRejectedSwitchRevokesThePreviousSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)

	alice := c.asProxy("alice")
	alice.Get("/api/users/me").Assert(t, 200)
	conn := alice.dialWS(t)
	waitConns(t, c.hub, 1)

	setSetting(t, c.st, settings.AuthExternalAutoCreate, false)
	alice.WithHeader("Remote-User", "bob").Get("/api/users/me").Assert(t, 403)

	expectWSClosed(t, conn)
	assertEq(t, len(sessionMethods(t, pool, "alice")), 0)
}

func TestProxyLogoutOnTheRequestThatConvertsTheSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)
	setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
	setSetting(t, c.st, settings.AuthProxyLogoutURL, "https://sso.example/logout")

	resp := c.asProxy("admin").Post("/api/auth/logout", nil).Assert(t, 200)
	assertEq(t, resp.JSON()["redirect_url"], "https://sso.example/logout")
	assertEq(t, len(sessionMethods(t, pool, "admin")), 0)
}

func TestProxyConcurrentRequestsShareOneSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	ctx := context.Background()
	c.asProxy("alice").Get("/api/users/me").Assert(t, 200)
	if _, err := pool.Exec(ctx, "DELETE FROM sessions"); err != nil {
		t.Fatal(err)
	}
	gate, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = gate.Rollback(ctx) }()
	// Hold inserts until every request reaches session creation or its advisory lock.
	if _, err := gate.Exec(ctx, "LOCK TABLE sessions IN SHARE MODE"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	statuses := make([]int, 6)
	for i := range statuses {
		wg.Go(func() {
			client := c.newSession(t).WithRemoteAddr(trustedPeerAddr).WithHeader("Remote-User", "alice")
			statuses[i] = client.Get("/api/users/me").StatusCode
		})
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		blocked, err := db.SelectScalar[int](ctx, pool, `
			SELECT count(DISTINCT pid) FROM pg_locks
			WHERE NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())
			  AND (locktype = 'advisory' OR relation = 'sessions'::regclass)
		`)
		if err != nil {
			t.Fatal(err)
		}
		if blocked == len(statuses) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d session requests reached the gate", blocked, len(statuses))
		}
		time.Sleep(2 * time.Millisecond)
	}
	if err := gate.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()

	for _, status := range statuses {
		assertEq(t, status, 200)
	}
	assertEq(t, len(sessionMethods(t, pool, "alice")), 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
}
