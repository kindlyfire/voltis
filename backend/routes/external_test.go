package routes

import (
	"context"
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
	return ExternalIdentity{Provider: models.SessionProxy, Subject: name, Username: name}
}

func resolve(t *testing.T, r *resolver, id ExternalIdentity) externalLogin {
	t.Helper()
	out, err := r.resolveExternalLogin(context.Background(), id)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return out
}

func TestExternalEmailMatch(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		rule     bool
		password string
		want     string // link, confirm or new
	}{
		{"claims a claimable account", models.SessionOIDC, true, "", "link"},
		{"confirms a password account", models.SessionOIDC, true, "hash", "confirm"},
		{"rule off", models.SessionOIDC, false, "", "new"},
		{"proxy skips a password account", models.SessionProxy, true, "hash", "new"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			setSetting(t, r.st, settings.AuthLinkMatchEmail, c.rule)
			userID := makeUser(t, pool, "alice", c.password, "Alice@Example.com")

			id := oidcIdentity("sub-1")
			if c.provider == models.SessionProxy {
				id = proxyIdentityFor("alice-idp")
			}
			id.Email, id.Username = "alice@example.com", "alice-idp"

			out := resolve(t, r, id)
			switch c.want {
			case "link":
				assertEq(t, out.User != nil && out.User.ID == userID, true)
			case "confirm":
				assertEq(t, out.Needs, needsConfirm)
				assertEq(t, out.Match != nil && out.Match.ID == userID, true)
				assertEq(t, out.MatchBy, "email")
			case "new":
				assertEq(t, out.User != nil && out.User.ID != userID, true)
			}
		})
	}
}

func TestExternalUsernameMatch(t *testing.T) {
	cases := []struct {
		name         string
		provider     string
		password     string
		existing     bool
		autoCreateOn bool
		wantLink     bool
		wantNeed     string
		wantErr      bool
	}{
		{"proxy refuses an account already linked", models.SessionProxy, "", true, true, false, "", true},
		{"oidc confirms", models.SessionOIDC, "hash", false, true, false, needsConfirm, false},
		{"oidc claims a pre-created account", models.SessionOIDC, "", false, true, true, "", false},
		{"oidc declines an account already linked", models.SessionOIDC, "", true, true, false, needsPickUsername, false},
		{"oidc without auto-create has no account", models.SessionOIDC, "", true, false, false, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := newTestPool(t)
			r := newResolver(t, pool)
			setSetting(t, r.st, settings.AuthLinkMatchUsername, true)
			setSetting(t, r.st, settings.AuthExternalAutoCreate, c.autoCreateOn)
			userID := makeUser(t, pool, "alice", c.password, "")
			if c.existing {
				makeIdentity(t, pool, userID, oidcIdentity("other-sub"))
			}

			id := oidcIdentity("sub-1")
			if c.provider == models.SessionProxy {
				id = proxyIdentityFor("alice")
			}
			id.Username = "alice"

			out, err := r.resolveExternalLogin(context.Background(), id)
			if c.wantErr {
				if err == nil {
					t.Fatalf("got %+v, want a rejection", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			assertEq(t, out.Needs, c.wantNeed)
			if c.wantLink != (out.User != nil && out.User.ID == userID) {
				t.Fatalf("got %+v, want linked=%v", out, c.wantLink)
			}
			// Only a confirm names the account; a decline must not leak it.
			if (c.wantNeed == needsConfirm) != (out.Match != nil && out.Match.ID == userID) {
				t.Fatalf("got match %+v for %q", out.Match, c.wantNeed)
			}
		})
	}
}

func TestExternalAutoCreate(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)

	id := oidcIdentity("sub-1")
	id.Username, id.Email = "newcomer", "New@Example.com"

	out := resolve(t, r, id)
	if out.User == nil {
		t.Fatalf("got %+v, want a new user", out)
	}
	assertEq(t, out.User.Username, "newcomer")
	assertEq(t, len(out.User.Permissions), 0)

	// A linked identity logs in by its subject, whatever its username now is.
	id.Username = "someone-else"
	again := resolve(t, r, id)
	assertEq(t, again.User.ID, out.User.ID)
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

func TestExternalPreCreatedAccountIsClaimedOnce(t *testing.T) {
	pool := newTestPool(t)
	r := newResolver(t, pool)
	setSetting(t, r.st, settings.AuthLinkMatchUsername, true)
	ctx := context.Background()
	userID := makeUser(t, pool, "alice", "", "")

	// sub-1 claims alice and holds the claim open.
	claim, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = claim.Rollback(ctx) }()
	first := oidcIdentity("sub-1")
	first.Username = "alice"
	if err := db.LockIdentity(ctx, claim, first.Provider, first.Issuer, first.Subject); err != nil {
		t.Fatalf("lock identity: %v", err)
	}
	if out, err := provision(ctx, claim, first, linkPolicy{matchUsername: true}); err != nil || out.User == nil {
		t.Fatalf("first claim: %+v, %v", out, err)
	}

	second := oidcIdentity("sub-2")
	second.Username = "alice"
	done := make(chan externalLogin, 1)
	go func() {
		login, err := r.resolveExternalLogin(ctx, second)
		if err != nil {
			t.Errorf("resolve: %v", err)
		}
		done <- login
	}()

	waitBlockedOn(t, pool, "WHERE username = $1 FOR UPDATE")
	if err := claim.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	assertEq(t, (<-done).User == nil, true)

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
