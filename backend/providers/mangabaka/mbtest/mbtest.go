// Package mbtest is an in-memory MangaBaka library API (/v1/my/*), for tests and the
// fake-mangabaka dev command. It answers with the API's envelopes, status codes and error bodies.
package mbtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	"voltis/providers"
	"voltis/providers/mangabaka"
)

// Req is a request the fake served.
type Req struct {
	Method, Path, Body string
}

type failure struct {
	status int
	body   string
}

type Handler struct {
	mux     *http.ServeMux
	mu      sync.Mutex
	tokens  map[string]bool
	entries map[string]map[string]mangabaka.Entry // by token, then series id
	reqs    []Req
	fail    []failure
	onWrite func()
}

// NewHandler is a fake with no tokens.
func NewHandler() *Handler {
	h := &Handler{mux: http.NewServeMux(), tokens: map[string]bool{},
		entries: map[string]map[string]mangabaka.Entry{}}
	h.mux.HandleFunc("GET /v1/my/profile", h.profile)
	h.mux.HandleFunc("GET /v1/my/library/batch", h.batch)
	h.mux.HandleFunc("GET /v1/my/library/{id}", h.get)
	h.mux.HandleFunc("POST /v1/my/library/{id}", h.write)
	h.mux.HandleFunc("PATCH /v1/my/library/{id}", h.write)
	return h
}

type Server struct {
	*httptest.Server
	*Handler
}

func New(t testing.TB) *Server {
	h := NewHandler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Server{Server: srv, Handler: h}
}

// Limiter is a limiter that never makes a test wait.
func (s *Server) Limiter() *providers.Limiter { return providers.NewLimiter(60000, 1000) }

// Token accepts token.
func (h *Handler) Token(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tokens[token] = true
	if h.entries[token] == nil {
		h.entries[token] = map[string]mangabaka.Entry{}
	}
}

// Revoke makes token answer 401, keeping its library.
func (h *Handler) Revoke(token string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.tokens, token)
}

func (h *Handler) Put(token, id string, e mangabaka.Entry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e.SeriesID, _ = strconv.ParseInt(id, 10, 64)
	if h.entries[token] == nil {
		h.entries[token] = map[string]mangabaka.Entry{}
	}
	h.entries[token][id] = e
}

func (h *Handler) Delete(token, id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.entries[token], id)
}

func (h *Handler) Get(token, id string) (mangabaka.Entry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.entries[token][id]
	return e, ok
}

// Requests returns the requests served since the last call.
func (h *Handler) Requests() []Req {
	h.mu.Lock()
	defer h.mu.Unlock()
	reqs := h.reqs
	h.reqs = nil
	return reqs
}

// FailNext answers the next write (POST or PATCH) with status and body, after its OnWrite hook. An
// empty body is the API's error body with a null code.
func (h *Handler) FailNext(status int, body string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if body == "" {
		body = fmt.Sprintf(`{"status":%d,"message":%q,"code":null}`, status, http.StatusText(status))
	}
	h.fail = append(h.fail, failure{status, body})
}

// OnWrite runs fn at the start of every write until it's replaced; nil removes it.
func (h *Handler) OnWrite(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onWrite = fn
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	h.mu.Lock()
	h.reqs = append(h.reqs, Req{Method: r.Method, Path: r.URL.RequestURI(), Body: string(body)})
	ok := h.tokens[r.Header.Get("x-api-key")]
	h.mu.Unlock()
	if !ok {
		reply(w, http.StatusUnauthorized, `{"status":401,"message":"Unauthorized","code":null}`)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	h.mux.ServeHTTP(w, r)
}

func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]any{"id": "acct-" + r.Header.Get("x-api-key"), "nickname": nil,
		"preferred_username": "Paper Moth", "auth_type": "pat", "scopes": []string{}})
}

func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("x-api-key")
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []mangabaka.Entry{}
	for _, id := range r.URL.Query()["series_id"] {
		if e, ok := h.entries[token][id]; ok {
			out = append(out, e)
		}
	}
	respond(w, http.StatusOK, out)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	e, found := h.Get(r.Header.Get("x-api-key"), r.PathValue("id"))
	if !found {
		reply(w, http.StatusNotFound, `{"status":404,"message":"Not Found","code":null}`)
		return
	}
	respond(w, http.StatusOK, e)
}

func (h *Handler) write(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	hook := h.onWrite
	h.mu.Unlock()
	if hook != nil {
		hook()
	}

	token, id := r.Header.Get("x-api-key"), r.PathValue("id")
	var in mangabaka.Write
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		reply(w, http.StatusBadRequest, `{"status":400,"message":"Invalid body","code":null}`)
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.fail) > 0 {
		f := h.fail[0]
		h.fail = h.fail[1:]
		reply(w, f.status, f.body)
		return
	}
	e, exists := h.entries[token][id]
	switch {
	case r.Method == http.MethodPost && exists:
		reply(w, http.StatusBadRequest, `{"status":400,"message":"Entry already exists","code":"duplicate_entry"}`)
		return
	case r.Method == http.MethodPatch && !exists:
		reply(w, http.StatusNotFound, `{"status":404,"message":"Not Found","code":null}`)
		return
	}
	e.SeriesID, _ = strconv.ParseInt(id, 10, 64)
	if r.Method == http.MethodPost {
		e.State = "plan_to_read"
	}
	if in.State != nil {
		e.State = *in.State
	}
	if in.Chapter != nil {
		e.Chapter = in.Chapter
	}
	if in.Volume != nil {
		e.Volume = in.Volume
	}
	h.entries[token][id] = e
	status := http.StatusOK
	if r.Method == http.MethodPost {
		status = http.StatusCreated
	}
	respond(w, status, true)
}

func respond(w http.ResponseWriter, status int, data any) {
	body, _ := json.Marshal(map[string]any{"status": status, "data": data})
	reply(w, status, string(body))
}

func reply(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}
