package routes

import (
	"context"
	"net/http"
	"net/url"
	"sync"
	"time"

	"voltis/settings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/labstack/echo/v4"
	"golang.org/x/oauth2"
)

const oidcCallbackPath = "/api/auth/oidc/callback"

// Comparable, so a cached provider can be matched against its settings.
type oidcConfig struct {
	issuer    string
	clientID  string
	secret    string
	publicURL string
	enabled   bool
}

func (c oidcConfig) usable() bool {
	return c.enabled && c.issuer != "" && c.clientID != "" && c.publicURL != ""
}

func (c oidcConfig) redirectURL() string {
	u, _ := url.JoinPath(c.publicURL, oidcCallbackPath)
	return u
}

type oidcClient struct {
	st     *settings.Store
	http   *http.Client
	mu     sync.Mutex
	cfg    oidcConfig
	loaded *oidc.Provider
}

func newOIDCClient(st *settings.Store) *oidcClient {
	client := &oidcClient{st: st, http: &http.Client{Timeout: 10 * time.Second}}
	st.OnChange(func([]string) { client.invalidate() })
	return client
}

func (o *oidcClient) invalidate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.loaded, o.cfg = nil, oidcConfig{}
}

func (o *oidcClient) config() oidcConfig { return oidcConfigOf(o.st) }

func oidcConfigOf(st *settings.Store) oidcConfig {
	values := st.Values()
	str := func(key string) string {
		v, _ := values[key].(string)
		return v
	}
	enabled, _ := values[settings.OIDCEnabled].(bool)
	return oidcConfig{
		issuer:    str(settings.OIDCIssuer),
		clientID:  str(settings.OIDCClientID),
		secret:    str(settings.OIDCClientSecret),
		publicURL: str(settings.AppPublicURL),
		enabled:   enabled,
	}
}

func errNotConfigured() error {
	return echo.NewHTTPError(http.StatusNotFound, "single sign-on is not configured")
}

// Discards a discovery whose configuration moved while it was in flight.
func (o *oidcClient) provider(ctx context.Context) (*oidc.Provider, oidcConfig, error) {
	for range 2 {
		cfg := o.config()
		if !cfg.usable() {
			return nil, cfg, errNotConfigured()
		}

		o.mu.Lock()
		cached, ok := o.loaded, o.cfg == cfg
		o.mu.Unlock()
		if ok && cached != nil {
			return cached, cfg, nil
		}

		// The provider's key set keeps this client for its JWKS fetches.
		provider, err := oidc.NewProvider(oidc.ClientContext(ctx, o.http), cfg.issuer)
		if err != nil {
			return nil, cfg, reject("the identity provider could not be reached", err)
		}
		if o.config() != cfg {
			continue
		}

		o.mu.Lock()
		o.loaded, o.cfg = provider, cfg
		o.mu.Unlock()
		return provider, cfg, nil
	}
	return nil, oidcConfig{}, reject("the single sign-on configuration changed, try again", nil)
}

func (o *oidcClient) oauth(ctx context.Context) (*oauth2.Config, *oidc.IDTokenVerifier, *oidc.Provider, error) {
	provider, cfg, err := o.provider(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	return &oauth2.Config{
		ClientID:     cfg.clientID,
		ClientSecret: cfg.secret,
		Endpoint:     provider.Endpoint(),
		RedirectURL:  cfg.redirectURL(),
		Scopes:       o.st.StringList(settings.OIDCScopes),
	}, provider.Verifier(&oidc.Config{ClientID: cfg.clientID}), provider, nil
}
