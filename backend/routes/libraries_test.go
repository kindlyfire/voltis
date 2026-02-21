package routes

import (
	"testing"
)

func TestLibraryCRUD(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// Create
	lib := c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": dir1}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(lib["name"]), "test")
	assertEq(t, s(lib["type"]), "comics")
	id := s(lib["id"])

	// List
	libs := c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 1)
	assertEq(t, s(libs[0]["id"]), id)

	// Update
	updated := c.Post("/api/libraries/"+id, map[string]any{
		"name": "test2", "type": "books",
		"sources": []map[string]any{{"path_uri": dir2}},
	}).Assert(t, 200).JSON()

	assertEq(t, s(updated["name"]), "test2")
	assertEq(t, s(updated["type"]), "comics") // type can't change

	// Delete
	c.Delete("/api/libraries/"+id).Assert(t, 200)

	// Verify gone
	libs = c.Get("/api/libraries").Assert(t, 200).JSONArray()
	assertLen(t, libs, 0)
}

func TestLibraryValidation(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	// Invalid source path
	c.Post("/api/libraries/new", map[string]any{
		"name": "test", "type": "comics",
		"sources": []map[string]any{{"path_uri": "/nonexistent/path"}},
	}).Assert(t, 400)

	// Delete nonexistent
	c.Delete("/api/libraries/nonexistent").Assert(t, 404)
}
