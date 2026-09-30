package metadata

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"voltis/db"
	"voltis/lib/fp"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound = errors.New("content not found")
	ErrConflict = errors.New("changed since it was read")
)

type EntryKey struct {
	Provider string `json:"provider"`
	ID       string `json:"id"`
}

// Decoder turns a stored provider snapshot into a layer; providers.Registry implements it.
type Decoder interface {
	Layer(key EntryKey, raw json.RawMessage) (Fields, error)
	Order(provider string) int
}

type Target struct {
	ContentID string `db:"id"`
	LibraryID string `db:"library_id"`
	URI       string `db:"uri"`
	Type      string `db:"type"`
}

// Doc is a row's data_raw: its local layers. Rev counts overrides writes.
type Doc struct {
	V         int    `json:"v"`
	Rev       int64  `json:"rev"`
	File      Fields `json:"file,omitzero"`
	Overrides Fields `json:"overrides,omitzero"`
}

const docVersion = 2

// Local resolves the layers a row holds itself, without providers.
func (d Doc) Local() Fields {
	return Merge(Layer{"file", d.File}, Layer{"overrides", d.Overrides}).Fields
}

type FileLayer struct {
	ContentID string
	Fields    Fields
}

type Layers struct {
	File, Overrides Fields
	OverridesRev    int64
	Providers       []Layer // decoded linked snapshots, in registry order
	Resolved        Resolved
}

// linkError is what a linked row's last_error should say: why its snapshot does not decode, or
// "" for nothing.
type linkError struct{ contentID, provider, err string }

type Store struct{ dec Decoder }

func NewStore(dec Decoder) *Store { return &Store{dec: dec} }

// ReadTarget reads content without locking; writers use Lock.
func ReadTarget(ctx context.Context, q db.Querier, contentID string) (Target, error) {
	t, err := db.SelectOne[Target](ctx, q, "SELECT id, library_id, uri, type FROM content WHERE id = $1", contentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, ErrNotFound
	}
	return t, err
}

// Lock takes the metadata lock of the content's library and reads the content under it, so the
// content still exists for the write. Every metadata writer starts here, or after publishing
// entries.
func (s *Store) Lock(ctx context.Context, tx pgx.Tx, contentID string) (Target, error) {
	t, err := ReadTarget(ctx, tx, contentID)
	if err != nil {
		return Target{}, err
	}
	if err := db.LockMetadata(ctx, tx, t.LibraryID); err != nil {
		return Target{}, err
	}
	return ReadTarget(ctx, tx, contentID)
}

// WriteFileLayers replaces the file layers of rows in a locked library.
func (s *Store) WriteFileLayers(ctx context.Context, tx pgx.Tx, rows []FileLayer, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	docs, err := readDocs(ctx, tx, fp.Map(rows, func(r FileLayer) string { return r.ContentID }))
	if err != nil {
		return err
	}
	var changed []string
	for _, r := range rows {
		d := docs[r.ContentID]
		if matchInputs(d.File, d.Overrides) != matchInputs(r.Fields, d.Overrides) {
			changed = append(changed, r.ContentID)
		}
		d.File = r.Fields
		docs[r.ContentID] = d
	}
	if err := TouchLinks(ctx, tx, changed, now); err != nil {
		return err
	}
	_, err = s.write(ctx, tx, docs, true, now)
	return err
}

// SetOverrides replaces a locked row's overrides, failing with ErrConflict unless they are still
// at expectRev.
func (s *Store) SetOverrides(ctx context.Context, tx pgx.Tx, t Target, expectRev int64, o Fields) error {
	docs, err := readDocs(ctx, tx, []string{t.ContentID})
	if err != nil {
		return err
	}
	d := docs[t.ContentID]
	if d.Rev != expectRev {
		return ErrConflict
	}
	now := time.Now().UTC()
	if matchInputs(d.File, d.Overrides) != matchInputs(d.File, o) {
		if err := TouchLinks(ctx, tx, []string{t.ContentID}, now); err != nil {
			return err
		}
	}
	d.Rev, d.Overrides = d.Rev+1, o
	docs[t.ContentID] = d
	_, err = s.write(ctx, tx, docs, true, now)
	return err
}

// Recompute derives data from the local layers and linked snapshots, and reports whether it
// changed a row's data or a link's error. A snapshot that no longer decodes is left out, and its
// link's last_error says why.
func (s *Store) Recompute(ctx context.Context, tx pgx.Tx, contentIDs []string) (changed bool, err error) {
	docs, err := readDocs(ctx, tx, contentIDs)
	if err != nil {
		return false, err
	}
	return s.write(ctx, tx, docs, false, time.Now().UTC())
}

// write derives the rows' data from docs and their linked snapshots, and writes data_raw too when
// raw is set. A row is written only when its layers or data change, or an older version derived
// it, and its meta_updated_at moves only with its layers or data.
func (s *Store) write(ctx context.Context, tx pgx.Tx, docs map[string]Doc, raw bool, now time.Time) (bool, error) {
	all, errs, err := s.load(ctx, tx, docs)
	if err != nil {
		return false, err
	}
	for _, c := range errs {
		if c.err != "" {
			slog.Warn("[metadata] leaving out a linked snapshot that does not decode",
				"content", c.contentID, "provider", c.provider, "err", c.err)
		}
	}
	if len(errs) > 0 {
		_, err := tx.Exec(ctx, `
			UPDATE metadata_links l SET last_error = NULLIF(r.err, '')
			FROM unnest($1::text[], $2::text[], $3::text[]) AS r(content_id, provider, err)
			WHERE l.content_id = r.content_id AND l.provider = r.provider
		`, fp.Map(errs, func(c linkError) string { return c.contentID }),
			fp.Map(errs, func(c linkError) string { return c.provider }),
			fp.Map(errs, func(c linkError) string { return c.err }))
		if err != nil {
			return false, err
		}
	}
	var ids []string
	var raws []*string // nil keeps data_raw
	var data []string
	for id, l := range all {
		b, err := json.Marshal(l.Resolved.Fields)
		if err != nil {
			return false, err
		}
		var r *string
		if raw {
			d := docs[id]
			d.V = docVersion
			rb, err := json.Marshal(d)
			if err != nil {
				return false, err
			}
			r = new(string(rb))
		}
		ids, raws, data = append(ids, id), append(raws, r), append(data, string(b))
	}
	changed, err := db.SelectScalars[bool](ctx, tx, `
		UPDATE content c SET data_raw = coalesce(r.raw, c.data_raw), data = r.data, data_version = $4,
			meta_updated_at = CASE WHEN c.data_raw <> coalesce(r.raw, c.data_raw) OR c.data <> r.data
				THEN $5 ELSE c.meta_updated_at END
		FROM unnest($1::text[], $2::jsonb[], $3::jsonb[]) AS r(id, raw, data)
		WHERE c.id = r.id AND (c.data_raw <> coalesce(r.raw, c.data_raw) OR c.data <> r.data OR c.data_version < $4)
		RETURNING old.data IS DISTINCT FROM new.data
	`, ids, raws, data, DataVersion, now)
	return len(errs) > 0 || slices.Contains(changed, true), err
}

// Load reads a row's layers, leaving out snapshots that no longer decode.
func (s *Store) Load(ctx context.Context, q db.Querier, t Target) (Layers, error) {
	docs, err := readDocs(ctx, q, []string{t.ContentID})
	if err != nil {
		return Layers{}, err
	}
	all, _, err := s.load(ctx, q, docs)
	if err != nil {
		return Layers{}, err
	}
	if l := all[t.ContentID]; l != nil {
		return *l, nil
	}
	return Layers{Resolved: Merge()}, nil
}

// LeafDoc is a leaf's data_raw and data, for a scan to write with the row.
type LeafDoc struct{ Raw, Data string }

// NewLeafDoc replaces a linkless leaf's file layer in d, and reports whether its match inputs changed.
func NewLeafDoc(d Doc, file Fields) (LeafDoc, bool, error) {
	changed := matchInputs(d.File, d.Overrides) != matchInputs(file, d.Overrides)
	d.V, d.File = docVersion, file
	raw, err := json.Marshal(d)
	if err != nil {
		return LeafDoc{}, false, err
	}
	data, err := json.Marshal(d.Local())
	return LeafDoc{string(raw), string(data)}, changed, err
}

// Matches is a condition that the bm25-indexed field of content c matches the words of the named
// argument @search, each as a prefix and, from three characters, with a typo: all of them, or any.
// The search goes in as a function argument: generic plans reject a parameter cast to pdb.fuzzy.
func Matches(c, field string, all bool, search string) string {
	dist := 1
	if len(search) < 3 {
		dist = 0
	}
	return fmt.Sprintf("%s.id @@@ paradedb.match('%s', @search, distance => %d, prefix => true, conjunction_mode => %t)",
		c, field, dist, all)
}

// ExactTitle is a condition that the named argument @search is one of content c's titles, whatever
// the case, read from search_text: its title line, or an alt title as its JSON string. Searches
// rank these first, as fuzzy matches all score alike.
const ExactTitle = `(lower(split_part(c.search_text, E'\n', 1)) = lower(@search)
	OR strpos(lower(c.search_text), lower(to_jsonb(@search::text)::text)) > 0)`

// LinkedEntry joins a link l to the entry e it reads: the one its id's merges lead to. join is
// "JOIN" or "LEFT JOIN".
func LinkedEntry(join string) string {
	return join + ` provider_entries a ON a.provider = l.provider AND a.external_id = l.external_id ` +
		join + ` provider_entries e ON e.provider = a.provider AND e.external_id = a.canonical_id`
}

// load reads the linked snapshots of rows whose local layers are in docs, and the links whose
// last_error no longer says why their snapshot does not decode.
func (s *Store) load(ctx context.Context, q db.Querier, docs map[string]Doc) (map[string]*Layers, []linkError, error) {
	type linked struct {
		ContentID  string          `db:"content_id"`
		Provider   string          `db:"provider"`
		ExternalID string          `db:"external_id"`
		Raw        json.RawMessage `db:"raw"`
		LastError  string          `db:"last_error"`
	}
	links, err := db.Select[linked](ctx, q, `
		SELECT l.content_id, l.provider, e.external_id, e.raw, coalesce(l.last_error, '') AS last_error
		FROM metadata_links l `+LinkedEntry("JOIN")+`
		WHERE l.content_id = ANY($1) AND l.state = 'linked'`, slices.Collect(maps.Keys(docs)))
	if err != nil {
		return nil, nil, err
	}

	out := make(map[string]*Layers, len(docs))
	for id, d := range docs {
		out[id] = &Layers{File: d.File, Overrides: d.Overrides, OverridesRev: d.Rev}
	}
	var changed []linkError
	for _, r := range links {
		l := out[r.ContentID]
		f, err := s.dec.Layer(EntryKey{r.Provider, r.ExternalID}, r.Raw)
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			l.Providers = append(l.Providers, Layer{r.Provider, f})
		}
		if msg != r.LastError {
			changed = append(changed, linkError{r.ContentID, r.Provider, msg})
		}
	}
	for _, l := range out {
		slices.SortFunc(l.Providers, func(a, b Layer) int { return cmp.Compare(s.dec.Order(a.Source), s.dec.Order(b.Source)) })
		l.Resolved = Merge(slices.Concat([]Layer{{"file", l.File}}, l.Providers, []Layer{{"overrides", l.Overrides}})...)
	}
	return out, changed, nil
}

// readDocs reads rows' local layers.
func readDocs(ctx context.Context, q db.Querier, contentIDs []string) (map[string]Doc, error) {
	type row struct {
		ID  string `db:"id"`
		Doc Doc    `db:"data_raw"`
	}
	rows, err := db.Select[row](ctx, q, "SELECT id, data_raw FROM content WHERE id = ANY($1)", contentIDs)
	if err != nil {
		return nil, err
	}
	docs := make(map[string]Doc, len(rows))
	for _, r := range rows {
		docs[r.ID] = r.Doc
	}
	return docs, nil
}

// matchInputs is what automatic matching reads from a row's own layers, as a series or as a child
// of one.
func matchInputs(file, overrides Fields) string {
	f := Doc{File: file, Overrides: overrides}.Local()
	b, _ := json.Marshal([]any{f.Title, f.AltTitles, f.PublicationDate, f.Staff, f.Series, f.Volume})
	return string(b)
}

// TouchLinks makes the pending links of the rows, and of their series, due again, since their
// inputs changed. Scans write content before its layers, so new children have their parent.
func TouchLinks(ctx context.Context, tx pgx.Tx, contentIDs []string, now time.Time) error {
	if len(contentIDs) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE metadata_links SET retry_at = $2
		WHERE state IN ('review', 'unmatched')
		  AND (content_id = ANY($1) OR content_id IN (SELECT parent_id FROM content WHERE id = ANY($1)))
	`, contentIDs, now)
	return err
}
