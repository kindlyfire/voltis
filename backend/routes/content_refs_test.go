package routes

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"voltis/db"
)

func TestBrokenRefsSeriesMove(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)
	_, memberID := newMemberClient(t, c)
	const (
		lb = "comic/Lantern Bay_2011"
		hl = "comic/Harbor Lights_2011"
	)

	f.exec(`INSERT INTO content (id, library_id, uri_part, uri, type) VALUES
		('c_lb', $1, 'Lantern Bay_2011', $2, 'comic_series'), ('c_qo', $1, 'Quiet Orchard', 'book/Quiet Orchard', 'book')`,
		f.libID, lb)
	for i, part := range []string{"ch1", "v1_ch2", "v1_ch3", "v2_ch3", "ch4"} {
		f.exec(`INSERT INTO content (id, library_id, parent_id, uri_part, uri, type, "order")
			VALUES ('c_' || $3, $1, 'c_lb', $3, $2 || '/' || $3, 'comic', $4)`, f.libID, lb, part, i)
	}
	row := func(id, userID, uri string) {
		f.exec("INSERT INTO user_to_content (id, user_id, library_id, uri) VALUES ($1, $2, $3, $4)",
			id, userID, f.libID, uri)
	}
	for id, uri := range map[string]string{"u_root": hl, "u_ch2": hl + "/ch2", "u_ch3": hl + "/ch3",
		"u_v4": hl + "/v4", "u_gone": "book/Gone Stem"} {
		row(id, f.userID, uri)
	}
	row("u_m2", memberID, hl+"/ch2")
	// The member's rows and entries at my destinations stay theirs.
	f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status) VALUES
		('u_mlive', $1, $2, $3 || '/ch1', 'on_hold'), ('u_mdst', $1, $2, $3 || '/v1_ch2', 'on_hold')`,
		memberID, f.libID, lb)
	f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status, last_read_at, starred, notes)
		VALUES ('u_ch1', $1, $2, $3, 'reading', $4, true, 'old note')`, f.userID, f.libID, hl+"/ch1", f.at(2))
	f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status, last_read_at, rating)
		VALUES ('u_live', $1, $2, $3, 'completed', $4, 4)`, f.userID, f.libID, lb+"/ch1", f.at(1))
	f.exec(`INSERT INTO custom_lists (id, name, visibility, user_id, updated_at) VALUES
		('cl_a', 'List A', 'private', $1, $3), ('cl_b', 'List B', 'private', $1, $3), ('cl_m', 'Theirs', 'private', $2, $3)`,
		f.userID, memberID, f.at(-100))
	f.exec(`INSERT INTO custom_list_to_content (id, custom_list_id, library_id, uri, notes, "order") VALUES
		('e_a1', 'cl_a', $1, $2 || '/ch1', 'moved note', 5), ('e_a0', 'cl_a', $1, $3 || '/ch1', NULL, 1),
		('e_gone', 'cl_a', $1, 'book/Gone Stem', NULL, 2), ('e_b2', 'cl_b', $1, $2 || '/ch2', NULL, 1),
		('e_b4', 'cl_b', $1, $2 || '/ch4', NULL, 2), ('e_m2', 'cl_m', $1, $2 || '/ch2', NULL, 1),
		('e_mdst', 'cl_m', $1, $3 || '/v1_ch2', NULL, 2)`, f.libID, hl, lb)

	base := "/api/content/broken-refs/" + f.libID
	query := func(sql string, args ...any) string {
		t.Helper()
		v, err := db.SelectScalar[string](context.Background(), pool, sql, args...)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return v
	}
	// uris lists the URIs of the rows or entries with these ids, in id order.
	uris := func(table string, ids ...string) string {
		t.Helper()
		return query("SELECT string_agg(uri, ',' ORDER BY id) FROM "+table+" WHERE id = ANY($1)", ids)
	}
	ids := func(group map[string]any, key string) string {
		var out []string
		for _, it := range group[key].([]any) {
			out = append(out, s(it.(map[string]any)["id"]))
		}
		return strings.Join(out, ",")
	}

	assertEq(t, s(c.Get("/api/content/broken-refs").Assert(t, 200).JSONArray()),
		s([]map[string]any{{"library_id": f.libID, "count": 2}}))
	page := c.Get(base+"?search=ch3").Assert(t, 200).JSON()
	assertEq(t, s(page["total"]), "1")
	group := page["data"].([]any)[0].(map[string]any)
	assertEq(t, group["uri"], any(hl))
	assertEq(t, ids(group, "items"), "u_root,u_ch1,u_ch2,u_ch3,u_v4")
	assertEq(t, ids(group, "list_entries"), "e_a1,e_b2,e_b4")
	assertEq(t, s(group["suggestions"].([]any)[0]), s(map[string]any{"uri": lb, "matched": 3, "total": 5, "same": false}))

	preview := func(body map[string]any) (string, map[string]any) {
		t.Helper()
		res := c.Post(base+"/preview", body).Assert(t, 200).JSON()
		var pairs []string
		for _, p := range res["pairs"].([]any) {
			p := p.(map[string]any)
			pairs = append(pairs, fmt.Sprintf("%s>%s:%s:%v:%v", strings.TrimPrefix(s(p["from"]), hl),
				strings.TrimPrefix(s(p["to"]), lb), p["by"], p["existing"] != nil, p["list_merges"]))
		}
		return strings.Join(pairs, ",") + " left " + s(res["leftovers"]) + " free " + s(res["free_targets"]), res
	}
	got, _ := preview(map[string]any{"from": hl, "to": lb})
	assertEq(t, got, ">:root:false:0,/ch1>/ch1:exact:true:1,/ch2>/v1_ch2:number:false:0,/ch4>/ch4:exact:false:0"+
		" left ["+hl+"/ch3 "+hl+"/v4] free ["+lb+"/v1_ch3 "+lb+"/v2_ch3]")
	got, res := preview(map[string]any{"from": hl, "to": lb, "overrides": map[string]any{hl + "/ch3": lb + "/v2_ch3"}})
	assertEq(t, strings.Split(got, " left ")[0], ">:root:false:0,/ch1>/ch1:exact:true:1,/ch2>/v1_ch2:number:false:0,"+
		"/ch3>/v2_ch3:manual:false:0,/ch4>/ch4:exact:false:0")
	pairs := map[string]string{}
	for _, p := range res["pairs"].([]any) {
		p := p.(map[string]any)
		pairs[s(p["from"])] = s(p["to"])
	}

	// A shared destination, a live source, and a live entry to delete next to a valid move.
	save := func(move map[string]string, entries ...string) *response {
		return c.Post(base, map[string]any{"moves": []any{map[string]any{"pairs": move}},
			"delete": map[string]any{"list_entries": entries}})
	}
	save(map[string]string{hl + "/ch2": lb + "/ch4", hl + "/ch3": lb + "/ch4"}).Assert(t, 409)
	save(map[string]string{lb + "/ch1": lb + "/ch4"}).Assert(t, 409)
	save(map[string]string{hl + "/ch2": lb + "/v1_ch2"}, "e_a0").Assert(t, 409)
	assertEq(t, uris("user_to_content", "u_ch2"), hl+"/ch2")
	assertEq(t, uris("custom_list_to_content", "e_a0", "e_b2"), lb+"/ch1,"+hl+"/ch2")

	res = c.Post(base, map[string]any{
		"moves":  []any{map[string]any{"keep": "target", "pairs": pairs}},
		"delete": map[string]any{"refs": []string{"u_gone"}, "list_entries": []string{"e_gone"}},
	}).Assert(t, 200).JSON()
	assertEq(t, s(res), s(map[string]any{"moved": 5, "merged": 2, "deleted": 2}))

	// The existing row keeps its state; stars and notes fill in from the moved one.
	assertEq(t, query(`SELECT string_agg(concat_ws(' ', id, status, starred, notes, rating, revision LIKE 'srv:%'), ',')
		FROM user_to_content WHERE uri = $1 AND user_id = $2`, lb+"/ch1", f.userID), "u_live completed t old note 4 t")
	assertEq(t, uris("user_to_content", "u_ch2", "u_ch3", "u_gone", "u_root"), lb+"/v1_ch2,"+lb+"/v2_ch3,"+lb)
	assertEq(t, query("SELECT bool_and(revision LIKE 'srv:%')::text FROM user_to_content WHERE id = ANY($1)",
		[]string{"u_root", "u_ch2", "u_ch3"}), "true")
	assertEq(t, query(`SELECT string_agg(concat_ws(' ', id, uri, "order", notes), ',' ORDER BY id)
		FROM custom_list_to_content WHERE custom_list_id IN ('cl_a', 'cl_b')`),
		"e_a0 "+lb+"/ch1 1 moved note,e_b2 "+lb+"/v1_ch2 1,e_b4 "+lb+"/ch4 2")
	assertEq(t, query(`SELECT string_agg(concat_ws(' ', id, uri, status), ',' ORDER BY id)
		FROM user_to_content WHERE user_id = $1`, memberID),
		"u_m2 "+hl+"/ch2,u_mdst "+lb+"/v1_ch2 on_hold,u_mlive "+lb+"/ch1 on_hold")
	assertEq(t, uris("custom_list_to_content", "e_m2", "e_mdst"), hl+"/ch2,"+lb+"/v1_ch2")
	assertEq(t, query("SELECT string_agg(id || ' ' || (updated_at > $1), ',' ORDER BY id) FROM custom_lists",
		f.at(-100)), "cl_a true,cl_b true,cl_m false")

	page = c.Get(base).Assert(t, 200).JSON()
	assertEq(t, s(page["total"]), "1")
	group = page["data"].([]any)[0].(map[string]any)
	assertEq(t, ids(group, "items")+" "+ids(group, "list_entries"), "u_v4 ")
}
