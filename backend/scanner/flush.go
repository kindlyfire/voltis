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
	INSERT INTO content (id, created_at, updated_at, uri_part, uri, valid, file_uri,
		file_mtime, file_size, cover_uri, type, "order", order_parts, file_data,
		parent_id, library_id, word_count, page_count)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	ON CONFLICT (id) DO UPDATE SET
		uri_part = $4, uri = $5, valid = $6, file_uri = $7, file_mtime = $8,
		file_size = $9, cover_uri = $10, type = $11, "order" = $12, order_parts = $13,
		file_data = $14, parent_id = $15, updated_at = $3, word_count = $17, page_count = $18`

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
	uri, uriPart string
	tick         int
}

func commit(ctx context.Context, tx pgx.Tx, store *metadata.Store, s FileScanner, libraryID string, f flush,
	now time.Time) (Counts, []RecentEntry, error) {
	if err := db.LockMetadata(ctx, tx, libraryID); err != nil {
		return Counts{}, nil, err
	}

	tallies := map[string]*recentTally{}
	tally := func(id, uri, uriPart string, tick int) *recentTally {
		t, ok := tallies[id]
		if !ok {
			t = &recentTally{RecentEntry: RecentEntry{ID: id}, uri: uri, uriPart: uriPart}
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
	var c models.Content
	err := query(ctx, tx, `
		SELECT id, created_at, uri, uri_part, type, file_uri, cover_uri, file_mtime, parent_id
		FROM content WHERE library_id = $1 AND id = ANY($2::text[])
	`, []any{libraryID, ids},
		[]any{&c.ID, &c.CreatedAt, &c.URI, &c.URIPart, &c.Type, &c.FileURI, &c.CoverURI, &c.FileMtime, &c.ParentID},
		func() error {
			cur[c.ID] = c
			key[c.ID] = Key{deref(c.ParentID), c.URIPart}
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
		var id, uri, part, title string
		var parentID *string
		// The title is read now: a leaf moving into a deleted item's URI later in the flush takes
		// its metadata.
		err := query(ctx, tx, `
			WITH d AS (
				DELETE FROM content WHERE library_id = $1 AND id = ANY($2::text[])
				RETURNING id, parent_id, uri, uri_part
			)
			SELECT d.id, d.parent_id, d.uri, d.uri_part, COALESCE(NULLIF(m.data->>'title', ''), d.uri_part)
			FROM d LEFT JOIN content_metadata m ON m.library_id = $1 AND m.uri = d.uri
		`, []any{libraryID, deletes}, []any{&id, &parentID, &uri, &part, &title}, func() error {
			if parentID == nil {
				t := tally(id, uri, part, deleteTick[id])
				t.Removed++
				t.Deleted = true
				t.Title = title
			} else {
				var ref SeriesRef
				if set, ok := f.sets[*parentID]; ok {
					ref = set.Ref
				}
				tally(*parentID, ref.URI, ref.URIPart, deleteTick[id]).Removed++
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

	var metaWrites []metadata.FileLayer
	for _, st := range steps {
		if st.write == nil {
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
		if err := upsertContent(ctx, tx, leafRow(st.write.id, libraryID, uri, *item, parentID, old, now)); err != nil {
			return Counts{}, nil, err
		}
		// Only the row tells an add from a moved file reclaiming a deleted item's ID.
		var t *recentTally
		if parentID != nil {
			t = tally(*parentID, st.set.Ref.URI, st.set.Ref.URIPart, st.write.tick)
		} else {
			t = tally(st.write.id, uri, item.URIPart, st.write.tick)
		}
		if old != nil {
			t.Updated++
		} else {
			t.Added++
		}
		metaWrites = append(metaWrites, metadata.FileLayer{URI: uri, Fields: item.MetaRaw})
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

	if err := store.WriteFileLayers(ctx, tx, libraryID, metaWrites, now); err != nil {
		return Counts{}, nil, err
	}

	if err := commitSeries(ctx, tx, store, s, libraryID, now, f.sets, seriesIDs, dropped); err != nil {
		return Counts{}, nil, err
	}

	if f.final {
		if !f.keep {
			var id string
			err := query(ctx, tx, `
				DELETE FROM content p
				WHERE p.library_id = $1 AND p.type IN ('comic_series', 'book_series')
				  AND NOT EXISTS (SELECT 1 FROM content c WHERE c.parent_id = p.id)
				RETURNING p.id
			`, []any{libraryID}, []any{&id}, func() error {
				if t, ok := tallies[id]; ok {
					t.Deleted = true
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

	counts, recent, err := recentEntries(ctx, tx, libraryID, tallies)
	if err == nil && f.final && !f.keep {
		// Only now: recentEntries titles deleted entries from their metadata.
		err = store.CollectOrphans(ctx, tx, libraryID)
	}
	return counts, recent, err
}

// recentEntries sums every entry's counts and details the newest entries. A deleted row is joined
// to its metadata by its last URI, since metadata outlives the row.
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
	uris := fp.Map(top, func(t *recentTally) string { return t.uri })
	parts := fp.Map(top, func(t *recentTally) string { return t.uriPart })
	var id, title string
	var hasCover bool
	var mtime *time.Time
	var cover *metadata.CoverRef
	err := query(ctx, tx, `
		SELECT r.id, COALESCE(NULLIF(m.data->>'title', ''), c.uri_part, r.uri_part),
		       c.cover_uri IS NOT NULL, c.file_mtime, CASE WHEN c.id IS NOT NULL THEN m.data->'cover' END
		FROM unnest($2::text[], $3::text[], $4::text[]) AS r(id, uri, uri_part)
		LEFT JOIN content c ON c.library_id = $1 AND c.id = r.id
		LEFT JOIN content_metadata m ON m.library_id = $1 AND m.uri = COALESCE(c.uri, r.uri)
	`, []any{libraryID, ids, uris, parts}, []any{&id, &title, &hasCover, &mtime, &cover}, func() error {
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

		seriesMeta = append(seriesMeta, metadata.FileLayer{URI: ref.URI, Fields: seriesLayer(ref, ordered)})
	}
	return store.WriteFileLayers(ctx, tx, libraryID, seriesMeta, now)
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
		SELECT c.id, c.uri_part, c.order_parts, c.cover_uri, c.file_mtime, c.parent_id,
		       COALESCE(m.data_raw->'file', '{}')
		FROM content c LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri
		WHERE c.library_id = $1 AND c.parent_id = ANY($2::text[])
	`, []any{libraryID, seriesIDs},
		[]any{&kid.ID, &kid.URIPart, &kid.OrderParts, &kid.CoverURI, &kid.FileMtime, &parentID, &kid.Meta}, func() error {
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
		})
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

// applyRenames moves refs in two phases: every source first moves to its own temporary URI, which no
// real URI can equal, so chains and swaps never overwrite a live source; then each moves to its
// destination, where the source wins over any row left there, which can only be an orphan.
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
	// Metadata and links at a destination belong to removed content, so they all go; a user's or
	// list's row goes only when the source brings its own.
	for _, t := range []struct{ table, owner string }{
		{"content_metadata", ""},
		{"metadata_links", ""},
		{"user_to_content", "user_id"},
		{"custom_list_to_content", "custom_list_id"},
	} {
		move := `UPDATE ` + t.table + ` m SET uri = r.dst FROM unnest($2::text[], $3::text[]) AS r(src, dst)
			WHERE m.library_id = $1 AND m.uri = r.src`
		orphans := `DELETE FROM ` + t.table + ` d USING unnest($2::text[], $3::text[]) AS r(src, dst)
			WHERE d.library_id = $1 AND d.uri = r.dst`
		if t.owner != "" {
			orphans += ` AND EXISTS (SELECT 1 FROM ` + t.table + ` s
				WHERE s.library_id = $1 AND s.uri = r.src AND s.` + t.owner + ` = d.` + t.owner + `)`
		}
		for _, st := range []struct {
			sql      string
			src, dst []string
		}{{move, olds, tmps}, {orphans, tmps, news}, {move, tmps, news}} {
			if _, err := tx.Exec(ctx, st.sql, libraryID, st.src, st.dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func upsertContent(ctx context.Context, tx pgx.Tx, c models.Content) error {
	data := c.FileData
	if data == nil {
		data = json.RawMessage("{}")
	}
	parts := c.OrderParts
	if parts == nil {
		parts = []*float32{}
	}
	_, err := tx.Exec(ctx, contentUpsert, c.ID, c.CreatedAt, c.UpdatedAt, c.URIPart, c.URI, c.Valid,
		c.FileURI, c.FileMtime, c.FileSize, c.CoverURI, c.Type, c.Order, parts, data, c.ParentID, c.LibraryID, c.WordCount, c.PageCount)
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
