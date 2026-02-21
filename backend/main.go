package main

import (
	"log/slog"
	"os"

	"voltis/config"
	"voltis/db"
	"voltis/routes"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
	"github.com/lmittmann/tint"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, nil)))
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		os.Exit(1)
	}

	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer database.Close()

	if err := db.Migrate(database); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	routes.Register(e, database)

	slog.Info("starting server", "url", "http://localhost:"+cfg.Port)
	e.Logger.Fatal(e.Start(":" + cfg.Port))
}
