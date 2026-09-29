package routes

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"

	"voltis/config"
	"voltis/covers"
	"voltis/lib/tasks"
	"voltis/linking"
	"voltis/metadata"
	"voltis/models"
	"voltis/providers"
	"voltis/scanner"
	"voltis/settings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func isDevOrigin(origin string) bool {
	return strings.HasPrefix(origin, "http://localhost:") ||
		strings.HasPrefix(origin, "https://localhost:") ||
		origin == "http://localhost" ||
		origin == "https://localhost"
}

func adminOnly(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := requireAdmin(c); err != nil {
			return err
		}
		return next(c)
	}
}

// Deps are the services built before the routes, since the metadata upgrade needs them.
type Deps struct {
	Hub       *WebSocketHub
	Providers *providers.Registry
	Metadata  *metadata.Store
	Links     *linking.Service
	Covers    *covers.Cache
	StaticDir string
}

// handleError answers an error as JSON. A request its client gave up on is left unanswered: its
// cancellation is not the server's error.
func handleError(err error, c echo.Context) {
	req := c.Request()
	if req.Context().Err() != nil {
		slog.Debug("request canceled", "err", err, "method", req.Method, "path", logPath(c))
		return
	}
	if c.Response().Committed {
		slog.Error("error after the response started", "err", err, "method", req.Method, "path", logPath(c))
		return
	}
	if he, ok := errors.AsType[*echo.HTTPError](err); ok {
		_ = c.JSON(he.Code, map[string]any{"error": he.Message})
		return
	}
	slog.Error("unhandled error", "err", err, "method", req.Method, "path", logPath(c))
	_ = c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
}

// logPath is the request's URL for logs, or its route pattern under /opds/, whose paths carry keys.
func logPath(c echo.Context) string {
	if strings.HasPrefix(c.Request().URL.Path, "/opds/") {
		return c.Path()
	}
	return c.Request().URL.String()
}

func Register(ctx context.Context, e *echo.Echo, pool *pgxpool.Pool, st *settings.Store, proxy config.ProxyAuth, deps Deps) *tasks.Manager {
	e.HTTPErrorHandler = handleError
	hub := deps.Hub
	res := &resolver{pool: pool, st: st, hub: hub, proxy: proxy}

	links := deps.Links
	manager := tasks.NewManager(pool, func(s tasks.Snapshot) {
		hub.TaskUpdate(s)
		if s.Name == scanner.TaskName && s.Status >= models.TaskStatusCompleted {
			links.Wake() // ended, however: matching waits for scans
		}
	})
	scanQueue := scanner.NewQueue(manager, pool, hub, deps.Metadata)
	if err := manager.Load(ctx); err != nil {
		slog.Error("failed to load pending tasks", "err", err)
	}
	go links.Run(ctx, func() bool { return st.Bool(settings.MetadataMatchingPaused) }, scanQueue.Scanning,
		hub.MetadataStatus)

	st.OnChange(func(keys []string) {
		hub.DropSessionless(context.Background(), pool)
		if slices.Contains(keys, settings.MetadataMatchingPaused) {
			links.Wake()
		}
	})

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc:  func(origin string) (bool, error) { return isDevOrigin(origin), nil },
		AllowCredentials: true,
	}))

	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		MinLength: 860,
		Skipper: func(c echo.Context) bool {
			p := c.Path()
			return strings.HasPrefix(p, "/api/files/") || strings.HasPrefix(p, "/opds/:key/file/") ||
				strings.HasPrefix(p, "/opds/:key/pse/") || strings.HasPrefix(p, "/opds/:key/cover/")
		},
	}))

	e.GET("/api/info", infoHandler(pool, st))

	api := e.Group("/api", authMiddleware(res))

	(&AuthRoutes{pool: pool, hub: hub, st: st}).Register(api.Group("/auth"))
	(&OIDCRoutes{res: res, oidc: newOIDCClient(st)}).Register(api.Group("/auth/oidc"))
	(&SettingsRoutes{st: st, proxy: proxy}).Register(api.Group("/settings"))
	(&LibraryRoutes{pool: pool, scanQueue: scanQueue, links: links, reg: deps.Providers, ctx: ctx}).Register(api.Group("/libraries"))
	(&UserRoutes{pool: pool, hub: hub, st: st, proxyEnabled: proxy.Enabled()}).Register(api.Group("/users"))
	(&AppKeyRoutes{pool: pool, st: st}).Register(api.Group("/users/me/app-keys"))
	(&ContentRoutes{pool: pool}).Register(api.Group("/content"))
	fr := &FileRoutes{pool: pool, covers: deps.Covers}
	fr.Register(api.Group("/files"))
	(&ContentRefRoutes{pool: pool, links: deps.Links}).Register(api.Group("/content"))
	(&CustomListRoutes{pool: pool}).Register(api.Group("/custom-lists"))
	(&TaskRoutes{pool: pool, manager: manager}).Register(api.Group("/tasks"))
	(&MetadataRoutes{pool: pool, hub: hub, store: deps.Metadata, links: deps.Links, reg: deps.Providers}).
		Register(api.Group("/metadata"))
	(&FsRoutes{}).Register(api.Group("/fs"))

	e.GET("/api/ws", wsHandler(res))

	// Outside /api: keys, never the session cookie or forwarded auth, authenticate OPDS.
	(&OPDSRoutes{pool: pool, st: st, files: fr}).Register(e.Group("/opds/:key", opdsCache, appKeyAuth(pool)))

	registerStaticRoutes(e, deps.StaticDir)
	return manager
}
