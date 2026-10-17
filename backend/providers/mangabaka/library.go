package mangabaka

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"voltis/providers"
)

const libraryBatchMax = 100

// Library reads and writes a user's MangaBaka library through /v1/my/*, with the user's personal
// access token. It has its own limiter, so a backoff never stalls metadata refreshes.
type Library struct {
	api  string
	http *http.Client
	lim  *providers.Limiter
}

// NewLibrary talks to api, or to MangaBaka when it's empty. A nil hc is a client with a 15 s timeout,
// and a nil lim allows 60 requests a minute; the spec documents no limit.
func NewLibrary(api string, hc *http.Client, lim *providers.Limiter) *Library {
	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}
	if lim == nil {
		lim = providers.NewLimiter(60, 5)
	}
	return &Library{api: cmp.Or(api, defaultAPI), http: hc, lim: lim}
}

type envelope[T any] struct {
	Status int `json:"status"`
	Data   T   `json:"data"`
}

type Profile struct {
	ID   string
	Name string
}

// Entry is a series in the user's library. MangaBaka stores a 0 progress as null.
type Entry struct {
	SeriesID int64    `json:"series_id"`
	State    string   `json:"state"`
	Chapter  *float64 `json:"progress_chapter"`
	Volume   *float64 `json:"progress_volume"`
}

// Write is the fields Voltis sets on an entry; nil ones are left alone.
type Write struct {
	State   *string  `json:"state,omitempty"`
	Chapter *float64 `json:"progress_chapter,omitempty"`
	Volume  *float64 `json:"progress_volume,omitempty"`
	Private *bool    `json:"is_private,omitempty"`
}

// Error is an error response of /v1/my/*.
type Error struct {
	Status        int
	Code, Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("mangabaka: %d %s: %s", e.Status, e.Code, e.Message)
}

func (l *Library) Profile(ctx context.Context, token string) (Profile, error) {
	var body envelope[struct {
		ID                string `json:"id"`
		Nickname          string `json:"nickname"`
		PreferredUsername string `json:"preferred_username"`
	}]
	if err := l.do(ctx, token, http.MethodGet, "/v1/my/profile", nil, nil, &body); err != nil {
		return Profile{}, err
	}
	d := body.Data
	return Profile{ID: d.ID, Name: cmp.Or(d.PreferredUsername, d.Nickname, d.ID)}, nil
}

// Entry reads the user's entry for a series, or nil when there is none.
func (l *Library) Entry(ctx context.Context, token, id string) (*Entry, error) {
	var body envelope[Entry]
	err := l.do(ctx, token, http.MethodGet, "/v1/my/library/"+url.PathEscape(id), nil, nil, &body)
	if e, ok := errors.AsType[*Error](err); ok && e.Status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &body.Data, nil
}

// Entries reads the user's entries for up to 100 series, by series id. Series without an entry
// are left out.
func (l *Library) Entries(ctx context.Context, token string, ids []string) (map[string]Entry, error) {
	if len(ids) > libraryBatchMax {
		return nil, fmt.Errorf("mangabaka: %d entries in one batch, at most %d", len(ids), libraryBatchMax)
	}
	var body envelope[[]Entry]
	q := url.Values{"series_id": ids}
	if err := l.do(ctx, token, http.MethodGet, "/v1/my/library/batch?"+q.Encode(), nil, nil, &body); err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(body.Data))
	for _, e := range body.Data {
		out[strconv.FormatInt(e.SeriesID, 10)] = e
	}
	return out, nil
}

// Create adds the series to the user's library. ready runs right before the request is sent; see
// providers.Request.
func (l *Library) Create(ctx context.Context, token, id string, w Write, ready func() error) error {
	return l.do(ctx, token, http.MethodPost, "/v1/my/library/"+url.PathEscape(id), w, ready, nil)
}

// Update changes the fields w sets on the user's entry, as Create.
func (l *Library) Update(ctx context.Context, token, id string, w Write, ready func() error) error {
	return l.do(ctx, token, http.MethodPatch, "/v1/my/library/"+url.PathEscape(id), w, ready, nil)
}

// do sends a request and turns an error response into an *Error. A 400 rate_limited pauses the
// limiter, as a 429 does.
func (l *Library) do(ctx context.Context, token, method, path string, body any, ready func() error, out any) error {
	h := http.Header{}
	h.Set("x-api-key", token)
	err := providers.DoJSON(ctx, l.http, l.lim,
		providers.Request{Method: method, URL: l.api + path, Header: h, Body: body, Ready: ready}, out)
	he, ok := errors.AsType[*providers.HTTPError](err)
	if !ok {
		return err
	}
	e := &Error{Status: he.Status}
	var raw struct {
		Message string `json:"message"`
		Code    string `json:"code"` // null decodes as ""
	}
	if json.Unmarshal([]byte(he.Body), &raw) == nil {
		e.Message, e.Code = raw.Message, raw.Code
	}
	e.Message = cmp.Or(e.Message, fmt.Sprintf("HTTP %d", he.Status))
	if e.Status == http.StatusBadRequest && e.Code == "rate_limited" {
		l.lim.PauseUntil(time.Now().Add(cmp.Or(max(he.RetryAfter, 0), time.Minute)))
	}
	return e
}

// Outcome is what a failed library request means for the scrobble that made it.
type Outcome int

const (
	Transient Outcome = iota // try again later
	Rejected                 // the token is no longer accepted
	Conflict                 // the entry changed between the read and the write
	Failed                   // MangaBaka refused the values
)

// Classify sorts an error of a request with method.
func Classify(err error, method string) Outcome {
	e, ok := errors.AsType[*Error](err)
	switch {
	case !ok:
		return Transient // network errors and timeouts
	case e.Status == http.StatusUnauthorized || e.Status == http.StatusForbidden:
		return Rejected
	case e.Status >= 500 || e.Status == http.StatusTooManyRequests ||
		e.Status == http.StatusBadRequest && e.Code == "rate_limited":
		return Transient
	case method == http.MethodPost && e.Status == http.StatusBadRequest && e.Code == "duplicate_entry",
		method == http.MethodPatch && e.Status == http.StatusNotFound:
		return Conflict
	}
	return Failed
}
