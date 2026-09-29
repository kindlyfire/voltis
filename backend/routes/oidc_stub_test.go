package routes

import (
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"voltis/settings"
)

type stubCode struct {
	nonce     string
	challenge string
}

type stubIdP struct {
	t      *testing.T
	server *httptest.Server
	key    *rsa.PrivateKey

	mu    sync.Mutex
	codes map[string]stubCode

	subject       string
	claims        map[string]any
	userinfo      map[string]any
	issuer        string
	audience      string
	audiences     []string
	alg           string
	expiry        time.Time
	nonceOverride string
	tamper        bool
	noUserinfo    bool
	tokenError    string
	onToken       func()
}

func newStubIdP(t *testing.T) *stubIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	idp := &stubIdP{
		t:       t,
		key:     key,
		codes:   map[string]stubCode{},
		subject: "sub-1",
		claims:  map[string]any{"preferred_username": "idpuser"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("/keys", idp.jwks)
	mux.HandleFunc("/authorize", idp.authorize)
	mux.HandleFunc("/token", idp.token)
	mux.HandleFunc("/userinfo", idp.userinfoHandler)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (s *stubIdP) discovery(w http.ResponseWriter, _ *http.Request) {
	doc := map[string]any{
		"issuer":                                s.server.URL,
		"authorization_endpoint":                s.server.URL + "/authorize",
		"token_endpoint":                        s.server.URL + "/token",
		"jwks_uri":                              s.server.URL + "/keys",
		"userinfo_endpoint":                     s.server.URL + "/userinfo",
		"id_token_signing_alg_values_supported": []string{"RS256"},
	}
	if s.noUserinfo {
		delete(doc, "userinfo_endpoint")
	}
	writeJSON(w, doc)
}

func (s *stubIdP) jwks(w http.ResponseWriter, _ *http.Request) {
	n := base64.RawURLEncoding.EncodeToString(s.key.N.Bytes())
	e := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(s.key.E)).Bytes())
	writeJSON(w, map[string]any{"keys": []any{
		map[string]any{"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "test", "n": n, "e": e},
	}})
}

func (s *stubIdP) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" {
		http.Error(w, "missing PKCE challenge", http.StatusBadRequest)
		return
	}
	code := "code-" + randomToken()[:16]

	s.mu.Lock()
	s.codes[code] = stubCode{nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
	s.mu.Unlock()

	target, _ := url.Parse(q.Get("redirect_uri"))
	rq := target.Query()
	rq.Set("code", code)
	rq.Set("state", q.Get("state"))
	target.RawQuery = rq.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func (s *stubIdP) token(w http.ResponseWriter, r *http.Request) {
	if s.onToken != nil {
		s.onToken()
	}
	_ = r.ParseForm()
	code := r.Form.Get("code")

	s.mu.Lock()
	stored, ok := s.codes[code]
	s.mu.Unlock()
	if !ok {
		s.failToken(w, "unknown code")
		return
	}

	// Checked before the code is consumed, so this is not a replay error.
	verifier := r.Form.Get("code_verifier")
	if verifier == "" {
		s.failToken(w, "missing code_verifier")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != stored.challenge {
		s.failToken(w, "invalid code_verifier")
		return
	}

	s.mu.Lock()
	delete(s.codes, code)
	s.mu.Unlock()

	nonce := stored.nonce
	if s.nonceOverride != "" {
		nonce = s.nonceOverride
	}
	writeJSON(w, map[string]any{
		"access_token": "access-token",
		"token_type":   "Bearer",
		"id_token":     s.idToken(nonce),
	})
}

func (s *stubIdP) failToken(w http.ResponseWriter, reason string) {
	s.mu.Lock()
	s.tokenError = reason
	s.mu.Unlock()
	http.Error(w, reason, http.StatusBadRequest)
}

func (s *stubIdP) rejection() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokenError
}

func (s *stubIdP) userinfoHandler(w http.ResponseWriter, _ *http.Request) {
	if s.userinfo == nil {
		http.Error(w, "no userinfo", http.StatusNotFound)
		return
	}
	writeJSON(w, s.userinfo)
}

func (s *stubIdP) idToken(nonce string) string {
	expiry := s.expiry
	if expiry.IsZero() {
		expiry = time.Now().Add(time.Hour)
	}
	var audience any = cmpOr(s.audience, "voltis")
	if s.audiences != nil {
		audience = s.audiences
	}
	claims := map[string]any{
		"iss":   cmpOr(s.issuer, s.server.URL),
		"aud":   audience,
		"sub":   s.subject,
		"exp":   expiry.Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
		"nonce": nonce,
	}
	for k, v := range s.claims {
		claims[k] = v
	}

	segment := func(v any) string {
		raw, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	alg := cmpOr(s.alg, "RS256")
	signing := segment(map[string]any{"alg": alg, "typ": "JWT", "kid": "test"}) + "." + segment(claims)

	var sig []byte
	digest := sha256.Sum256([]byte(signing))
	switch alg {
	case "none":
	case "HS256":
		mac := hmac.New(sha256.New, s.key.N.Bytes())
		mac.Write([]byte(signing))
		sig = mac.Sum(nil)
	default:
		var err error
		if sig, err = rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:]); err != nil {
			s.t.Fatalf("sign: %v", err)
		}
	}
	if s.tamper {
		sig[0] ^= 0xff
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func newOIDCPair(t *testing.T) (*testClient, *stubIdP) {
	t.Helper()
	c := newClient(t, newTestPool(t))
	idp := newStubIdP(t)
	setSetting(t, c.st, settings.AppPublicURL, c.server.URL)
	setSetting(t, c.st, settings.OIDCIssuer, idp.server.URL)
	setSetting(t, c.st, settings.OIDCClientID, "voltis")
	setSetting(t, c.st, settings.OIDCClientSecret, "secret")
	setSetting(t, c.st, settings.OIDCEnabled, true)
	return c, idp
}

func (c *testClient) authorize(t *testing.T, path string) url.Values {
	t.Helper()
	start := c.Get(path)
	if start.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect to the provider, got %d: %s", start.StatusCode, start.Body)
	}
	return c.authorizeAt(t, start.Headers.Get("Location"))
}

func (c *testClient) authorizeAt(t *testing.T, authURL string) url.Values {
	t.Helper()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Get(authURL)
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("the provider did not redirect back: %d", resp.StatusCode)
	}
	back, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse callback: %v", err)
	}
	return back.Query()
}

func (c *testClient) callback(query url.Values) *response {
	return c.Get(oidcCallbackPath + "?" + query.Encode())
}

func (c *testClient) oidcLogin(t *testing.T) *response {
	t.Helper()
	return c.callback(c.authorize(t, "/api/auth/oidc/login"))
}

func redirectTarget(t *testing.T, resp *response) (string, string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("expected a redirect, got %d: %s", resp.StatusCode, resp.Body)
	}
	target, err := url.Parse(resp.Headers.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect: %v", err)
	}
	return target.Path, target.Query().Get("error")
}

func assertRedirect(t *testing.T, resp *response, path string) {
	t.Helper()
	got, errMsg := redirectTarget(t, resp)
	if got != path || errMsg != "" {
		t.Fatalf("got %s (error %q), want %s", got, errMsg, path)
	}
}

func assertRejected(t *testing.T, resp *response, contains string) {
	t.Helper()
	path, errMsg := redirectTarget(t, resp)
	if path != "/auth/login" && path != accountPath {
		t.Fatalf("got a redirect to %s, want the error page", path)
	}
	if !strings.Contains(errMsg, contains) {
		t.Fatalf("got error %q, want it to contain %q", errMsg, contains)
	}
}
