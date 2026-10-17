//go:build devtools

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"voltis/providers/mangabaka"
	"voltis/providers/mangabaka/mbtest"

	"github.com/urfave/cli/v3"
)

// fake-mangabaka serves a fake MangaBaka library API, for testing scrobbling by hand with
// APP_MANGABAKA_LIBRARY_URL. Run it with `go run -tags devtools . dev fake-mangabaka`.
func init() {
	devCommands = append(devCommands, &cli.Command{
		Name:  "fake-mangabaka",
		Usage: "Serve a fake MangaBaka library API (token mb-test)",
		Flags: []cli.Flag{&cli.StringFlag{Name: "addr", Value: "127.0.0.1:18091"}},
		Action: func(ctx context.Context, c *cli.Command) error {
			fake := mbtest.NewHandler()
			fake.Token("mb-test")

			mux := http.NewServeMux()
			mux.Handle("/", fake)
			mux.HandleFunc("POST /_fake/revoke", func(w http.ResponseWriter, r *http.Request) {
				fake.Revoke(r.URL.Query().Get("token"))
			})
			mux.HandleFunc("POST /_fake/entries/{token}/{id}", func(w http.ResponseWriter, r *http.Request) {
				var e mangabaka.Entry
				if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				fake.Put(r.PathValue("token"), r.PathValue("id"), e)
			})
			logged := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mux.ServeHTTP(w, r)
				for _, req := range fake.Requests() {
					slog.Info("[fake-mangabaka]", "method", req.Method, "path", req.Path, "body", req.Body)
				}
			})
			slog.Info("fake MangaBaka listening", "url", "http://"+c.String("addr"))
			return http.ListenAndServe(c.String("addr"), logged)
		},
	})
}
