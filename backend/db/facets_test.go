package db_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"voltis/db/dbtest"
)

func TestFacetTransitions(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)

	t.Run("functions", func(t *testing.T) {
		// key: facet_key, also checked for idempotence. rows: facet_rows as kind|key|roles|labels.
		// sizes: facet_rows as kind and key length. links: facet_links, compared as JSON.
		queries := map[string]string{
			"key": "SELECT public.facet_key($1)",
			"rows": `SELECT coalesce(string_agg(format('%s|%s|%s|%s', kind, key, roles, labels), '; '
				ORDER BY kind, key), '') FROM public.facet_rows($1::jsonb)`,
			"sizes": `SELECT coalesce(string_agg(kind || ' ' || octet_length(key), ', '
				ORDER BY kind, octet_length(key)), '') FROM public.facet_rows($1::jsonb)`,
			"links": "SELECT public.facet_links($1::jsonb)::text",
		}
		a1000, a1001 := strings.Repeat("a", 1000), strings.Repeat("a", 1001)
		halves := strings.Repeat("½", 400)
		combining := func(n int) string { return "a" + strings.Repeat("\u0344", n) }
		js := func(v any) string {
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			return string(b)
		}
		capped := []string{a1000, a1001, halves}
		cases := []struct{ fn, in, want string }{
			{"key", "  Álvar   BRISK ", "alvar brisk"},
			{"key", "Ale\u0301x", "alex"},
			{"key", "Œuvre Ørn", "oeuvre orn"},
			{"key", "Straße", "strasse"},
			{"key", "Jo\x1fe", "jo e"},
			{"key", "J.M. Varden", "j m varden"},
			{"key", "J. M. Varden", "j m varden"},
			{"key", "J.R.R. Quillon", "j r r quillon"},
			{"key", "J. R. R. Quillon", "j r r quillon"},
			{"key", "J.Mira Stavros", "j mira stavros"},
			{"key", "J. Mira Stavros", "j mira stavros"},
			{"key", "J Mira Stavros", "j mira stavros"},
			{"key", "U.S.A.", "u s a"},
			{"key", "USA", "usa"},
			{"key", "O'Dara", "odara"},
			{"key", "O’Dara", "odara"},
			{"key", "Sci-Fi", "sci fi"},
			{"key", "sci_fi", "sci fi"},
			{"key", "Sci / Fi", "sci fi"},
			{"key", "a:b", "a b"},
			{"key", "???", ""},
			{"key", "山田\u3000 太郎", "山田 太郎"},
			{"key", "Q++", "q++"},
			{"key", "Q#", "q#"},
			{"key", "Q", "q"},
			{"key", "प्रकाश", "प्रकाश"},
			{"key", "+", "+"},
			{"key", "«Le Pont» — 24…", "le pont 24"},
			{"key", "‹Ko›", "ko"},
			{"key", "“Ko” ‘Rin’", "ko rin"},
			{"key", "½ Moon", "1 2 moon"},
			{"key", "Ʀ", "r"},
			{"key", "ʀ", "r"},
			{"key", "ｶﾀｶﾅ", "カタカナ"},
			{"key", "ＡＢＣ\u3000１２", "abc 12"},
			{"key", "ﬁne", "fine"},
			{"key", "\u0627\u0300\u0653", "\u0622"},

			{"rows", `{"genres": "sci_fi", "staff": {"name": "Ann Vole"}, "tags": "Moss", "publishers": {"a": 1}}`, ""},
			{"rows", `{"genres": [7, "drama"], "tags": [true, null, "Moss"], "publishers": [{"name": "Kite"}, "Kite Press"]}`,
				"genre|drama|{}|{drama}; publisher|kite press|{}|{\"Kite Press\"}; tag|moss|{}|{Moss}"},
			{"rows", `{"staff": [{"name": false}, {"name": "Ann Vole", "role": 42}, "Ben Roe", {"name": "Cara Lund"},
				{"name": "Dov Pine", "role": null}, {"name": "  "}, {"name": "?!"}, {"name": "Eli Fenn", "role": "writer"}]}`,
				"person|cara lund|{}|{\"Cara Lund\"}; person|dov pine|{}|{\"Dov Pine\"}; person|eli fenn|{writer}|{\"Eli Fenn\"}"},
			{"rows", `{"genres": ["sci_fi", "sci_fi"], "tags": ["Slow Burn", "Slów  Burn", "Slow Burn"],
				"staff": [{"name": "Dana Kell", "role": "writer"}, {"name": "Dána Kell", "role": "inker"}, {"name": "Dana Kell", "role": "writer"}]}`,
				"genre|sci fi|{}|{sci_fi}; person|dana kell|{inker,writer}|{\"Dana Kell\",\"Dána Kell\"}; tag|slow burn|{}|{\"Slow Burn\",\"Slów Burn\"}"},

			{"sizes", js(map[string]any{
				"genres":     append(capped, combining(499), combining(500), strings.Repeat(" ", 1001)+"sci_fi"),
				"tags":       capped,
				"publishers": capped,
				"staff": []map[string]any{{"name": a1000}, {"name": a1001}, {"name": halves},
					{"name": "Ann Vole", "role": strings.Repeat("r", 201)}},
			}), "genre 1, genre 1000, person 1000, publisher 1000, tag 1000"},

			{"links", js(map[string]any{
				"staff": []any{map[string]any{"name": "Ann Vole", "role": "writer"}, map[string]any{"name": false},
					map[string]any{"name": " "}, map[string]any{"name": "Bo Lind", "role": strings.Repeat("r", 201)},
					map[string]any{"name": a1001}, "Cy Moss"},
				"genres": []any{"sci_fi", 7, "???", strings.Repeat(" ", 1001) + "sci_fi", combining(499), combining(500)},
				"tags":   []any{"Moss", nil, halves},
			}), `{"staff": ["ann vole", null, null, null, null, null], "genres": ["sci fi", null, null, null, "a", null],
				"tags": ["moss", null, null], "publishers": []}`},
		}
		for _, c := range cases {
			var got string
			if err := pool.QueryRow(ctx, queries[c.fn], c.in).Scan(&got); err != nil {
				t.Fatalf("%s %.40q: %v", c.fn, c.in, err)
			}
			want := c.want
			switch c.fn {
			case "key":
				var again string
				if err := pool.QueryRow(ctx, queries["key"], got).Scan(&again); err != nil || again != got {
					t.Errorf("facet_key(%q) = %q refolds to %q (%v)", c.in, got, again, err)
				}
			case "links":
				if err := pool.QueryRow(ctx, "SELECT $1::jsonb::text", c.want).Scan(&want); err != nil {
					t.Fatal(err)
				}
			}
			if got != want {
				t.Errorf("%s %.80q = %q, want %q", c.fn, c.in, got, want)
			}
		}
	})

	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	count := func(id string) int {
		t.Helper()
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM content_facets WHERE content_id = $1", id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	xmins := func(id string) string {
		t.Helper()
		var s string
		if err := pool.QueryRow(ctx, "SELECT array_agg(xmin::text ORDER BY kind, key)::text FROM content_facets WHERE content_id = $1", id).Scan(&s); err != nil {
			t.Fatal(err)
		}
		return s
	}
	check := func(step string, want map[string]int) {
		t.Helper()
		dbtest.AssertFacetsConsistent(t, pool)
		for id, n := range want {
			if got := count(id); got != n {
				t.Errorf("%s: %s has %d facet rows, want %d", step, id, got, n)
			}
		}
	}

	r1 := `{"title": "Root", "genres": ["sci_fi"], "tags": ["Slow Burn", "slow-burn"],
		"staff": [{"name": "Dana Kell", "role": "writer"}, {"name": "Dana Kell", "role": "artist"}], "publishers": ["Kite Press"]}`
	exec("INSERT INTO libraries (id, name, type) VALUES ('l1', 'One', 'comics'), ('l2', 'Two', 'comics')")
	exec(`INSERT INTO content (id, library_id, uri_part, uri, type, parent_id, valid, data) VALUES
		('r1', 'l1', 'r1', 'r1', 'comic_series', NULL, true, $1),
		('k1', 'l1', 'k1', 'k1', 'comic', 'r1', true, '{"genres": ["drama"], "tags": ["Moss"]}'),
		('r2', 'l1', 'r2', 'r2', 'comic', NULL, false, '{"genres": ["drama"]}'),
		('r3', 'l2', 'r3', 'r3', 'comic', NULL, true, '{"tags": ["Moss"]}'),
		('r4', 'l2', 'r4', 'r4', 'comic', NULL, true, $2)`, r1, `{"tags": ["Moss", "`+strings.Repeat("a", 1001)+`"]}`)
	check("insert", map[string]int{"r1": 4, "k1": 0, "r2": 0, "r3": 1, "r4": 1})

	exec(`UPDATE content SET data = '{"title": "Root", "genres": ["sci_fi", "drama"], "tags": ["Slow-Burn"],
		"staff": [{"name": "Dana Kell", "role": "inker"}]}' WHERE id = 'r1'`)
	check("update", map[string]int{"r1": 4})
	before := xmins("r1")
	exec(`INSERT INTO content (id, library_id, uri_part, uri, type, data) SELECT id, library_id, uri_part, uri, type, data
		FROM content WHERE id = 'r1' ON CONFLICT (id) DO UPDATE SET data = EXCLUDED.data`)
	check("identical upsert", nil)
	if got := xmins("r1"); got != before {
		t.Errorf("identical upsert rewrote facet rows: %s, was %s", got, before)
	}
	exec(`UPDATE content SET data = jsonb_set(data, '{title}', '"Renamed"') WHERE id = 'r1'`)
	check("title update", nil)
	if got := xmins("r1"); got != before {
		t.Errorf("title update rewrote facet rows: %s, was %s", got, before)
	}

	exec("UPDATE content SET valid = false WHERE id = 'r1'")
	check("invalidate", map[string]int{"r1": 0})
	exec("UPDATE content SET valid = true WHERE id = 'r1'")
	check("revalidate", map[string]int{"r1": 4})

	// Swap root and child in one statement.
	exec("UPDATE content SET parent_id = CASE id WHEN 'k1' THEN NULL ELSE 'k1' END WHERE id IN ('r1', 'k1')")
	check("root to child", map[string]int{"r1": 0, "k1": 2})
	exec("UPDATE content SET parent_id = CASE id WHEN 'r1' THEN NULL ELSE 'r1' END WHERE id IN ('r1', 'k1')")
	check("child to root", map[string]int{"r1": 4, "k1": 0})

	exec("DELETE FROM content WHERE id = 'r3'")
	check("delete root", map[string]int{"r3": 0})

	exec("DELETE FROM libraries WHERE id = 'l2'")
	check("delete library", map[string]int{"r4": 0, "r1": 4})
}
