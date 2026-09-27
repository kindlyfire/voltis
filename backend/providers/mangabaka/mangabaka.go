// Package mangabaka reads series from MangaBaka's v2 API.
package mangabaka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"
)

const (
	name       = "mangabaka"
	lookupMax  = 20
	batchMax   = 50
	defaultAPI = "https://api.mangabaka.org"
)

type Provider struct {
	api    string
	http   *http.Client
	lookup *providers.Limiter // search and match share it; match documents no limit of its own
	batch  *providers.Limiter
}

func New() *Provider {
	return &Provider{
		api:    defaultAPI,
		http:   &http.Client{Timeout: 15 * time.Second},
		lookup: providers.NewLimiter(20, 5),
		batch:  providers.NewLimiter(100, 20),
	}
}

func (p *Provider) Name() string  { return name }
func (p *Provider) Label() string { return "MangaBaka" }
func (p *Provider) MaxBatch() int { return batchMax }

// "other" is left out: it is mostly doujinshi.
func (p *Provider) Kinds(contentType string) []metadata.Kind {
	switch contentType {
	case "comic_series":
		return []metadata.Kind{metadata.Manga, metadata.Manhwa, metadata.Manhua, metadata.OEL}
	case "book_series":
		return []metadata.Kind{metadata.Novel}
	}
	return nil
}

var idPattern = regexp.MustCompile(`^(?:https?://(?:www\.)?mangabaka\.(?:org|dev)/(?:[a-z]+/)?)?(\d+)(?:[/?#].*)?$`)

func (p *Provider) ParseID(input string) (string, bool) {
	m := idPattern.FindStringSubmatch(strings.TrimSpace(input))
	if m == nil {
		return "", false
	}
	id, err := strconv.Atoi(m[1])
	return strconv.Itoa(id), err == nil && id > 0
}

func (p *Provider) Search(ctx context.Context, contentType, title string, limit int) ([]providers.Entry, error) {
	return p.find(ctx, "search", contentType, title, limit)
}

func (p *Provider) Match(ctx context.Context, contentType, title string, limit int) ([]providers.Entry, error) {
	return p.find(ctx, "match", contentType, title, limit)
}

// find queries a title endpoint; search and match take the same parameters and answer alike.
func (p *Provider) find(ctx context.Context, endpoint, contentType, title string, limit int) ([]providers.Entry, error) {
	q := url.Values{"q": {title}, "limit": {strconv.Itoa(min(limit, lookupMax))}, "schema": {"full"}}
	for _, k := range p.Kinds(contentType) {
		q.Add("type", string(k))
	}
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := providers.GetJSON(ctx, p.http, p.lookup, p.api+"/v2/series/"+endpoint+"?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	return entries(body.Data)
}

// Fetch reads a batch. The batch endpoint omits merged and deleted series, so those are read one
// by one, which lets merges be followed.
func (p *Provider) Fetch(ctx context.Context, ids []string) ([]providers.Entry, error) {
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	q := url.Values{"id": ids, "schema": {"full"}}
	if err := providers.GetJSON(ctx, p.http, p.batch, p.api+"/v2/series/batch?"+q.Encode(), &body); err != nil {
		return nil, err
	}
	out, err := entries(body.Data)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if slices.ContainsFunc(out, func(e providers.Entry) bool { return e.Key.ID == id }) {
			continue
		}
		var one struct {
			Data json.RawMessage `json:"data"`
		}
		err := providers.GetJSON(ctx, p.http, p.batch, p.api+"/v2/series/"+url.PathEscape(id)+"?schema=full", &one)
		if he, ok := errors.AsType[*providers.HTTPError](err); ok && he.Status == http.StatusNotFound {
			continue
		}
		if err != nil {
			return nil, err
		}
		e, err := entries([]json.RawMessage{one.Data})
		if err != nil {
			return nil, err
		}
		out = append(out, e...)
	}
	return out, nil
}

func entries(raws []json.RawMessage) ([]providers.Entry, error) {
	out := make([]providers.Entry, len(raws))
	for i, raw := range raws {
		var s struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(raw, &s); err != nil || s.ID <= 0 {
			return nil, fmt.Errorf("mangabaka: series without an id: %.100s", raw)
		}
		out[i] = providers.Entry{Key: metadata.EntryKey{Provider: name, ID: strconv.Itoa(s.ID)}, Raw: raw}
	}
	return out, nil
}

type tag struct {
	Name      string `json:"name"`
	IsGenre   bool   `json:"is_genre"`
	IsSpoiler *bool  `json:"is_spoiler"`
}

type title struct {
	Language  string   `json:"language"`
	Title     string   `json:"title"`
	Traits    []string `json:"traits"`
	IsPrimary *bool    `json:"is_primary"`
}

func (t title) primary() bool { return t.IsPrimary != nil && *t.IsPrimary }
func (t title) native() bool  { return slices.Contains(t.Traits, "native") }
func (t title) latin() bool   { return strings.HasSuffix(t.Language, "-Latn") }

// series is a v2 series with schema=full.
type series struct {
	ID            int      `json:"id"`
	State         string   `json:"state"`
	MergedWith    *int     `json:"merged_with"`
	CanonicalURL  string   `json:"canonical_url"`
	Authors       []string `json:"authors"`
	Artists       []string `json:"artists"`
	Description   *string  `json:"description"`
	Status        string   `json:"status"`
	ContentRating string   `json:"content_rating"`
	Type          string   `json:"type"`
	Rating        *float64 `json:"rating"`
	Publishers    []struct {
		Name *string `json:"name"`
	} `json:"publishers"`
	Titles    []title `json:"titles"`
	Published struct {
		StartDate *string `json:"start_date"`
	} `json:"published"`
	FinalVolume *float64 `json:"final_volume"`
	Tags        []tag    `json:"tags"`
	Cover       struct {
		Raw *string `json:"raw"`
	} `json:"cover"`
}

func (p *Provider) Decode(e providers.Entry) (providers.Record, error) {
	var s series
	if err := json.Unmarshal(e.Raw, &s); err != nil {
		return providers.Record{}, fmt.Errorf("mangabaka: %w", err)
	}

	switch s.State {
	case "merged":
		if s.MergedWith == nil {
			return providers.Record{}, errors.New("mangabaka: merged series without a target")
		}
		return providers.Record{MergedInto: strconv.Itoa(*s.MergedWith)}, nil
	case "deleted":
		return providers.Record{Deleted: true}, nil
	}

	main := mainTitle(s.Titles)
	if main == "" {
		return providers.Record{}, errors.New("mangabaka: series without a title")
	}
	f := metadata.Fields{
		Title:         metadata.Val(main),
		AltTitles:     metadata.Val(fp.Map(s.Titles, func(t title) string { return t.Title })),
		Staff:         metadata.Val(staff(s.Authors, s.Artists)),
		ContentRating: metadata.Val(metadata.ContentRating(s.ContentRating)),
		Status:        metadata.Val(metadata.Status(s.Status)),
		Kind:          metadata.Val(metadata.Kind(s.Type)),
	}
	if s.Description != nil {
		f.Description = metadata.Val(*s.Description)
	}
	var publishers, genres, tags []string
	for _, p := range s.Publishers {
		if p.Name != nil {
			publishers = append(publishers, *p.Name)
		}
	}
	for _, t := range s.Tags {
		switch {
		case t.IsGenre:
			genres = append(genres, t.Name)
		case t.IsSpoiler == nil || !*t.IsSpoiler:
			tags = append(tags, t.Name)
		}
	}
	f.Publishers, f.Genres, f.Tags = metadata.Val(publishers), metadata.Val(genres), metadata.Val(tags)
	if s.Published.StartDate != nil {
		f.PublicationDate = metadata.Val(*s.Published.StartDate)
	}
	if s.Rating != nil {
		f.Rating = metadata.Val(*s.Rating)
	}
	if v := s.FinalVolume; v != nil && *v == float64(int(*v)) {
		f.Count = metadata.Val(int(*v))
	}

	rec := providers.Record{Fields: f.Normalize(), URL: s.CanonicalURL}
	if rec.URL == "" {
		rec.URL = fmt.Sprintf("https://mangabaka.org/%d", s.ID)
	}
	if s.Cover.Raw != nil {
		rec.CoverURL = *s.Cover.Raw
	}
	return rec, nil
}

// mainTitle prefers the primary English title, then a romanization, then the native title.
func mainTitle(ts []title) string {
	for _, pick := range []func(title) bool{
		func(t title) bool { return t.primary() && t.Language == "en" },
		func(t title) bool { return t.primary() && t.latin() },
		func(t title) bool { return t.native() && t.latin() },
		title.native,
		title.primary,
		func(title) bool { return true },
	} {
		if i := slices.IndexFunc(ts, pick); i >= 0 {
			return ts[i].Title
		}
	}
	return ""
}

func staff(authors, artists []string) []metadata.Staff {
	var out []metadata.Staff
	for _, a := range authors {
		out = append(out, metadata.Staff{Name: a, Role: "author"})
	}
	for _, a := range artists {
		out = append(out, metadata.Staff{Name: a, Role: "artist"})
	}
	return out
}
