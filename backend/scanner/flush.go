package scanner

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rename struct{ Old, New string }

const contentUpsert = `
	INSERT INTO content (` + upsertColumns + `, meta_updated_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
		COALESCE($19::jsonb, '{}'), COALESCE($20::jsonb, '{}'), COALESCE($21::int, 0),
		CASE WHEN $19::jsonb IS NULL THEN NULL ELSE $3::timestamptz END)
	ON CONFLICT (id) DO UPDATE SET
		uri_part = $4, uri = $5, valid = $6, file_uri = $7, file_mtime = $8,
		file_size = $9, cover_uri = $10, type = $11, "order" = $12, order_parts = $13,
		file_data = $14, parent_id = $15, updated_at = $3, word_count = $17, page_count = $18,
		data_raw = COALESCE($19, content.data_raw), data = COALESCE($20, content.data),
		data_version = COALESCE($21, content.data_version),
		meta_updated_at = CASE WHEN $19 IS NOT NULL AND (content.data_raw <> $19 OR content.data <> $20)
			THEN $3 ELSE content.meta_updated_at END`

func commitLoop(ctx context.Context, pool *pgxpool.Pool, store *metadata.Store, s FileScanner, libraryID string,
	in <-chan flush, out chan<- committed) {
	for f := range in {
		var counts Counts
		var recent []RecentEntry
		var err error
		for range 3 {
			err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
				var txErr error
				counts, recent, txErr = commit(ctx, tx, store, s, libraryID, f, time.Now().UTC())
				return txErr
			})
			if err == nil || ctx.Err() != nil || !retryable(err) {
				break
			}
		}
		if err != nil {
			counts, recent = Counts{}, nil
		}
		select {
		case out <- committed{seq: f.seq, counts: counts, recent: recent, err: err}:
		case <-ctx.Done():
			return
		}
		if err != nil {
			return
		}
	}
}

func retryable(err error) bool {
	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	return ok && slices.Contains([]string{"40001", "40P01", "23505"}, pgErr.Code)
}

// recentTally accumulates what a flush did to one entry: a series, or a standalone item.
type recentTally struct {
	RecentEntry
	uriPart string
	tick    int
}

func commit(ctx context.Context, tx pgx.Tx, store *metadata.Store, s FileScanner, libraryID string, f flush,
	now time.Time) (Counts, []RecentEntry, error) {
	if err := db.LockMetadata(ctx, tx, libraryID); err != nil {
		return Counts{}, nil, err
	}

	tallies := map[string]*recentTally{}
	tally := func(id, uriPart string, tick int) *recentTally {
		t, ok := tallies[id]
		if !ok {
			t = &recentTally{RecentEntry: RecentEntry{ID: id}, uriPart: uriPart}
			tallies[id] = t
		}
		t.tick = max(t.tick, tick)
		return t
	}

	setIDs := slices.Sorted(maps.Keys(f.sets))
	var ids, seriesIDs, invalid, deletes []string
	deleteTick := map[string]int{}
	for _, id := range setIDs {
		set := f.sets[id]
		if set.Ref.ID != "" {
			ids = append(ids, set.Ref.ID)
			seriesIDs = append(seriesIDs, set.Ref.ID)
		}
		for _, w := range set.Writes {
			ids = append(ids, w.id)
		}
		invalid = append(invalid, set.Invalid...)
		for _, d := range set.Deletes {
			deletes = append(deletes, d.id)
			deleteTick[d.id] = d.tick
		}
	}

	cur := map[string]models.Content{}
	key := map[string]Key{}
	docs := map[string]metadata.Doc{}
	var c models.Content
	var doc metadata.Doc
	err := query(ctx, tx, `
		SELECT id, created_at, uri, uri_part, type, file_uri, cover_uri, file_mtime, parent_id, "order", data_raw
		FROM content WHERE library_id = $1 AND id = ANY($2::text[])
	`, []any{libraryID, ids},
		[]any{&c.ID, &c.CreatedAt, &c.URI, &c.URIPart, &c.Type, &c.FileURI, &c.CoverURI, &c.FileMtime, &c.ParentID, &c.Order, &doc},
		func() error {
			cur[c.ID] = c
			key[c.ID] = Key{deref(c.ParentID), c.URIPart}
			docs[c.ID] = doc
			// JSON scans merge into the destination, so the next row starts from empty.
			doc = metadata.Doc{}
			return nil
		})
	if err != nil {
		return Counts{}, nil, err
	}
	dropped := map[string]bool{}
	maps.Copy(dropped, f.gone)
	for _, id := range deletes {
		dropped[id] = true
	}

	// Ref moves, from stored URIs, for renamed series and their unwritten children, then for every
	// written leaf. A series created in this flush stored nothing, whatever its OldURI says.
	var moves []rename
	for _, id := range setIDs {
		set := f.sets[id]
		stored, ok := cur[id]
		if set.New || !ok || stored.URI == set.Ref.URI {
			continue
		}
		moves = append(moves, rename{Old: stored.URI, New: set.Ref.URI})
		var childID, uri, part string
		err := query(ctx, tx, `SELECT id, uri, uri_part FROM content WHERE parent_id = $1`,
			[]any{set.Ref.ID}, []any{&childID, &uri, &part}, func() error {
				// A written child (cur holds exactly the written rows) moves once, from its stored URI
				// to its final one, with the leaves.
				if _, written := cur[childID]; !dropped[childID] && !written {
					moves = append(moves, rename{Old: uri, New: set.Ref.URI + "/" + part})
				}
				return nil
			})
		if err != nil {
			return Counts{}, nil, err
		}
	}

	if len(deletes) > 0 {
		var id, part, title string
		var parentID *string
		err := query(ctx, tx, `
			DELETE FROM content WHERE library_id = $1 AND id = ANY($2::text[])
			RETURNING id, parent_id, uri_part, COALESCE(NULLIF(data->>'title', ''), uri_part)
		`, []any{libraryID, deletes}, []any{&id, &parentID, &part, &title}, func() error {
			if parentID == nil {
				t := tally(id, part, deleteTick[id])
				t.Removed++
				t.Deleted = true
				t.Title = title
			} else {
				var ref SeriesRef
				if set, ok := f.sets[*parentID]; ok {
					ref = set.Ref
				}
				tally(*parentID, ref.URIPart, deleteTick[id]).Removed++
			}
			return nil
		})
		if err != nil {
			return Counts{}, nil, err
		}
	}

	steps, err := orderSteps(f, key)
	if err != nil {
		return Counts{}, nil, err
	}

	var touched []string
	var leaves leafBatch
	for _, st := range steps {
		if st.write == nil {
			if err := leaves.flush(ctx, tx); err != nil {
				return Counts{}, nil, err
			}
			if err := identityStep(ctx, tx, libraryID, st.set, now); err != nil {
				return Counts{}, nil, err
			}
			continue
		}

		item := st.write.item
		uri := item.URIPrefix + "/" + item.URIPart
		var parentID *string
		if st.set.Ref.ID != "" {
			parentID = new(st.set.Ref.ID)
			uri = st.set.Ref.URI + "/" + item.URIPart
		}
		var old *models.Content
		if c, ok := cur[st.write.id]; ok {
			old = &c
			if c.URI != uri {
				moves = append(moves, rename{Old: c.URI, New: uri})
			}
		}
		// Leaves have no links, so their metadata is written with the row.
		meta, changed, err := metadata.NewLeafDoc(docs[st.write.id], item.MetaRaw)
		if err != nil {
			return Counts{}, nil, err
		}
		if changed {
			touched = append(touched, st.write.id)
		}
		if err := leaves.add(ctx, tx, leafRow(st.write.id, libraryID, uri, *item, parentID, old, now), &meta); err != nil {
			return Counts{}, nil, err
		}
		// Only the row tells an add from a moved file reclaiming a deleted item's ID.
		var t *recentTally
		if parentID != nil {
			t = tally(*parentID, st.set.Ref.URIPart, st.write.tick)
		} else {
			t = tally(st.write.id, item.URIPart, st.write.tick)
		}
		if old != nil {
			t.Updated++
		} else {
			t.Added++
		}
	}
	if err := leaves.flush(ctx, tx); err != nil {
		return Counts{}, nil, err
	}
	if err := applyRenames(ctx, tx, libraryID, moves); err != nil {
		return Counts{}, nil, err
	}

	for _, id := range setIDs {
		set := f.sets[id]
		if id == "" || set.New || set.renamed() {
			continue
		}
		c, ok := cur[id]
		if !ok || (c.URIPart == set.Ref.URIPart && fp.PtrEq(c.FileURI, set.Ref.FileURI)) {
			continue
		}
		_, err := tx.Exec(ctx, "UPDATE content SET uri_part = $2, file_uri = $3, updated_at = $4 WHERE id = $1",
			id, set.Ref.URIPart, set.Ref.FileURI, now)
		if err != nil {
			return Counts{}, nil, err
		}
	}

	if len(invalid) > 0 {
		_, err := tx.Exec(ctx, "UPDATE content SET valid = false, updated_at = $2 WHERE library_id = $1 AND id = ANY($3::text[])",
			libraryID, now, invalid)
		if err != nil {
			return Counts{}, nil, err
		}
	}

	if err := metadata.TouchLinks(ctx, tx, touched, now); err != nil {
		return Counts{}, nil, err
	}

	if err := commitSeries(ctx, tx, store, s, libraryID, now, f.sets, seriesIDs, dropped); err != nil {
		return Counts{}, nil, err
	}

	if f.final {
		if !f.keep {
			var id, title string
			err := query(ctx, tx, `
				-- Materialized: find empty root series by index first, then touch only their heap rows.
				WITH gone AS MATERIALIZED (
					SELECT s.id FROM content s
					WHERE s.library_id = $1 AND s.parent_id IS NULL AND s.type IN ('comic_series', 'book_series')
					  AND NOT EXISTS (SELECT 1 FROM content c WHERE c.parent_id = s.id)
				)
				DELETE FROM content p USING gone WHERE p.id = gone.id
				RETURNING p.id, COALESCE(NULLIF(p.data->>'title', ''), p.uri_part)
			`, []any{libraryID}, []any{&id, &title}, func() error {
				if t, ok := tallies[id]; ok {
					t.Deleted, t.Title = true, title
				}
				return nil
			})
			if err != nil {
				return Counts{}, nil, err
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE libraries SET scanned_at = $1 WHERE id = $2", now, libraryID); err != nil {
			return Counts{}, nil, err
		}
	}

	return recentEntries(ctx, tx, libraryID, tallies)
}

// recentEntries sums every entry's counts and details the newest entries. Deleted entries carry
// their titles from the delete.
func recentEntries(ctx context.Context, tx pgx.Tx, libraryID string, tallies map[string]*recentTally) (Counts, []RecentEntry, error) {
	var counts Counts
	for _, t := range tallies {
		counts.add(t.Counts)
	}
	top := slices.SortedFunc(maps.Values(tallies), func(a, b *recentTally) int {
		return cmp.Or(cmp.Compare(b.tick, a.tick), cmp.Compare(a.ID, b.ID))
	})
	top = top[:min(len(top), RecentCap)]
	if len(top) == 0 {
		return counts, nil, nil
	}

	ids := fp.Map(top, func(t *recentTally) string { return t.ID })
	parts := fp.Map(top, func(t *recentTally) string { return t.uriPart })
	var id, title string
	var hasCover bool
	var mtime *time.Time
	var cover *metadata.CoverRef
	err := query(ctx, tx, `
		SELECT r.id, COALESCE(NULLIF(c.data->>'title', ''), c.uri_part, r.uri_part),
		       c.cover_uri IS NOT NULL, c.file_mtime, c.data->'cover'
		FROM unnest($2::text[], $3::text[]) AS r(id, uri_part)
		LEFT JOIN content c ON c.library_id = $1 AND c.id = r.id
	`, []any{libraryID, ids, parts}, []any{&id, &title, &hasCover, &mtime, &cover}, func() error {
		t := tallies[id]
		t.Title, t.CoverVersion = cmp.Or(t.Title, title), covers.Version(cover, hasCover, mtime)
		return nil
	})
	if err != nil {
		return Counts{}, nil, err
	}
	return counts, fp.Map(top, func(t *recentTally) RecentEntry { return t.RecentEntry }), nil
}

func commitSeries(ctx context.Context, tx pgx.Tx, store *metadata.Store, s FileScanner, libraryID string,
	now time.Time, sets map[string]*SeriesChanges, seriesIDs []string, dropped map[string]bool) error {
	children, err := loadChildren(ctx, tx, libraryID, seriesIDs, dropped)
	if err != nil {
		return err
	}

	var seriesMeta []metadata.FileLayer
	for _, id := range seriesIDs {
		kids := children[id]
		if len(kids) == 0 {
			continue
		}
		ref := sets[id].Ref
		ordered := order(kids)

		orderIDs := make([]string, len(ordered))
		orderVals := make([]int, len(ordered))
		for i := range ordered {
			orderIDs[i], orderVals[i] = ordered[i].ID, i
		}
		_, err := tx.Exec(ctx, `
			UPDATE content c SET "order" = r.o
			FROM unnest($2::text[], $3::int[]) AS r(id, o)
			WHERE c.library_id = $1 AND c.id = r.id AND c."order" IS DISTINCT FROM r.o
		`, libraryID, orderIDs, orderVals)
		if err != nil {
			return err
		}

		cover, mtime := s.SeriesCover(ref, ordered)
		_, err = tx.Exec(ctx, `
			UPDATE content SET cover_uri = $2, file_mtime = $3, updated_at = $4
			WHERE id = $1 AND (cover_uri IS DISTINCT FROM $2 OR file_mtime IS DISTINCT FROM $3)
		`, id, cover, mtime, now)
		if err != nil {
			return err
		}

		seriesMeta = append(seriesMeta, metadata.FileLayer{ContentID: id, Fields: seriesLayer(ref, ordered)})
	}
	return store.WriteFileLayers(ctx, tx, seriesMeta, now)
}

// loadChildren reads the children of series, with their file layers, leaving out dropped ones.
func loadChildren(ctx context.Context, tx pgx.Tx, libraryID string, seriesIDs []string,
	dropped map[string]bool) (map[string][]Child, error) {
	children := map[string][]Child{}
	if len(seriesIDs) == 0 {
		return children, nil
	}
	var kid Child
	var parentID string
	err := query(ctx, tx, `
		SELECT c.id, c.uri_part, c.order_parts, c.cover_uri, c.file_mtime, NOT c.valid, c.parent_id,
		       COALESCE(c.data_raw->'file', '{}')
		FROM content c
		WHERE c.library_id = $1 AND c.parent_id = ANY($2::text[])
	`, []any{libraryID, seriesIDs},
		[]any{&kid.ID, &kid.URIPart, &kid.OrderParts, &kid.CoverURI, &kid.FileMtime, &kid.Invalid, &parentID, &kid.Meta}, func() error {
			if !dropped[kid.ID] {
				children[parentID] = append(children[parentID], kid)
			}
			// JSON scans merge into the destination, so the next row starts from empty.
			kid.Meta = metadata.Fields{}
			return nil
		})
	return children, err
}

func identityStep(ctx context.Context, tx pgx.Tx, libraryID string, set *SeriesChanges, now time.Time) error {
	ref := set.Ref
	if set.New {
		return upsertContent(ctx, tx, models.Content{
			ID:        ref.ID,
			LibraryID: libraryID,
			CreatedAt: now,
			UpdatedAt: now,
			Type:      ref.Type,
			URI:       ref.URI,
			URIPart:   ref.URIPart,
			Valid:     true,
			FileURI:   ref.FileURI,
		}, nil)
	}

	_, err := tx.Exec(ctx, "UPDATE content SET uri = $2, uri_part = $3, file_uri = $4, updated_at = $5 WHERE id = $1",
		ref.ID, ref.URI, ref.URIPart, ref.FileURI, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE content SET uri = $2 || '/' || uri_part, updated_at = $3 WHERE parent_id = $1",
		ref.ID, ref.URI, now)
	return err
}

// applyRenames moves user and list refs in two phases: every source first moves to its own
// temporary URI, which no real URI can equal, so chains and swaps never overwrite a live source;
// then each moves to its destination. A row already at a destination, left by removed content,
// merges with the source's for the same user, keeping the more recent reading state; a list's
// goes when the source brings its own. Moved user rows get a fresh revision.
func applyRenames(ctx context.Context, tx pgx.Tx, libraryID string, pairs []rename) error {
	if len(pairs) == 0 {
		return nil
	}
	olds := fp.Map(pairs, func(r rename) string { return r.Old })
	news := fp.Map(pairs, func(r rename) string { return r.New })
	tmps := make([]string, len(pairs))
	for i := range tmps {
		tmps[i] = fmt.Sprintf("\x01rename/%d", i)
	}
	rev := db.ServerRevision()
	for _, t := range []struct{ table, owner string }{
		{"user_to_content", "user_id"},
		{"custom_list_to_content", "custom_list_id"},
	} {
		set, args := "", []any{}
		if t.table == "user_to_content" {
			set, args = ", revision = $4", []any{rev}
		}
		move := `UPDATE ` + t.table + ` m SET uri = r.dst` + set + ` FROM unnest($2::text[], $3::text[]) AS r(src, dst)
			WHERE m.library_id = $1 AND m.uri = r.src`
		if _, err := tx.Exec(ctx, move, append([]any{libraryID, olds, tmps}, args...)...); err != nil {
			return err
		}
		var err error
		if t.table == "user_to_content" {
			_, err = tx.Exec(ctx, db.MergeUserToContentSQL, libraryID, tmps, news, make([]string, len(pairs)), rev, nil)
		} else {
			_, err = tx.Exec(ctx, `DELETE FROM `+t.table+` d USING unnest($2::text[], $3::text[]) AS r(src, dst)
				WHERE d.library_id = $1 AND d.uri = r.dst AND EXISTS (SELECT 1 FROM `+t.table+` s
					WHERE s.library_id = $1 AND s.uri = r.src AND s.`+t.owner+` = d.`+t.owner+`)`,
				libraryID, tmps, news)
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, move, append([]any{libraryID, tmps, news}, args...)...); err != nil {
			return err
		}
	}
	return nil
}

// upsertContent writes a row, and its metadata unless meta is nil.
func upsertContent(ctx context.Context, tx pgx.Tx, c models.Content, meta *metadata.LeafDoc) error {
	_, err := tx.Exec(ctx, contentUpsert, upsertArgs(c, meta)...)
	return err
}

func upsertArgs(c models.Content, meta *metadata.LeafDoc) []any {
	data := c.FileData
	if data == nil {
		data = json.RawMessage("{}")
	}
	parts := c.OrderParts
	if parts == nil {
		parts = []*float32{}
	}
	var raw, derived *string
	var version *int
	if meta != nil {
		raw, derived, version = &meta.Raw, &meta.Data, new(metadata.DataVersion)
	}
	return []any{c.ID, c.CreatedAt, c.UpdatedAt, c.URIPart, c.URI, c.Valid,
		c.FileURI, c.FileMtime, c.FileSize, c.CoverURI, c.Type, c.Order, parts, data, c.ParentID, c.LibraryID, c.WordCount, c.PageCount,
		raw, derived, version}
}

const upsertColumns = `id, created_at, updated_at, uri_part, uri, valid, file_uri, file_mtime, file_size,
	cover_uri, type, "order", order_parts, file_data, parent_id, library_id, word_count, page_count,
	data_raw, data, data_version`

// leafUpsert is contentUpsert for many leaves at once; leaves always carry metadata. Rows go in
// step order, so unique keys are claimed in the same order as row by row.
const leafUpsert = `
	INSERT INTO content (` + upsertColumns + `, meta_updated_at)
	SELECT ` + upsertColumns + `, updated_at FROM pg_temp.scan_leaves ORDER BY seq
	ON CONFLICT (id) DO UPDATE SET
		uri_part = excluded.uri_part, uri = excluded.uri, valid = excluded.valid, file_uri = excluded.file_uri,
		file_mtime = excluded.file_mtime, file_size = excluded.file_size, cover_uri = excluded.cover_uri,
		type = excluded.type, "order" = excluded."order", order_parts = excluded.order_parts,
		file_data = excluded.file_data, parent_id = excluded.parent_id, updated_at = excluded.updated_at,
		word_count = excluded.word_count, page_count = excluded.page_count,
		data_raw = excluded.data_raw, data = excluded.data, data_version = excluded.data_version,
		meta_updated_at = CASE WHEN content.data_raw <> excluded.data_raw OR content.data <> excluded.data
			THEN excluded.updated_at ELSE content.meta_updated_at END`

// leafBatch buffers leaf upserts between identity steps, which must land first.
type leafBatch struct {
	rows [][]any
	ids  map[string]bool
}

func (b *leafBatch) add(ctx context.Context, tx pgx.Tx, c models.Content, meta *metadata.LeafDoc) error {
	// One statement cannot update a row twice.
	if b.ids[c.ID] {
		if err := b.flush(ctx, tx); err != nil {
			return err
		}
	}
	if b.ids == nil {
		b.ids = map[string]bool{}
	}
	b.ids[c.ID] = true
	b.rows = append(b.rows, append(upsertArgs(c, meta), len(b.rows)))
	return nil
}

func (b *leafBatch) flush(ctx context.Context, tx pgx.Tx) error {
	rows := b.rows
	b.rows, b.ids = nil, nil
	if len(rows) == 1 {
		_, err := tx.Exec(ctx, contentUpsert, rows[0][:len(rows[0])-1]...)
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	// Dropped at commit, so no pooled session keeps a copy of an older content schema; a later
	// flush in the same transaction reuses it.
	_, err := tx.Exec(ctx, `
		CREATE TEMP TABLE IF NOT EXISTS pg_temp.scan_leaves ON COMMIT DROP AS
			SELECT `+upsertColumns+`, 0 AS seq FROM content LIMIT 0;
		TRUNCATE pg_temp.scan_leaves`)
	if err != nil {
		return err
	}
	cols := []string{"id", "created_at", "updated_at", "uri_part", "uri", "valid", "file_uri", "file_mtime",
		"file_size", "cover_uri", "type", "order", "order_parts", "file_data", "parent_id", "library_id",
		"word_count", "page_count", "data_raw", "data", "data_version", "seq"}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"pg_temp", "scan_leaves"}, cols, pgx.CopyFromRows(rows)); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, leafUpsert)
	return err
}

func query(ctx context.Context, tx pgx.Tx, sql string, args, dest []any, visit func() error) error {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	_, err = pgx.ForEachRow(rows, dest, visit)
	return err
}
