package routes

import (
	"context"
	"encoding/json"
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

func countAdmins(t *testing.T, users []map[string]any) int {
	t.Helper()
	n := 0
	for _, u := range users {
		perms, _ := u["permissions"].([]any)
		for _, p := range perms {
			if s(p) == "ADMIN" {
				n++
				break
			}
		}
	}
	return n
}

func TestUserAdminDemotionGuards(t *testing.T) {
	pool := newTestPool(t)
	a := newAdminClient(t, pool)

	users := a.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 1)
	adminAID := s(users[0]["id"])

	adminB := a.Post("/api/users/new", map[string]any{
		"username": "admin2", "password": "admin2pass123", "permissions": []string{"ADMIN"},
	}).Assert(t, 200).JSON()
	adminBID := s(adminB["id"])

	b := newClient(t, pool)
	b.Post("/api/auth/login", map[string]any{
		"username": "admin2", "password": "admin2pass123",
	}).Assert(t, 200)

	b.Post("/api/users/"+adminAID, map[string]any{
		"username": "admin", "permissions": []string{},
	}).Assert(t, 200)

	a.Post("/api/users/"+adminBID, map[string]any{
		"username": "admin2", "permissions": []string{},
	}).Assert(t, 403)

	users = b.Get("/api/users").Assert(t, 200).JSONArray()
	assertLen(t, users, 2)
	assertEq(t, countAdmins(t, users), 1)

	b.Post("/api/users/"+adminBID, map[string]any{
		"username": "admin2", "permissions": []string{},
	}).Assert(t, 403)

	users = b.Get("/api/users").Assert(t, 200).JSONArray()
	assertEq(t, countAdmins(t, users), 1)
}

type testPrefs struct {
	Libraries map[string]struct {
		Visibility string `json:"visibility"`
	} `json:"libraries"`
	Tutorials struct {
		ComicReader bool `json:"comicReader"`
		BookReader  bool `json:"bookReader"`
	} `json:"tutorials"`
}

// Decodes into typed fields, so a wrong JSON type fails instead of matching a
// stringified comparison.
func prefs(t *testing.T, r *response) testPrefs {
	t.Helper()
	var dto struct {
		Preferences testPrefs `json:"preferences"`
	}
	if err := json.Unmarshal(r.Body, &dto); err != nil {
		t.Fatalf("invalid preferences: %s", string(r.Body))
	}
	return dto.Preferences
}

func rawPrefs(t *testing.T, r *response) string {
	t.Helper()
	var dto struct {
		Preferences json.RawMessage `json:"preferences"`
	}
	if err := json.Unmarshal(r.Body, &dto); err != nil {
		t.Fatalf("invalid response: %s", string(r.Body))
	}
	return string(dto.Preferences)
}

func TestPatchPreferences(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	c.Patch("/api/users/me/preferences", map[string]any{
		"libraries": map[string]any{"x": map[string]any{"visibility": "hide"}},
	}).Assert(t, 200)

	p := prefs(t, c.Patch("/api/users/me/preferences", map[string]any{
		"tutorials": map[string]any{"comicReader": true},
	}).Assert(t, 200))
	assertEq(t, p.Tutorials.ComicReader, true)
	assertEq(t, p.Tutorials.BookReader, false)
	assertEq(t, p.Libraries["x"].Visibility, "hide")

	p = prefs(t, c.Patch("/api/users/me/preferences", map[string]any{
		"tutorials": map[string]any{"bookReader": true},
	}).Assert(t, 200))
	assertEq(t, p.Tutorials.ComicReader, true)
	assertEq(t, p.Tutorials.BookReader, true)
	assertEq(t, p.Libraries["x"].Visibility, "hide")

	// A null member deletes the key.
	p = prefs(t, c.Patch("/api/users/me/preferences", map[string]any{
		"libraries": map[string]any{"x": nil},
	}).Assert(t, 200))
	assertEq(t, len(p.Libraries), 0)
	assertEq(t, p.Tutorials.ComicReader, true)
	assertEq(t, p.Tutorials.BookReader, true)

	for _, body := range []string{"", "null", "[1]", `"str"`, "{", "{} {}", "{} garbage"} {
		c.PatchRaw("/api/users/me/preferences", body).Assert(t, 400)
	}

	c.newSession(t).Patch("/api/users/me/preferences", map[string]any{}).Assert(t, 401)

	// POST /users/me no longer touches preferences, even when its body
	// conflicts with everything stored.
	c.Patch("/api/users/me/preferences", map[string]any{
		"libraries": map[string]any{"z": map[string]any{"visibility": "overflow"}},
	}).Assert(t, 200)
	before := rawPrefs(t, c.Get("/api/users/me").Assert(t, 200))

	c.Post("/api/users/me", map[string]any{
		"username": "admin",
		"preferences": map[string]any{
			"tutorials": map[string]any{"comicReader": false, "bookReader": false},
			"libraries": map[string]any{"z": map[string]any{"visibility": "hide"}},
		},
	}).Assert(t, 200)
	assertEq(t, rawPrefs(t, c.Get("/api/users/me").Assert(t, 200)), before)
}

// The column holds arbitrary JSON, which the old POST /users/me accepted. A
// non-object must be replaced, not 500.
func TestPatchPreferencesOverNonObject(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	me := c.Get("/api/users/me").Assert(t, 200).JSON()
	if _, err := pool.Exec(context.Background(),
		"UPDATE users SET preferences = $1 WHERE id = $2", json.RawMessage(`[1,2]`), s(me["id"])); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p := prefs(t, c.Patch("/api/users/me/preferences", map[string]any{
		"tutorials": map[string]any{"comicReader": true},
	}).Assert(t, 200))
	assertEq(t, p.Tutorials.ComicReader, true)
}
