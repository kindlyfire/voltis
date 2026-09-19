package scanner

import (
	"context"
	"encoding/json"
	"errors"
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
			counts = Counts{}
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
	if !ok {
		return false
	}
	switch pgErr.Code {
	case "40001", "40P01", "23505":
		return true
	}
	return false
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
	ids = append(ids, invalid...)
	ids = append(ids, deletes...)

	cur := map[string]models.Content{}
	key := map[string]Key{}
	err := query(ctx, tx, `
		SELECT id, created_at, uri, uri_part, type, file_uri, cover_uri, file_mtime, parent_id
		FROM content WHERE library_id = $1 AND id = ANY($2::text[])
	`, []any{libraryID, ids}, func(rows pgx.Rows) error {
		var c models.Content
		if err := rows.Scan(&c.ID, &c.CreatedAt, &c.URI, &c.URIPart, &c.Type, &c.FileURI, &c.CoverURI, &c.FileMtime, &c.ParentID); err != nil {
			return err
		}
		cur[c.ID] = c
		key[c.ID] = Key{deref(c.ParentID), c.URIPart}
		return nil
	})
	if err != nil {
		return counts, err
	}

	dropped := maps.Clone(f.gone)
	if dropped == nil {
		dropped = map[string]bool{}
	}
	for _, id := range deletes {
		dropped[id] = true
	}

	pairs := map[string][]rename{}
	for _, id := range setIDs {
		set := f.sets[id]
		if !set.renamed() {
			continue
		}
		list := []rename{{Old: set.OldURI, New: set.Ref.URI}}
		err := query(ctx, tx, `SELECT id, uri_part FROM content WHERE parent_id = $1`,
			[]any{set.Ref.ID}, func(rows pgx.Rows) error {
				var childID, part string
				if err := rows.Scan(&childID, &part); err != nil {
					return err
				}
				if !dropped[childID] {
					list = append(list, rename{Old: set.OldURI + "/" + part, New: set.Ref.URI + "/" + part})
				}
				return nil
			})
		if err != nil {
			return counts, err
		}
		pairs[id] = list
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
			if err := identityStep(ctx, tx, libraryID, st.set, pairs[st.set.Ref.ID], now); err != nil {
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

	children := map[string][]Child{}
	if len(seriesIDs) > 0 {
		err := query(ctx, tx, `
			SELECT c.id, c.uri, c.uri_part, c."order", c.order_parts, c.cover_uri, c.file_mtime, c.valid, c.parent_id,
			       COALESCE(m.data_raw, '{}') AS data_raw
			FROM content c LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri
			WHERE c.library_id = $1 AND c.parent_id = ANY($2::text[])
		`, []any{libraryID, seriesIDs}, func(rows pgx.Rows) error {
			var kid Child
			var parentID string
			var raw json.RawMessage
			if err := rows.Scan(&kid.ID, &kid.URI, &kid.URIPart, &kid.Order, &kid.OrderParts,
				&kid.CoverURI, &kid.FileMtime, &kid.Valid, &parentID, &raw); err != nil {
				return err
			}
			if dropped[kid.ID] {
				return nil
			}
			kid.Meta = metaraw.From(raw)
			children[parentID] = append(children[parentID], kid)
			return nil
		})
		if err != nil {
			return counts, err
		}
	}

	var seriesMeta []metaWrite
	for _, id := range seriesIDs {
		kids := children[id]
		if len(kids) == 0 {
			continue
		}
		ref := f.sets[id].Ref
		ordered := order(kids)

		orderIDs := make([]string, len(ordered))
		orderVals := make([]int, len(ordered))
		for i := range ordered {
			orderIDs[i], orderVals[i] = ordered[i].ID, i
			ordered[i].Order = &orderVals[i]
		}
		_, err := tx.Exec(ctx, `
			UPDATE content c SET "order" = r.o
			FROM unnest($2::text[], $3::int[]) AS r(id, o)
			WHERE c.library_id = $1 AND c.id = r.id AND c."order" IS DISTINCT FROM r.o
		`, libraryID, orderIDs, orderVals)
		if err != nil {
			return counts, err
		}

		cover, mtime := s.SeriesCover(ref, ordered)
		_, err = tx.Exec(ctx, `
			UPDATE content SET cover_uri = $2, file_mtime = $3, updated_at = $4
			WHERE id = $1 AND (cover_uri IS DISTINCT FROM $2 OR file_mtime IS DISTINCT FROM $3)
		`, id, cover, mtime, now)
		if err != nil {
			return counts, err
		}

		seriesMeta = append(seriesMeta, metaWrite{uri: ref.URI, file: inherit(ref, ordered)})
	}
	if err := writeMetadata(ctx, tx, libraryID, now, seriesMeta); err != nil {
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

func identityStep(ctx context.Context, tx pgx.Tx, libraryID string, set *SeriesChanges, pairs []rename, now time.Time) error {
	ref := set.Ref
	if set.New {
		return upsertContent(ctx, tx, models.Content{
			ID:         ref.ID,
			LibraryID:  libraryID,
			CreatedAt:  now,
			UpdatedAt:  now,
			Type:       ref.Type,
			URI:        ref.URI,
			URIPart:    ref.URIPart,
			Valid:      true,
			FileURI:    ref.FileURI,
			OrderParts: []*float32{},
		})
	}

	_, err := tx.Exec(ctx, "UPDATE content SET uri = $2, uri_part = $3, file_uri = $4, updated_at = $5 WHERE id = $1",
		ref.ID, ref.URI, ref.URIPart, ref.FileURI, now)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE content SET uri = $2 || '/' || uri_part, updated_at = $3 WHERE parent_id = $1",
		ref.ID, ref.URI, now)
	if err != nil {
		return err
	}
	return applyRenames(ctx, tx, libraryID, pairs)
}

func applyRenames(ctx context.Context, tx pgx.Tx, libraryID string, pairs []rename) error {
	if len(pairs) == 0 {
		return nil
	}
	olds := fp.Map(pairs, func(r rename) string { return r.Old })
	news := fp.Map(pairs, func(r rename) string { return r.New })
	stmts := []string{
		`DELETE FROM content_metadata m USING unnest($2::text[], $3::text[]) AS r(old, new)
		 WHERE m.library_id = $1 AND m.uri = r.new
		   AND EXISTS (SELECT 1 FROM content_metadata s WHERE s.library_id = $1 AND s.uri = r.old)`,
		`UPDATE content_metadata m SET uri = r.new
		 FROM unnest($2::text[], $3::text[]) AS r(old, new)
		 WHERE m.library_id = $1 AND m.uri = r.old`,
		`UPDATE user_to_content u SET uri = r.new
		 FROM unnest($2::text[], $3::text[]) AS r(old, new)
		 WHERE u.library_id = $1 AND u.uri = r.old
		   AND NOT EXISTS (SELECT 1 FROM user_to_content d
		                   WHERE d.user_id = u.user_id AND d.library_id = u.library_id AND d.uri = r.new)`,
		`UPDATE custom_list_to_content l SET uri = r.new
		 FROM unnest($2::text[], $3::text[]) AS r(old, new)
		 WHERE l.library_id = $1 AND l.uri = r.old
		   AND NOT EXISTS (SELECT 1 FROM custom_list_to_content d
		                   WHERE d.custom_list_id = l.custom_list_id AND d.library_id = l.library_id AND d.uri = r.new)`,
	}
	for _, stmt := range stmts {
		if _, err := tx.Exec(ctx, stmt, libraryID, olds, news); err != nil {
			return err
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
	err := query(ctx, tx, "SELECT uri, data_raw FROM content_metadata WHERE library_id = $1 AND uri = ANY($2::text[])",
		[]any{libraryID, uris}, func(qr pgx.Rows) error {
			var uri string
			var raw json.RawMessage
			if err := qr.Scan(&uri, &raw); err != nil {
				return err
			}
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

func query(ctx context.Context, tx pgx.Tx, sql string, args []any, scan func(pgx.Rows) error) error {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
