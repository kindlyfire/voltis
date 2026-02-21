package main

import (
	"log/slog"
	"os"

	"voltis/config"
	"voltis/db"

	"github.com/labstack/echo/v4"
	_ "github.com/lib/pq"
	"github.com/lmittmann/tint"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, nil)))
	cfg := config.Load()

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

	e.GET("/api/health", func(c echo.Context) error {
		return c.JSON(200, map[string]string{"status": "ok"})
	})

	slog.Info("starting server", "url", "http://localhost:"+cfg.Port)
	e.Logger.Fatal(e.Start(":" + cfg.Port))
}
