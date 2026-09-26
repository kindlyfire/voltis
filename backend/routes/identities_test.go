package routes

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"
	"voltis/settings"
)

func identityIDs(t *testing.T, c *testClient, path string) []string {
	t.Helper()
	ids := []string{}
	for _, row := range c.Get(path).Assert(t, 200).JSONArray() {
		ids = append(ids, s(row["id"]))
	}
	return ids
}

func seedIdentity(t *testing.T, c *testClient, userID, subject string) string {
	t.Helper()
	id := models.MakeIdentityID()
	if _, err := c.pool().Exec(context.Background(), `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id)
		VALUES ($1, 'oidc', 'https://idp.example', $2, $3)
	`, id, subject, userID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	return id
}

func TestIdentitiesListAndUnlink(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	first := seedIdentity(t, c, userID, "sub-1")
	seedIdentity(t, c, userID, "sub-2")

	assertEq(t, len(identityIDs(t, c, "/api/users/me/identities")), 2)

	conn := c.dialWS(t)
	waitConns(t, c.hub, 1)

	c.Delete("/api/users/me/identities/"+first).Assert(t, 200)

	// Unlinking revokes every session and drops the sockets.
	expectWSClosed(t, conn)
	c.Get("/api/users/me").Assert(t, 401)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions"), 0)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

func TestUnlinkKeepsALoginMethod(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	only := seedIdentity(t, c, userID, "sub-1")

	// A password is still a way in, so the last identity may go.
	c.Delete("/api/users/me/identities/"+only).Assert(t, 200)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)

	c.Post("/api/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)
	again := seedIdentity(t, c, userID, "sub-2")
	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}

	c.Delete("/api/users/me/identities/"+again).Assert(t, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
	c.Get("/api/users/me").Assert(t, 200)
}

func TestUnlinkWithPasswordLoginDisabled(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	only := seedIdentity(t, c, userID, "sub-1")

	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))
	setSetting(t, c.st, settings.AuthPasswordLoginEnabled, false)

	// The password is unusable, so this identity is the only way in.
	external.Delete("/api/users/me/identities/"+only).Assert(t, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

func TestConcurrentUnlinksKeepOne(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	first := seedIdentity(t, c, userID, "sub-1")
	second := seedIdentity(t, c, userID, "sub-2")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}

	// Sessions first: their own foreign key waits would otherwise satisfy the
	// barrier before either unlink reached the user row.
	clients := []*testClient{c.newSession(t), c.newSession(t)}
	for _, client := range clients {
		client.SetCookie("voltis_session",
			insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))
	}

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID); err != nil {
		t.Fatalf("hold row: %v", err)
	}

	results := make(chan int, 2)
	for i, identity := range []string{first, second} {
		go func() {
			results <- clients[i].Delete("/api/users/me/identities/" + identity).StatusCode
		}()
	}

	waitBlockedN(t, pool, 2)
	if err := hold.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	statuses := []int{<-results, <-results}
	slices.Sort(statuses)
	if statuses[0] != 200 || statuses[1] != 400 {
		t.Fatalf("got %v, want one unlink accepted and one refused", statuses)
	}
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

func TestAdminManagesIdentities(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	member := admin.Post("/api/users/new", map[string]any{
		"username": "member", "permissions": []string{},
	}).Assert(t, 200).JSON()
	memberID := s(member["id"])
	assertEq(t, member["has_password"], false)

	linked := admin.Post("/api/users/"+memberID+"/identities", map[string]any{
		"provider": "oidc", "issuer": "https://idp.example", "subject": "sub-1",
	}).Assert(t, 200).JSONArray()
	assertLen(t, linked, 1)

	// The same identity cannot be taken from its owner.
	admin.Post("/api/users/"+meID(t, admin)+"/identities", map[string]any{
		"provider": "oidc", "issuer": "https://idp.example", "subject": "sub-1",
	}).Assert(t, http.StatusConflict)

	admin.Post("/api/users/"+memberID+"/identities", map[string]any{
		"provider": "nonsense", "subject": "x",
	}).Assert(t, 400)

	// An admin may leave an account with no way in: it is theirs to fix.
	ids := identityIDs(t, admin, "/api/users/"+memberID+"/identities")
	admin.Delete("/api/users/"+memberID+"/identities/"+ids[0]).Assert(t, 200)
	assertEq(t, countRows(t, admin, "SELECT count(*) FROM user_identities"), 0)
	admin.Get("/api/users/me").Assert(t, 200)
}

func TestPrecreatedAccountIsClaimedByItsFirstLogin(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	admin.Post("/api/users/new", map[string]any{
		"username": "newcomer", "email": "Newcomer@Example.com", "permissions": []string{},
	}).Assert(t, 200)
	setSetting(t, admin.st, settings.AuthLinkMatchUsername, true)

	r := &resolver{pool: pool, st: admin.st, hub: admin.hub}
	id := oidcIdentity("sub-1")
	id.Username = "newcomer"
	out := resolve(t, r, id)
	if out.User == nil || out.User.Username != "newcomer" {
		t.Fatalf("got %+v, want the pre-created account", out)
	}
}

func TestEmailEditing(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	me := c.Post("/api/users/me", map[string]any{
		"username": "admin", "email": "  Admin@Example.COM ",
	}).Assert(t, 200).JSON()
	assertEq(t, s(me["email"]), "admin@example.com")

	c.Post("/api/users/me", map[string]any{"username": "admin", "email": "nope"}).Assert(t, 400)

	// The unique index is case-insensitive, matching how logins compare.
	other := c.Post("/api/users/new", map[string]any{
		"username": "other", "permissions": []string{},
	}).Assert(t, 200).JSON()
	c.Post("/api/users/"+s(other["id"]), map[string]any{
		"username": "other", "permissions": []string{}, "email": "ADMIN@example.com",
	}).Assert(t, 400)

	cleared := c.Post("/api/users/me", map[string]any{"username": "admin", "email": ""}).Assert(t, 200).JSON()
	assertEq(t, cleared["email"], nil)
}

func TestMeReportsTheSession(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)

	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	assertEq(t, s(me["session_method"]), models.SessionPassword)
	assertEq(t, me["can_logout"], true)
	assertEq(t, me["has_password"], true)

	proxied := c.asProxy("proxied").Get("/api/users/me").Assert(t, 200).JSON()
	assertEq(t, s(proxied["session_method"]), models.SessionProxy)
	assertEq(t, proxied["can_logout"], false)

	setSetting(t, c.st, settings.AuthProxyLogoutURL, "https://sso.example/logout")
	proxied = c.asProxy("proxied").Get("/api/users/me").Assert(t, 200).JSON()
	assertEq(t, proxied["can_logout"], true)
}

func TestDisablingPasswordLoginIsEndToEnd(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	seedIdentity(t, c, userID, "sub-1")

	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))

	c.Post("/api/settings", map[string]any{settings.AuthPasswordLoginEnabled: false}).Assert(t, 200)

	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions WHERE method = 'password'"), 0)
	c.Get("/api/users/me").Assert(t, 401)
	external.Get("/api/users/me").Assert(t, 200)
	external.Post("/api/users/me", map[string]any{
		"username": "admin", "password": "anotherpass123",
	}).Assert(t, 403)
	c.newSession(t).Post("/api/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 403)
}

func TestLoginPolicyLockOrderSurvivesAConcurrentDisable(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	identity := seedIdentity(t, c, userID, "sub-1")
	ctx := context.Background()

	disable, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = disable.Rollback(ctx) }()
	if err := settings.WriteTx(ctx, disable, settings.AuthPasswordLoginEnabled, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))

	result := make(chan int, 1)
	go func() { result <- external.Delete("/api/users/me/identities/" + identity).StatusCode }()

	waitBlocked(t, pool)
	if err := disable.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The unlink serialized behind the disable and then saw the new policy.
	assertEq(t, <-result, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

func TestIdentityAuthorization(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	victim := admin.Post("/api/users/new", map[string]any{
		"username": "victim", "password": "victimpass123", "permissions": []string{},
	}).Assert(t, 200).JSON()
	victimID := s(victim["id"])
	stolen := seedIdentity(t, admin, victimID, "sub-1")

	member, _ := newMemberClient(t, admin)

	// Someone else's identity is not theirs to unlink, even by id.
	member.Delete("/api/users/me/identities/"+stolen).Assert(t, 404)
	member.Get("/api/users/"+victimID+"/identities").Assert(t, 403)
	member.Delete("/api/users/"+victimID+"/identities/"+stolen).Assert(t, 403)
	member.Post("/api/users/"+victimID+"/identities", map[string]any{
		"provider": "oidc", "issuer": "https://idp.example", "subject": "sub-2",
	}).Assert(t, 403)
	member.Get("/api/settings/proxy-auth").Assert(t, 403)

	assertEq(t, countRows(t, admin, "SELECT count(*) FROM user_identities"), 1)
}

func TestEmailCollisionAcrossCase(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	// Stored as the database has it, not as the API would normalize it.
	other := c.Post("/api/users/new", map[string]any{
		"username": "other", "permissions": []string{},
	}).Assert(t, 200).JSON()
	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET email = 'Person@Example.com' WHERE id = $1", s(other["id"])); err != nil {
		t.Fatalf("seed email: %v", err)
	}

	c.Post("/api/users/me", map[string]any{
		"username": "admin", "email": "person@example.com",
	}).Assert(t, 400)
}

func TestEmailRejectsMalformedAddresses(t *testing.T) {
	c := newAdminClient(t, newTestPool(t))

	for _, email := range []string{"@", "a@@b.com", "no-at", "a@b .com", "a@b\nc@d.com", "a@"} {
		c.Post("/api/users/me", map[string]any{
			"username": "admin", "email": email,
		}).Assert(t, 400)
	}
	assertEq(t, c.Get("/api/users/me").Assert(t, 200).JSON()["email"], nil)
}

func TestProfileSaveKeepsAConcurrentPassword(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	seedIdentity(t, c, userID, "sub-1")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}

	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))

	// A rename that read the account before it had a password.
	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx,
		"UPDATE users SET password_hash = $1 WHERE id = $2", hashOf(t, "newpass12345"), userID); err != nil {
		t.Fatalf("set password: %v", err)
	}

	done := make(chan int, 1)
	go func() {
		done <- external.Post("/api/users/me", map[string]any{"username": "renamed"}).StatusCode
	}()

	waitBlocked(t, pool)
	if err := hold.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	assertEq(t, <-done, 200)

	kept, err := db.SelectScalar[*string](ctx, pool, "SELECT password_hash FROM users WHERE id = $1", userID)
	if err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if kept == nil {
		t.Fatal("the profile save restored the password it read at authentication")
	}
	assertEq(t, s(c.Get("/api/users/me").Assert(t, 200).JSON()["username"]), "renamed")
}
