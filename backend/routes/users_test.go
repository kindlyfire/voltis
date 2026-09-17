package routes

import (
	"testing"
)

func TestUserCRUD(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	// List (should have admin from registration)
	users := c.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 1)
	assertEq(t, s(users[0]["username"]), "admin")

	// Create
	user := c.Post("/api/users/new", map[string]any{
		"username": "newuser", "password": "newpass123", "permissions": []string{"read"},
	}).Assert(t, 200).JSON()

	assertEq(t, s(user["username"]), "newuser")
	_, hasPassword := user["password"]
	_, hasHash := user["password_hash"]
	assertEq(t, hasPassword, false)
	assertEq(t, hasHash, false)
	userID := s(user["id"])

	// List again
	users = c.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 2)

	// Update
	updated := c.Post("/api/users/"+userID, map[string]any{
		"username": "updateduser", "permissions": []string{"read", "write"},
	}).Assert(t, 200).JSON()

	assertEq(t, s(updated["username"]), "updateduser")

	// Delete
	c.Delete("/api/users/"+userID).Assert(t, 200)

	// Verify gone
	users = c.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 1)

	adminID := s(users[0]["id"])

	plainUser := c.Post("/api/users/new", map[string]any{
		"username": "plain", "password": "plainpass123", "permissions": []string{},
	}).Assert(t, 200).JSON()
	plainID := s(plainUser["id"])

	plain := newClient(t, pool)
	plain.Post("/api/auth/login", map[string]any{
		"username": "plain", "password": "plainpass123",
	}).Assert(t, 200)

	newClient(t, pool).Delete("/api/users/"+plainID).Assert(t, 401)
	c.Delete("/api/users/nonexistent").Assert(t, 404)
	plain.Delete("/api/users/"+adminID).Assert(t, 403)
	plain.Delete("/api/users/"+plainID).Assert(t, 403)

	c.Delete("/api/users/"+plainID).Assert(t, 200)
	plain.Get("/api/users/me").Assert(t, 401)

	c.Delete("/api/users/"+adminID).Assert(t, 403)

	second := c.Post("/api/users/new", map[string]any{
		"username": "admin2", "password": "admin2pass123", "permissions": []string{"ADMIN"},
	}).Assert(t, 200).JSON()
	secondID := s(second["id"])

	c.Delete("/api/users/"+adminID).Assert(t, 200)
	c.Get("/api/users/me").Assert(t, 401)

	other := newClient(t, pool)
	other.Post("/api/auth/login", map[string]any{
		"username": "admin2", "password": "admin2pass123",
	}).Assert(t, 200)
	other.Delete("/api/users/"+secondID).Assert(t, 403)
	users = other.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 1)
}
