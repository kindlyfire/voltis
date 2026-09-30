// Package linking owns provider links and entries: fetching, matching, and refreshing them.
package linking

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"voltis/covers"
	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool   *pgxpool.Pool
	store  *metadata.Store
	reg    *providers.Registry
	covers *covers.Cache
	notify func(libraryID string)
	wake   chan struct{}
	undos  undos

	mu       sync.Mutex // guards the status
	status   WorkerStatus
	onStatus func(WorkerStatus)
	pushing  bool // a push of the status is due

	// The worker's own state.
	scanning    func(libraryID string) bool
	recomputed  bool                 // no stale row is left
	cursors     map[[2]string]string // (library, provider) -> the last URI matched
	lastLib     string               // the library matched last
	collectedAt time.Time
}

func New(pool *pgxpool.Pool, store *metadata.Store, reg *providers.Registry, cov *covers.Cache,
	notify func(libraryID string)) *Service {
	return &Service{pool: pool, store: store, reg: reg, covers: cov, notify: notify, wake: make(chan struct{}, 1),
		undos:  undos{m: map[linkKey]undo{}, now: time.Now},
		status: WorkerStatus{Activity: Idle}, scanning: func(string) bool { return false }, cursors: map[[2]string]string{}}
}

// ProviderError is a provider failing to answer.
type ProviderError struct{ Err error }

func (e *ProviderError) Error() string { return e.Err.Error() }
func (e *ProviderError) Unwrap() error { return e.Err }

type MetadataView struct {
	Type         string              `json:"type"` // the content type, which selects the fields
	OverridesRev int64               `json:"overrides_rev"`
	Merged       metadata.Fields     `json:"merged"`
	Sources      map[string][]string `json:"sources"`
	Layers       []LayerView         `json:"layers"`
	Links        []LinkView          `json:"links"`
}

type LayerView struct {
	Source string          `json:"source"`
	Label  string          `json:"label"`
	Kind   string          `json:"kind"` // file | provider | overrides
	Fields metadata.Fields `json:"fields"`
	Raw    json.RawMessage `json:"raw,omitempty"` // a provider's snapshot
}

type LinkView struct {
	Provider   string        `json:"provider"`
	Label      string        `json:"label"`
	Rev        *int64        `json:"rev"`
	State      State         `json:"state"`
	ExternalID *string       `json:"external_id"`
	Origin     *Origin       `json:"origin"`
	Entry      *EntrySummary `json:"entry"`
	Deleted    bool          `json:"deleted"`
	Candidates []Candidate   `json:"candidates"`
	Rejected   []string      `json:"rejected"`
	FetchedAt  *time.Time    `json:"fetched_at"`
	LastError  *string       `json:"last_error"` // the last match failed, or the linked snapshot does not decode
	// The linked entry's refreshing: when it is due, and how it failed since the last success.
	RefreshAt       *time.Time `json:"refresh_at"`
	RefreshAttempts int        `json:"refresh_attempts"`
	RefreshError    *string    `json:"refresh_error"`
}

func (s *Service) View(ctx context.Context, q db.Querier, contentID string) (MetadataView, error) {
	t, err := metadata.ReadTarget(ctx, q, contentID)
	if err != nil {
		return MetadataView{}, err
	}
	layers, err := s.store.Load(ctx, q, t)
	if err != nil {
		return MetadataView{}, err
	}
	v := MetadataView{
		Type:         t.Type,
		OverridesRev: layers.OverridesRev,
		Merged:       layers.Resolved.Fields,
		Sources:      layers.Resolved.Sources,
		Layers:       []LayerView{{Source: "file", Label: "File", Kind: "file", Fields: layers.File.Normalize()}},
		Links:        []LinkView{},
	}

	for _, p := range s.reg.For(t.Type) {
		l, err := readLink(ctx, q, t, p.Name())
		if err != nil {
			return MetadataView{}, err
		}
		keys := candidateKeys(l)
		linked := metadata.EntryKey{Provider: p.Name()}
		if l.State == StateLinked {
			linked.ID = *l.ExternalID
			keys = append(keys, linked)
		}
		finals, err := canonical(ctx, q, keys)
		if err != nil {
			return MetadataView{}, err
		}
		var e *linkedEntry
		if l.State == StateLinked {
			entry, err := db.SelectOne[linkedEntry](ctx, q,
				`SELECT external_id, raw, fetched_at, deleted, refresh_at, attempts, last_error
				FROM provider_entries WHERE provider = $1 AND external_id = $2`,
				p.Name(), finals[linked].ID)
			if err != nil {
				return MetadataView{}, err
			}
			e = &entry
			if i := slices.IndexFunc(layers.Providers, func(pl metadata.Layer) bool { return pl.Source == p.Name() }); i >= 0 {
				v.Layers = append(v.Layers, LayerView{Source: p.Name(), Label: p.Label(), Kind: "provider",
					Fields: layers.Providers[i].Fields, Raw: e.Raw})
			}
		}
		v.Links = append(v.Links, linkView(p, l, e, finals))
	}
	v.Layers = append(v.Layers, LayerView{Source: "overrides", Label: "Overrides", Kind: "overrides", Fields: layers.Overrides})
	return v, nil
}

// linkedEntry is the stored entry a link reads.
type linkedEntry struct {
	ID        string          `db:"external_id"`
	Raw       json.RawMessage `db:"raw"`
	FetchedAt time.Time       `db:"fetched_at"`
	Deleted   bool            `db:"deleted"`
	RefreshAt time.Time       `db:"refresh_at"`
	Attempts  int             `db:"attempts"`
	LastError *string         `db:"last_error"`
}

func candidateKeys(l Link) []metadata.EntryKey {
	return fp.Map(l.Candidates, func(c Candidate) metadata.EntryKey { return c.Key })
}

// linkView shows a link with the candidates it offers as the entries their merges lead to (finals,
// from canonical), once each.
func linkView(p providers.Provider, l Link, e *linkedEntry, finals map[metadata.EntryKey]metadata.EntryKey) LinkView {
	lv := LinkView{
		Provider: p.Name(), Label: p.Label(), Rev: l.revision(), State: l.State, ExternalID: l.ExternalID,
		Origin: l.Origin, Candidates: []Candidate{}, Rejected: append([]string{}, l.Rejected...),
		LastError: l.LastError,
	}
	for _, c := range l.Candidates {
		c.Key = finals[c.Key]
		if !slices.ContainsFunc(lv.Candidates, func(o Candidate) bool { return o.Key == c.Key }) {
			lv.Candidates = append(lv.Candidates, c)
		}
	}
	if e != nil {
		key := metadata.EntryKey{Provider: p.Name(), ID: e.ID}
		if rec, err := p.Decode(providers.Entry{Key: key, Raw: e.Raw}); err == nil {
			lv.Entry = new(summarize(key, rec))
		}
		lv.FetchedAt, lv.Deleted = &e.FetchedAt, e.Deleted
		lv.RefreshAt, lv.RefreshAttempts, lv.RefreshError = &e.RefreshAt, e.Attempts, e.LastError
	}
	return lv
}

// Candidates looks entries up by ID or URL, and searches by title, the series' own by default.
// Bare digits are both, as a title can be a number: the entry with that ID comes first.
func (s *Service) Candidates(ctx context.Context, contentID, provider, input string) ([]Candidate, error) {
	t, p, err := s.target(ctx, contentID, provider)
	if err != nil {
		return nil, err
	}
	ctx = providers.Interactive(ctx)
	q, err := buildQuery(ctx, s.pool, t)
	if err != nil {
		return nil, err
	}
	input = strings.TrimSpace(input)
	if input == "" && len(q.Titles) > 0 {
		input = q.Titles[0]
	}

	var byID, searched []Fetched
	id, isID := p.ParseID(input)
	if isID {
		byID = fetch(ctx, p, []string{id})
		if byID[0].Outcome == Failed {
			return nil, &ProviderError{byID[0].Err}
		}
	}
	if !isID || strings.Trim(input, "0123456789") == "" {
		if searched, err = retrieve(ctx, p, p.Search, MatchQuery{ContentType: t.Type, Titles: []string{input}}, false); err != nil {
			return nil, err
		}
	}

	candidates := func(found []Fetched) []Candidate {
		var out []Candidate
		for _, f := range found {
			if f.Outcome == Found && f.Record.MergedInto == "" && supports(p, t.Type, f.Record) {
				out = append(out, Candidate{summarize(f.Entry.Key, f.Record), evaluate(q, f.Record.Fields)})
			}
		}
		slices.SortStableFunc(out, func(a, b Candidate) int { return cmp.Compare(b.Evaluation.Score, a.Evaluation.Score) })
		return out
	}
	out := []Candidate{}
	for _, c := range append(candidates(byID), candidates(searched)...) {
		if !slices.ContainsFunc(out, func(o Candidate) bool { return o.Key == c.Key }) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Link links an entry by ID or URL, as an admin chose it.
func (s *Service) Link(ctx context.Context, contentID, provider, input string, expectRev *int64) error {
	_, errs := s.LinkAll(ctx, []LinkRequest{{contentID, provider, input, expectRev}})
	return errs[0]
}

// LinkRequest is an admin's choice of an entry, by ID or URL, for a series.
type LinkRequest struct {
	ContentID, Provider, Input string
	ExpectRev                  *int64
}

// linkItem is a request LinkAll is carrying out: its index, target, parsed id, and fetched entry.
type linkItem struct {
	i       int
	req     LinkRequest
	target  metadata.Target
	id      string
	fetched Fetched
}

// LinkAll links entries as admins chose them, reporting how each went and the revision it saved.
// It fetches them in batches, publishes them at once, and then links each series unless it changed
// since it was read.
func (s *Service) LinkAll(ctx context.Context, reqs []LinkRequest) ([]int64, []error) {
	revs, errs := make([]int64, len(reqs)), make([]error, len(reqs))
	byProvider := map[string][]*linkItem{}
	for i, r := range reqs {
		t, p, err := s.target(ctx, r.ContentID, r.Provider)
		if err != nil {
			errs[i] = err
			continue
		}
		id, ok := p.ParseID(r.Input)
		if !ok {
			errs[i] = &metadata.ValidationError{Field: "external_id", Msg: "not a " + p.Label() + " ID or URL"}
			continue
		}
		byProvider[r.Provider] = append(byProvider[r.Provider], &linkItem{i: i, req: r, target: t, id: id})
	}

	var linking []*linkItem
	for _, p := range s.reg.All() {
		items := byProvider[p.Name()]
		fetched := fetch(providers.Interactive(ctx), p, fp.Map(items, func(it *linkItem) string { return it.id }))
		for k, it := range items {
			f := fetched[k]
			switch {
			case f.Outcome == Failed:
				errs[it.i] = &ProviderError{f.Err}
			case f.Outcome == Unavailable:
				errs[it.i] = &metadata.ValidationError{Field: "external_id", Msg: "not found on " + p.Label()}
			case f.Outcome == Deleted:
				errs[it.i] = &metadata.ValidationError{Field: "external_id", Msg: "deleted on " + p.Label()}
			case !supports(p, it.target.Type, f.Record):
				errs[it.i] = &metadata.ValidationError{Field: "external_id", Msg: fmt.Sprintf("a %s cannot describe this series", f.Record.Fields.Kind.V)}
			default:
				it.fetched = f
				linking = append(linking, it)
			}
		}
	}
	if len(linking) == 0 {
		return revs, errs
	}
	err := s.run(ctx, func(o *op) error {
		finals, err := s.publish(ctx, o, fp.Map(linking, func(it *linkItem) Fetched { return it.fetched }),
			fp.Map(linking, func(it *linkItem) string { return it.target.LibraryID })...)
		if err != nil {
			return err
		}
		for _, it := range linking {
			t, err := s.store.Lock(ctx, o.tx, it.req.ContentID)
			if err == nil {
				revs[it.i], err = s.decide(ctx, o, t, it.req.Provider, Expect{Rev: it.req.ExpectRev}, func(l *Link) error {
					l.LinkTo(finals[it.fetched.Requested].ID, OriginManual)
					return nil
				})
			}
			// A series that changed or went writes nothing, so the others still commit.
			if stale(err) {
				errs[it.i] = err
			} else if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		for _, it := range linking {
			revs[it.i] = 0
			if errs[it.i] == nil {
				errs[it.i] = err
			}
		}
	}
	return revs, errs
}

// Ignore records that the provider has nothing for the series, returning the revision it saved.
func (s *Service) Ignore(ctx context.Context, contentID, provider string, expectRev *int64) (int64, error) {
	if _, _, err := s.target(ctx, contentID, provider); err != nil {
		return 0, err
	}
	var rev int64
	err := s.run(ctx, func(o *op) error {
		t, err := s.store.Lock(ctx, o.tx, contentID)
		if err != nil {
			return err
		}
		rev, err = s.decide(ctx, o, t, provider, Expect{Rev: expectRev}, func(l *Link) error {
			l.Ignore()
			return nil
		})
		return err
	})
	return rev, err
}

// Reject records that none of the entries fits the series, nor its linked one; matching goes on
// without them. It needs something to reject: dropping the candidates alone would lose them.
// Entries not stored yet get a stub, which refreshing fetches, so that their merges, which reject
// the targets too, are recorded. It returns the revision it saved.
func (s *Service) Reject(ctx context.Context, contentID, provider string, ids []string, expectRev *int64) (int64, error) {
	_, p, err := s.target(ctx, contentID, provider)
	if err != nil {
		return 0, err
	}
	if len(ids) > 50 {
		return 0, &metadata.ValidationError{Field: "external_ids", Msg: "at most 50"}
	}
	ids = slices.Clone(ids)
	for i, input := range ids {
		var ok bool
		if ids[i], ok = p.ParseID(input); !ok {
			return 0, &metadata.ValidationError{Field: "external_ids", Msg: fmt.Sprintf("%q is not a %s ID", input, p.Label())}
		}
	}
	now := time.Now()
	var rev int64
	err = s.run(ctx, func(o *op) error {
		if err := db.LockProvider(ctx, o.tx, provider); err != nil {
			return err
		}
		if _, err := o.tx.Exec(ctx, entryStub, provider, ids); err != nil {
			return err
		}
		t, err := s.store.Lock(ctx, o.tx, contentID)
		if err != nil {
			return err
		}
		rev, err = s.decide(ctx, o, t, provider, Expect{Rev: expectRev}, func(l *Link) error {
			if len(ids) == 0 && l.State != StateLinked {
				return &metadata.ValidationError{Field: "external_ids", Msg: "no entry to reject"}
			}
			l.Reject(ids, now)
			return nil
		})
		return err
	})
	if err == nil {
		s.Wake() // the stubs are due
	}
	return rev, err
}

// Refresh fetches the linked entry again now.
func (s *Service) Refresh(ctx context.Context, contentID, provider string) error {
	t, p, err := s.target(ctx, contentID, provider)
	if err != nil {
		return err
	}
	l, err := readLink(ctx, s.pool, t, provider)
	if err != nil {
		return err
	}
	if l.State != StateLinked {
		return &metadata.ValidationError{Field: "provider", Msg: "not linked"}
	}
	key := metadata.EntryKey{Provider: provider, ID: *l.ExternalID}
	finals, err := canonical(ctx, s.pool, []metadata.EntryKey{key})
	if err != nil {
		return err
	}
	id := finals[key].ID
	res := fetch(providers.Interactive(ctx), p, []string{id})
	switch res[0].Outcome {
	case Failed:
		return &ProviderError{res[0].Err}
	case Unavailable:
		return &ProviderError{fmt.Errorf("%s no longer serves %s", p.Label(), id)}
	}
	return s.run(ctx, func(o *op) error {
		_, err := s.publish(ctx, o, res)
		return err
	})
}

// target reads content without locking and checks that the provider can describe it.
func (s *Service) target(ctx context.Context, contentID, provider string) (metadata.Target, providers.Provider, error) {
	t, err := metadata.ReadTarget(ctx, s.pool, contentID)
	if err != nil {
		return t, nil, err
	}
	p, ok := s.reg.Get(provider)
	if !ok || len(p.Kinds(t.Type)) == 0 {
		return t, nil, &metadata.ValidationError{Field: "provider", Msg: fmt.Sprintf("%q cannot describe a %s", provider, t.Type)}
	}
	return t, p, nil
}

func supports(p providers.Provider, contentType string, r providers.Record) bool {
	k, ok := r.Fields.Kind.Get()
	return ok && slices.Contains(p.Kinds(contentType), k)
}
