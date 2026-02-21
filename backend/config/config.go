package config

import (
	"os"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	DatabaseURL         string
	Port                string
	CacheDir            string
	CORS                string
	StaticDir           string
	RegistrationEnabled bool
}

var cached *Config

func Get() Config {
	if cached != nil {
		return *cached
	}
	c, err := Load()
	if err != nil {
		panic("config: " + err.Error())
	}
	return c
}

func Load() (Config, error) {
	if err := godotenv.Load(); err != nil {
		return Config{}, err
	}

	c := Config{
		DatabaseURL:         appendSSLDisable(envOr("APP_DATABASE_URL", "")),
		Port:                envOr("APP_PORT", "8080"),
		CacheDir:            envOr("APP_CACHE_DIR", "/tmp/voltis_cache"),
		CORS:                envOr("APP_CORS", ""),
		StaticDir:           envOr("APP_STATIC_DIR", ""),
		RegistrationEnabled: os.Getenv("APP_REGISTRATION_ENABLED") == "true",
	}
	cached = &c
	return c, nil
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
