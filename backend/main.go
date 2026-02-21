package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"voltis/config"
	"voltis/db"
	"voltis/routes"

	"github.com/labstack/echo/v4"
	"github.com/lmittmann/tint"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, nil)))
	cfg := config.Load()

	ctx := context.Background()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		var he *echo.HTTPError
		if errors.As(err, &he) {
			_ = c.JSON(he.Code, map[string]any{"error": he.Message})
		} else {
			slog.Error("unhandled error", "err", err, "method", c.Request().Method, "path", c.Request().URL.String())
			_ = c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}

	routes.Register(e, pool)

	slog.Info("starting server", "url", "http://localhost:"+cfg.Port)
	e.Logger.Fatal(e.Start(":" + cfg.Port))
}
