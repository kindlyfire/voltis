package mangabaka

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"slices"
	"testing"

	"voltis/metadata"
	"voltis/providers"
)

var update = flag.Bool("update", false, "rewrite the golden files")

func TestDecodeGolden(t *testing.T) {
	for _, name := range []string{"series", "merged", "deleted"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile("testdata/" + name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			rec, err := New().Decode(providers.Entry{Raw: raw})
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.MarshalIndent(rec, "", "  ")
			path := "testdata/" + name + ".golden"
			if *update {
				if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(want) != string(got)+"\n" {
				t.Fatalf("decoded differently; run with -update to accept:\n%s", got)
			}
		})
	}
}

func TestDecodeRejectsSeriesWithoutTitle(t *testing.T) {
	if _, err := New().Decode(providers.Entry{Raw: json.RawMessage(`{"id": 1, "state": "active", "titles": []}`)}); err == nil {
		t.Fatal("decoded a series without a title")
	}
}

func TestRegistryLayerAddsLinkAndCover(t *testing.T) {
	raw, _ := os.ReadFile("testdata/series.json")
	f, err := providers.NewRegistry(New()).Layer(metadata.EntryKey{Provider: name, ID: "1995"}, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := []metadata.Link{{Label: "MangaBaka", URL: "https://mangabaka.org/manga/1995/Frieren-Beyond-Journey-s-End"}}
	if !reflect.DeepEqual(f.Links.V, want) {
		t.Fatalf("links = %+v", f.Links)
	}
	cover := metadata.CoverRef{URL: "https://images.mangabaka.dev/2/c/8/f/c/a/b/7/8b6b/4b85/b23d/49d0bf08af76"}
	if f.Cover.V != cover {
		t.Fatalf("cover = %+v", f.Cover)
	}
}

func TestMainTitle(t *testing.T) {
	primary := new(true)
	for _, c := range []struct {
		name   string
		titles []title
		want   string
	}{
		{"english", []title{{Language: "ko", Title: "나 혼자만 레벨업", IsPrimary: primary},
			{Language: "en", Title: "Solo Leveling", IsPrimary: primary}}, "Solo Leveling"},
		{"romanized", []title{{Language: "ko", Title: "나 혼자만 레벨업", Traits: []string{"native"}, IsPrimary: primary},
			{Language: "ko-Latn", Title: "Na Honjaman Level Up", IsPrimary: primary}}, "Na Honjaman Level Up"},
		{"romanized native", []title{{Language: "zh", Title: "全职高手", Traits: []string{"native"}},
			{Language: "zh-Latn", Title: "Quanzhi Gaoshou", Traits: []string{"native"}}}, "Quanzhi Gaoshou"},
		{"native", []title{{Language: "fr", Title: "Autre", IsPrimary: primary},
			{Language: "ja", Title: "葬送のフリーレン", Traits: []string{"native"}}}, "葬送のフリーレン"},
		{"none", nil, ""},
	} {
		if got := mainTitle(c.titles); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseID(t *testing.T) {
	for in, want := range map[string]string{
		"1995":   "1995",
		" 0042 ": "42",
		"https://mangabaka.org/manga/1995/Frieren-Beyond-Journey-s-End": "1995",
		"https://mangabaka.org/1995":                                    "1995",
		"https://www.mangabaka.dev/1995?x=1":                            "1995",
		"0":                                                             "",
		"frieren":                                                       "",
		"https://example.org/1995":                                      "",
	} {
		got, ok := New().ParseID(in)
		if ok != (want != "") || (ok && got != want) {
			t.Errorf("ParseID(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestFetchReadsSeriesTheBatchOmits(t *testing.T) {
	series, _ := os.ReadFile("testdata/series.json")
	merged, _ := os.ReadFile("testdata/merged.json")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v2/series/batch":
			if ids := r.URL.Query()["id"]; !slices.Equal(ids, []string{"1995", "100001", "7"}) {
				t.Errorf("batch ids = %v", ids)
			}
			_, _ = w.Write(append(append([]byte(`{"status": 200, "data": [`), series...), `]}`...))
		case "/v2/series/100001":
			_, _ = w.Write(append(append([]byte(`{"status": 200, "data": `), merged...), '}'))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := New()
	p.api, p.http = srv.URL, srv.Client()

	entries, err := p.Fetch(context.Background(), []string{"1995", "100001", "7"})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		ids = append(ids, e.Key.ID)
	}
	if !slices.Equal(ids, []string{"1995", "100001"}) {
		t.Fatalf("entries = %v", ids)
	}
	if want := []string{"/v2/series/batch", "/v2/series/100001", "/v2/series/7"}; !slices.Equal(paths, want) {
		t.Fatalf("requests = %v", paths)
	}
}

func TestMatchAndSearchRequests(t *testing.T) {
	series, _ := os.ReadFile("testdata/series.json")
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.Path+"?"+r.URL.RawQuery)
		_, _ = w.Write(append(append([]byte(`{"status": 200, "data": [`), series...), `]}`...))
	}))
	defer srv.Close()
	p := New()
	p.api, p.http = srv.URL, srv.Client()

	for _, find := range []func(context.Context, string, string, int) ([]providers.Entry, error){p.Match, p.Search} {
		entries, err := find(context.Background(), "comic_series", "Frieren: Beyond", 50)
		if err != nil || len(entries) != 1 || entries[0].Key.ID != "1995" {
			t.Fatalf("entries = %v (%v)", entries, err)
		}
	}
	const q = "?limit=20&q=Frieren%3A+Beyond&schema=full&type=manga&type=manhwa&type=manhua&type=oel"
	if want := []string{"/v2/series/match" + q, "/v2/series/search" + q}; !slices.Equal(got, want) {
		t.Fatalf("requests = %v", got)
	}
}
