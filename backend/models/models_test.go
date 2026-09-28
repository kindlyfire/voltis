package models

import (
	"encoding/json"
	"maps"
	"testing"
)

func TestResolve(t *testing.T) {
	lib := LibrarySettings{BookSeriesInference: "off", AutoMatch: map[string]bool{"a": true, "b": false}}
	got := lib.Resolve(SourceSettings{AutoMatch: map[string]bool{"a": false, "c": true}})
	if !maps.Equal(got.AutoMatch, map[string]bool{"a": false, "b": false, "c": true}) || got.BookSeriesInference != "off" {
		t.Fatalf("resolved = %+v", got)
	}
	if !maps.Equal(lib.AutoMatch, map[string]bool{"a": true, "b": false}) {
		t.Fatalf("the library's settings changed: %+v", lib)
	}
	if got := (LibrarySettings{}).Resolve(SourceSettings{}); got.AutoMatch == nil {
		t.Fatal("resolved a nil map")
	}
}

func TestPrefix(t *testing.T) {
	for path, want := range map[string]string{"/": "/", "/lib": "/lib/", "/lib/": "/lib/", "/lib/./a/..": "/lib/", "rel/a": "rel/a/"} {
		if got, ok := (LibrarySource{PathURI: path}).Prefix(); !ok || got != want {
			t.Errorf("%q: %q, %v", path, got, ok)
		}
	}
	for _, path := range []string{".", "", "./"} {
		if got, ok := (LibrarySource{PathURI: path}).Prefix(); ok || got != "" {
			t.Errorf("%q: %q, %v", path, got, ok)
		}
	}
}

func TestParseNormalisesNil(t *testing.T) {
	for _, raw := range []string{"null", "", `[`} {
		if got := ParseLibrarySources(JSONB(raw)); got == nil || len(got) != 0 {
			t.Errorf("%q: %+v", raw, got)
		}
	}
	src := ParseLibrarySources(JSONB(`[{"path_uri": "/a", "settings": null}, {"path_uri": "/b", "settings": {"auto_match": {"x": false}}}]`))
	if len(src) != 2 || src[0].Settings.AutoMatch != nil || src[1].Settings.AutoMatch["x"] != false || len(src[1].Settings.AutoMatch) != 1 {
		t.Fatalf("sources = %+v", src)
	}
	var s LibrarySettings
	if err := json.Unmarshal([]byte(`{"auto_match": {"a": true, "b": null}}`), &s); err == nil {
		t.Errorf("a null switch decoded: %+v", s)
	}
	for _, raw := range []string{"{}", `{"auto_match": null}`, "null"} {
		if s := ParseLibrarySettings(JSONB(raw)); s.AutoMatch == nil || s.BookSeriesInference != BookSeriesInferenceConservative {
			t.Errorf("%q: %+v", raw, s)
		}
	}
}
