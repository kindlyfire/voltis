package routes

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/labstack/echo/v4"
)

// staticBuildID returns the ID the frontend build wrote to build.json, or "" when there is none.
func staticBuildID(dir string) string {
	if _, err := os.Stat(dir); err != nil {
		return ""
	}
	var build struct {
		ID string `json:"id"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "build.json"))
	if err == nil {
		err = json.Unmarshal(data, &build)
	}
	if err != nil {
		slog.Warn("no usable build.json in static dir; open tabs won't be prompted to reload", "dir", dir, "err", err)
	}
	return build.ID
}

func registerStaticRoutes(e *echo.Echo, dir string) {
	if dir == "" {
		return
	}
	if _, err := os.Stat(dir); err != nil {
		return
	}

	fileServer := http.FileServer(http.Dir(dir))

	e.GET("/*", echo.WrapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if strings.HasPrefix(path, "/api/") || path == "/opds" || strings.HasPrefix(path, "/opds/") {
			http.NotFound(w, r)
			return
		}

		filePath := dir + "/" + strings.TrimPrefix(path, "/")
		info, err := os.Stat(filePath)
		exists := err == nil && !info.IsDir()
		if strings.HasPrefix(path, "/assets/") {
			// A stale chunk must fail rather than parse index.html as JS.
			if !exists {
				http.NotFound(w, r)
				return
			}
			// Vite hashes asset names, so their content never changes.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
			if !exists {
				r.URL.Path = "/" // SPA fallback: serve index.html
			}
		}
		fileServer.ServeHTTP(w, r)
	})))
}
