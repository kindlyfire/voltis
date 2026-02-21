package scanner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type metadataRow struct {
	URI       string
	LibraryID string
	Data      map[string]any
	DataRaw   map[string]json.RawMessage
	dirty     bool
}

func (m *metadataRow) setSource(source string, data map[string]any, raw map[string]any) {
	entry := map[string]any{"data": data}
	if raw != nil {
		entry["raw"] = raw
	} else {
		entry["raw"] = map[string]any{}
	}
	entryJSON, _ := json.Marshal(entry)
	m.DataRaw[source] = entryJSON
	m.dirty = true
}

// merge recomputes merged data from all layers.
func (m *metadataRow) merge() {
	merged := map[string]any{}
	for _, source := range metadataMergeOrder {
		raw, ok := m.DataRaw[source]
		if !ok {
			continue
		}
		var entry struct {
			Data map[string]any `json:"data"`
		}
		if json.Unmarshal(raw, &entry) == nil && entry.Data != nil {
			for k, v := range entry.Data {
				merged[k] = v
			}
		}
	}
	m.Data = merged
}

var metadataMergeOrder = []string{"file", "mangabaka", "overrides"}

type repository struct {
	pool      *pgxpool.Pool
	libraryID string

	content    []models.Content
	contentD   []models.Content // deleted
	metadata   []*metadataRow
	dirtyIDs   map[string]bool // content IDs that were modified
	uriRenames map[string]string
	parents    map[string]*models.Content // resolved series by URI
}

func newRepository(pool *pgxpool.Pool, libraryID string) *repository {
	return &repository{
		pool:       pool,
		libraryID:  libraryID,
		dirtyIDs:   map[string]bool{},
		uriRenames: map[string]string{},
		parents:    map[string]*models.Content{},
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
		var data map[string]any
		var dataRaw map[string]json.RawMessage
		_ = json.Unmarshal(m.Data, &data)
		_ = json.Unmarshal(m.DataRaw, &dataRaw)
		if data == nil {
			data = map[string]any{}
		}
		if dataRaw == nil {
			dataRaw = map[string]json.RawMessage{}
		}
		r.metadata[i] = &metadataRow{
			URI:       m.URI,
			LibraryID: m.LibraryID,
			Data:      data,
			DataRaw:   dataRaw,
		}
	}
	return nil
}

func (r *repository) markDirty(c *models.Content) {
	r.dirtyIDs[c.ID] = true
}

func (r *repository) getMetadata(uri string) *metadataRow {
	for _, m := range r.metadata {
		if m.URI == uri {
			return m
		}
	}
	m := &metadataRow{
		URI:       uri,
		LibraryID: r.libraryID,
		Data:      map[string]any{},
		DataRaw:   map[string]json.RawMessage{},
		dirty:     true,
	}
	r.metadata = append(r.metadata, m)
	return m
}

func (r *repository) matchDeletedItem(uriPart string, parentID *string) *models.Content {
	for i, c := range r.contentD {
		if c.URIPart == uriPart && ptrEq(c.ParentID, parentID) {
			r.contentD = append(r.contentD[:i], r.contentD[i+1:]...)
			r.content = append(r.content, c)
			return &r.content[len(r.content)-1]
		}
	}
	return nil
}

func (r *repository) checkURIAvailable(c *models.Content) bool {
	count := 0
	for i := range r.content {
		other := &r.content[i]
		if other.URIPart == c.URIPart && ptrEq(other.ParentID, c.ParentID) {
			if !r.isDeleted(other) {
				count++
			}
		}
	}
	if r.isInContent(c) {
		return count <= 1
	}
	return count == 0
}

func (r *repository) isDeleted(c *models.Content) bool {
	for i := range r.contentD {
		if r.contentD[i].ID == c.ID {
			return true
		}
	}
	return false
}

func (r *repository) isInContent(c *models.Content) bool {
	for i := range r.content {
		if r.content[i].ID == c.ID {
			return true
		}
	}
	return false
}

func (r *repository) removeContent(c *models.Content) {
	for i := range r.content {
		if r.content[i].ID == c.ID {
			r.contentD = append(r.contentD, r.content[i])
			r.content = append(r.content[:i], r.content[i+1:]...)
			return
		}
	}
}

func (r *repository) findContentByFileURI(fileURI string) *models.Content {
	for i := range r.content {
		if r.content[i].FileURI != nil && *r.content[i].FileURI == fileURI {
			return &r.content[i]
		}
	}
	return nil
}

func (r *repository) getSeries(uri, uriPart string, fileURI *string, contentType, title string) *models.Content {
	if c, ok := r.parents[uri]; ok {
		return c
	}

	// Find existing by URI or file_uri
	for i := range r.content {
		c := &r.content[i]
		if c.URI == uri || (fileURI != nil && c.FileURI != nil && *c.FileURI == *fileURI) {
			if c.URI != uri {
				r.updateURIs(c, uri)
			}
			c.URIPart = uriPart
			c.FileURI = fileURI
			r.markDirty(c)
			r.parents[uri] = c
			return c
		}
	}

	// Create new
	now := time.Now().UTC()
	newContent := models.Content{
		ID:         models.MakeContentID(),
		LibraryID:  r.libraryID,
		URIPart:    uriPart,
		URI:        uri,
		Type:       contentType,
		FileURI:    fileURI,
		OrderParts: []*float32{},
		Valid:      true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	r.content = append(r.content, newContent)
	c := &r.content[len(r.content)-1]
	r.markDirty(c)

	meta := r.getMetadata(uri)
	meta.setSource("file", map[string]any{"title": title}, nil)

	r.parents[uri] = c
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

func (r *repository) childrenOf(parentID string) []*models.Content {
	var children []*models.Content
	for i := range r.content {
		if r.content[i].ParentID != nil && *r.content[i].ParentID == parentID {
			children = append(children, &r.content[i])
		}
	}
	return children
}

func (r *repository) commit(ctx context.Context) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete removed content
	if len(r.contentD) > 0 {
		ids := make([]string, len(r.contentD))
		for i, c := range r.contentD {
			ids[i] = c.ID
		}
		_, err := tx.Exec(ctx, "DELETE FROM content WHERE id = ANY($1)", ids)
		if err != nil {
			return fmt.Errorf("delete content: %w", err)
		}
	}

	// Delete orphaned series
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
		// Remove from content slice (reverse order to preserve indices)
		for i := len(orphanIdxs) - 1; i >= 0; i-- {
			idx := orphanIdxs[i]
			r.content = append(r.content[:idx], r.content[idx+1:]...)
		}
	}

	// Upsert modified content
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

	// Handle URI renames
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

	// Upsert metadata
	for _, m := range r.metadata {
		if !m.dirty {
			continue
		}
		m.merge()
		dataJSON, _ := json.Marshal(m.Data)
		dataRawJSON, _ := json.Marshal(m.DataRaw)
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

	// Update library scanned_at
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

func compareOrderParts(a, b []*float32) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		ai, bi := a[i], b[i]
		if ai == nil && bi == nil {
			continue
		}
		if ai == nil {
			return 1
		}
		if bi == nil {
			return -1
		}
		if *ai < *bi {
			return -1
		}
		if *ai > *bi {
			return 1
		}
	}
	return len(a) - len(b)
}

func slog_scan(msg string, args ...any) {
	slog.Info("[scanner] "+msg, args...)
}
