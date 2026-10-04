package routes

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"testing"

	"voltis/db/dbtest"
	"voltis/models"
)

func TestFacetRoutes(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)

	// r1 credits "Dana Kell" in three roles; r2 and r3, in the other library, credit "DANA KELL" as
	// writer, so the global name differs from l1's majority. The child and the invalid root never count.
	l1, l2 := models.MakeLibraryID(), models.MakeLibraryID()
	mustExec(t, pool, "INSERT INTO libraries (id, name, type) VALUES ($1, 'one', 'comics'), ($2, 'two', 'comics')", l1, l2)
	mustExec(t, pool, `INSERT INTO content (id, uri_part, uri, type, library_id, parent_id, valid, data) VALUES
		('r1', 'r1', 'file:///one/r1', 'comic_series', $1, NULL, true, '{"title": "Glass Tide",
			"staff": [{"name": "Dana Kell", "role": "writer"}, {"name": "Dana Kell", "role": "penciller"},
				{"name": "Dana Kell", "role": "inker"}, {"name": "Álvar Brisk", "role": "writer"}, {"name": "  "}],
			"genres": ["sci_fi"], "tags": ["Moon Gate"], "publishers": ["Lumen Press"]}'),
		('ch', 'ch', 'file:///one/r1/ch', 'comic', $1, 'r1', true, '{"title": "Glass Tide 1",
			"staff": [{"name": "Wren Child", "role": "writer"}], "genres": ["horror"]}'),
		('inv', 'inv', 'file:///one/inv', 'comic', $1, NULL, false, '{"title": "Broken",
			"staff": [{"name": "Dana Kell", "role": "writer"}], "genres": ["sci_fi"]}'),
		('r2', 'r2', 'file:///two/r2', 'comic', $2, NULL, true, '{"title": "Paper Moons",
			"staff": [{"name": "DANA KELL", "role": "writer"}, {"name": "Álvar Brisk", "role": "artist"},
				{"name": "J. Mira Stavros", "role": "artist"}],
			"genres": ["sci_fi", "drama"], "tags": ["Moon Gate", "Q+"], "publishers": ["Lumen Press"]}'),
		('r3', 'r3', 'file:///two/r3', 'comic', $2, NULL, true, '{"title": "Quiet Orbit",
			"staff": [{"name": "DANA KELL", "role": "writer"}], "genres": ["drama"]}')`, l1, l2)

	t.Run("list", func(t *testing.T) {
		for _, tc := range []struct{ path, want string }{
			{"people", "3 [dana kell:DANA KELL:3 alvar brisk:Álvar Brisk:2 j mira stavros:J. Mira Stavros:1]"},
			{"people?order=asc", "3 [j mira stavros:J. Mira Stavros:1 alvar brisk:Álvar Brisk:2 dana kell:DANA KELL:3]"},
			{"people?sort=name", "3 [alvar brisk:Álvar Brisk:2 dana kell:DANA KELL:3 j mira stavros:J. Mira Stavros:1]"},
			{"people?sort=name&order=desc&limit=2", "3 [j mira stavros:J. Mira Stavros:1 dana kell:DANA KELL:3]"},
			{"people?limit=1&offset=1", "3 [alvar brisk:Álvar Brisk:2]"},
			{"people?offset=10", "3 []"},
			{"people?sort=name&offset=10", "3 []"},
			// Counts are per library, names stay global.
			{"people?library_id=" + l1, "2 [alvar brisk:Álvar Brisk:1 dana kell:DANA KELL:1]"},
			{"people?q=alvar", "1 [alvar brisk:Álvar Brisk:2]"},
			{"people?q=j+mira&sort=name", "1 [j mira stavros:J. Mira Stavros:1]"},
			{"genres?q=sci-fi", "1 [sci fi:sci_fi:2]"},
			{"genres", "2 [drama:drama:2 sci fi:sci_fi:2]"},
			{"tags", "2 [moon gate:Moon Gate:2 q+:Q+:1]"},
			{"publishers", "1 [lumen press:Lumen Press:2]"},
		} {
			res := c.Get("/api/facets/"+tc.path).Assert(t, 200).JSON()
			var rows []string
			for _, r := range res["data"].([]any) {
				r := r.(map[string]any)
				rows = append(rows, fmt.Sprintf("%v:%v:%v", r["key"], r["name"], r["count"]))
			}
			assertEq(t, fmt.Sprintf("%v %v", res["total"], rows), tc.want)
		}
	})

	entry := func(path string) string {
		e := c.Get("/api/facets/"+path).Assert(t, 200).JSON()
		return fmt.Sprintf("%v:%v:%v %v", e["key"], e["name"], e["count"], e["roles"])
	}
	t.Run("entry", func(t *testing.T) {
		for _, tc := range []struct{ path, want string }{
			{"people/dana-kell", "dana kell:DANA KELL:3 [map[count:3 role:writer] map[count:1 role:inker] map[count:1 role:penciller]]"},
			{"people/Dana%20KELL", "dana kell:DANA KELL:3 [map[count:3 role:writer] map[count:1 role:inker] map[count:1 role:penciller]]"},
			{"people/dana-kell?library_id=" + l2, "dana kell:DANA KELL:2 [map[count:2 role:writer]]"},
			{"genres/Sci-Fi", "sci fi:sci_fi:2 []"},
			{"tags/Q%2B", "q+:Q+:1 []"}, // RawPath is set, so the param stays escaped
		} {
			assertEq(t, entry(tc.path), tc.want)
		}
		for _, path := range []string{"colors", "colors/blue", "genres/horror", "tags/lumen-press", "people/nobody"} {
			c.Get("/api/facets/"+path).Assert(t, 404)
		}
	})

	t.Run("facet_keys", func(t *testing.T) {
		res := c.Get("/api/content/r1").Assert(t, 200).JSON()
		keys := res["facet_keys"].(map[string]any)
		assertEq(t, s(keys), s(map[string]any{
			"staff":  []any{"dana kell", "dana kell", "dana kell", "alvar brisk", nil},
			"genres": []any{"sci fi"}, "tags": []any{"moon gate"}, "publishers": []any{"lumen press"},
		}))
		kinds := map[string]string{"staff": "people", "genres": "genres", "tags": "tags", "publishers": "publishers"}
		for field, ks := range keys {
			for _, k := range ks.([]any) {
				if k != nil {
					c.Get("/api/facets/"+kinds[field]+"/"+url.PathEscape(k.(string))).Assert(t, 200)
				}
			}
		}
		for _, id := range []string{"ch", "inv"} {
			if _, ok := c.Get("/api/content/"+id).Assert(t, 200).JSON()["facet_keys"]; ok {
				t.Fatalf("%s has facet_keys", id)
			}
		}
	})

	t.Run("grid filter", func(t *testing.T) {
		for _, tc := range []struct {
			query string
			want  []string
		}{
			{"facet_kind=people&facet=dana-kell", []string{"r1", "r2", "r3"}},
			{"facet_kind=people&facet=Dana+Kell&facet_role=inker", []string{"r1"}},
			{"facet_kind=people&facet=dana-kell&facet_role=writer&library_id=" + l2, []string{"r2", "r3"}},
			{"facet_kind=genres&facet=Sci-Fi", []string{"r1", "r2"}},
			{"facet_kind=genres&facet=horror", nil},
		} {
			list := c.Get("/api/content?sort=title&"+tc.query).Assert(t, 200).JSON()
			var got []string
			for _, item := range list["data"].([]any) {
				got = append(got, s(item.(map[string]any)["id"]))
			}
			var ids []string
			for _, id := range c.Get("/api/content/ids?limit=100&sort=title&"+tc.query).Assert(t, 200).JSON()["ids"].([]any) {
				ids = append(ids, s(id))
			}
			buckets := c.Get("/api/content/buckets?sort=title&"+tc.query).Assert(t, 200).JSON()
			slices.Sort(got)
			slices.Sort(ids)
			assertEq(t, s(got), s(tc.want))
			assertEq(t, s(ids), s(tc.want))
			assertEq(t, s(list["total"]), s(len(tc.want)))
			assertEq(t, s(buckets["total"]), s(len(tc.want)))
		}

		// pgx switches to generic plans from a statement's sixth run.
		gp := dbtest.GenericPlans(t, pool)
		f := contentFilter{FacetKind: "person", Facet: "dana kell", FacetRole: "writer"}
		n, err := countContent(context.Background(), gp, "", f)
		if err != nil || n != 3 {
			t.Fatalf("count = %d (%v)", n, err)
		}
		ids, err := listContentIDs(context.Background(), gp, "", f, "title", "asc", nil, 0)
		if err != nil || len(ids) != 3 {
			t.Fatalf("ids = %v (%v)", ids, err)
		}

		for _, q := range []string{"facet_role=writer", "facet=dana-kell"} {
			c.Get("/api/content?"+q).Assert(t, 400)
		}
	})
}
