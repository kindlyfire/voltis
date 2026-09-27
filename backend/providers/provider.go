package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"

	"voltis/metadata"
)

// Provider is a metadata source. Only its package knows its API, IDs, and payloads.
type Provider interface {
	Name() string
	Label() string
	Kinds(contentType string) []metadata.Kind // empty = unsupported
	// ParseID reads an ID or a URL as typed by an admin.
	ParseID(input string) (string, bool)
	Search(ctx context.Context, contentType, title string, limit int) ([]Entry, error)
	// Match finds entries by their full title, and nothing rather than a guess; automatic matching
	// uses it.
	Match(ctx context.Context, contentType, title string, limit int) ([]Entry, error)
	// Fetch returns at most MaxBatch entries; ids it omits are unavailable.
	Fetch(ctx context.Context, ids []string) ([]Entry, error)
	MaxBatch() int
	// Decode handles merged and deleted payloads before requiring content fields.
	Decode(e Entry) (Record, error)
}

type Entry struct {
	Key metadata.EntryKey
	Raw json.RawMessage
}

type Record struct {
	Fields     metadata.Fields // Status drives the refresh cadence
	URL        string
	CoverURL   string
	MergedInto string
	Deleted    bool
}

type Registry struct{ ps []Provider }

func NewRegistry(ps ...Provider) *Registry { return &Registry{ps: ps} }

func (r *Registry) All() []Provider { return r.ps }

func (r *Registry) Get(name string) (Provider, bool) {
	i := slices.IndexFunc(r.ps, func(p Provider) bool { return p.Name() == name })
	if i < 0 {
		return nil, false
	}
	return r.ps[i], true
}

// ContentTypes lists the content types a provider can describe.
func ContentTypes(p Provider) []string {
	return slices.DeleteFunc(slices.Clone(metadata.SeriesTypes), func(t string) bool { return len(p.Kinds(t)) == 0 })
}

// For lists the providers that support a content type.
func (r *Registry) For(contentType string) []Provider {
	return slices.DeleteFunc(slices.Clone(r.ps), func(p Provider) bool { return len(p.Kinds(contentType)) == 0 })
}

// Layer decodes a stored snapshot into its layer, adding the provider's link and cover.
func (r *Registry) Layer(key metadata.EntryKey, raw json.RawMessage) (metadata.Fields, error) {
	p, ok := r.Get(key.Provider)
	if !ok {
		return metadata.Fields{}, fmt.Errorf("unknown provider %q", key.Provider)
	}
	rec, err := p.Decode(Entry{key, raw})
	if err != nil {
		return metadata.Fields{}, err
	}
	f := rec.Fields
	if rec.URL != "" {
		f.Links = metadata.Val([]metadata.Link{{Label: p.Label(), URL: rec.URL}})
	}
	// Covers are downloaded, so only from the web.
	if u, err := url.Parse(rec.CoverURL); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		f.Cover = metadata.Val(metadata.CoverRef{URL: rec.CoverURL})
	}
	return f, nil
}

func (r *Registry) Order(provider string) int {
	return slices.IndexFunc(r.ps, func(p Provider) bool { return p.Name() == provider })
}
