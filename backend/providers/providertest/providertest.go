// Package providertest serves provider entries from memory, for tests.
package providertest

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"

	"voltis/metadata"
	"voltis/providers"
)

// Payload is an entry as the fake provider serves it; anything else fails to decode.
type Payload struct {
	Fields     metadata.Fields `json:"fields,omitzero"`
	CoverURL   string          `json:"cover_url,omitempty"`
	MergedInto string          `json:"merged_into,omitempty"`
	Deleted    bool            `json:"deleted,omitempty"`
}

func Series(title string, kind metadata.Kind) Payload {
	return Payload{Fields: metadata.Fields{Title: metadata.Val(title), Kind: metadata.Val(kind)}}
}

type Provider struct {
	mu       sync.Mutex
	entries  map[string]json.RawMessage
	err      error
	fetches  [][]string
	searches []string
	matched  func(title string)
}

func New() *Provider { return &Provider{entries: map[string]json.RawMessage{}} }

// Put serves a payload, or raw JSON as is, under an id.
func (p *Provider) Put(id string, payload any) {
	raw, ok := payload.(json.RawMessage)
	if !ok {
		raw, _ = json.Marshal(payload)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.entries[id] = raw
}

// Remove stops serving an id.
func (p *Provider) Remove(id string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.entries, id)
}

// Fail makes Search, Match and Fetch return err until it is reset with nil.
func (p *Provider) Fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

// OnMatch calls fn with each title matched, before the match answers.
func (p *Provider) OnMatch(fn func(title string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.matched = fn
}

// Searches lists the titles searched so far.
func (p *Provider) Searches() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.searches)
}

// Fetches lists the id batches requested so far.
func (p *Provider) Fetches() [][]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.fetches)
}

func (p *Provider) Name() string  { return "fake" }
func (p *Provider) Label() string { return "Fake" }
func (p *Provider) MaxBatch() int { return 2 }

func (p *Provider) Kinds(contentType string) []metadata.Kind {
	switch contentType {
	case "comic_series":
		return []metadata.Kind{metadata.Manga, metadata.Manhwa}
	case "book_series":
		return []metadata.Kind{metadata.Novel}
	}
	return nil
}

func (p *Provider) ParseID(input string) (string, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(input))
	return strconv.Itoa(n), err == nil && n > 0
}

// Search finds titles containing the query, in id order.
func (p *Provider) Search(_ context.Context, _, title string, limit int) ([]providers.Entry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.searches = append(p.searches, title)
	return p.find(limit, func(f metadata.Fields) bool {
		return strings.Contains(strings.ToLower(f.Title.V), strings.ToLower(title))
	})
}

// Match finds the title or an alt title equal after NormalizeTitle, in id order.
func (p *Provider) Match(_ context.Context, _, title string, limit int) ([]providers.Entry, error) {
	p.mu.Lock()
	matched := p.matched
	p.mu.Unlock()
	if matched != nil {
		matched(title)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	want := metadata.NormalizeTitle(title)
	return p.find(limit, func(f metadata.Fields) bool {
		return slices.ContainsFunc(append([]string{f.Title.V}, f.AltTitles.V...), func(t string) bool {
			return metadata.NormalizeTitle(t) == want
		})
	})
}

// find serves the entries whose fields match, up to limit. Called with mu held.
func (p *Provider) find(limit int, match func(metadata.Fields) bool) ([]providers.Entry, error) {
	if p.err != nil {
		return nil, p.err
	}
	var out []providers.Entry
	for _, id := range slices.Sorted(maps.Keys(p.entries)) {
		var pl Payload
		if json.Unmarshal(p.entries[id], &pl) == nil && match(pl.Fields) && len(out) < limit {
			out = append(out, p.entry(id))
		}
	}
	return out, nil
}

func (p *Provider) Fetch(_ context.Context, ids []string) ([]providers.Entry, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.fetches = append(p.fetches, ids)
	if p.err != nil {
		return nil, p.err
	}
	var out []providers.Entry
	for _, id := range ids {
		if _, ok := p.entries[id]; ok {
			out = append(out, p.entry(id))
		}
	}
	return out, nil
}

func (p *Provider) entry(id string) providers.Entry {
	return providers.Entry{Key: metadata.EntryKey{Provider: p.Name(), ID: id}, Raw: p.entries[id]}
}

func (p *Provider) Decode(e providers.Entry) (providers.Record, error) {
	var pl Payload
	if err := json.Unmarshal(e.Raw, &pl); err != nil {
		return providers.Record{}, err
	}
	switch {
	case pl.MergedInto != "":
		return providers.Record{MergedInto: pl.MergedInto}, nil
	case pl.Deleted:
		return providers.Record{Deleted: true}, nil
	case pl.Fields.Title.P != metadata.Value:
		return providers.Record{}, errors.New("fake: no title")
	}
	return providers.Record{Fields: pl.Fields.Normalize(), URL: "https://fake/" + e.Key.ID,
		CoverURL: pl.CoverURL}, nil
}
