package routes

import (
	"context"
	"testing"

	"voltis/db"
)

func TestFixBrokenRefs(t *testing.T) {
	pool := newTestPool(t)
	c := newAdminClient(t, pool)
	f := newRecentFixture(t, pool, c)

	// seed puts a broken row (read at minute srcAt, unless negative) and a row at a live item (read
	// at dstAt), and returns the broken row's id and the item's URI.
	seed := func(srcAt, dstAt int) (string, string) {
		t.Helper()
		item := f.content("comic", nil, 0)
		uri := "file:///lib/" + item
		at := func(m int) any {
			if m < 0 {
				return nil
			}
			return f.at(m)
		}
		f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status, last_read_at, starred, notes,
			revision) VALUES ('src_' || $3, $1, $2, '/gone/' || $3, 'reading', $4, true, 'src', 'srv:old')`,
			f.userID, f.libID, item, at(srcAt))
		f.exec(`INSERT INTO user_to_content (id, user_id, library_id, uri, status, last_read_at, rating)
			VALUES ('dst_' || $3, $1, $2, $4, 'completed', $5, 5)`, f.userID, f.libID, item, uri, at(dstAt))
		return "src_" + item, uri
	}
	fix := func(body map[string]any, code int) {
		t.Helper()
		c.Post("/api/content/broken-refs/"+f.libID, body).Assert(t, code)
	}
	// row is the user's row at uri as "id status starred notes rating srv-revision".
	row := func(uri string) string {
		t.Helper()
		var v string
		if err := pool.QueryRow(context.Background(), `
			SELECT concat_ws(' ', id, status, starred, notes, rating, revision LIKE 'srv:%' AND revision <> 'srv:old')
			FROM user_to_content WHERE uri = $1`, uri).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	for _, tc := range []struct {
		name         string
		srcAt, dstAt int
		keep         string
		status       string
	}{
		{"newer target", 1, 2, "", "completed"},
		{"newer source", 2, 1, "", "reading"},
		{"tie", -1, -1, "", "reading"},
		{"keep source", 1, 2, "source", "reading"},
		{"keep target", 2, 1, "target", "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, uri := seed(tc.srcAt, tc.dstAt)
			body := map[string]any{"update": map[string]string{src: uri}}
			if tc.keep != "" {
				body["keep"] = map[string]string{src: tc.keep}
			}
			fix(body, 200)
			assertEq(t, row(uri), "dst_"+uri[len("file:///lib/"):]+" "+tc.status+" t src 5 t")
			assertEq(t, utcCount(t, pool, uri[len("file:///lib/"):]), 1)
		})
	}

	t.Run("move without a destination row", func(t *testing.T) {
		src, uri := seed(1, 2)
		f.exec("DELETE FROM user_to_content WHERE uri = $1", uri)
		fix(map[string]any{"update": map[string]string{src: uri}}, 200)
		assertEq(t, row(uri), src+" reading t src t")
	})

	t.Run("repair onto its own URI", func(t *testing.T) {
		_, uri := seed(1, 2)
		id := "dst_" + uri[len("file:///lib/"):]
		fix(map[string]any{"update": map[string]string{id: uri}}, 200)
		assertEq(t, row(uri), id+" completed f 5")
		// The shared merge never pairs a row with itself.
		f.exec(db.MergeUserToContentSQL, f.libID, []string{uri}, []string{uri}, []string{""}, "srv:new", nil)
		assertEq(t, row(uri), id+" completed f 5")
	})

	t.Run("preview", func(t *testing.T) {
		_, uri := seed(1, 2)
		got := c.Get("/api/content/refs/"+f.libID+"/user-data?uri="+uri).Assert(t, 200).JSON()
		assertEq(t, got["status"], any("completed"))
		assertNotNil(t, "last_read_at", got["last_read_at"])
		if body := string(c.Get("/api/content/refs/"+f.libID+"/user-data?uri=nowhere").Assert(t, 200).Body); body != "null\n" {
			t.Fatalf("no row: %q", body)
		}
		src, _ := seed(1, 2)
		fix(map[string]any{"update": map[string]string{src: uri}, "keep": map[string]string{src: "newer"}}, 400)
	})
}
