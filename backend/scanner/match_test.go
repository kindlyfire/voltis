package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"voltis/linking"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/providertest"
)

// Matching never waits for scans: a series a scan renames or removes while it is looked up is
// skipped.
func TestScanDuringAMatch(t *testing.T) {
	p := newPipeline(t, "comics")
	fake := providertest.New()
	reg := providers.NewRegistry(fake)
	store := metadata.NewStore(reg)
	p.def = NewScanTask(p.notify, store)
	p.manager.Register(p.def)
	writeCBZFixture(t, filepath.Join(p.root, "Bar", "ch1.cbz"), "Bar", "1")
	writeCBZFixture(t, filepath.Join(p.root, "Foo", "ch1.cbz"), "Foo", "1")
	p.mustScan(ScanInput{})

	fake.OnMatch(func(title string) {
		switch title {
		case "Bar":
			must(t, os.RemoveAll(filepath.Join(p.root, "Bar")))
		case "Foo":
			writeCBZFixture(t, filepath.Join(p.root, "Foo", "ch1.cbz"), "Foo Renamed", "1")
		}
		p.mustScan(ScanInput{})
	})
	links := linking.New(p.pool, store, reg, nil, func(string) {})
	// Both skipped, then the renamed series, which the scan put under a new URI, is matched afresh.
	res, err := links.MatchLibrary(context.Background(), p.lib, func(string) bool { return false }, nil)
	if err != nil || res != (linking.MatchResult{Skipped: 2, Unmatched: 1}) {
		t.Fatalf("result = %+v (%v)", res, err)
	}
}
