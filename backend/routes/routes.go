package routes

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

func Register(e *echo.Echo, pool *pgxpool.Pool) {
	e.GET("/api/info", infoHandler(pool))

	api := e.Group("/api", authMiddleware(pool))

	(&AuthRoutes{pool: pool}).Register(api.Group("/auth"))
	(&LibraryRoutes{pool: pool}).Register(api.Group("/libraries"))
	(&UserRoutes{pool: pool}).Register(api.Group("/users"))
	(&ContentRoutes{pool: pool}).Register(api.Group("/content"))
	(&FileRoutes{pool: pool}).Register(api.Group("/files"))

	registerStaticRoutes(e)
}
