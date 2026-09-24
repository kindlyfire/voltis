package routes

import (
	"context"
	"sync"
	"testing"

	"slices"

	"voltis/db"
	"voltis/models"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
)

func newResolver(t *testing.T, pool *pgxpool.Pool) *resolver {
	t.Helper()
	return &resolver{pool: pool, st: newStore(t, pool), hub: NewHub()}
}

func makeUser(t *testing.T, pool *pgxpool.Pool, username, password, email string) string {
	t.Helper()
	id := models.MakeUserID()
	if _, err := pool.Exec(context.Background(),
		"INSERT INTO users (id, username, password_hash, email) VALUES ($1, $2, $3, $4)",
		id, username, nullable(password), nullable(email)); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

func makeIdentity(t *testing.T, pool *pgxpool.Pool, userID string, id ExternalIdentity) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO user_identities (id, provider, issuer, subject, user_id)
		VALUES ($1, $2, $3, $4, $5)
	`, models.MakeIdentityID(), id.Provider, id.Issuer, id.Subject, userID); err != nil {
		t.Fatalf("insert identity: %v", err)
	}
}

func oidcIdentity(subject string) ExternalIdentity {
	return ExternalIdentity{Provider: models.SessionOIDC, Issuer: "https://idp.example", Subject: subject}
}

func proxyIdentityFor(name string) ExternalIdentity {
	return ExternalIdentity{Provider: models.SessionProxy, Subject: name, Username: name, EmailVerified: true}
}

func resolve(t *testing.T, r *resolver, id ExternalIdentity) externalLogin {
	t.Helper()
	out, err := r.resolveExternalLogin(context.Background(), id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return out
}

func TestExternalExistingIdentityLogsIn(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	userID := makeUser(t, pool, "alice", "", "")
	id := oidcIdentity("sub-1")
	id.Username = "someone-else"
	makeIdentity(t, pool, userID, id)

	out := resolve(t, r, id)
	if out.User == nil || out.User.ID != userID {
		t.Fatalf("got %+v, want the linked user", out)
	}
}

func TestExternalEmailMatch(t *testing.T) {
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
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			setSetting(t, r.st, settings.AuthLinkMatchEmail, c.enabled)
			userID := makeUser(t, pool, "alice", "hash", "Alice@Example.com")

			id := oidcIdentity("sub-1")
			id.Email, id.EmailVerified, id.Username = "alice@example.com", c.verified, "alice-idp"

			out := resolve(t, r, id)
			if c.linked {
				if out.User == nil || out.User.ID != userID {
					t.Fatalf("got %+v, want the matched account", out)
				}
				return
			}
			if out.User != nil && out.User.ID == userID {
				t.Fatal("linked an account it should not have matched")
			}
		})
	}
}

func TestExternalUsernameMatch(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		password string
		existing bool
		wantLink bool
		wantNeed string
	}{
		{"proxy links", models.SessionProxy, "hash", false, true, ""},
		{"oidc confirms", models.SessionOIDC, "hash", false, false, needsConfirm},
		{"oidc claims a pre-created account", models.SessionOIDC, "", false, true, ""},
		{"oidc declines an account already linked", models.SessionOIDC, "", true, false, needsPickUsername},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			setSetting(t, r.st, settings.AuthLinkMatchUsername, true)
			userID := makeUser(t, pool, "alice", c.password, "")
			if c.existing {
				makeIdentity(t, pool, userID, oidcIdentity("other-sub"))
			}

			id := oidcIdentity("sub-1")
			if c.provider == models.SessionProxy {
				id = proxyIdentityFor("alice")
			}
			id.Username = "alice"

			out := resolve(t, r, id)
			assertEq(t, out.Needs, c.wantNeed)
			if c.wantLink != (out.User != nil && out.User.ID == userID) {
				t.Fatalf("got %+v, want linked=%v", out, c.wantLink)
			}
			if c.wantNeed != "" && (out.Match == nil || out.Match.ID != userID) {
				t.Fatalf("got match %+v, want the matched account", out.Match)
			}
		})
	}
}

func TestExternalAutoCreate(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)

	id := oidcIdentity("sub-1")
	id.Username, id.Email, id.EmailVerified = "newcomer", "New@Example.com", true

	out := resolve(t, r, id)
	if out.User == nil {
		t.Fatalf("got %+v, want a new user", out)
	}
	assertEq(t, out.User.Username, "newcomer")
	assertEq(t, len(out.User.Permissions), 0)

	again := resolve(t, r, id)
	assertEq(t, again.User.ID, out.User.ID)
}

func TestExternalAutoCreateOffRejects(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	setSetting(t, r.st, settings.AuthExternalAutoCreate, false)

	id := oidcIdentity("sub-1")
	id.Username = "newcomer"
	if _, err := r.resolveExternalLogin(context.Background(), id); err == nil {
		t.Fatal("expected a rejection")
	}
}

func TestExternalUsernameTaken(t *testing.T) {
	for _, provider := range []string{models.SessionOIDC, models.SessionProxy} {
		t.Run(provider, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			makeUser(t, pool, "alice", "hash", "")

			id := oidcIdentity("sub-1")
			if provider == models.SessionProxy {
				id = proxyIdentityFor("alice")
			}
			id.Username = "alice"

			out, err := r.resolveExternalLogin(context.Background(), id)
			if provider == models.SessionProxy {
				if err == nil {
					t.Fatal("expected the proxy login to be rejected")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			assertEq(t, out.Needs, needsPickUsername)
			assertEq(t, out.User == nil, true)
		})
	}
}

func TestExternalConcurrentFirstLogins(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	id := oidcIdentity("sub-1")
	id.Username = "newcomer"

	var wg sync.WaitGroup
	results := make([]externalLogin, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Go(func() {
			results[i], errs[i] = r.resolveExternalLogin(context.Background(), id)
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
		if results[i].User == nil || results[i].User.ID != results[0].User.ID {
			t.Fatalf("resolve %d produced a different user: %+v", i, results[i])
		}
	}

	users, err := db.SelectScalar[int](context.Background(), pool, "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	assertEq(t, users, 1)

	identities, err := db.SelectScalar[int](context.Background(), pool, "SELECT count(*) FROM user_identities")
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	assertEq(t, identities, 1)
}

func TestExternalEmailSyncSkipsCollisions(t *testing.T) {
	pool := newTestPool(t)
	ctx := context.Background()
	makeUser(t, pool, "taken", "", "shared@example.com")
	userID := makeUser(t, pool, "alice", "", "")

	user, err := db.SelectOne[models.User](ctx, pool, "SELECT * FROM users WHERE id = $1", userID)
	if err != nil {
		t.Fatalf("read user: %v", err)
	}

	syncEmail(ctx, pool, &user, "Shared@Example.com")
	if user.Email != nil {
		t.Fatalf("synced a colliding email: %v", *user.Email)
	}

	syncEmail(ctx, pool, &user, "Alice@Example.com")
	if user.Email == nil || *user.Email != "alice@example.com" {
		t.Fatalf("got %v, want the normalized email", user.Email)
	}
	stored, err := db.SelectScalar[*string](ctx, pool, "SELECT email FROM users WHERE id = $1", userID)
	if err != nil || stored == nil || *stored != "alice@example.com" {
		t.Fatalf("got %v (%v), want the email to be stored", stored, err)
	}
}

func TestExternalAdminSync(t *testing.T) {
	cases := []struct {
		name      string
		groups    []string
		hasGroups bool
		start     []string
		wantAdmin bool
	}{
		{"grants", []string{"admins", "users"}, true, nil, true},
		{"revokes", []string{"users"}, true, []string{"ADMIN"}, false},
		{"leaves an absent claim alone", nil, false, []string{"ADMIN"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			setSetting(t, r.st, settings.AuthAdminGroup, "admins")
			ctx := context.Background()

			userID := makeUser(t, pool, "alice", "", "")
			if c.start != nil {
				if _, err := pool.Exec(ctx, "UPDATE users SET permissions = $1 WHERE id = $2", c.start, userID); err != nil {
					t.Fatalf("seed permissions: %v", err)
				}
			}
			user, err := db.SelectOne[models.User](ctx, pool, "SELECT * FROM users WHERE id = $1", userID)
			if err != nil {
				t.Fatalf("read user: %v", err)
			}

			id := oidcIdentity("sub-1")
			id.Groups, id.HasGroups = c.groups, c.hasGroups
			if err := r.syncAdmin(ctx, &user, id); err != nil {
				t.Fatalf("sync: %v", err)
			}

			stored, err := db.SelectScalar[bool](ctx, pool,
				"SELECT permissions @> ARRAY['ADMIN'] FROM users WHERE id = $1", userID)
			if err != nil {
				t.Fatalf("read permissions: %v", err)
			}
			assertEq(t, stored, c.wantAdmin)
			assertEq(t, slices.Contains(user.Permissions, "ADMIN"), c.wantAdmin)
		})
	}
}

func TestParseGroups(t *testing.T) {
	list, ok := parseGroups("admins, users")
	assertEq(t, ok, true)
	assertEq(t, len(list), 2)
	assertEq(t, list[1], "users")

	list, ok = parseGroups([]any{"admins", "users"})
	assertEq(t, ok, true)
	assertEq(t, len(list), 2)

	if _, ok := parseGroups(nil); ok {
		t.Fatal("an absent claim must not look present")
	}
}

func TestExternalRetriesWhenTheUsernameIsClaimedMidFlight(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	ctx := context.Background()

	claim, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = claim.Rollback(ctx) }()
	if _, err := claim.Exec(ctx,
		"INSERT INTO users (id, username) VALUES ('u_other', 'newcomer')"); err != nil {
		t.Fatalf("claim username: %v", err)
	}

	id := oidcIdentity("sub-1")
	id.Username = "newcomer"
	out := make(chan externalLogin, 1)
	errs := make(chan error, 1)
	go func() {
		login, err := r.resolveExternalLogin(ctx, id)
		out <- login
		errs <- err
	}()

	waitBlocked(t, pool)
	if err := claim.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := <-errs; err != nil {
		t.Fatalf("resolve: %v", err)
	}
	login := <-out
	assertEq(t, login.Needs, needsPickUsername)
	assertEq(t, login.User == nil, true)

	users, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM users")
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	assertEq(t, users, 1)
}

func TestExternalPreCreatedAccountIsClaimedOnce(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	setSetting(t, r.st, settings.AuthLinkMatchUsername, true)
	ctx := context.Background()
	userID := makeUser(t, pool, "alice", "", "")

	hold, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = hold.Rollback(ctx) }()
	if _, err := hold.Exec(ctx, "SELECT id FROM users WHERE id = $1 FOR UPDATE", userID); err != nil {
		t.Fatalf("hold row: %v", err)
	}

	logins := make(chan externalLogin, 2)
	errs := make(chan error, 2)
	for _, subject := range []string{"sub-1", "sub-2"} {
		go func() {
			id := oidcIdentity(subject)
			id.Username = "alice"
			login, err := r.resolveExternalLogin(ctx, id)
			logins <- login
			errs <- err
		}()
	}

	waitBlockedN(t, pool, 2)
	if err := hold.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	claimed := 0
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("resolve: %v", err)
		}
		if login := <-logins; login.User != nil {
			claimed++
		}
	}
	assertEq(t, claimed, 1)

	identities, err := db.SelectScalar[int](ctx, pool,
		"SELECT count(*) FROM user_identities WHERE user_id = $1", userID)
	if err != nil {
		t.Fatalf("count identities: %v", err)
	}
	assertEq(t, identities, 1)
}

func TestExternalAdminGrantClosesBootstrap(t *testing.T) {
	pool := newTestPool(t)
	c := newProxiedClient(t, pool)
	setSetting(t, c.st, settings.AuthAdminGroup, "admins")
	assertEq(t, c.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], true)

	granted := c.asProxy("alice", "Remote-Groups", "admins").Get("/api/users/me").Assert(t, 200)
	assertEq(t, hasAdmin(granted.JSON()), true)

	assertEq(t, c.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], false)
	c.newSession(t).Post("/api/auth/register", map[string]any{
		"username": "sneaky", "password": "sneakypass123",
	}).Assert(t, 403)
}
