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
	"voltis/lib/notify"
	"voltis/lib/tasks"
	"voltis/linking"
	"voltis/metadata"
	"voltis/models"
	"voltis/providers"
	"voltis/providers/mangabaka"
	"voltis/scanner"
	"voltis/scrobble"
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

// Services are what the routes use. NewServices builds them; Run starts the workers.
type Services struct {
	Hub       *WebSocketHub
	Manager   *tasks.Manager
	Scans     *scanner.Queue
	Scrobbles *scrobble.Service
	Links     *linking.Service
	Providers *providers.Registry
	Metadata  *metadata.Store
	Covers    *covers.Cache
	Library   *mangabaka.Library
	StaticDir string
	Ctx       context.Context // the app's: work that outlives a request stops with it

	pool *pgxpool.Pool
	st   *settings.Store
}

// NewServices builds the services. ctx is the app's.
func NewServices(ctx context.Context, pool *pgxpool.Pool, st *settings.Store, reg *providers.Registry, cov *covers.Cache,
	lib *mangabaka.Library, staticDir string) Services {
	hub := NewHub()
	meta := metadata.NewStore(reg)
	links := linking.New(pool, meta, reg, cov, hub.LibraryChanged)
	manager := tasks.NewManager(pool, func(s tasks.Snapshot) {
		hub.TaskUpdate(s)
		if s.Name == scanner.TaskName && s.Status >= models.TaskStatusCompleted {
			links.Wake() // ended, however: matching waits for scans
		}
	})
	hub.unread = func(ctx context.Context, ids []string) (map[string]int, error) {
		return notify.Unread(ctx, pool, ids)
	}
	st.OnChange(func(keys []string) {
		hub.DropSessionless(context.Background(), pool)
		if slices.Contains(keys, settings.MetadataMatchingPaused) {
			links.Wake()
		}
	})
	return Services{Hub: hub, Manager: manager, Scans: scanner.NewQueue(manager, pool, hub, meta),
		Scrobbles: scrobble.New(pool, lib, hub.ScrobblesChanged, hub.NotificationsChanged), Links: links,
		Providers: reg, Metadata: meta, Covers: cov, Library: lib, StaticDir: staticDir, Ctx: ctx,
		pool: pool, st: st}
}

// Run loads the pending tasks and starts the workers that stop with Ctx, except the scrobble
// worker. A load failure is returned, and the workers run regardless.
func (s Services) Run() error {
	err := s.Manager.Load(s.Ctx)
	go s.Links.Run(s.Ctx, func() bool { return s.st.Bool(settings.MetadataMatchingPaused) }, s.Scans.Scanning,
		s.Hub.MetadataStatus, s.Hub.NotificationsChanged)
	go notify.RunSweep(s.Ctx, s.pool)
	return err
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

// Register mounts the routes.
func Register(e *echo.Echo, pool *pgxpool.Pool, st *settings.Store, proxy config.ProxyAuth, svc Services) {
	e.HTTPErrorHandler = handleError
	res := &resolver{pool: pool, st: st, hub: svc.Hub, proxy: proxy}

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

	e.GET("/api/info", infoHandler(pool, st, staticBuildID(svc.StaticDir)))

	api := e.Group("/api", authMiddleware(res))

	(&AuthRoutes{pool: pool, hub: svc.Hub, st: st, proxyEnabled: proxy.Enabled()}).Register(api.Group("/auth"))
	(&OIDCRoutes{res: res, oidc: newOIDCClient(st)}).Register(api.Group("/auth/oidc"))
	(&SettingsRoutes{st: st, proxy: proxy}).Register(api.Group("/settings"))
	(&LibraryRoutes{pool: pool, scanQueue: svc.Scans, links: svc.Links, reg: svc.Providers, hub: svc.Hub, ctx: svc.Ctx,
		scrobbles: svc.Scrobbles}).Register(api.Group("/libraries"))
	(&UserRoutes{pool: pool, hub: svc.Hub, st: st, proxyEnabled: proxy.Enabled(), scrobbles: svc.Scrobbles}).
		Register(api.Group("/users"))
	(&AppKeyRoutes{pool: pool, st: st}).Register(api.Group("/users/me/app-keys"))
	(&ContentRoutes{pool: pool, scrobbles: svc.Scrobbles}).Register(api.Group("/content"))
	(&FacetRoutes{pool: pool}).Register(api.Group("/facets"))
	fr := &FileRoutes{pool: pool, covers: svc.Covers}
	fr.Register(api.Group("/files"))
	(&ContentRefRoutes{pool: pool, scrobbles: svc.Scrobbles}).Register(api.Group("/content"))
	(&CustomListRoutes{pool: pool}).Register(api.Group("/custom-lists"))
	(&TaskRoutes{pool: pool, manager: svc.Manager, scans: svc.Scans}).Register(api.Group("/tasks"))
	(&MetadataRoutes{pool: pool, hub: svc.Hub, store: svc.Metadata, links: svc.Links, reg: svc.Providers}).
		Register(api.Group("/metadata"))
	(&NotificationRoutes{pool: pool, hub: svc.Hub}).Register(api.Group("/notifications"))
	(&ScrobbleRoutes{pool: pool, svc: svc.Scrobbles, lib: svc.Library, hub: svc.Hub}).
		Register(api.Group("/users/me/scrobbling"), api.Group("/scrobbles"), api.Group("/content"))
	(&FsRoutes{}).Register(api.Group("/fs"))

	e.GET("/api/ws", wsHandler(res))

	// Outside /api: keys, never the session cookie or forwarded auth, authenticate OPDS.
	(&OPDSRoutes{pool: pool, st: st, files: fr, scrobbles: svc.Scrobbles}).
		Register(e.Group("/opds/:key", opdsCache, appKeyAuth(pool)))

	registerStaticRoutes(e, svc.StaticDir)
}
