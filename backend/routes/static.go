package routes

import (
	"net/http"
	"os"
	"strings"

	"github.com/labstack/echo/v4"
)

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
		if _, err := os.Stat(filePath); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}

		// SPA fallback: serve index.html
		r.URL.Path = "/"
		fileServer.ServeHTTP(w, r)
	})))
}
