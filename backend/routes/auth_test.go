package routes

import (
	"testing"
)

func TestRegisterAndLogin(t *testing.T) {
	pool := newTestPool(t)
	c := newClient(t, pool)

	// Register
	c.Post("/api/auth/register", map[string]any{
		"username": "testuser", "password": "testpass123",
	}).Assert(t, 200)
	assertEq(t, c.HasCookie("voltis_session"), true)

	c = newClient(t, pool)

	// Login
	c.Post("/api/auth/login", map[string]any{
		"username": "testuser", "password": "testpass123",
	}).Assert(t, 200)
	assertEq(t, c.HasCookie("voltis_session"), true)
}
