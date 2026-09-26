package config

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
	"unicode"

	"github.com/joho/godotenv"
)

var AppVersion = "dev"

type Config struct {
	DatabaseURL  string
	Host         string
	Port         string
	CacheDir     string
	CORS         string
	StaticDir    string
	ProxyAuth    ProxyAuth
	ProxyAuthErr error
}

// ProxyAuth stays in env: a wrong CIDR lets anyone take over any account.
type ProxyAuth struct {
	TrustedCIDRs []netip.Prefix
	UserHeader   string
	EmailHeader  string
	GroupsHeader string
}

func (p ProxyAuth) Enabled() bool { return len(p.TrustedCIDRs) > 0 && p.UserHeader != "" }

var cached *Config

func Get() Config {
	if cached != nil {
		return *cached
	}
	return Load()
}

func Load() Config {
	_ = godotenv.Load()

	c := Config{
		DatabaseURL: appendSSLDisable(envOr("APP_DATABASE_URL", "")),
		Host:        envOr("APP_HOST", ""),
		Port:        envOr("APP_PORT", "8080"),
		CacheDir:    envOr("APP_CACHE_DIR", "/tmp/voltis_cache"),
		CORS:        envOr("APP_CORS", ""),
		StaticDir:   envOr("APP_STATIC_DIR", ""),
	}
	c.ProxyAuth, c.ProxyAuthErr = loadProxyAuth()
	cached = &c
	return c
}

func loadProxyAuth() (ProxyAuth, error) {
	p := ProxyAuth{
		UserHeader:   envOr("APP_AUTH_PROXY_USER_HEADER", ""),
		EmailHeader:  envOr("APP_AUTH_PROXY_EMAIL_HEADER", ""),
		GroupsHeader: envOr("APP_AUTH_PROXY_GROUPS_HEADER", ""),
	}
	for _, raw := range strings.FieldsFunc(os.Getenv("APP_AUTH_PROXY_TRUSTED_CIDRS"), func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	}) {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return p, fmt.Errorf("APP_AUTH_PROXY_TRUSTED_CIDRS: %q is not a CIDR", raw)
		}
		if prefix.Bits() == 0 {
			return p, fmt.Errorf("APP_AUTH_PROXY_TRUSTED_CIDRS: %q would trust every client", raw)
		}
		p.TrustedCIDRs = append(p.TrustedCIDRs, prefix.Masked())
	}
	if len(p.TrustedCIDRs) > 0 && p.UserHeader == "" {
		return p, errors.New("APP_AUTH_PROXY_USER_HEADER is required when APP_AUTH_PROXY_TRUSTED_CIDRS is set")
	}
	return p, nil
}

func appendSSLDisable(url string) string {
	if url == "" {
		return url
	}
	if strings.Contains(url, "sslmode=") {
		return url
	}
	if strings.Contains(url, "?") {
		return url + "&sslmode=disable"
	} else {
		return url + "?sslmode=disable"
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
