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
}
