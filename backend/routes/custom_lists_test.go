package routes

import (
	"slices"
	"strings"
	"testing"

	"voltis/lib/fp"
)

func TestCustomListVisibility(t *testing.T) {
	pool := newTestPool(t)
	owner := newAdminClient(t, pool)

	owner.Post("/api/users/new", map[string]any{
		"username": "other", "password": "otherpass123", "permissions": []string{},
	}).Assert(t, 200)

	other := newClient(t, pool)
	other.Post("/api/auth/login", map[string]any{
		"username": "other", "password": "otherpass123",
	}).Assert(t, 200)

	create := func(c *testClient, visibility string) string {
		t.Helper()
		return s(c.Post("/api/custom-lists", map[string]any{
			"name": visibility + " list", "visibility": visibility,
		}).Assert(t, 200).JSON()["id"])
	}

	ownerIDs := map[string]string{}
	for _, v := range []string{"public", "unlisted", "private"} {
		ownerIDs[v] = create(owner, v)
	}
	otherIDs := map[string]string{}
	for _, v := range []string{"public", "private"} {
		otherIDs[v] = create(other, v)
	}

	assertIDs := func(c *testClient, path string, want ...string) {
		t.Helper()
		lists := c.Get(path).Assert(t, 200).JSONArray()
		got := fp.Map(lists, func(l map[string]any) string { return s(l["id"]) })
		slices.Sort(got)
		slices.Sort(want)
		assertEq(t, strings.Join(got, ","), strings.Join(want, ","))
	}

	ownerAll := []string{ownerIDs["public"], ownerIDs["unlisted"], ownerIDs["private"], otherIDs["public"]}
	otherAll := []string{otherIDs["public"], otherIDs["private"], ownerIDs["public"]}

	assertIDs(owner, "/api/custom-lists?user=me", ownerIDs["public"], ownerIDs["unlisted"], ownerIDs["private"])
	assertIDs(owner, "/api/custom-lists?user=others", otherIDs["public"])
	assertIDs(owner, "/api/custom-lists?user=all", ownerAll...)
	assertIDs(owner, "/api/custom-lists", ownerAll...)

	assertIDs(other, "/api/custom-lists?user=me", otherIDs["public"], otherIDs["private"])
	assertIDs(other, "/api/custom-lists?user=others", ownerIDs["public"])
	assertIDs(other, "/api/custom-lists?user=all", otherAll...)
	assertIDs(other, "/api/custom-lists", otherAll...)

	for _, v := range []string{"public", "unlisted", "private"} {
		owner.Get("/api/custom-lists/"+ownerIDs[v]).Assert(t, 200)
	}
	other.Get("/api/custom-lists/"+ownerIDs["public"]).Assert(t, 200)
	other.Get("/api/custom-lists/"+ownerIDs["unlisted"]).Assert(t, 200)
	other.Get("/api/custom-lists/"+ownerIDs["private"]).Assert(t, 404)

	mutate := func(id string, want int) {
		t.Helper()
		message := "Not allowed"
		if want == 404 {
			message = "List not found"
		}
		base := "/api/custom-lists/" + id
		for _, do := range []func() *response{
			func() *response { return other.Post(base, map[string]any{"name": "hijacked", "visibility": "public"}) },
			func() *response { return other.Delete(base) },
			func() *response { return other.Post(base+"/entries", map[string]any{"content_id": "ct_dummy"}) },
			func() *response {
				return other.Post(base+"/entries/reorder", map[string]any{"ctc_ids": []string{"ctc_dummy"}})
			},
			func() *response { return other.Post(base+"/entries/ctc_dummy", map[string]any{"notes": "x"}) },
			func() *response { return other.Delete(base + "/entries/ctc_dummy") },
		} {
			assertEq(t, s(do().Assert(t, want).JSON()["error"]), message)
		}
	}

	mutate(ownerIDs["private"], 404)
	mutate("cl_doesnotexist", 404)
	mutate(ownerIDs["public"], 403)
	mutate(ownerIDs["unlisted"], 403)

	assertIDs(owner, "/api/custom-lists?user=me", ownerIDs["public"], ownerIDs["unlisted"], ownerIDs["private"])
}
