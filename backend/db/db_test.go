package db_test

import (
	"cmp"
	"context"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"

	"voltis/db"
)

// connString is the tests database's connection string, URL or keyword/value, without the options
// under test and with the raw parameters in extra ("k=v&k2=v2") added.
func connString(t *testing.T, extra string) string {
	t.Helper()
	base := cmp.Or(os.Getenv("APP_TESTS_DATABASE_URL"),
		"postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable")
	tested := []string{"jit", "pool_max_conns"}
	if strings.HasPrefix(base, "postgres://") || strings.HasPrefix(base, "postgresql://") {
		u, err := url.Parse(base)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		for _, k := range tested {
			q.Del(k)
		}
		u.RawQuery = strings.Trim(q.Encode()+"&"+extra, "&")
		return u.String()
	}
	if strings.Contains(extra, "%") {
		t.Skip("percent-encoding is URL syntax")
	}
	fields := slices.DeleteFunc(strings.Fields(base), func(f string) bool {
		return slices.ContainsFunc(tested, func(k string) bool { return strings.HasPrefix(f, k+"=") })
	})
	return strings.Join(append(fields, strings.Fields(strings.ReplaceAll(extra, "&", " "))...), " ")
}

func TestConnectTurnsJITOffUnlessTheURLSaysOtherwise(t *testing.T) {
	for extra, want := range map[string]string{"": "off", "jit=on": "on"} {
		t.Run(extra, func(t *testing.T) {
			pool, err := db.Connect(context.Background(), connString(t, extra))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var got string
			if err := pool.QueryRow(context.Background(), "SHOW jit").Scan(&got); err != nil || got != want {
				t.Errorf("jit %q (%v), want %q", got, err, want)
			}
		})
	}
}

func TestConnectPoolSizeDefaultsTo50UnlessTheURLSaysOtherwise(t *testing.T) {
	for extra, want := range map[string]int32{
		"":                                50,
		"pool_max_conns=7":                7,
		"%70ool_max_conns=7":              7,
		"application_name=pool_max_conns": 50,
	} {
		t.Run(extra, func(t *testing.T) {
			pool, err := db.Connect(context.Background(), connString(t, extra))
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			if got := pool.Config().MaxConns; got != want {
				t.Errorf("MaxConns %d, want %d", got, want)
			}
		})
	}
}
