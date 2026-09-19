package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"
	"voltis/models/metaraw"

	"github.com/jackc/pgx/v5/pgxpool"
)

type metadataRow struct {
	URI       string
	LibraryID string
	DataRaw   metaraw.MetadataRaw
	dirty     bool
}

type repository struct {
	pool      *pgxpool.Pool
	libraryID string

	content        []models.Content
	deletedContent []models.Content
	metadata       []*metadataRow
	dirtyIDs       map[string]bool
	uriRenames     map[string]string
	parents        map[string]string
	dirs           map[string]*dirPick
	seeded         map[string]string
	moved          map[string]bool
}

func newRepository(pool *pgxpool.Pool, libraryID string) *repository {
	return &repository{
		pool:       pool,
		libraryID:  libraryID,
		dirtyIDs:   map[string]bool{},
		uriRenames: map[string]string{},
		parents:    map[string]string{},
		dirs:       map[string]*dirPick{},
		seeded:     map[string]string{},
		moved:      map[string]bool{},
	}
}

func (r *repository) load(ctx context.Context) error {
	var err error
	r.content, err = db.Select[models.Content](ctx, r.pool,
		"SELECT * FROM content WHERE library_id = $1", r.libraryID)
	if err != nil {
		return err
	}

	type metaDBRow struct {
		URI       string          `db:"uri"`
		LibraryID string          `db:"library_id"`
		Data      json.RawMessage `db:"data"`
		DataRaw   json.RawMessage `db:"data_raw"`
	}
	dbMeta, err := db.Select[metaDBRow](ctx, r.pool,
		"SELECT uri, library_id, data, data_raw FROM content_metadata WHERE library_id = $1",
		r.libraryID)
	if err != nil {
		return err
	}

	r.metadata = make([]*metadataRow, len(dbMeta))
	for i, m := range dbMeta {
		dataRaw := metaraw.From(m.DataRaw)
		r.metadata[i] = &metadataRow{
			URI:       m.URI,
			LibraryID: m.LibraryID,
			DataRaw:   dataRaw,
		}
	}
	return nil
}

func (r *repository) markDirty(c *models.Content) {
	r.dirtyIDs[c.ID] = true
}

func (r *repository) getMetadata(uri string) *metadataRow {
	if i := slices.IndexFunc(r.metadata, func(m *metadataRow) bool { return m.URI == uri }); i >= 0 {
		return r.metadata[i]
	}
	m := &metadataRow{
		URI:       uri,
		LibraryID: r.libraryID,
		dirty:     true,
	}
	r.metadata = append(r.metadata, m)
	return m
}

func (r *repository) matchDeletedItem(uriPart string, parentID *string) *models.Content {
	for i, c := range r.deletedContent {
		if c.URIPart == uriPart && ptrEq(c.ParentID, parentID) {
			r.deletedContent = slices.Delete(r.deletedContent, i, i+1)
			r.content = append(r.content, c)
			return &r.content[len(r.content)-1]
		}
	}
	return nil
}

func (r *repository) checkURIAvailable(parsed *ParsedItem, parentID *string) bool {
	return !slices.ContainsFunc(r.content, func(other models.Content) bool {
		return other.URIPart == parsed.URIPart && ptrEq(other.ParentID, parentID) &&
			(other.FileURI == nil || *other.FileURI != parsed.File.Path)
	})
}

func (r *repository) removeContent(c *models.Content) {
	if i := slices.IndexFunc(r.content, func(x models.Content) bool { return x.ID == c.ID }); i >= 0 {
		r.deletedContent = append(r.deletedContent, r.content[i])
		r.content = slices.Delete(r.content, i, i+1)
	}
}

func (r *repository) invalidateFile(path string) *string {
	c := r.findContentByFileURI(path)
	if c == nil {
		return nil
	}
	c.Valid = false
	c.UpdatedAt = time.Now().UTC()
	r.markDirty(c)
	return c.ParentID
}

func (r *repository) byID(id string) *models.Content {
	if i := slices.IndexFunc(r.content, func(c models.Content) bool { return c.ID == id }); i >= 0 {
		return &r.content[i]
	}
	return nil
}

func (r *repository) findContentByFileURI(fileURI string) *models.Content {
	for i := range r.content {
		if r.content[i].FileURI != nil && *r.content[i].FileURI == fileURI {
			return &r.content[i]
		}
	}
	return nil
}

func (r *repository) seriesDirs(c *models.Content) *dirPick {
	if d, ok := r.dirs[c.ID]; ok {
		return d
	}
	d := &dirPick{stored: c.FileURI}
	r.dirs[c.ID] = d
	if c.FileURI == nil {
		return d
	}
	for i := range r.content {
		m := &r.content[i]
		if m.FileURI != nil && m.ParentID != nil && *m.ParentID == c.ID {
			dir := filepath.Dir(*m.FileURI)
			d.add(dir)
			r.seeded[m.ID] = dir
		}
	}
	return d
}

func (r *repository) placeSeries(c *models.Content, dir *string) bool {
	d := r.seriesDirs(c)
	if dir != nil {
		d.add(*dir)
	}
	picked := d.pick()
	if ptrEq(c.FileURI, picked) {
		return false
	}
	c.FileURI = picked
	r.markDirty(c)
	return true
}

func (r *repository) retarget() {
	for i := range r.content {
		c := &r.content[i]
		if isGroupingType(c.Type) && r.placeSeries(c, nil) {
			r.moved[c.ID] = true
		}
	}
}

func (r *repository) reparent(c *models.Content, parentID *string) {
	if c.ParentID == nil || ptrEq(c.ParentID, parentID) {
		return
	}
	old := r.byID(*c.ParentID)
	if old == nil {
		return
	}
	d := r.seriesDirs(old)
	dir, ok := r.seeded[c.ID]
	if !ok {
		return
	}
	delete(r.seeded, c.ID)
	d.drop(dir)
	if r.placeSeries(old, nil) {
		r.moved[old.ID] = true
	}
}

func (r *repository) findSeries(uri string, fileURI *string) *models.Content {
	var byDir *models.Content
	for i := range r.content {
		c := &r.content[i]
		if c.URI == uri {
			return c
		}
		if fileURI != nil && c.FileURI != nil && *c.FileURI == *fileURI &&
			(byDir == nil || c.ID < byDir.ID) {
			byDir = c
		}
	}
	return byDir
}

func (r *repository) getSeries(uri, uriPart string, fileURI *string, contentType, title string) *models.Content {
	if c := r.byID(r.parents[uri]); c != nil {
		return c
	}

	if c := r.findSeries(uri, fileURI); c != nil {
		if !isGroupingType(c.Type) {
			return nil
		}
		if c.URI != uri {
			r.updateURIs(c, uri)
		}
		c.URIPart = uriPart
		r.markDirty(c)
		r.parents[uri] = c.ID
		return c
	}

	now := time.Now().UTC()
	newContent := models.Content{
		ID:         models.MakeContentID(),
		LibraryID:  r.libraryID,
		URIPart:    uriPart,
		URI:        uri,
		Type:       contentType,
		OrderParts: []*float32{},
		Valid:      true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.content = append(r.content, newContent)
	c := &r.content[len(r.content)-1]
	r.dirs[c.ID] = &dirPick{}
	r.markDirty(c)

	meta := r.getMetadata(uri)
	meta.DataRaw.File = &metaraw.RawContainer[models.Metadata]{Raw: models.Metadata{Title: title}}
	meta.dirty = true

	r.parents[uri] = c.ID
	return c
}

func (r *repository) updateURIs(c *models.Content, newURI string) {
	if c.URI == newURI {
		return
	}
	oldURI := c.URI
	c.URI = newURI
	r.uriRenames[oldURI] = newURI

	for _, m := range r.metadata {
		if m.URI == oldURI {
			m.URI = newURI
			m.dirty = true
		}
	}

	for i := range r.content {
		child := &r.content[i]
		if child.ParentID != nil && *child.ParentID == c.ID {
			childNewURI := newURI + "/" + child.URIPart
			r.updateURIs(child, childNewURI)
		}
	}
}

func (r *repository) children(items []*models.Content) []Child {
	return fp.Map(items, func(c *models.Content) Child {
		return Child{
			ID:         c.ID,
			URI:        c.URI,
			URIPart:    c.URIPart,
			Order:      c.Order,
			OrderParts: c.OrderParts,
			CoverURI:   c.CoverURI,
			FileMtime:  c.FileMtime,
			Valid:      c.Valid,
			Meta:       r.getMetadata(c.URI).DataRaw,
		}
	})
}

func seriesRef(c *models.Content) SeriesRef {
	return SeriesRef{ID: c.ID, URI: c.URI, URIPart: c.URIPart, Type: c.Type, FileURI: c.FileURI}
}

func (r *repository) childrenOf(parentID string) []*models.Content {
	var children []*models.Content
	for i := range r.content {
		if r.content[i].ParentID != nil && *r.content[i].ParentID == parentID {
			children = append(children, &r.content[i])
		}
	}
	return children
}

func (r *repository) commitGroup(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for i := range r.content {
		c := &r.content[i]
		if !r.dirtyIDs[c.ID] {
			continue
		}

		fileDataJSON := c.FileData
		if fileDataJSON == nil {
			fileDataJSON = json.RawMessage("{}")
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO content (id, created_at, updated_at, uri_part, uri, valid, file_uri,
				file_mtime, file_size, cover_uri, type, "order", order_parts, file_data,
				parent_id, library_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			ON CONFLICT (id) DO UPDATE SET
				uri_part = $4, uri = $5, valid = $6, file_uri = $7, file_mtime = $8,
				file_size = $9, cover_uri = $10, type = $11, "order" = $12, order_parts = $13,
				file_data = $14, parent_id = $15, updated_at = $3
		`, c.ID, c.CreatedAt, c.UpdatedAt, c.URIPart, c.URI, c.Valid, c.FileURI,
			c.FileMtime, c.FileSize, c.CoverURI, c.Type, c.Order, c.OrderParts,
			fileDataJSON, c.ParentID, c.LibraryID)
		if err != nil {
			return fmt.Errorf("upsert content %s: %w", c.ID, err)
		}
	}

	for oldURI, newURI := range r.uriRenames {
		_, _ = tx.Exec(ctx, `
			DELETE FROM content_metadata WHERE uri = $1 AND library_id = $2
		`, oldURI, r.libraryID)
		_, _ = tx.Exec(ctx, `
			UPDATE user_to_content SET uri = $1 WHERE uri = $2 AND library_id = $3
		`, newURI, oldURI, r.libraryID)
		_, _ = tx.Exec(ctx, `
			UPDATE custom_list_to_content SET uri = $1 WHERE uri = $2 AND library_id = $3
		`, newURI, oldURI, r.libraryID)
	}

	for _, m := range r.metadata {
		if !m.dirty {
			continue
		}
		dataJSON, _ := json.Marshal(m.DataRaw.Merge())
		dataRawJSON := m.DataRaw.Dump()
		now := time.Now().UTC()

		_, err := tx.Exec(ctx, `
			INSERT INTO content_metadata (uri, library_id, data, data_raw, updated_at)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (uri, library_id) DO UPDATE SET
				data = $3, data_raw = $4, updated_at = $5
		`, m.URI, m.LibraryID, dataJSON, dataRawJSON, now)
		if err != nil {
			return fmt.Errorf("upsert metadata %s: %w", m.URI, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	clear(r.dirtyIDs)
	clear(r.uriRenames)
	for _, m := range r.metadata {
		m.dirty = false
	}
	return nil
}

func (r *repository) commitFinal(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if len(r.deletedContent) > 0 {
		ids := fp.Map(r.deletedContent, func(c models.Content) string { return c.ID })
		_, err := tx.Exec(ctx, "DELETE FROM content WHERE id = ANY($1)", ids)
		if err != nil {
			return fmt.Errorf("delete content: %w", err)
		}
	}

	parentIDs := map[string]bool{}
	for i := range r.content {
		if r.content[i].ParentID != nil {
			parentIDs[*r.content[i].ParentID] = true
		}
	}
	var orphanIDs []string
	var orphanIdxs []int
	for i := range r.content {
		c := &r.content[i]
		if isGroupingType(c.Type) && !parentIDs[c.ID] {
			orphanIDs = append(orphanIDs, c.ID)
			orphanIdxs = append(orphanIdxs, i)
		}
	}
	if len(orphanIDs) > 0 {
		_, err := tx.Exec(ctx, "DELETE FROM content WHERE id = ANY($1)", orphanIDs)
		if err != nil {
			return fmt.Errorf("delete orphans: %w", err)
		}
		for _, idx := range slices.Backward(orphanIdxs) {
			r.content = slices.Delete(r.content, idx, idx+1)
		}
	}

	_, err = tx.Exec(ctx, "UPDATE libraries SET scanned_at = $1 WHERE id = $2",
		time.Now().UTC(), r.libraryID)
	if err != nil {
		return fmt.Errorf("update library: %w", err)
	}

	return tx.Commit(ctx)
}

func isGroupingType(t string) bool {
	return t == "comic_series" || t == "book_series"
}

func ptrEq(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func slog_scan(msg string, args ...any) {
	slog.Info("[scanner] "+msg, args...)
}
