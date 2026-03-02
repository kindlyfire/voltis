package sources

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// These tests hit the live MangaBaka API and are skipped in short mode.
// Run with: go test -run TestMangaBaka ./lib/sources/

func skipIfShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}
}

func assert(t *testing.T, cond bool, msg string) {
	t.Helper()
	if !cond {
		t.Fatal(msg)
	}
}

func TestMangaBakaSearchFrieren(t *testing.T) {
	skipIfShort(t)

	client := NewMangaBaka()
	results, err := client.SeriesSearch(context.Background(), SeriesSearchOpts{
		Query: "Frieren: Beyond Journey's End",
	})
	assert(t, err == nil, fmt.Sprintf("search: %v", err))

	assert(t, len(results.Data) > 0, "search should return results")
	found := false
	for _, s := range results.Data {
		if s.ID == 1995 {
			found = true
			break
		}
	}
	assert(t, found, "search results should contain Frieren (ID 1995)")
}

func TestMangaBakaGetSeriesByID(t *testing.T) {
	skipIfShort(t)

	client := NewMangaBaka()
	series, err := client.SeriesGet(context.Background(), 1995)
	assert(t, err == nil, fmt.Sprintf("get series: %v", err))

	assert(t, series.ID == 1995, "expected ID 1995")
	assert(t, strings.Contains(series.Title, "Frieren"), "expected title to contain 'Frieren'")
	assert(t, series.Description != nil, "expected description to be set")
	assert(t, len(series.Authors) > 0, "expected authors to be non-empty")
	assert(t, len(series.Genres) > 0, "expected genres to be non-empty")
}
