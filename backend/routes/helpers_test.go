package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"voltis/config"
	"voltis/covers"
	"voltis/db/dbtest"
	"voltis/linking"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/providertest"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

const testRemoteAddrHeader = "X-Test-Remote-Addr"

func newTestPool(t *testing.T) *pgxpool.Pool {
	return dbtest.Pool(t)
}

type testClient struct {
	t       *testing.T
	server  *httptest.Server
	http    *http.Client
	hub     *WebSocketHub
	st      *settings.Store
	db      *pgxpool.Pool
	fake    *providertest.Provider
	headers map[string]string
	extra   http.Header
}

func (c *testClient) pool() *pgxpool.Pool { return c.db }

func newStore(t *testing.T, pool *pgxpool.Pool) *settings.Store {
	t.Helper()
	st, err := settings.New(context.Background(), pool)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	st.Listen()
	t.Cleanup(st.Close)
	return st
}

func newClient(t *testing.T, pool *pgxpool.Pool) *testClient {
	return newProxyClient(t, pool, config.ProxyAuth{})
}

func newProxyClient(t *testing.T, pool *pgxpool.Pool, proxy config.ProxyAuth) *testClient {
	return newServerClient(t, pool, proxy, "")
}

// newServerClient serves the SPA from staticDir, when set.
func newServerClient(t *testing.T, pool *pgxpool.Pool, proxy config.ProxyAuth, staticDir string) *testClient {
	t.Helper()

	st := newStore(t, pool)

	e := echo.New()
	hub, fake := NewHub(), providertest.New()
	reg := providers.NewRegistry(fake)
	store := metadata.NewStore(reg)
	cov := covers.New(t.TempDir())
	links := linking.New(pool, store, reg, cov, hub.LibraryChanged)
	manager := Register(t.Context(), e, pool, st, proxy, Deps{Hub: hub, Providers: reg, Metadata: store, Links: links,
		Covers: cov, StaticDir: staticDir})
	t.Cleanup(manager.Close)
	t.Cleanup(autoMatching.Wait) // before the database goes; t.Context() has cancelled them

	server := httptest.NewTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if addr := r.Header.Get(testRemoteAddrHeader); addr != "" {
			r.RemoteAddr = addr
			r.Header.Del(testRemoteAddrHeader)
		}
		e.ServeHTTP(w, r)
	}))
	server.Start()

	return &testClient{t: t, server: server, http: newHTTPClient(), hub: hub, st: st, db: pool, fake: fake}
}

func newHTTPClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func (c *testClient) newSession(t *testing.T) *testClient {
	t.Helper()
	clone := *c
	clone.t = t
	clone.http = newHTTPClient()
	return &clone
}

func (c *testClient) WithHeader(key, value string) *testClient {
	clone := *c
	clone.headers = maps.Clone(c.headers)
	if clone.headers == nil {
		clone.headers = map[string]string{}
	}
	clone.headers[key] = value
	return &clone
}

// WithRawHeader sends a header verbatim, including more than once.
func (c *testClient) WithRawHeader(key string, values ...string) *testClient {
	clone := *c
	clone.extra = http.Header{key: values}
	return &clone
}

func (c *testClient) WithRemoteAddr(addr string) *testClient {
	return c.WithHeader(testRemoteAddrHeader, addr)
}

func newAdminClient(t *testing.T, pool *pgxpool.Pool) *testClient {
	t.Helper()
	c := newClient(t, pool)

	resp := c.Post("/api/auth/register", map[string]any{
		"username": "admin", "password": "adminpass123",
	})
	if resp.StatusCode != 200 {
		t.Fatalf("register failed: %d", resp.StatusCode)
	}

	return c
}

// newMemberClient creates a user named "member" and returns a client logged in as them, and their id.
func newMemberClient(t *testing.T, admin *testClient, perms ...string) (*testClient, string) {
	t.Helper()
	// Non-nil, so it encodes as [] rather than null.
	perms = append([]string{}, perms...)
	user := admin.Post("/api/users/new", map[string]any{
		"username": "member", "password": "memberpass123", "permissions": perms,
	}).Assert(t, 200).JSON()
	member := admin.newSession(t)
	member.Post("/api/auth/login", map[string]any{
		"username": "member", "password": "memberpass123",
	}).Assert(t, 200)
	return member, s(user["id"])
}

func (c *testClient) url() *url.URL {
	u, _ := url.Parse(c.server.URL)
	return u
}

func (c *testClient) HasCookie(name string) bool {
	return slices.ContainsFunc(c.http.Jar.Cookies(c.url()), func(ck *http.Cookie) bool { return ck.Name == name })
}

func (c *testClient) cookie(name string) string {
	for _, ck := range c.http.Jar.Cookies(c.url()) {
		if ck.Name == name {
			return ck.Value
		}
	}
	return ""
}

func (c *testClient) SetCookie(name, value string) {
	c.http.Jar.SetCookies(c.url(), []*http.Cookie{{Name: name, Value: value}})
}

func (c *testClient) do(method, path string, body any) *response {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		reader = strings.NewReader(string(data))
	}
	return c.doRaw(method, path, reader)
}

func (c *testClient) doRaw(method, path string, body io.Reader) *response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.server.URL+path, body)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	for k, values := range c.extra {
		for _, v := range values {
			req.Header.Add(k, v)
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	return readResponse(resp)
}

func (c *testClient) Get(path string) *response { return c.do(http.MethodGet, path, nil) }

func (c *testClient) Patch(path string, body any) *response {
	return c.do(http.MethodPatch, path, body)
}

func (c *testClient) PatchRaw(path, body string) *response {
	return c.doRaw(http.MethodPatch, path, strings.NewReader(body))
}

func (c *testClient) Post(path string, body any) *response {
	if body == nil {
		body = map[string]any{}
	}
	return c.do(http.MethodPost, path, body)
}

// Mirrors the client, which sends a JSON body so the CSRF gate is satisfied.
func (c *testClient) Delete(path string) *response {
	return c.do(http.MethodDelete, path, map[string]any{})
}

type response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
	Cookies    []*http.Cookie
}

func readResponse(r *http.Response) *response {
	defer func() { _ = r.Body.Close() }()
	body, _ := io.ReadAll(r.Body)
	return &response{StatusCode: r.StatusCode, Body: body, Headers: r.Header, Cookies: r.Cookies()}
}

func (r *response) Cookie(name string) *http.Cookie {
	for _, ck := range r.Cookies {
		if ck.Name == name {
			return ck
		}
	}
	return nil
}

func (r *response) JSON() map[string]any {
	var v map[string]any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		panic(fmt.Sprintf("invalid JSON response: %s", string(r.Body)))
	}
	return v
}

func (r *response) JSONArray() []map[string]any {
	var v []map[string]any
	if err := json.Unmarshal(r.Body, &v); err != nil {
		panic(fmt.Sprintf("invalid JSON response: %s", string(r.Body)))
	}
	return v
}

func (r *response) Assert(t *testing.T, code int) *response {
	t.Helper()
	if r.StatusCode != code {
		t.Fatalf("expected status %d, got %d: %s", code, r.StatusCode, string(r.Body))
	}
	return r
}

func assertEq[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func assertLen(t *testing.T, arr []map[string]any, n int) {
	t.Helper()
	if len(arr) != n {
		t.Fatalf("got len %d, want %d: %v", len(arr), n, arr)
	}
}

func s(v any) string { return fmt.Sprintf("%v", v) }
