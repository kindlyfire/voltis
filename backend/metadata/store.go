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
	"strings"
	"time"

	"voltis/db"
	"voltis/lib/fp"

	"github.com/jackc/pgx/v5"
)

var (
	ErrNotFound = errors.New("content not found")
	ErrConflict = errors.New("changed since it was read")
	ErrOccupied = errors.New("destination occupied")
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
	URI    string
	Fields Fields
}

type Layers struct {
	File, Overrides Fields
	OverridesRev    int64
	Providers       []Layer // decoded linked snapshots, in registry order
	Resolved        Resolved
}

// linkError is what a linked row's last_error should say: why its snapshot does not decode, or
// "" for nothing.
type linkError struct{ uri, provider, err string }

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

// Lock takes the metadata lock of the content's library and reads the content under it, so a
// write lands on its current URI. Every metadata writer starts here, or after publishing entries.
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
func (s *Store) WriteFileLayers(ctx context.Context, tx pgx.Tx, libraryID string, rows []FileLayer, now time.Time) error {
	if len(rows) == 0 {
		return nil
	}
	uris := fp.Map(rows, func(r FileLayer) string { return r.URI })
	docs, err := readDocs(ctx, tx, libraryID, uris)
	if err != nil {
		return err
	}
	var changed []string
	out := make([]Doc, len(rows))
	for i, r := range rows {
		d := docs[r.URI]
		if matchInputs(d.File, d.Overrides) != matchInputs(r.Fields, d.Overrides) {
			changed = append(changed, r.URI)
		}
		d.File = r.Fields
		out[i] = d
	}
	if err := writeDocs(ctx, tx, libraryID, uris, out, now); err != nil {
		return err
	}
	if err := touchLinks(ctx, tx, libraryID, changed, now); err != nil {
		return err
	}
	_, err = s.Recompute(ctx, tx, libraryID, uris)
	return err
}

// SetOverrides replaces a locked row's overrides, failing with ErrConflict unless they are still
// at expectRev.
func (s *Store) SetOverrides(ctx context.Context, tx pgx.Tx, t Target, expectRev int64, o Fields) error {
	docs, err := readDocs(ctx, tx, t.LibraryID, []string{t.URI})
	if err != nil {
		return err
	}
	d := docs[t.URI]
	if d.Rev != expectRev {
		return ErrConflict
	}
	now := time.Now().UTC()
	if matchInputs(d.File, d.Overrides) != matchInputs(d.File, o) {
		if err := touchLinks(ctx, tx, t.LibraryID, []string{t.URI}, now); err != nil {
			return err
		}
	}
	d.Rev, d.Overrides = d.Rev+1, o
	if err := writeDocs(ctx, tx, t.LibraryID, []string{t.URI}, []Doc{d}, now); err != nil {
		return err
	}
	_, err = s.Recompute(ctx, tx, t.LibraryID, []string{t.URI})
	return err
}

// Recompute derives data from the local layers and linked snapshots, and reports whether it
// changed a row or a link's error; a row whose data comes out the same is not written. A snapshot
// that no longer decodes is left out, and its link's last_error says why.
func (s *Store) Recompute(ctx context.Context, tx pgx.Tx, libraryID string, uris []string) (changed bool, err error) {
	all, errs, err := s.load(ctx, tx, libraryID, uris)
	if err != nil {
		return false, err
	}
	for _, c := range errs {
		if c.err != "" {
			slog.Warn("[metadata] leaving out a linked snapshot that does not decode",
				"library", libraryID, "uri", c.uri, "provider", c.provider, "err", c.err)
		}
	}
	if len(errs) > 0 {
		_, err := tx.Exec(ctx, `
			UPDATE metadata_links l SET last_error = NULLIF(r.err, '')
			FROM unnest($2::text[], $3::text[], $4::text[]) AS r(uri, provider, err)
			WHERE l.library_id = $1 AND l.uri = r.uri AND l.provider = r.provider
		`, libraryID, fp.Map(errs, func(c linkError) string { return c.uri }),
			fp.Map(errs, func(c linkError) string { return c.provider }),
			fp.Map(errs, func(c linkError) string { return c.err }))
		if err != nil {
			return false, err
		}
	}
	var keys, data []string
	for uri, l := range all {
		b, err := json.Marshal(l.Resolved.Fields)
		if err != nil {
			return false, err
		}
		keys, data = append(keys, uri), append(data, string(b))
	}
	empty, _ := json.Marshal(Doc{V: docVersion})
	tag, err := tx.Exec(ctx, `
		INSERT INTO content_metadata (uri, library_id, data, data_raw, data_version, updated_at)
		SELECT r.uri, $1, r.data, $2, $3, now() FROM unnest($4::text[], $5::jsonb[]) AS r(uri, data)
		ON CONFLICT (uri, library_id) DO UPDATE
		SET data = EXCLUDED.data, data_version = EXCLUDED.data_version, updated_at = EXCLUDED.updated_at
		WHERE content_metadata.data <> EXCLUDED.data
	`, libraryID, empty, DataVersion, keys, data)
	if err != nil {
		return false, err
	}
	// A row an older version derived the same way only needs its version, which leaves data and
	// the search index alone.
	_, err = tx.Exec(ctx, `
		UPDATE content_metadata SET data_version = $3 WHERE library_id = $1 AND uri = ANY($2) AND data_version < $3
	`, libraryID, keys, DataVersion)
	return len(errs) > 0 || tag.RowsAffected() > 0, err
}

// Load reads a row's layers, leaving out snapshots that no longer decode and links it cannot hold.
func (s *Store) Load(ctx context.Context, q db.Querier, t Target) (Layers, error) {
	all, _, err := s.load(ctx, q, t.LibraryID, []string{t.URI})
	if err != nil {
		return Layers{}, err
	}
	if l := all[t.URI]; l != nil {
		return *l, nil
	}
	return Layers{Resolved: Merge()}, nil
}

// Conditions on content_metadata m and metadata_links l for rows that hold an admin's decision,
// and so outlive their content: metadata with overrides, and links that are decided or reject ids.
const (
	KeptMetadata = `m.data_raw ? 'overrides'`
	KeptLink     = `(l.state IN ('linked', 'ignored') OR l.rejected <> '{}')`
	// HeldMetadata is a condition on metadata m that content has its URI.
	HeldMetadata = `EXISTS (SELECT 1 FROM content c WHERE c.library_id = m.library_id AND c.uri = m.uri)`
)

var (
	// SeriesContent is a condition on content c: provider data attaches to series only. It assumes
	// providers describe every series type, as FixOrphans alone checks a provider's kinds.
	SeriesContent = "c.type IN ('" + strings.Join(SeriesTypes, "', '") + "')"
	// HeldLink is a condition on a link l that its URI holds content it attaches to; any other
	// link is an orphan, even on a leaf that took over a removed series' URI.
	HeldLink = `EXISTS (SELECT 1 FROM content c WHERE c.library_id = l.library_id AND c.uri = l.uri AND ` +
		SeriesContent + `)`
)

// Matches is a condition that the bm25-indexed field of metadata m matches the words of the named
// argument @search, each as a prefix and, from three characters, with a typo: all of them, or any.
// The search goes in as a function argument: generic plans reject a parameter cast to pdb.fuzzy.
func Matches(m, field string, all bool, search string) string {
	dist := 1
	if len(search) < 3 {
		dist = 0
	}
	return fmt.Sprintf("%s.id @@@ paradedb.match('%s', @search, distance => %d, prefix => true, conjunction_mode => %t)",
		m, field, dist, all)
}

// ExactTitle is a condition that the named argument @search is one of metadata m's titles, whatever
// the case, read from search_text: its title line, or an alt title as its JSON string; false
// without metadata. Searches rank these first, as fuzzy matches all score alike.
const ExactTitle = `coalesce(lower(split_part(m.search_text, E'\n', 1)) = lower(@search)
	OR strpos(lower(m.search_text), lower(to_jsonb(@search::text)::text)) > 0, false)`

// LinkedEntry joins a link l to the entry e it reads: the one its id's merges lead to. join is
// "JOIN" or "LEFT JOIN".
func LinkedEntry(join string) string {
	return join + ` provider_entries a ON a.provider = l.provider AND a.external_id = l.external_id ` +
		join + ` provider_entries e ON e.provider = a.provider AND e.external_id = a.canonical_id`
}

// CollectOrphans deletes the orphaned rows that keep nothing an admin decided. A kept link on a
// leaf moves up to its series first, when that has no row for the provider, unless it is linked:
// the entry was chosen for another series, so it stays an orphan to repair.
func (s *Store) CollectOrphans(ctx context.Context, tx pgx.Tx, libraryID string) error {
	for _, sql := range []string{`
		WITH moves AS (
			SELECT DISTINCT ON (p.uri, l.provider) l.uri, l.provider, p.uri AS parent
			FROM metadata_links l
			JOIN content c ON c.library_id = l.library_id AND c.uri = l.uri
			JOIN content p ON p.id = c.parent_id
			WHERE l.library_id = $1 AND NOT ` + SeriesContent + ` AND ` + KeptLink + ` AND l.state <> 'linked'
			  AND NOT EXISTS (SELECT 1 FROM metadata_links x
			                  WHERE x.library_id = $1 AND x.uri = p.uri AND x.provider = l.provider)
			ORDER BY p.uri, l.provider, l.uri)
		UPDATE metadata_links l SET uri = m.parent,
			retry_at = CASE WHEN l.state IN ('review', 'unmatched') THEN now() END -- matching reads the series now
		FROM moves m WHERE l.library_id = $1 AND l.uri = m.uri AND l.provider = m.provider`, `
		DELETE FROM content_metadata m
		WHERE m.library_id = $1 AND NOT ` + KeptMetadata + ` AND NOT ` + HeldMetadata, `
		DELETE FROM metadata_links l WHERE l.library_id = $1 AND NOT ` + KeptLink + ` AND NOT ` + HeldLink,
	} {
		if _, err := tx.Exec(ctx, sql, libraryID); err != nil {
			return err
		}
	}
	return nil
}

// FixOrphans moves the overrides of each source in moves onto its destination, then deletes the
// rows in deleted, in a locked library whose content the caller checked the repair against. A move
// keeps the destination's file layer and brings the overrides only onto a row without any, else
// fails with ErrOccupied. The caller moves the links and recomputes the destinations.
func (s *Store) FixOrphans(ctx context.Context, tx pgx.Tx, libraryID string, deleted []string, moves map[string]string) error {
	docs, err := readDocs(ctx, tx, libraryID, slices.Concat(slices.Collect(maps.Keys(moves)), slices.Collect(maps.Values(moves))))
	if err != nil {
		return err
	}
	var uris, changed []string
	var out []Doc
	for _, src := range slices.Sorted(maps.Keys(moves)) {
		from, dst := docs[src], moves[src]
		if from.Overrides.IsZero() {
			continue
		}
		to := docs[dst]
		if !to.Overrides.IsZero() {
			return fmt.Errorf("%w: %s has overrides", ErrOccupied, dst)
		}
		if matchInputs(to.File, to.Overrides) != matchInputs(to.File, from.Overrides) {
			changed = append(changed, dst)
		}
		to.Rev, to.Overrides = to.Rev+1, from.Overrides
		docs[dst] = to
		uris, out = append(uris, dst), append(out, to)
	}
	if _, err := tx.Exec(ctx, "DELETE FROM content_metadata WHERE library_id = $1 AND uri = ANY($2)", libraryID, deleted); err != nil {
		return err
	}
	now := time.Now().UTC()
	if err := writeDocs(ctx, tx, libraryID, uris, out, now); err != nil {
		return err
	}
	return touchLinks(ctx, tx, libraryID, changed, now)
}

// load reads rows' layers, and the links whose last_error no longer says why their snapshot does
// not decode.
func (s *Store) load(ctx context.Context, q db.Querier, libraryID string, uris []string) (map[string]*Layers, []linkError, error) {
	docs, err := readDocs(ctx, q, libraryID, uris)
	if err != nil {
		return nil, nil, err
	}
	type linked struct {
		URI        string          `db:"uri"`
		Provider   string          `db:"provider"`
		ExternalID string          `db:"external_id"`
		Raw        json.RawMessage `db:"raw"`
		LastError  string          `db:"last_error"`
	}
	links, err := db.Select[linked](ctx, q, `
		SELECT l.uri, l.provider, e.external_id, e.raw, coalesce(l.last_error, '') AS last_error
		FROM metadata_links l `+LinkedEntry("JOIN")+`
		WHERE l.library_id = $1 AND l.uri = ANY($2) AND l.state = 'linked' AND `+HeldLink, libraryID, uris)
	if err != nil {
		return nil, nil, err
	}

	out := map[string]*Layers{}
	get := func(uri string) *Layers {
		if out[uri] == nil {
			d := docs[uri]
			out[uri] = &Layers{File: d.File, Overrides: d.Overrides, OverridesRev: d.Rev}
		}
		return out[uri]
	}
	for uri := range docs {
		get(uri)
	}
	var changed []linkError
	for _, r := range links {
		l := get(r.URI)
		f, err := s.dec.Layer(EntryKey{r.Provider, r.ExternalID}, r.Raw)
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			l.Providers = append(l.Providers, Layer{r.Provider, f})
		}
		if msg != r.LastError {
			changed = append(changed, linkError{r.URI, r.Provider, msg})
		}
	}
	for _, l := range out {
		slices.SortFunc(l.Providers, func(a, b Layer) int { return cmp.Compare(s.dec.Order(a.Source), s.dec.Order(b.Source)) })
		l.Resolved = Merge(slices.Concat([]Layer{{"file", l.File}}, l.Providers, []Layer{{"overrides", l.Overrides}})...)
	}
	return out, changed, nil
}

func readDocs(ctx context.Context, q db.Querier, libraryID string, uris []string) (map[string]Doc, error) {
	type row struct {
		URI string `db:"uri"`
		Doc Doc    `db:"data_raw"`
	}
	rows, err := db.Select[row](ctx, q,
		"SELECT uri, data_raw FROM content_metadata WHERE library_id = $1 AND uri = ANY($2)", libraryID, uris)
	if err != nil {
		return nil, err
	}
	docs := make(map[string]Doc, len(rows))
	for _, r := range rows {
		docs[r.URI] = r.Doc
	}
	return docs, nil
}

func writeDocs(ctx context.Context, tx pgx.Tx, libraryID string, uris []string, docs []Doc, now time.Time) error {
	raws := make([]string, len(docs))
	for i, d := range docs {
		d.V = docVersion
		b, err := json.Marshal(d)
		if err != nil {
			return err
		}
		raws[i] = string(b)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO content_metadata (uri, library_id, data_raw, updated_at)
		SELECT r.uri, $1, r.data_raw, $2 FROM unnest($3::text[], $4::jsonb[]) AS r(uri, data_raw)
		ON CONFLICT (uri, library_id) DO UPDATE SET data_raw = EXCLUDED.data_raw, updated_at = EXCLUDED.updated_at
	`, libraryID, now, uris, raws)
	return err
}

// matchInputs is what automatic matching reads from a row's own layers, as a series or as a child
// of one.
func matchInputs(file, overrides Fields) string {
	f := Doc{File: file, Overrides: overrides}.Local()
	b, _ := json.Marshal([]any{f.Title, f.AltTitles, f.PublicationDate, f.Staff, f.Series, f.Volume})
	return string(b)
}

// touchLinks makes the pending links of the rows, and of their series, due again, since their
// inputs changed. Scans write content before its layers, so new children have their parent.
func touchLinks(ctx context.Context, tx pgx.Tx, libraryID string, uris []string, now time.Time) error {
	if len(uris) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `
		UPDATE metadata_links SET retry_at = $3
		WHERE library_id = $1 AND state IN ('review', 'unmatched') AND (uri = ANY($2) OR uri IN (
			SELECT p.uri FROM content c JOIN content p ON p.id = c.parent_id
			WHERE c.library_id = $1 AND c.uri = ANY($2)))
	`, libraryID, uris, now)
	return err
}
