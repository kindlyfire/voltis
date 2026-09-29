package routes

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"golang.org/x/crypto/bcrypt"
)

func hashOf(t *testing.T, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	return string(hash)
}

func countRows(t *testing.T, c *testClient, query string) int {
	t.Helper()
	n, err := db.SelectScalar[int](context.Background(), c.pool(), query)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestOIDCCreatesAUser(t *testing.T) {
	c, _ := newOIDCPair(t)

	// An off-site redirect is dropped.
	assertRedirect(t, c.callback(c.authorize(t, "/api/auth/oidc/login?redirect=%2F%2Fevil.example")), "/")
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "idpuser")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities WHERE provider = 'oidc'"), 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions WHERE method = 'oidc'"), 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)
}

func TestOIDCExistingIdentityLogsIn(t *testing.T) {
	c, idp := newOIDCPair(t)
	userID := makeUser(t, c.pool(), "local", "hash", "")
	makeIdentity(t, c.pool(), userID, ExternalIdentity{
		Provider: models.SessionOIDC, Issuer: idp.server.URL, Subject: "sub-1",
	})

	resp := c.callback(c.authorize(t, "/api/auth/oidc/login?redirect=%2Fc_fixture%3Fpage%3D2"))
	assertRedirect(t, resp, "/c_fixture")
	assertEq(t, resp.Headers.Get("Location"), "/c_fixture?page=2")
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["id"]), userID)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
}

func TestOIDCAutoCreateOff(t *testing.T) {
	c, _ := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthExternalAutoCreate, false)

	assertRejected(t, c.oidcLogin(t), "no account for this login")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
}

func TestOIDCRejections(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, c *testClient, idp *stubIdP)
		mangle  func(c *testClient, idp *stubIdP, query map[string][]string)
		want    string
	}{
		{
			name:   "a state that does not match",
			mangle: func(_ *testClient, _ *stubIdP, q map[string][]string) { q["state"] = []string{"forged"} },
			want:   "state did not match",
		},
		{
			name:    "a nonce that does not match",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) { idp.nonceOverride = "forged" },
			want:    "nonce did not match",
		},
		{
			name:    "a tampered signature",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) { idp.tamper = true },
			want:    "ID token was rejected",
		},
		{
			name:    "an issuer that does not match",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) { idp.issuer = "https://evil.example" },
			want:    "ID token was rejected",
		},
		{
			name:    "an audience that does not match",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) { idp.audience = "someone-else" },
			want:    "ID token was rejected",
		},
		{
			name:    "an expired token",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) { idp.expiry = time.Now().Add(-time.Hour) },
			want:    "ID token was rejected",
		},
		{
			name: "a PKCE verifier that does not match",
			mangle: func(c *testClient, _ *stubIdP, _ map[string][]string) {
				if _, err := c.pool().Exec(context.Background(),
					`UPDATE auth_pending SET data = jsonb_set(data, '{verifier}', '"wrong-verifier"')`); err != nil {
					panic(err)
				}
			},
			want: "authorization code was rejected",
		},
		{
			name: "a userinfo response for another subject",
			arrange: func(_ *testing.T, _ *testClient, idp *stubIdP) {
				idp.claims = map[string]any{}
				idp.userinfo = map[string]any{"sub": "someone-else", "preferred_username": "idpuser"}
			},
			want: "belongs to a different subject",
		},
		{
			name: "an authorization the user refused",
			mangle: func(_ *testClient, _ *stubIdP, q map[string][]string) {
				delete(q, "code")
				q["error"] = []string{"access_denied"}
			},
			want: "the identity provider refused the sign-in",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			if tc.arrange != nil {
				tc.arrange(t, c, idp)
			}

			query := c.authorize(t, "/api/auth/oidc/login")
			if tc.mangle != nil {
				tc.mangle(c, idp, query)
			}

			assertRejected(t, c.callback(query), tc.want)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions"), 0)
		})
	}
}

func TestOIDCCallbackCannotBeReplayed(t *testing.T) {
	c, _ := newOIDCPair(t)

	query := c.authorize(t, "/api/auth/oidc/login")
	pending := c.cookie(pendingCookie)
	assertRedirect(t, c.callback(query), "/")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)

	c.SetCookie(pendingCookie, pending)
	assertRejected(t, c.callback(query), "expired")

	// A code the provider would still accept, against a consumed pending ID.
	c.SetCookie(pendingCookie, pending)
	fresh := c.authorizeAt(t, c.Get("/api/auth/oidc/login").Headers.Get("Location"))
	c.SetCookie(pendingCookie, pending)
	assertRejected(t, c.callback(fresh), "expired")

	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions WHERE method = 'oidc'"), 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
}

func TestOIDCEmailMatch(t *testing.T) {
	cases := []struct {
		name     string
		enabled  bool
		verified bool
		linked   bool
	}{
		{"on and verified", true, true, true},
		{"off", false, true, false},
		{"unverified", true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			setSetting(t, c.st, settings.AuthLinkMatchEmail, tc.enabled)
			userID := makeUser(t, c.pool(), "local", "hash", "person@example.com")
			idp.claims = map[string]any{
				"preferred_username": "idpuser",
				"email":              "Person@Example.com",
				"email_verified":     tc.verified,
			}

			assertRedirect(t, c.oidcLogin(t), "/")
			me := c.Get("/api/users/me").Assert(t, 200).JSON()
			assertEq(t, s(me["id"]) == userID, tc.linked)
		})
	}
}

func TestOIDCUsernameMatchConfirm(t *testing.T) {
	c, _ := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
	userID := makeUser(t, c.pool(), "idpuser", "", "")
	if _, err := c.pool().Exec(context.Background(),
		"UPDATE users SET password_hash = $1 WHERE id = $2", hashOf(t, "localpass123"), userID); err != nil {
		t.Fatalf("set password: %v", err)
	}

	assertRedirect(t, c.callback(c.authorize(t, "/api/auth/oidc/login?redirect=%2Flists")), "/auth/oidc/complete")

	waiting := c.Get("/api/auth/oidc/pending").Assert(t, 200).JSON()
	assertEq(t, s(waiting["needs"]), needsConfirm)
	assertEq(t, s(waiting["match_username"]), "idpuser")
	assertEq(t, s(waiting["redirect"]), "/lists")

	c.Post("/api/auth/oidc/confirm", map[string]any{"password": "wrong-password"}).Assert(t, 401)
	c.Get("/api/auth/oidc/pending").Assert(t, 200)

	c.Post("/api/auth/oidc/confirm", map[string]any{"password": "localpass123"}).Assert(t, 200)
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["id"]), userID)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)

	c.Post("/api/auth/oidc/confirm", map[string]any{"password": "localpass123"}).Assert(t, 404)
}

func TestOIDCDeclineAndPickAUsername(t *testing.T) {
	c, _ := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
	makeUser(t, c.pool(), "idpuser", "hash", "")

	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

	c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "idpuser"}).Assert(t, 400)
	c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "x"}).Assert(t, 400)
	c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 200)

	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	assertEq(t, s(me["username"]), "chosen")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 2)
}

func TestOIDCUsernameTakenOffersAPick(t *testing.T) {
	c, _ := newOIDCPair(t)
	makeUser(t, c.pool(), "idpuser", "hash", "")

	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")
	waiting := c.Get("/api/auth/oidc/pending").Assert(t, 200).JSON()
	assertEq(t, s(waiting["needs"]), needsPickUsername)

	c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 200)
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "chosen")
}

func TestOIDCSelfLink(t *testing.T) {
	c, _ := newOIDCPair(t)
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)
	userID := meID(t, c)

	target := s(c.Post("/api/auth/oidc/link", map[string]any{}).Assert(t, 200).JSON()["url"])
	assertRedirect(t, c.callback(c.authorizeAt(t, target)), accountPath)

	owner, err := db.SelectScalar[string](context.Background(), c.pool(),
		"SELECT user_id FROM user_identities WHERE subject = 'sub-1'")
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	assertEq(t, owner, userID)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
}

func TestOIDCSelfLinkRejectsAnIdentityOwnedByAnotherUser(t *testing.T) {
	c, idp := newOIDCPair(t)
	other := makeUser(t, c.pool(), "other", "hash", "")
	makeIdentity(t, c.pool(), other, ExternalIdentity{
		Provider: models.SessionOIDC, Issuer: idp.server.URL, Subject: "sub-1",
	})
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)

	target := s(c.Post("/api/auth/oidc/link", map[string]any{}).Assert(t, 200).JSON()["url"])
	assertRejected(t, c.callback(c.authorizeAt(t, target)), "already linked to another account")

	owner, err := db.SelectScalar[string](context.Background(), c.pool(),
		"SELECT user_id FROM user_identities WHERE subject = 'sub-1'")
	if err != nil {
		t.Fatalf("read identity: %v", err)
	}
	assertEq(t, owner, other)
}

func TestOIDCSelfLinkRejectsAChangedSession(t *testing.T) {
	c, _ := newOIDCPair(t)
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)

	target := s(c.Post("/api/auth/oidc/link", map[string]any{}).Assert(t, 200).JSON()["url"])
	query := c.authorizeAt(t, target)
	c.Post("/api/auth/logout", nil).Assert(t, 200)

	assertRejected(t, c.callback(query), "your session changed")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
}

func TestOIDCAdminSync(t *testing.T) {
	c, idp := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")

	idp.claims = map[string]any{"preferred_username": "idpuser", "groups": []string{"users", "admins"}}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), true)

	// userinfo answers and carries no groups: an assertion, not an outage.
	idp.claims = map[string]any{"preferred_username": "idpuser"}
	idp.userinfo = map[string]any{"sub": "sub-1", "preferred_username": "idpuser"}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), true)

	idp.userinfo = nil
	idp.claims = map[string]any{"preferred_username": "idpuser", "groups": "users"}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), false)
}

func TestOIDCBootstrapStaysClosedAfterADemotion(t *testing.T) {
	c, idp := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")

	idp.claims = map[string]any{"preferred_username": "idpuser", "groups": []string{"admins"}}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), true)

	idp.claims = map[string]any{"preferred_username": "idpuser", "groups": []string{}}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users WHERE permissions @> ARRAY['ADMIN']"), 0)

	restarted := newClient(t, c.pool())
	assertEq(t, restarted.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], false)
	restarted.Post("/api/auth/register", map[string]any{
		"username": "sneaky", "password": "sneakypass123",
	}).Assert(t, 403)
}

func TestOIDCDisabledRejects(t *testing.T) {
	c, _ := newOIDCPair(t)
	setSetting(t, c.st, settings.OIDCEnabled, false)

	assertRejected(t, c.Get("/api/auth/oidc/login"), "not configured")
	c.Post("/api/auth/oidc/link", map[string]any{}).Assert(t, http.StatusUnauthorized)
}

func TestOIDCRechecksPolicyOnCompletion(t *testing.T) {
	t.Run("confirm", func(t *testing.T) {
		c, _ := newOIDCPair(t)
		setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
		userID := makeUser(t, c.pool(), "idpuser", "", "")
		if _, err := c.pool().Exec(context.Background(),
			"UPDATE users SET password_hash = $1 WHERE id = $2", hashOf(t, "localpass123"), userID); err != nil {
			t.Fatalf("set password: %v", err)
		}
		assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

		setSetting(t, c.st, settings.AuthLinkMatchUsername, false)
		c.Post("/api/auth/oidc/confirm", map[string]any{"password": "localpass123"}).Assert(t, 403)
		assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
	})

	t.Run("choose-username", func(t *testing.T) {
		c, _ := newOIDCPair(t)
		makeUser(t, c.pool(), "idpuser", "hash", "")
		assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

		setSetting(t, c.st, settings.AuthExternalAutoCreate, false)
		c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 403)
		assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
	})

	t.Run("sign-on disabled", func(t *testing.T) {
		c, _ := newOIDCPair(t)
		makeUser(t, c.pool(), "idpuser", "hash", "")
		assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

		setSetting(t, c.st, settings.OIDCEnabled, false)
		c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 403)
	})
}

func TestOIDCRejectsUnexpectedAudiences(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(idp *stubIdP)
		want    string
	}{
		{"an extra audience", func(idp *stubIdP) { idp.audiences = []string{"voltis", "other"} }, "unexpected audience"},
		{"an authorized party for another client", func(idp *stubIdP) {
			idp.claims["azp"] = "other"
		}, "unexpected authorized party"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			tc.arrange(idp)

			assertRejected(t, c.oidcLogin(t), tc.want)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
		})
	}
}

func TestOIDCRejectsATokenWithoutASubject(t *testing.T) {
	for _, subject := range []string{"", "  "} {
		c, idp := newOIDCPair(t)
		idp.subject = subject

		assertRejected(t, c.oidcLogin(t), "did not identify the user")
		assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
	}
}

func TestOIDCRejectsUnsupportedAlgorithms(t *testing.T) {
	for _, alg := range []string{"none", "HS256"} {
		t.Run(alg, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			idp.alg = alg

			assertRejected(t, c.oidcLogin(t), "ID token was rejected")
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
		})
	}
}

func TestOIDCPKCE(t *testing.T) {
	cases := []struct {
		name   string
		mangle string
		want   string
	}{
		{"a verifier that does not match", `"wrong-verifier"`, "invalid code_verifier"},
		{"a missing verifier", `""`, "missing code_verifier"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			query := c.authorize(t, "/api/auth/oidc/login")
			if _, err := c.pool().Exec(context.Background(),
				`UPDATE auth_pending SET data = jsonb_set(data, '{verifier}', $1::jsonb)`, tc.mangle); err != nil {
				t.Fatalf("mangle verifier: %v", err)
			}

			resp := c.callback(query)
			assertRejected(t, resp, "authorization code was rejected")
			assertEq(t, idp.rejection(), tc.want)
			if strings.Contains(resp.Headers.Get("Location"), "code_verifier") {
				t.Fatalf("the upstream error text reached the browser: %s", resp.Headers.Get("Location"))
			}
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
		})
	}
}

func TestOIDCUserinfoFallback(t *testing.T) {
	c, idp := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")
	idp.claims = map[string]any{}
	idp.userinfo = map[string]any{
		"sub":                "sub-1",
		"preferred_username": "fromuserinfo",
		"email":              "Person@Example.com",
		"email_verified":     true,
		"groups":             []string{"admins"},
	}

	assertRedirect(t, c.oidcLogin(t), "/")
	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	assertEq(t, s(me["username"]), "fromuserinfo")
	assertEq(t, hasAdmin(me), true)

	email, err := db.SelectScalar[*string](context.Background(), c.pool(),
		"SELECT email FROM users WHERE username = 'fromuserinfo'")
	if err != nil || email == nil {
		t.Fatalf("read email: %v (%v)", email, err)
	}
	assertEq(t, *email, "person@example.com")
}

func TestOIDCEmailAndVerifiedComeFromOneSource(t *testing.T) {
	c, idp := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthLinkMatchEmail, true)
	local := makeUser(t, c.pool(), "local", "hash", "person@example.com")

	// The ID token carries the email, so userinfo's email_verified is ignored.
	idp.claims = map[string]any{"email": "person@example.com"}
	idp.userinfo = map[string]any{
		"sub": "sub-1", "preferred_username": "idpuser",
		"email": "person@example.com", "email_verified": true,
	}

	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["id"]) == local, false)
}

func TestOIDCPendingRowsAreIsolated(t *testing.T) {
	c, _ := newOIDCPair(t)
	c.authorize(t, "/api/auth/oidc/login")
	raw := c.cookie(pendingCookie)

	stored, err := db.SelectScalar[string](context.Background(), c.pool(), "SELECT id FROM auth_pending")
	if err != nil {
		t.Fatalf("read pending: %v", err)
	}
	if stored == raw || stored != pendingKey(raw) {
		t.Fatal("the table must store a hash of the cookie, not the cookie")
	}

	// A flow row is not a completion.
	c.Get("/api/auth/oidc/pending").Assert(t, 404)
	c.Post("/api/auth/oidc/confirm", map[string]any{"password": "x"}).Assert(t, 404)
}

func TestOIDCPendingExpires(t *testing.T) {
	c, _ := newOIDCPair(t)
	query := c.authorize(t, "/api/auth/oidc/login")
	if _, err := c.pool().Exec(context.Background(),
		"UPDATE auth_pending SET expires_at = NOW() - interval '1 minute'"); err != nil {
		t.Fatalf("expire: %v", err)
	}

	assertRejected(t, c.callback(query), "expired")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)
}

func TestOIDCCompletionIsSingleUse(t *testing.T) {
	c, _ := newOIDCPair(t)
	makeUser(t, c.pool(), "idpuser", "hash", "")
	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")
	raw := c.cookie(pendingCookie)

	results := make(chan int, 4)
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			client := c.newSession(t)
			client.SetCookie(pendingCookie, raw)
			results <- client.Post("/api/auth/oidc/choose-username",
				map[string]any{"username": fmt.Sprintf("chosen%d", i)}).StatusCode
		})
	}
	wg.Wait()
	close(results)

	accepted := 0
	for status := range results {
		if status == 200 {
			accepted++
		}
	}
	assertEq(t, accepted, 1)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 2)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)
}

func TestOIDCRetryKeepsTheOriginalExpiry(t *testing.T) {
	c, _ := newOIDCPair(t)
	makeUser(t, c.pool(), "idpuser", "hash", "")
	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

	expiry := func() time.Time {
		at, err := db.SelectScalar[time.Time](context.Background(), c.pool(), "SELECT expires_at FROM auth_pending")
		if err != nil {
			t.Fatalf("read expiry: %v", err)
		}
		return at
	}
	before := expiry()

	for range 3 {
		c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "idpuser"}).Assert(t, 400)
	}
	if got := expiry(); !got.Equal(before) {
		t.Fatalf("a rejected name extended the pending row from %v to %v", before, got)
	}
}

func TestOIDCSelfLinkRequiresTheSameBrowserSession(t *testing.T) {
	cases := []struct {
		name string
		act  func(t *testing.T, c *testClient)
	}{
		{"no session at all", func(_ *testing.T, c *testClient) { c.SetCookie("voltis_session", "") }},
		{"another session of the same user", func(t *testing.T, c *testClient) {
			c.Post("/api/auth/login", map[string]any{
				"username": "admin", "password": "adminpass123",
			}).Assert(t, 200)
		}},
		{"a session belonging to someone else", func(t *testing.T, c *testClient) {
			c.Post("/api/users/new", map[string]any{
				"username": "other", "password": "otherpass123", "permissions": []string{},
			}).Assert(t, 200)
			c.Post("/api/auth/login", map[string]any{
				"username": "other", "password": "otherpass123",
			}).Assert(t, 200)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newOIDCPair(t)
			c.Post("/api/auth/register", map[string]any{
				"username": "admin", "password": "adminpass123",
			}).Assert(t, 200)

			target := s(c.Post("/api/auth/oidc/link", map[string]any{}).Assert(t, 200).JSON()["url"])
			query := c.authorizeAt(t, target)
			tc.act(t, c)

			assertRejected(t, c.callback(query), "your session changed")
			assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
		})
	}
}

func TestOIDCIssuerChangeBlocksCompletion(t *testing.T) {
	c, _ := newOIDCPair(t)
	makeUser(t, c.pool(), "idpuser", "hash", "")
	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

	setSetting(t, c.st, settings.OIDCIssuer, "https://other-idp.example")
	c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 403)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
}

func TestOIDCMintsAFreshSession(t *testing.T) {
	c, idp := newOIDCPair(t)
	c.Post("/api/auth/register", map[string]any{
		"username": "idpuser", "password": "adminpass123",
	}).Assert(t, 200)
	setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
	idp.claims = map[string]any{"preferred_username": "idpuser"}

	before := c.cookie("voltis_session")
	assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")
	c.Post("/api/auth/oidc/confirm", map[string]any{"password": "adminpass123"}).Assert(t, 200)

	after := c.cookie("voltis_session")
	if after == "" || after == before {
		t.Fatal("the completion reused the session it started with")
	}
	method, err := db.SelectScalar[string](context.Background(), c.pool(),
		"SELECT method FROM sessions WHERE token = $1", after)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	assertEq(t, method, models.SessionOIDC)
}

func TestOIDCUserinfoOutageBlocksGroupMapping(t *testing.T) {
	c, idp := newOIDCPair(t)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")
	idp.claims = map[string]any{"preferred_username": "idpuser"}

	// No groups anywhere and userinfo down: a demotion and an outage look the
	// same, so the sign-in is refused.
	assertRejected(t, c.oidcLogin(t), "group membership could not be checked")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 0)

	// Without a group mapping the same outage is harmless.
	setSetting(t, c.st, settings.AuthAdminGroup, "")
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), false)
}

func TestOIDCUnlinkRacingTheCallback(t *testing.T) {
	c, idp := newOIDCPair(t)
	c.Post("/api/auth/register", map[string]any{
		"username": "idpuser", "password": "adminpass123",
	}).Assert(t, 200)
	userID := meID(t, c)
	ctx := context.Background()
	identity := models.MakeIdentityID()
	if _, err := c.pool().Exec(ctx, `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id)
		VALUES ($1, 'oidc', $2, 'sub-1', $3)
	`, identity, idp.server.URL, userID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	browser := c.newSession(t)
	query := browser.authorize(t, "/api/auth/oidc/login")

	hold, err := c.pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if err := db.LockIdentity(ctx, hold, models.SessionOIDC, idp.server.URL, "sub-1"); err != nil {
		t.Fatalf("hold identity: %v", err)
	}

	done := make(chan *response, 1)
	go func() { done <- browser.callback(query) }()
	waitBlocked(t, c.pool())

	c.Delete("/api/users/me/identities/"+identity).Assert(t, 200)
	if err := hold.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	assertRejected(t, <-done, "the account changed during sign-in")
	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions"), 0)
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/x?a=1":        "/x?a=1",
		"//evil":        "",
		"/\\evil":       "",
		"/\t/evil":      "",
		"https://evil":  "",
		"/%2e%2e//evil": "",
		"evil":          "",
		"/auth/login":   "",
		"/auth":         "",
		"/AUTH/login":   "",
		"":              "",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
