package routes

import (
	"context"
	"net/http"
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

// Call before seeding sessions: the issuer transition revokes oidc sessions.
func enableOIDC(t *testing.T, st *settings.Store, issuer string) {
	t.Helper()
	setSetting(t, st, settings.AppPublicURL, "http://voltis.example")
	setSetting(t, st, settings.OIDCIssuer, issuer)
	setSetting(t, st, settings.OIDCClientID, "voltis")
	setSetting(t, st, settings.OIDCEnabled, true)
}

func TestUnlinkKeepsALoginMethod(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	enableOIDC(t, c.st, "https://idp.example")
	userID := meID(t, c)
	ctx := context.Background()
	only := seedIdentity(t, c, userID, "sub-1")

	// A password is still a way in, so the last identity may go.
	c.Delete("/api/users/me/identities/"+only).Assert(t, 200)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)

	c.Post("/api/auth/login", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)
	again := seedIdentity(t, c, userID, "sub-2")
	if _, err := pool.Exec(ctx, "UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}

	c.Delete("/api/users/me/identities/"+again).Assert(t, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
	c.Get("/api/users/me").Assert(t, 200)

	// An identity from an old issuer can no longer log in.
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id)
		VALUES ($1, 'oidc', 'https://old-idp.example', 'sub-3', $2)
	`, models.MakeIdentityID(), userID); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	c.Delete("/api/users/me/identities/"+again).Assert(t, 400)

	// Nor can one while the OIDC configuration is incomplete.
	current := seedIdentity(t, c, userID, "sub-4")
	setSetting(t, c.st, settings.AppPublicURL, "")
	c.Delete("/api/users/me/identities/"+again).Assert(t, 400)
	setSetting(t, c.st, settings.AppPublicURL, "http://voltis.example")
	c.Delete("/api/users/me/identities/"+current).Assert(t, 200)

	// Nor can a password while password login is disabled.
	if _, err := pool.Exec(ctx,
		"UPDATE users SET password_hash = $1 WHERE id = $2", hashOf(t, "adminpass123"), userID); err != nil {
		t.Fatalf("set password: %v", err)
	}
	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))
	setSetting(t, c.st, settings.AuthPasswordLoginEnabled, false)

	external.Delete("/api/users/me/identities/"+again).Assert(t, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 2)
}

func TestConcurrentUnlinksKeepOne(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	enableOIDC(t, c.st, "https://idp.example")
	userID := meID(t, c)
	first := seedIdentity(t, c, userID, "sub-1")
	second := seedIdentity(t, c, userID, "sub-2")
	ctx := context.Background()
	if _, err := pool.Exec(ctx, "UPDATE users SET password_hash = NULL WHERE id = $1", userID); err != nil {
		t.Fatalf("clear password: %v", err)
	}
	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))

	// The first unlink, held open after its check passed on the second identity.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lockUser(ctx, tx, userID); err != nil {
		t.Fatalf("lock user: %v", err)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM user_identities WHERE id = $1", first); err != nil {
		t.Fatalf("unlink: %v", err)
	}
	if err := requireLoginMethod(ctx, tx, userID, false); err != nil {
		t.Fatalf("first check: %v", err)
	}

	done := make(chan int, 1)
	go func() { done <- external.Delete("/api/users/me/identities/" + second).StatusCode }()

	waitBlockedOn(t, pool, "FROM users WHERE id = $1 FOR UPDATE")
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	assertEq(t, <-done, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

// A callback that resolved the identity before the unlink must not leave a
// session behind it.
func TestUnlinkRevokesARacingOIDCLogin(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	enableOIDC(t, c.st, "https://idp.example")
	userID := meID(t, c)
	identity := seedIdentity(t, c, userID, "sub-1")
	ctx := context.Background()

	// Holds the callback at its session insert, after it resolved the identity.
	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, "LOCK TABLE sessions IN SHARE MODE"); err != nil {
		t.Fatalf("lock sessions: %v", err)
	}

	o := &OIDCRoutes{res: &resolver{pool: pool, st: c.st, hub: c.hub}}
	login := make(chan error, 1)
	go func() {
		_, err := o.finalize(ctx, finalizeRequest{Op: finalizeLogin, ID: oidcIdentity("sub-1"), ClientID: "voltis"})
		login <- err
	}()
	waitBlockedOn(t, pool, "INSERT INTO sessions")

	ur := &UserRoutes{pool: pool, hub: c.hub, st: c.st}
	unlink := make(chan error, 1)
	go func() { unlink <- ur.unlink(ctx, userID, identity) }()
	waitBlockedOn(t, pool, "FROM settings_version FOR SHARE")
	if err := hold.Rollback(ctx); err != nil {
		t.Fatalf("release: %v", err)
	}

	if err := <-login; err != nil {
		t.Fatalf("login: %v", err)
	}
	if err := <-unlink; err != nil {
		t.Fatalf("unlink: %v", err)
	}
	assertEq(t, countRows(t, c, "SELECT count(*) FROM sessions"), 0)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 0)
}

func TestLoginPolicyLockOrderSurvivesAConcurrentDisable(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	userID := meID(t, c)
	identity := seedIdentity(t, c, userID, "sub-1")
	ctx := context.Background()
	external := c.newSession(t)
	external.SetCookie("voltis_session",
		insertSession(t, pool, userID, models.SessionOIDC, time.Now().Add(time.Hour), nil))

	disable, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = disable.Rollback(ctx) }()
	if err := settings.WriteTx(ctx, disable, settings.AuthPasswordLoginEnabled, false); err != nil {
		t.Fatalf("disable: %v", err)
	}

	result := make(chan int, 1)
	go func() { result <- external.Delete("/api/users/me/identities/" + identity).StatusCode }()

	waitBlockedOn(t, pool, "FROM settings_version FOR SHARE")
	if err := disable.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The unlink waited for the disable and then saw the password as unusable.
	assertEq(t, <-result, 400)
	assertEq(t, countRows(t, c, "SELECT count(*) FROM user_identities"), 1)
}

func TestAdminManagesIdentities(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)
	enableOIDC(t, admin.st, "https://idp.example")
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

	// Not even an admin may leave an account with no way in.
	ids := identityIDs(t, admin, "/api/users/"+memberID+"/identities")
	admin.Delete("/api/users/"+memberID+"/identities/"+ids[0]).Assert(t, 400)
	assertEq(t, countRows(t, admin, "SELECT count(*) FROM user_identities"), 1)

	admin.Post("/api/users/"+memberID, map[string]any{
		"username": "member", "password": "memberpass123", "permissions": []string{},
	}).Assert(t, 200)
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

	waitBlockedOn(t, pool, "username = COALESCE($1::text, username)")
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
