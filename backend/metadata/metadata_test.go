package metadata

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestOptRoundTrip(t *testing.T) {
	var f Fields
	if err := json.Unmarshal([]byte(`{"count": 0, "title": null, "volume": "1.5"}`), &f); err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string][2]Presence{
		"count":       {f.Count.P, Value},
		"title":       {f.Title.P, Null},
		"volume":      {f.Volume.P, Value},
		"description": {f.Description.P, Absent},
	} {
		if got[0] != got[1] {
			t.Errorf("%s presence = %d, want %d", name, got[0], got[1])
		}
	}
	if f.Count.V != 0 || f.Volume.V != "1.5" {
		t.Fatalf("values = %v, %q", f.Count.V, f.Volume.V)
	}
	b, _ := json.Marshal(f)
	if string(b) != `{"title":null,"volume":"1.5","count":0}` {
		t.Fatalf("marshal = %s", b)
	}
}

func TestEveryFieldHasADef(t *testing.T) {
	var keys []string
	typ := reflect.TypeFor[Fields]()
	for i := range typ.NumField() {
		key, _, _ := strings.Cut(typ.Field(i).Tag.Get("json"), ",")
		keys = append(keys, key)
	}
	defKeys := make([]string, len(Defs()))
	for i, d := range Defs() {
		defKeys[i] = d.Key
	}
	slices.Sort(keys)
	slices.Sort(defKeys)
	if !slices.Equal(keys, defKeys) {
		t.Fatalf("fields %v\ndefs   %v", keys, defKeys)
	}
}

func TestNormalize(t *testing.T) {
	f := Fields{
		Title:           Val("  Title  "),
		Description:     Val("   "),
		AltTitles:       Val([]string{" A ", "a", "", "B"}),
		Language:        Val("en_US"),
		PublicationDate: Val("2014-6-5"),
		Genres:          Val([]string{"Sci-Fi, Slice of Life", "sci fi"}),
		Staff:           Val([]Staff{{Name: " Ann ", Role: "Cover Artist"}, {Name: "ann", Role: "cover_artist"}, {Name: " "}}),
		Publishers:      Val([]string{}),
		ContentRating:   Val(ContentRating("adult")),
		Status:          Val(Hiatus),
		Rating:          Val(120.0),
		Count:           Val(0),
	}.Normalize()
	want := Fields{
		Title:           Val("Title"),
		AltTitles:       Val([]string{"A", "B"}),
		Language:        Val("en-us"),
		PublicationDate: Val("2014-06-05"),
		Genres:          Val([]string{"sci_fi", "slice_of_life"}),
		Staff:           Val([]Staff{{Name: "Ann", Role: "cover_artist"}}),
		Status:          Val(Hiatus),
		Count:           Val(0),
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got  %+v\nwant %+v", f, want)
	}
}

func TestParsePartialDate(t *testing.T) {
	for in, want := range map[string]string{
		"2014":                      "2014",
		"2014-06-15":                "2014-06-15",
		"2014-06-15T00:00:00+00:00": "2014-06-15",
		"2014-6-5":                  "2014-06-05",
		"2014-06":                   "2014-06",
		" 2014 ":                    "2014",
		"2014-02-30":                "",
		"2014-13":                   "",
		"14":                        "",
		"0000":                      "",
		"2014-+1":                   "",
		"soon":                      "",
		"":                          "",
	} {
		if got := ParsePartialDate(in); got != want {
			t.Errorf("ParsePartialDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := map[string]string{
		"Frieren: Beyond Journey’s End": "frieren beyond journey s end",
		"Café & Chill":                  "cafe and chill",
		"ＨＥＬＬＳＩＮＧ":                      "hellsing",
		"  --  ":                        "",
	}
	// Run concurrently so -race catches shared transformer state.
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				for in, want := range cases {
					if got := NormalizeTitle(in); got != want {
						t.Errorf("NormalizeTitle(%q) = %q, want %q", in, got, want)
					}
				}
			}
		})
	}
	wg.Wait()
}

func TestAgeRating(t *testing.T) {
	for in, want := range map[string]Opt[ContentRating]{
		"Everyone":        Val(Safe),
		"Kids to Adults":  Val(Safe),
		"PG":              Val(Safe),
		"Teen":            Val(Suggestive),
		"MA15+":           Val(Suggestive),
		"Mature 17+":      Val(Suggestive),
		"Adults Only 18+": Val(Erotica),
		"R18+":            Val(Erotica),
		"X18+":            Val(Pornographic),
		"Unknown":         {},
		"Rating Pending":  {},
		"":                {},
	} {
		if got := AgeRating(in); got != want {
			t.Errorf("AgeRating(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestMerge(t *testing.T) {
	file := Layer{"file", Fields{
		Title: Val("Local"), AltTitles: Val([]string{"Folder"}), Description: Val("From the file"),
		Publishers: Val([]string{"Press"}), Count: Val(3),
	}}
	provider := Layer{"mangabaka", Fields{
		Title: Val("Remote"), AltTitles: Val([]string{"folder", "Local", "Native"}), Description: Val("From upstream"),
		Links: Val([]Link{{Label: "MangaBaka", URL: "https://mb/1"}}),
	}}
	overrides := Layer{"overrides", Fields{Description: Opt[string]{P: Null}, Count: Val(0), Title: Val("Chosen")}}

	got := Merge(file, provider, overrides)
	want := Fields{
		Title:      Val("Chosen"),
		AltTitles:  Val([]string{"Folder", "Local", "Native", "Remote"}),
		Publishers: Val([]string{"Press"}),
		Links:      Val([]Link{{Label: "MangaBaka", URL: "https://mb/1"}}),
		Count:      Val(0),
	}
	if !reflect.DeepEqual(got.Fields, want) {
		t.Fatalf("fields\ngot  %+v\nwant %+v", got.Fields, want)
	}
	wantSources := map[string][]string{
		"title":      {"overrides"},
		"alt_titles": {"mangabaka", "file"},
		"publishers": {"file"},
		"links":      {"mangabaka"},
		"count":      {"overrides"},
	}
	if !reflect.DeepEqual(got.Sources, wantSources) {
		t.Fatalf("sources = %v, want %v", got.Sources, wantSources)
	}

	// A cleared title shadows nothing.
	cleared := Merge(Layer{"file", Fields{Title: Val("Local")}}, Layer{"overrides", Fields{Title: Opt[string]{P: Null}}})
	if cleared.Fields.Title.P != Absent || cleared.Fields.AltTitles.P != Absent {
		t.Fatalf("cleared title = %+v", cleared.Fields)
	}

	// A title shadowed by the final one is not listed as an alternative to itself.
	if alts := Merge(Layer{"file", Fields{Title: Val("Same"), AltTitles: Val([]string{"same"})}}).Fields.AltTitles; alts.P != Absent {
		t.Fatalf("alt titles = %+v", alts)
	}
}

func TestDecodeOverrides(t *testing.T) {
	f, err := DecodeOverrides(json.RawMessage(`{
		"title": "  Kept ", "description": "", "genres": [], "count": 0, "rating": null,
		"cover": null, "status": "hiatus", "publication_date": "2014-6"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	want := Fields{
		Title: Val("Kept"), Description: Opt[string]{P: Null}, Genres: Opt[[]string]{P: Null},
		Count: Val(0), Rating: Opt[float64]{P: Null}, Cover: Opt[CoverRef]{P: Null}, Status: Val(Hiatus),
		PublicationDate: Val("2014-06"),
	}
	if !reflect.DeepEqual(f, want) {
		t.Fatalf("got  %+v\nwant %+v", f, want)
	}

	for body, field := range map[string]string{
		`[]`:                             "",
		`{"nope": 1}`:                    "nope",
		`{"alt_titles": ["x"]}`:          "alt_titles",
		`{"cover": {"url": "x"}}`:        "cover",
		`{"count": "one"}`:               "count",
		`{"count": -1}`:                  "count",
		`{"rating": 101}`:                "rating",
		`{"status": "done"}`:             "status",
		`{"publication_date": "sooner"}`: "publication_date",
	} {
		_, err := DecodeOverrides(json.RawMessage(body))
		if ve, ok := errors.AsType[*ValidationError](err); !ok || ve.Field != field {
			t.Errorf("%s: err = %v, want a validation error on %q", body, err, field)
		}
	}
}
