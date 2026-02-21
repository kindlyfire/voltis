package main

import (
	"context"
	"log/slog"
	"os"

	"voltis/config"
	"voltis/db"
	"voltis/routes"

	"github.com/labstack/echo/v4"
	"github.com/lmittmann/tint"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, nil)))
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

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

	routes.Register(e, pool)

	slog.Info("starting server", "url", "http://localhost:"+cfg.Port)
	e.Logger.Fatal(e.Start(":" + cfg.Port))
}
