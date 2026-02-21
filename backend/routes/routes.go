package routes

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

func Register(e *echo.Echo, pool *pgxpool.Pool) {
	api := e.Group("/api", authMiddleware(pool))

	(&AuthRoutes{pool: pool}).Register(api.Group("/auth"))
	(&LibraryRoutes{pool: pool}).Register(api.Group("/libraries"))

	registerStaticRoutes(e)
}
