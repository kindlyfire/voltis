package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type rename struct{ Old, New string }

const contentUpsert = `
	INSERT INTO content (id, created_at, updated_at, uri_part, uri, valid, file_uri,
		file_mtime, file_size, cover_uri, type, "order", order_parts, file_data,
		parent_id, library_id)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	ON CONFLICT (id) DO UPDATE SET
		uri_part = $4, uri = $5, valid = $6, file_uri = $7, file_mtime = $8,
		file_size = $9, cover_uri = $10, type = $11, "order" = $12, order_parts = $13,
		file_data = $14, parent_id = $15, updated_at = $3`

func commitLoop(ctx context.Context, pool *pgxpool.Pool, s FileScanner, libraryID string, in <-chan flush, out chan<- committed) {
	for f := range in {
		var counts Counts
		var err error
		for range 3 {
			err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
				var txErr error
				counts, txErr = commit(ctx, tx, s, libraryID, f, time.Now().UTC())
				return txErr
			})
			if err == nil || ctx.Err() != nil || !retryable(err) {
				break
			}
		}
		if err != nil {
			counts = Counts{}
		}
		select {
		case out <- committed{seq: f.seq, counts: counts, err: err}:
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

func commit(ctx context.Context, tx pgx.Tx, s FileScanner, libraryID string, f flush, now time.Time) (Counts, error) {
	var counts Counts
	if err := db.LockMetadata(ctx, tx, libraryID); err != nil {
		return counts, err
	}

	setIDs := slices.Sorted(maps.Keys(f.sets))
	var ids, seriesIDs, invalid, deletes []string
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
		deletes = append(deletes, set.Deletes...)
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
		return counts, err
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
			return counts, err
		}
	}

	if len(deletes) > 0 {
		tag, err := tx.Exec(ctx, "DELETE FROM content WHERE library_id = $1 AND id = ANY($2::text[])", libraryID, deletes)
		if err != nil {
			return counts, err
		}
		counts.Removed += int(tag.RowsAffected())
	}

	steps, err := orderSteps(f, key)
	if err != nil {
		return counts, err
	}

	var metaWrites []metaWrite
	for _, st := range steps {
		if st.write == nil {
			if err := identityStep(ctx, tx, libraryID, st.set, now); err != nil {
				return counts, err
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
			return counts, err
		}
		if st.write.added {
			counts.Added++
		} else {
			counts.Updated++
		}
		metaWrites = append(metaWrites, metaWrite{uri: uri, file: item.MetaRaw})
	}
	if err := applyRenames(ctx, tx, libraryID, moves); err != nil {
		return counts, err
	}

	for _, id := range setIDs {
		set := f.sets[id]
		if id == "" || set.New || set.renamed() {
			continue
		}
		c, ok := cur[id]
		if !ok || (c.URIPart == set.Ref.URIPart && ptrEq(c.FileURI, set.Ref.FileURI)) {
			continue
		}
		_, err := tx.Exec(ctx, "UPDATE content SET uri_part = $2, file_uri = $3, updated_at = $4 WHERE id = $1",
			id, set.Ref.URIPart, set.Ref.FileURI, now)
		if err != nil {
			return counts, err
		}
	}

	if len(invalid) > 0 {
		_, err := tx.Exec(ctx, "UPDATE content SET valid = false, updated_at = $2 WHERE library_id = $1 AND id = ANY($3::text[])",
			libraryID, now, invalid)
		if err != nil {
			return counts, err
		}
	}

	if err := writeMetadata(ctx, tx, libraryID, now, metaWrites); err != nil {
		return counts, err
	}

	if err := commitSeries(ctx, tx, s, libraryID, now, f.sets, seriesIDs, dropped); err != nil {
		return counts, err
	}

	if f.final {
		if !f.keep {
			_, err := tx.Exec(ctx, `
				DELETE FROM content p
				WHERE p.library_id = $1 AND p.type IN ('comic_series', 'book_series')
				  AND NOT EXISTS (SELECT 1 FROM content c WHERE c.parent_id = p.id)
			`, libraryID)
			if err != nil {
				return counts, err
			}
		}
		if _, err := tx.Exec(ctx, "UPDATE libraries SET scanned_at = $1 WHERE id = $2", now, libraryID); err != nil {
			return counts, err
		}
	}

	return counts, nil
}

func commitSeries(ctx context.Context, tx pgx.Tx, s FileScanner, libraryID string, now time.Time,
	sets map[string]*SeriesChanges, seriesIDs []string, dropped map[string]bool) error {
	children := map[string][]Child{}
	if len(seriesIDs) > 0 {
		var kid Child
		var parentID string
		var raw json.RawMessage
		err := query(ctx, tx, `
			SELECT c.id, c.uri_part, c.order_parts, c.cover_uri, c.file_mtime, c.parent_id,
			       COALESCE(m.data_raw, '{}') AS data_raw
			FROM content c LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri
			WHERE c.library_id = $1 AND c.parent_id = ANY($2::text[])
		`, []any{libraryID, seriesIDs},
			[]any{&kid.ID, &kid.URIPart, &kid.OrderParts, &kid.CoverURI, &kid.FileMtime, &parentID, &raw}, func() error {
				if dropped[kid.ID] {
					return nil
				}
				kid.Meta = metaraw.From(raw)
				children[parentID] = append(children[parentID], kid)
				return nil
			})
		if err != nil {
			return err
		}
	}

	var seriesMeta []metaWrite
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

		seriesMeta = append(seriesMeta, metaWrite{uri: ref.URI, file: inherit(ref, ordered)})
	}
	return writeMetadata(ctx, tx, libraryID, now, seriesMeta)
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
	for _, t := range []struct{ table, owner string }{
		{"content_metadata", "library_id"},
		{"user_to_content", "user_id"},
		{"custom_list_to_content", "custom_list_id"},
	} {
		move := `UPDATE ` + t.table + ` m SET uri = r.dst FROM unnest($2::text[], $3::text[]) AS r(src, dst)
			WHERE m.library_id = $1 AND m.uri = r.src`
		orphans := `DELETE FROM ` + t.table + ` d USING unnest($2::text[], $3::text[]) AS r(src, dst)
			WHERE d.library_id = $1 AND d.uri = r.dst AND EXISTS (SELECT 1 FROM ` + t.table + ` s
				WHERE s.library_id = $1 AND s.uri = r.src AND s.` + t.owner + ` = d.` + t.owner + `)`
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

type metaWrite struct {
	uri  string
	file models.Metadata
}

func writeMetadata(ctx context.Context, tx pgx.Tx, libraryID string, now time.Time, rows []metaWrite) error {
	if len(rows) == 0 {
		return nil
	}

	uris := fp.Map(rows, func(m metaWrite) string { return m.uri })
	existing := map[string]json.RawMessage{}
	var uri string
	var raw json.RawMessage
	err := query(ctx, tx, "SELECT uri, data_raw FROM content_metadata WHERE library_id = $1 AND uri = ANY($2::text[])",
		[]any{libraryID, uris}, []any{&uri, &raw}, func() error {
			existing[uri] = raw
			return nil
		})
	if err != nil {
		return err
	}

	data := make([]string, len(rows))
	raws := make([]string, len(rows))
	for i, m := range rows {
		mr := metaraw.From(existing[m.uri])
		mr.File = &metaraw.RawContainer[models.Metadata]{Raw: m.file}
		merged, err := json.Marshal(mr.Merge())
		if err != nil {
			return err
		}
		data[i], raws[i] = string(merged), string(mr.Dump())
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO content_metadata (uri, library_id, data, data_raw, updated_at)
		SELECT r.uri, $1, r.data, r.data_raw, $2
		FROM unnest($3::text[], $4::jsonb[], $5::jsonb[]) AS r(uri, data, data_raw)
		ON CONFLICT (uri, library_id) DO UPDATE
		SET data = EXCLUDED.data, data_raw = EXCLUDED.data_raw, updated_at = EXCLUDED.updated_at
	`, libraryID, now, uris, data, raws)
	return err
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
		c.FileURI, c.FileMtime, c.FileSize, c.CoverURI, c.Type, c.Order, parts, data, c.ParentID, c.LibraryID)
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
