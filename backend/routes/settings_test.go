package routes

import (
	"context"
	"testing"
	"time"

	"voltis/cmd"
	"voltis/settings"

	"github.com/labstack/echo/v4"
)

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 500 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func settingByKey(t *testing.T, r *response, key string) map[string]any {
	t.Helper()
	for _, s := range r.JSONArray() {
		if s["key"] == key {
			return s
		}
	}
	t.Fatalf("setting %s not listed", key)
	return nil
}

func TestSettingsListHidesSecretsAndInternals(t *testing.T) {
	c := newAdminClient(t, newTestPool(t))

	resp := c.Get("/api/settings").Assert(t, 200)
	for _, s := range resp.JSONArray() {
		if s["key"] == settings.BootstrapCompleted {
			t.Fatal("internal settings must not be listed")
		}
	}

	secret := settingByKey(t, resp, settings.OIDCClientSecret)
	assertEq(t, secret["value"], nil)
	assertEq(t, secret["set"], false)
	if _, ok := settingByKey(t, resp, settings.AppPublicURL)["set"]; ok {
		t.Fatal("set is meaningful for secrets only")
	}

	c.Post("/api/settings", map[string]any{settings.OIDCClientSecret: "hunter2"}).Assert(t, 200)

	secret = settingByKey(t, c.Get("/api/settings").Assert(t, 200), settings.OIDCClientSecret)
	assertEq(t, secret["value"], nil)
	assertEq(t, secret["set"], true)
	assertEq(t, c.st.String(settings.OIDCClientSecret), "hunter2")
}

func TestSettingsUpdate(t *testing.T) {
	c := newAdminClient(t, newTestPool(t))

	c.Post("/api/settings", map[string]any{
		settings.AuthRegistrationEnabled:    true,
		settings.AppPublicURL:               "https://voltis.example/",
		settings.AuthExternalSessionMaxDays: 7,
		settings.OIDCScopes:                 []string{"openid", "email"},
	}).Assert(t, 200)

	assertEq(t, c.st.Bool(settings.AuthRegistrationEnabled), true)
	assertEq(t, c.st.String(settings.AppPublicURL), "https://voltis.example")
	assertEq(t, c.st.Int(settings.AuthExternalSessionMaxDays), 7)
	assertEq(t, c.Get("/api/info").Assert(t, 200).JSON()["registration_enabled"], true)

	resp := c.Get("/api/settings").Assert(t, 200)
	assertEq(t, settingByKey(t, resp, settings.AppPublicURL)["value"], "https://voltis.example")
}

func TestSettingsRejectsBadInput(t *testing.T) {
	c := newAdminClient(t, newTestPool(t))

	cases := []map[string]any{
		{"auth.nope": true},
		{settings.BootstrapCompleted: true},
		{settings.AuthRegistrationEnabled: "true"},
		{settings.AuthExternalSessionMaxDays: 0},
		{settings.AppPublicURL: "nope"},
		{settings.OIDCScopes: []string{"email"}},
	}
	for _, body := range cases {
		c.Post("/api/settings", body).Assert(t, 400)
	}
	assertEq(t, c.st.Bool(settings.AuthRegistrationEnabled), false)
}

func TestSettingsRequireAdminAndJSON(t *testing.T) {
	pool := newTestPool(t)
	admin := newAdminClient(t, pool)

	member, _ := newMemberClient(t, admin)

	member.Get("/api/settings").Assert(t, 403)
	member.Post("/api/settings", map[string]any{settings.AuthRegistrationEnabled: true}).Assert(t, 403)

	form := admin.WithHeader(echo.HeaderContentType, echo.MIMEApplicationForm)
	form.Post("/api/settings", map[string]any{settings.AuthRegistrationEnabled: true}).Assert(t, 415)
	form.Post("/api/auth/logout", nil).Assert(t, 415)
}

func TestSettingsUpdateIsAtomic(t *testing.T) {
	c := newAdminClient(t, newTestPool(t))

	c.Post("/api/settings", map[string]any{
		settings.AuthRegistrationEnabled: true,
		settings.AuthProxyLogoutURL:      "nonsense",
	}).Assert(t, 400)

	assertEq(t, c.st.Bool(settings.AuthRegistrationEnabled), false)
}

func TestCLIWriteReachesRunningServer(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	assertEq(t, c.st.Bool(settings.AuthRegistrationEnabled), false)

	if err := cmd.SetSetting(context.Background(), pool, settings.AuthRegistrationEnabled, "true"); err != nil {
		t.Fatalf("cli set: %v", err)
	}

	eventually(t, "the server to pick up the CLI write", func() bool {
		return c.st.Bool(settings.AuthRegistrationEnabled)
	})
	assertEq(t, c.Get("/api/info").Assert(t, 200).JSON()["registration_enabled"], true)

	if err := cmd.SetSetting(context.Background(), pool, settings.BootstrapCompleted, "false"); err == nil {
		t.Fatal("the CLI must not accept internal keys")
	}
	if err := cmd.SetSetting(context.Background(), pool, settings.AuthExternalSessionMaxDays, "0"); err == nil {
		t.Fatal("the CLI must not accept invalid values")
	}
}

func TestBootstrapStaysClosedAfterRestart(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	assertEq(t, c.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], false)

	if _, err := pool.Exec(context.Background(), "DELETE FROM users"); err != nil {
		t.Fatalf("delete users: %v", err)
	}

	restarted := newClient(t, pool)
	assertEq(t, restarted.Get("/api/info").Assert(t, 200).JSON()["first_user_flow"], false)
	restarted.Post("/api/auth/register", map[string]any{
		"username": "sneaky", "password": "sneakypass123",
	}).Assert(t, 403)
}

func TestSettingsProxyAuthStatus(t *testing.T) {
	c := newProxiedClient(t, newTestPool(t))
	c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	}).Assert(t, 200)

	status := c.Get("/api/settings/proxy-auth").Assert(t, 200).JSON()
	assertEq(t, status["enabled"], true)
	assertEq(t, s(status["user_header"]), "Remote-User")
	assertEq(t, s(status["trusted_cidrs"]), "[10.0.0.0/8]")
}
