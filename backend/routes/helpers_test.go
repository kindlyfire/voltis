package routes

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"voltis/db"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("APP_TESTS_DATABASE_URL")
	if url == "" {
		url = "postgresql://postgres:postgres@localhost:5432/voltis_tests?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Reset database
	pool.Exec(ctx, "DROP SCHEMA public CASCADE")
	pool.Exec(ctx, "CREATE SCHEMA public")
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

type testClient struct {
	t      *testing.T
	server *httptest.Server
	http   *http.Client
}

func newClient(t *testing.T, pool *pgxpool.Pool) *testClient {
	t.Helper()

	// Reset first-user flag so registration creates an admin
	firstUserFlow = true

	e := echo.New()
	Register(e, pool)

	server := httptest.NewServer(e)
	t.Cleanup(server.Close)

	jar, _ := cookiejar.New(nil)
	return &testClient{
		t:      t,
		server: server,
		http:   &http.Client{Jar: jar},
	}
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

func (c *testClient) HasCookie(name string) bool {
	u, _ := url.Parse(c.server.URL)
	for _, cookie := range c.http.Jar.Cookies(u) {
		if cookie.Name == name {
			return true
		}
	}
	return false
}

func (c *testClient) Get(path string) *response {
	resp, err := c.http.Get(c.server.URL + path)
	if err != nil {
		c.t.Fatalf("GET %s: %v", path, err)
	}
	return readResponse(resp)
}

func (c *testClient) Post(path string, body any) *response {
	data, _ := json.Marshal(body)
	resp, err := c.http.Post(c.server.URL+path, "application/json", strings.NewReader(string(data)))
	if err != nil {
		c.t.Fatalf("POST %s: %v", path, err)
	}
	return readResponse(resp)
}

func (c *testClient) Delete(path string) *response {
	req, _ := http.NewRequest("DELETE", c.server.URL+path, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("DELETE %s: %v", path, err)
	}
	return readResponse(resp)
}

type response struct {
	StatusCode int
	Body       []byte
}

func readResponse(r *http.Response) *response {
	defer r.Body.Close()
	body, _ := io.ReadAll(r.Body)
	return &response{StatusCode: r.StatusCode, Body: body}
}

func (r *response) JSON() map[string]any {
	var v map[string]any
	json.Unmarshal(r.Body, &v)
	return v
}

func (r *response) JSONArray() []map[string]any {
	var v []map[string]any
	json.Unmarshal(r.Body, &v)
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
