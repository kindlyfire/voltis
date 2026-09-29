package routes

import (
	"context"
	"fmt"
	"strings"
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

	// Each login refreshes the identity email; a login without one keeps it.
	identityEmail := func() string {
		email, err := db.SelectScalar[string](context.Background(), c.pool(),
			"SELECT email FROM user_identities WHERE subject = 'sub-1'")
		if err != nil {
			t.Fatalf("read identity email: %v", err)
		}
		return email
	}
	idp.claims["email"] = " Someone@Example.test "
	idp.claims["email_verified"] = true
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, identityEmail(), "someone@example.test")
	delete(idp.claims, "email")
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, identityEmail(), "someone@example.test")
	idp.claims["email"] = "other@example.test"
	idp.claims["email_verified"] = false
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, identityEmail(), "someone@example.test")
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
		keeps   bool // the flow survives for the genuine callback
	}{
		{
			name:   "a state that does not match",
			mangle: func(_ *testClient, _ *stubIdP, q map[string][]string) { q["state"] = []string{"forged"} },
			want:   "state did not match",
			keeps:  true,
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
			assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending") == 1, tc.keeps)
		})
	}
}

func TestOIDCCallbackCannotBeReplayed(t *testing.T) {
	c, _ := newOIDCPair(t)

	query := c.authorize(t, "/api/auth/oidc/login")
	pending := c.cookie(pendingCookie)
	// A cross-site callback cannot cancel the sign-in in progress.
	assertRejected(t, c.Get(oidcCallbackPath+"?error=access_denied&state=forged"), "state did not match")
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
	for _, verified := range []bool{true, false} {
		t.Run(fmt.Sprintf("verified=%v", verified), func(t *testing.T) {
			c, idp := newOIDCPair(t)
			setSetting(t, c.st, settings.AuthLinkMatchEmail, true)
			userID := makeUser(t, c.pool(), "local", "", "person@example.com")
			idp.claims = map[string]any{
				"preferred_username": "idpuser",
				"email":              "Person@Example.com",
				"email_verified":     verified,
			}

			assertRedirect(t, c.oidcLogin(t), "/")
			me := c.Get("/api/users/me").Assert(t, 200).JSON()
			assertEq(t, s(me["id"]) == userID, verified)
			if !verified {
				// An unverified email is not stored anywhere.
				assertEq(t, countRows(t, c, "SELECT count(*) FROM users WHERE username = 'idpuser' AND email IS NULL"), 1)
				assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities WHERE email IS NULL"), 1)
			}
		})
	}
}

func TestOIDCMatchConfirm(t *testing.T) {
	for _, rule := range []string{"username", "email"} {
		t.Run(rule, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			c.Post("/api/auth/register", map[string]any{
				"username": "idpuser", "password": "localpass123",
			}).Assert(t, 200)
			userID := meID(t, c)
			if rule == "email" {
				setSetting(t, c.st, settings.AuthLinkMatchEmail, true)
				if _, err := c.pool().Exec(context.Background(),
					"UPDATE users SET email = 'person@example.com' WHERE id = $1", userID); err != nil {
					t.Fatalf("set email: %v", err)
				}
				idp.claims = map[string]any{
					"preferred_username": "idpuser", "email": "Person@Example.com", "email_verified": true,
				}
			} else {
				setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
			}

			before := c.cookie("voltis_session")
			assertRedirect(t, c.callback(c.authorize(t, "/api/auth/oidc/login?redirect=%2Flists")), "/auth/oidc/complete")
			waiting := c.Get("/api/auth/oidc/pending").Assert(t, 200).JSON()
			assertEq(t, s(waiting["needs"]), needsConfirm)
			assertEq(t, s(waiting["match_username"]), "idpuser")
			assertEq(t, s(waiting["redirect"]), "/lists")
			assertEq(t, waiting["can_create"], true)

			confirm := func(password string) *response {
				return c.Post("/api/auth/oidc/confirm", map[string]any{"password": password})
			}
			if rule == "email" {
				for range maxConfirmAttempts {
					confirm("wrong-password").Assert(t, 401)
				}
				assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)
				confirm("localpass123").Assert(t, 404)
				assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
				return
			}

			confirm("wrong-password").Assert(t, 401)
			c.Get("/api/auth/oidc/pending").Assert(t, 200)
			confirm("localpass123").Assert(t, 200)
			assertEq(t, meID(t, c), userID)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM auth_pending"), 0)

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

			confirm("localpass123").Assert(t, 404)
		})
	}
}

func TestOIDCFinalizeRechecksPolicy(t *testing.T) {
	cases := []struct {
		name string
		run  func(t *testing.T, c *testClient, idp *stubIdP)
	}{
		{"enabled", func(t *testing.T, c *testClient, idp *stubIdP) {
			var err error
			idp.onToken = func() { err = c.st.Set(context.Background(), settings.OIDCEnabled, false) }
			assertRejected(t, c.oidcLogin(t), "no longer available")
			assertEq(t, err, nil)
		}},
		{"issuer", func(t *testing.T, c *testClient, idp *stubIdP) {
			var err error
			idp.onToken = func() { err = c.st.Set(context.Background(), settings.OIDCIssuer, "https://other-idp.example") }
			assertRejected(t, c.oidcLogin(t), "configuration changed")
			assertEq(t, err, nil)
		}},
		{"rule", func(t *testing.T, c *testClient, _ *stubIdP) {
			setSetting(t, c.st, settings.AuthLinkMatchUsername, true)
			makeUser(t, c.pool(), "idpuser", hashOf(t, "localpass123"), "")
			assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

			setSetting(t, c.st, settings.AuthLinkMatchUsername, false)
			c.Post("/api/auth/oidc/confirm", map[string]any{"password": "localpass123"}).Assert(t, 403)
		}},
		{"auto-create", func(t *testing.T, c *testClient, _ *stubIdP) {
			makeUser(t, c.pool(), "idpuser", "hash", "")
			assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")

			setSetting(t, c.st, settings.AuthExternalAutoCreate, false)
			c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 403)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			tc.run(t, c, idp)
			// Seeded accounts have passwords; one made by sign-on has none.
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users WHERE password_hash IS NULL"), 0)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions"), 0)
		})
	}
}

func TestOIDCPickAUsername(t *testing.T) {
	for _, match := range []bool{true, false} {
		t.Run(fmt.Sprintf("match_username=%v", match), func(t *testing.T) {
			c, _ := newOIDCPair(t)
			setSetting(t, c.st, settings.AuthLinkMatchUsername, match)
			makeUser(t, c.pool(), "idpuser", "hash", "")

			assertRedirect(t, c.oidcLogin(t), "/auth/oidc/complete")
			raw := c.cookie(pendingCookie)
			// A match asks to confirm first; declining it leads to the same picker.
			want := needsPickUsername
			if match {
				want = needsConfirm
			}
			assertEq(t, s(c.Get("/api/auth/oidc/pending").Assert(t, 200).JSON()["needs"]), want)

			expiry := func() time.Time {
				at, err := db.SelectScalar[time.Time](context.Background(), c.pool(), "SELECT expires_at FROM auth_pending")
				if err != nil {
					t.Fatalf("read expiry: %v", err)
				}
				return at
			}
			before := expiry()

			// A rejected name rolls back, so the row keeps its original expiry.
			c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "idpuser"}).Assert(t, 400)
			if got := expiry(); !got.Equal(before) {
				t.Fatalf("a rejected name extended the pending row from %v to %v", before, got)
			}
			c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "x"}).Assert(t, 400)
			c.Post("/api/auth/oidc/choose-username", map[string]any{"username": "chosen"}).Assert(t, 200)

			assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "chosen")

			// The completion is single use.
			replay := c.newSession(t)
			replay.SetCookie(pendingCookie, raw)
			replay.Post("/api/auth/oidc/choose-username", map[string]any{"username": "again"}).Assert(t, 404)
			assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 2)
		})
	}
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
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), false)

	idp.claims = map[string]any{"preferred_username": "idpuser", "groups": []string{"admins"}}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), true)

	// No userinfo endpoint: the token is the only source. The settings write
	// drops the cached discovery.
	idp.noUserinfo = true
	setSetting(t, c.st, settings.OIDCButtonLabel, "Fixture SSO")
	idp.claims = map[string]any{"preferred_username": "idpuser"}
	assertRedirect(t, c.oidcLogin(t), "/")
	assertEq(t, hasAdmin(c.Get("/api/users/me").Assert(t, 200).JSON()), false)

	// The grant closed bootstrap, and the demotion does not reopen it.
	restarted := newClient(t, c.pool())
	assertEq(t, restarted.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], false)
	restarted.Post("/api/auth/register", map[string]any{
		"username": "sneaky", "password": "sneakypass123",
	}).Assert(t, 403)
}

func TestOIDCConfigChangeRevokesSessions(t *testing.T) {
	c, idp := newOIDCPair(t)
	assertRedirect(t, c.oidcLogin(t), "/")

	// Another IdP user whose name is taken waits on the username picker.
	waiting := c.newSession(t)
	idp.subject = "sub-2"
	assertRedirect(t, waiting.oidcLogin(t), "/auth/oidc/complete")
	waiting.Get("/api/auth/oidc/pending").Assert(t, 200)

	setSetting(t, c.st, settings.OIDCClientID, "voltis-new")
	c.Get("/api/users/me").Assert(t, 401)
	waiting.Get("/api/auth/oidc/pending").Assert(t, 404)

	setSetting(t, c.st, settings.OIDCClientID, "voltis")
	idp.subject = "sub-1"
	assertRedirect(t, c.oidcLogin(t), "/")
	c.Get("/api/users/me").Assert(t, 200)

	setSetting(t, c.st, settings.OIDCEnabled, false)
	c.Get("/api/users/me").Assert(t, 401)
	assertRejected(t, c.Get("/api/auth/oidc/login"), "not configured")
}

func TestOIDCAudience(t *testing.T) {
	cases := []struct {
		name      string
		audiences []string
		azp       any
		want      string // empty: signs in
	}{
		{"an extra audience without an authorized party", []string{"voltis", "other"}, nil, "unexpected authorized party"},
		{"an extra audience authorized for us", []string{"voltis", "other"}, "voltis", ""},
		{"an authorized party for another client", nil, "other", "unexpected authorized party"},
		{"an authorized party that is not a string", nil, []string{"voltis"}, "unexpected authorized party"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, idp := newOIDCPair(t)
			idp.audiences = tc.audiences
			if tc.azp != nil {
				idp.claims["azp"] = tc.azp
			}

			if tc.want == "" {
				assertRedirect(t, c.oidcLogin(t), "/")
				assertEq(t, countRows(t, c, "SELECT count(*) FROM users"), 1)
				return
			}
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
	// Claimable, so a regression would link it.
	local := makeUser(t, c.pool(), "local", "", "person@example.com")

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
		"/%zz":          "",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q, want %q", in, got, want)
		}
	}
}
