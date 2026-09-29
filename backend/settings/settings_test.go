package settings

import (
	"context"
	"reflect"
	"testing"

	"voltis/db"
	"voltis/db/dbtest"
)

func TestParseValidation(t *testing.T) {
	cases := []struct {
		key   string
		value any
		want  any
		ok    bool
	}{
		{AuthRegistrationEnabled, true, true, true},
		{AuthRegistrationEnabled, "true", nil, false},
		{AuthExternalSessionMaxDays, float64(7), 7, true},
		{AuthExternalSessionMaxDays, 0, nil, false},
		{AuthExternalSessionMaxDays, 1.5, nil, false},
		{AppPublicURL, "https://voltis.example/", "https://voltis.example", true},
		{AppPublicURL, "not-a-url", nil, false},
		{AppPublicURL, "", "", true},
		{AppPublicURL, "https://voltis.example/?x=1", nil, false},
		{AppPublicURL, "https://voltis.example/#app", nil, false},
		{AppPublicURL, "https://u:p@voltis.example", nil, false},
		{OIDCScopes, []any{"openid", "email"}, []string{"openid", "email"}, true},
		{OIDCScopes, []any{"email"}, nil, false},
		{OIDCScopes, []any{"openid", 3}, nil, false},
		{OIDCButtonLabel, " ", nil, false},
		{OIDCClientSecret, "s3cret", "s3cret", true},
	}
	for _, c := range cases {
		def, ok := Lookup(c.key)
		if !ok {
			t.Fatalf("unknown key %s", c.key)
		}
		got, err := def.Parse(c.value)
		if c.ok != (err == nil) {
			t.Fatalf("%s=%v: err %v, wanted ok=%v", c.key, c.value, err, c.ok)
		}
		if c.ok && !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s=%v: got %#v, want %#v", c.key, c.value, got, c.want)
		}
	}
}

func TestParseString(t *testing.T) {
	cases := []struct {
		key   string
		value string
		want  any
		ok    bool
	}{
		{AuthRegistrationEnabled, "true", true, true},
		{AuthRegistrationEnabled, "yes", nil, false},
		{AuthExternalSessionMaxDays, "7", 7, true},
		{AuthExternalSessionMaxDays, "seven", nil, false},
		{OIDCScopes, "openid, profile email", []string{"openid", "profile", "email"}, true},
	}
	for _, c := range cases {
		def, _ := Lookup(c.key)
		got, err := def.ParseString(c.value)
		if c.ok != (err == nil) {
			t.Fatalf("%s=%q: err %v, wanted ok=%v", c.key, c.value, err, c.ok)
		}
		if c.ok && !reflect.DeepEqual(got, c.want) {
			t.Fatalf("%s=%q: got %#v, want %#v", c.key, c.value, got, c.want)
		}
	}
}

func TestStoreDefaultsAndWrite(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	store, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !store.Bool(AuthPasswordLoginEnabled) || store.Bool(AuthRegistrationEnabled) {
		t.Fatal("unexpected defaults")
	}
	if store.Int(AuthExternalSessionMaxDays) != 30 {
		t.Fatalf("got %d", store.Int(AuthExternalSessionMaxDays))
	}

	if err := store.Set(ctx, AuthRegistrationEnabled, true); err != nil {
		t.Fatalf("set: %v", err)
	}
	if !store.Bool(AuthRegistrationEnabled) {
		t.Fatal("value not applied")
	}
	if store.Version() == 0 {
		t.Fatal("version was not bumped")
	}

	if err := store.Set(ctx, "auth.nope", true); err == nil {
		t.Fatal("expected an unknown key to be rejected")
	}
	if err := store.Set(ctx, AuthExternalSessionMaxDays, 0); err == nil {
		t.Fatal("expected an invalid value to be rejected")
	}

	other, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !other.Bool(AuthRegistrationEnabled) || other.Version() != store.Version() {
		t.Fatal("the second store did not see the write")
	}
}

func TestSeedRegistrationFromEnv(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	t.Setenv("APP_REGISTRATION_ENABLED", "true")

	store, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if !store.Bool(AuthRegistrationEnabled) {
		t.Fatal("the env value was not seeded")
	}

	if err := store.Set(ctx, AuthRegistrationEnabled, false); err != nil {
		t.Fatalf("set: %v", err)
	}
	reloaded, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if reloaded.Bool(AuthRegistrationEnabled) {
		t.Fatal("the env value overwrote the stored one")
	}
}

func TestReloadHooksFireOnEveryVersionAdvance(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	store, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	fired := 0
	store.OnChange(func([]string) { fired++ })

	writer, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := writer.Set(ctx, AuthPasswordLoginEnabled, false); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := writer.Set(ctx, AuthPasswordLoginEnabled, true); err != nil {
		t.Fatalf("set: %v", err)
	}

	if err := store.Reload(ctx); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if fired != 1 {
		t.Fatalf("hooks fired %d times, want 1: a value written twice looks unchanged", fired)
	}
}

func TestSetManyRollsBackEverything(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	store, err := New(ctx, pool)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, username, password_hash) VALUES ('u1', 'u1', 'x');
	`); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO sessions (token, user_id, expires_at, method)
		VALUES ('t1', 'u1', NOW() + interval '1 day', 'password')
	`); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	before := store.Version()

	// SetMany applies keys in sorted order, so the first write succeeds, deletes
	// the password session and bumps the version before the second one fails.
	err = store.SetMany(ctx, map[string]any{
		AuthPasswordLoginEnabled: false,
		AuthProxyLogoutURL:       "nonsense",
	})
	if err == nil {
		t.Fatal("expected the invalid value to fail the patch")
	}

	rows, err := db.SelectScalar[int](ctx, pool,
		"SELECT count(*) FROM settings WHERE key = $1", AuthPasswordLoginEnabled)
	if err != nil {
		t.Fatalf("count settings: %v", err)
	}
	if rows != 0 {
		t.Fatal("the failed patch left a settings row behind")
	}

	version, err := db.SelectScalar[int64](ctx, pool, "SELECT version FROM settings_version")
	if err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != before {
		t.Fatalf("version went from %d to %d despite the rollback", before, version)
	}

	sessions, err := db.SelectScalar[int](ctx, pool, "SELECT count(*) FROM sessions")
	if err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessions != 1 {
		t.Fatal("the transition hook deleted a session the patch rolled back")
	}
}
