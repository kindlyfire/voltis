package routes

import (
	"context"
	"log/slog"
	"strings"

	"voltis/config"
	"voltis/lib/sources"
	"voltis/lib/tasks"
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

func Register(e *echo.Echo, pool *pgxpool.Pool, st *settings.Store, proxy config.ProxyAuth) (*WebSocketHub, *tasks.Manager) {
	hub := NewHub()
	res := &resolver{pool: pool, st: st, hub: hub, proxy: proxy}

	manager := tasks.NewManager(pool, hub.TaskUpdate)
	scanQueue := scanner.NewQueue(manager, pool, hub)
	if err := manager.Load(context.Background()); err != nil {
		slog.Error("failed to load pending tasks", "err", err)
	}

	st.OnChange(func([]string) { hub.DropSessionless(context.Background(), pool) })

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc:  func(origin string) (bool, error) { return isDevOrigin(origin), nil },
		AllowCredentials: true,
	}))

	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		MinLength: 860,
		Skipper: func(c echo.Context) bool {
			return strings.HasPrefix(c.Path(), "/api/files/")
		},
	}))

	e.GET("/api/info", infoHandler(pool, st))

	api := e.Group("/api", authMiddleware(res))

	(&AuthRoutes{pool: pool, hub: hub, st: st}).Register(api.Group("/auth"))
	(&OIDCRoutes{res: res, oidc: newOIDCClient(st)}).Register(api.Group("/auth/oidc"))
	(&SettingsRoutes{st: st, proxy: proxy}).Register(api.Group("/settings"))
	(&LibraryRoutes{pool: pool, scanQueue: scanQueue}).Register(api.Group("/libraries"))
	(&UserRoutes{pool: pool, hub: hub, st: st}).Register(api.Group("/users"))
	(&ContentRoutes{pool: pool}).Register(api.Group("/content"))
	(&FileRoutes{pool: pool}).Register(api.Group("/files"))
	(&ContentRefRoutes{pool: pool}).Register(api.Group("/content"))
	(&CustomListRoutes{pool: pool}).Register(api.Group("/custom-lists"))
	(&TaskRoutes{pool: pool, manager: manager}).Register(api.Group("/tasks"))
	(&MetadataSourceRoutes{pool: pool, mangabaka: sources.NewMangaBaka()}).Register(api.Group("/metadata-sources"))
	(&FsRoutes{}).Register(api.Group("/fs"))

	e.GET("/api/ws", wsHandler(res))

	registerStaticRoutes(e)
	return hub, manager
}
