package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"

	"voltis/cmd"
	"voltis/config"
	"voltis/db"
	"voltis/routes"

	"github.com/cshum/vipsgen/vips"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/lmittmann/tint"
	"github.com/urfave/cli/v3"
)

func main() {
	slog.SetDefault(slog.New(tint.NewHandler(os.Stderr, nil)))

	app := &cli.Command{
		Name:  "voltis",
		Usage: "Voltis media server",
		Commands: []*cli.Command{
			{
				Name:   "server",
				Usage:  "Start the HTTP server",
				Action: func(ctx context.Context, _ *cli.Command) error { return runServer(ctx) },
			},
			{
				Name:  "users",
				Usage: "User management commands",
				Commands: []*cli.Command{
					{
						Name:      "create",
						Usage:     "Create a new user",
						ArgsUsage: "<username>",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:     "password",
								Usage:    "Password for the user (use - to read from stdin)",
								Required: true,
							},
							&cli.BoolFlag{
								Name:  "admin",
								Usage: "Grant admin permissions",
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							username := c.Args().First()
							if username == "" {
								return errors.New("username is required")
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.CreateUser(ctx, pool, username, c.String("password"), c.Bool("admin"))
						},
					},
					{
						Name:      "update",
						Usage:     "Update an existing user",
						ArgsUsage: "<username>",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:  "username",
								Usage: "New username",
							},
							&cli.StringFlag{
								Name:  "password",
								Usage: "New password (use - to read from stdin)",
							},
							&cli.BoolWithInverseFlag{
								Name:  "admin",
								Usage: "Grant or revoke admin permissions",
							},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							name := c.Args().First()
							if name == "" {
								return errors.New("username is required")
							}
							var usernamePtr, passwordPtr *string
							var adminPtr *bool
							if c.IsSet("username") {
								usernamePtr = new(c.String("username"))
							}
							if c.IsSet("password") {
								passwordPtr = new(c.String("password"))
							}
							if c.IsSet("admin") || c.IsSet("no-admin") {
								adminPtr = new(c.Bool("admin"))
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.UpdateUser(ctx, pool, name, usernamePtr, passwordPtr, adminPtr)
						},
					},
				},
			},
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func connectDB(ctx context.Context) *pgxpool.Pool {
	cfg := config.Load()
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "err", err)
		os.Exit(1)
	}
	return pool
}

func runServer(ctx context.Context) error {
	pool := connectDB(ctx)
	defer pool.Close()
	defer vips.Shutdown()

	cfg := config.Get()

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.HTTPErrorHandler = func(err error, c echo.Context) {
		if c.Response().Committed {
			return
		}
		if he, ok := errors.AsType[*echo.HTTPError](err); ok {
			_ = c.JSON(he.Code, map[string]any{"error": he.Message})
		} else {
			slog.Error("unhandled error", "err", err, "method", c.Request().Method, "path", c.Request().URL.String())
			_ = c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		}
	}

	routes.Register(e, pool)

	slog.Info("starting server", "url", "http://localhost:"+cfg.Port)
	return e.Start(":" + cfg.Port)
}
