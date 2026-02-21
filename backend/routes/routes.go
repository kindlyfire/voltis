package routes

import (
	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

func Register(e *echo.Echo, db *sqlx.DB) {
	api := e.Group("/api", authMiddleware(db))

	(&AuthRoutes{db: db}).Register(api.Group("/auth"))

	registerStaticRoutes(e)
}
