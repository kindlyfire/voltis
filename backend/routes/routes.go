package routes

import (
	"context"
	"log/slog"
	"strings"

	"voltis/lib/sources"
	"voltis/lib/tasks"
	"voltis/scanner"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func adminOnly(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := requireAdmin(c); err != nil {
			return err
		}
		return next(c)
	}
}

func Register(e *echo.Echo, pool *pgxpool.Pool) (*WebSocketHub, *tasks.Manager) {
	hub := NewHub()

	manager := tasks.NewManager(pool, hub.TaskUpdate)
	manager.Register(scanner.ScanTask)
	if err := manager.Load(context.Background()); err != nil {
		slog.Error("failed to load pending tasks", "err", err)
	}

	scanQueue := scanner.NewQueue(manager, pool, hub)

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOriginFunc: func(origin string) (bool, error) {
			return strings.HasPrefix(origin, "http://localhost:") ||
				strings.HasPrefix(origin, "https://localhost:") ||
				origin == "http://localhost" ||
				origin == "https://localhost", nil
		},
		AllowCredentials: true,
	}))

	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		MinLength: 860,
		Skipper: func(c echo.Context) bool {
			return strings.HasPrefix(c.Path(), "/api/files/")
		},
	}))

	e.GET("/api/info", infoHandler(pool))

	api := e.Group("/api", authMiddleware(pool))

	(&AuthRoutes{pool: pool, hub: hub}).Register(api.Group("/auth"))
	(&LibraryRoutes{pool: pool, scanQueue: scanQueue}).Register(api.Group("/libraries"))
	(&UserRoutes{pool: pool, hub: hub}).Register(api.Group("/users"))
	(&ContentRoutes{pool: pool}).Register(api.Group("/content"))
	(&FileRoutes{pool: pool}).Register(api.Group("/files"))
	(&ContentRefRoutes{pool: pool}).Register(api.Group("/content"))
	(&CustomListRoutes{pool: pool}).Register(api.Group("/custom-lists"))
	(&TaskRoutes{pool: pool, manager: manager}).Register(api.Group("/tasks"))
	(&MetadataSourceRoutes{pool: pool, mangabaka: sources.NewMangaBaka()}).Register(api.Group("/metadata-sources"))

	e.GET("/api/ws", wsHandler(pool, hub))

	registerStaticRoutes(e)
	return hub, manager
}
