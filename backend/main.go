package main

import (
	"cmp"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"

	"voltis/cmd"
	"voltis/config"
	"voltis/covers"
	"voltis/db"
	"voltis/linking"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/mangabaka"
	"voltis/routes"
	"voltis/searcheval"
	"voltis/settings"

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
				Name:  "settings",
				Usage: "Read and write server settings",
				Commands: []*cli.Command{
					{
						Name:  "list",
						Usage: "List all settings",
						Action: func(ctx context.Context, _ *cli.Command) error {
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.ListSettings(ctx, pool)
						},
					},
					{
						Name:      "get",
						Usage:     "Show the value of a setting",
						ArgsUsage: "<key>",
						Action: func(ctx context.Context, c *cli.Command) error {
							key := c.Args().First()
							if key == "" {
								return errors.New("key is required")
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.GetSetting(ctx, pool, key)
						},
					},
					{
						Name:      "set",
						Usage:     "Change the value of a setting",
						ArgsUsage: "<key> <value>",
						Action: func(ctx context.Context, c *cli.Command) error {
							key, value := c.Args().Get(0), c.Args().Get(1)
							if key == "" || c.Args().Len() < 2 {
								return errors.New("key and value are required")
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.SetSetting(ctx, pool, key, value)
						},
					},
				},
			},
			{
				Name:  "metadata",
				Usage: "Metadata provider commands",
				Commands: []*cli.Command{
					{
						Name:  "match",
						Usage: "Match a library's series with metadata providers",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "library", Usage: "Library ID", Required: true},
							&cli.BoolFlag{Name: "dry-run", Usage: "Print each decision without writing it"},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							pool := connectDB(ctx)
							defer pool.Close()
							reg := newProviders()
							links := linking.New(pool, metadata.NewStore(reg), reg, covers.New(config.Get().CacheDir), func(string) {})
							return cmd.MatchLibrary(ctx, pool, links, c.String("library"), c.Bool("dry-run"))
						},
					},
				},
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
								Name:  "password",
								Usage: "Password for the user (use - to read from stdin)",
							},
							&cli.BoolFlag{
								Name:  "no-password",
								Usage: "Create the user without a password, for an external login to claim",
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
							if c.IsSet("password") == c.Bool("no-password") {
								return errors.New("pass exactly one of --password and --no-password")
							}
							var password *string
							if c.IsSet("password") {
								password = new(c.String("password"))
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.CreateUser(ctx, pool, username, password, c.Bool("admin"))
						},
					},
					{
						Name:      "link",
						Usage:     "Link an external identity to a user",
						ArgsUsage: "<username>",
						Flags: []cli.Flag{
							&cli.StringFlag{Name: "provider", Usage: "proxy or oidc", Required: true},
							&cli.StringFlag{Name: "issuer", Usage: "OIDC issuer URL (oidc only)"},
							&cli.StringFlag{Name: "subject", Usage: "Proxy username or OIDC subject", Required: true},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							username := c.Args().First()
							if username == "" {
								return errors.New("username is required")
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.LinkIdentity(ctx, pool, username, c.String("provider"), c.String("issuer"), c.String("subject"))
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
			{
				Name:  "identities",
				Usage: "External identity commands",
				Commands: []*cli.Command{
					{
						Name:      "set-issuer",
						Usage:     "Move OIDC identities to a new issuer URL",
						ArgsUsage: "<old> <new>",
						Action: func(ctx context.Context, c *cli.Command) error {
							from, to := c.Args().Get(0), c.Args().Get(1)
							if from == "" || to == "" {
								return errors.New("old and new issuer are required")
							}
							if from == to {
								return errors.New("old and new issuer are the same")
							}
							pool := connectDB(ctx)
							defer pool.Close()
							return cmd.SetIssuer(ctx, pool, from, to)
						},
					},
				},
			},
			{
				Name:   "dev",
				Hidden: true,
				Commands: []*cli.Command{
					{
						Name:  "search-eval",
						Usage: "Rank a relevance fixture's searches against a restored database",
						Flags: []cli.Flag{
							// No APP_DATABASE_URL fallback, so it never runs against the dev database by accident.
							&cli.StringFlag{Name: "db", Usage: "Database URL", Required: true},
							&cli.StringFlag{Name: "cases", Usage: "JSONL cases file", Required: true},
							&cli.StringFlag{Name: "out", Usage: "Results JSON file", Required: true},
							&cli.StringFlag{Name: "baseline", Usage: "Results JSON file to diff ranks against"},
						},
						Action: func(ctx context.Context, c *cli.Command) error {
							pool, err := db.Connect(ctx, c.String("db"))
							if err != nil {
								return err
							}
							defer pool.Close()
							return searcheval.Run(ctx, pool, searcheval.Options{
								Cases: c.String("cases"), Out: c.String("out"), Baseline: c.String("baseline"),
							})
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

// newProviders lists the metadata providers, in merge order.
func newProviders() *providers.Registry { return providers.NewRegistry(mangabaka.New()) }

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
	if cfg.ProxyAuthErr != nil {
		return cfg.ProxyAuthErr
	}

	store, err := settings.New(ctx, pool)
	if err != nil {
		return err
	}
	store.Listen()
	defer store.Close()

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true

	hub := routes.NewHub()
	reg := newProviders()
	meta := metadata.NewStore(reg)
	cov := covers.New(cfg.CacheDir)
	links := linking.New(pool, meta, reg, cov, hub.LibraryChanged)
	routes.Register(ctx, e, pool, store, cfg.ProxyAuth,
		routes.Deps{Hub: hub, Providers: reg, Metadata: meta, Links: links, Covers: cov, StaticDir: cfg.StaticDir})

	slog.Info("starting server", "url", "http://"+net.JoinHostPort(cmp.Or(cfg.Host, "localhost"), cfg.Port))
	return e.Start(net.JoinHostPort(cfg.Host, cfg.Port))
}
