package linking

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"
	"voltis/providers/mangabaka"
)

func TestEvaluate(t *testing.T) {
	remote := metadata.Fields{
		Title:           metadata.Val("Frieren: Beyond Journey's End"),
		AltTitles:       metadata.Val([]string{"Sousou no Frieren"}),
		PublicationDate: metadata.Val("2020-04-28"),
		Staff:           metadata.Val([]metadata.Staff{{Name: "Kanehito Yamada", Role: "author"}}),
		Status:          metadata.Val(metadata.Completed),
		Count:           metadata.Val(10),
	}
	for _, c := range []struct {
		name string
		q    MatchQuery
		want Evaluation
	}{
		{"alt title exact", MatchQuery{Titles: []string{"Sousou no Frieren"}},
			Evaluation{Title: 1, Exact: true, Score: 1, Eligible: true}},
		{"one edit away", MatchQuery{Titles: []string{"Sosou no Frieren"}},
			Evaluation{Title: 0.9411764705882353, Score: 0.9411764705882353}},
		{"one edit away with the year", MatchQuery{Titles: []string{"Sosou no Frieren"}, Year: new(2020)},
			Evaluation{Title: 0.9411764705882353, Year: match, Score: 1.2411764705882353, Eligible: true}},
		{"close title with staff", MatchQuery{Titles: []string{"Frieren Beyond Journeys Ends"}, Staff: []string{"Yamada Kanehito"}},
			Evaluation{Title: 0.9285714285714286, Staff: match, Score: 1.2285714285714286, Eligible: true}},
		{"close title alone", MatchQuery{Titles: []string{"Frieren Beyond Journeys Ends"}},
			Evaluation{Title: 0.9285714285714286, Score: 0.9285714285714286}},
		{"folder year near", MatchQuery{Titles: []string{"Frieren"}, Year: new(2021)},
			Evaluation{Title: 0.4117647058823529, Year: match, Score: 0.7117647058823529}},
		{"folder year far", MatchQuery{Titles: []string{"Sousou no Frieren"}, Year: new(2017)},
			Evaluation{Title: 1, Exact: true, Year: conflict, Score: 1}},
		{"edition before the original", MatchQuery{Titles: []string{"Sousou no Frieren"}, EditionYear: new(2018)},
			Evaluation{Title: 1, Exact: true, Year: conflict, Score: 1}},
		{"staff spelt otherwise", MatchQuery{Titles: []string{"Sousou no Frieren"}, Staff: []string{"Kanehitou Yamada and Tsukasa Abe"}},
			Evaluation{Title: 1, Exact: true, Staff: match, Score: 1.3, Eligible: true}},
		{"other staff", MatchQuery{Titles: []string{"Sousou no Frieren"}, Staff: []string{"Someone Else"}},
			Evaluation{Title: 1, Exact: true, Staff: conflict, Score: 1}},
		{"more volumes than a finished series", MatchQuery{Titles: []string{"Sousou no Frieren"}, MaxVolume: new(11)},
			Evaluation{Title: 1, Exact: true, Volumes: conflict, Score: 1}},
	} {
		if got := evaluate(c.q, remote); got != c.want {
			t.Errorf("%s:\ngot  %+v\nwant %+v", c.name, got, c.want)
		}
	}

	// Numbers tell works apart however they are written, but not other numbers.
	for _, c := range []struct {
		local, remote string
		title         float64
	}{
		{"7 Seeds", "7SEEDS", 0.8571428571428572},
		{"3x3 Eyes", "3×3 Eyes", 0.875},
		{"86", "86 -Eighty Six-", 0.15384615384615385},
		{"Vol 01", "Vol 1", 0.8333333333333334},
		{"Part 2", "Part 3", 0},
		{"Ascendance of a Bookworm: Part 2", "Ascendance of a Bookworm: Part 3", 0},
	} {
		got := evaluate(MatchQuery{Titles: []string{c.local}}, metadata.Fields{Title: metadata.Val(c.remote)})
		if got.Title != c.title {
			t.Errorf("%q vs %q: title %v, want %v", c.local, c.remote, got.Title, c.title)
		}
	}
}

func TestDecide(t *testing.T) {
	cand := func(id string, e Evaluation) Candidate {
		return Candidate{EntrySummary: EntrySummary{Key: metadata.EntryKey{Provider: "fake", ID: id}}, Evaluation: e}
	}
	exact := Evaluation{Title: 1, Exact: true, Score: 1, Eligible: true}
	corroborated := Evaluation{Title: 0.93, Year: match, Score: 1.23, Eligible: true}
	conflicting := Evaluation{Title: 1, Exact: true, Staff: conflict, Score: 1}
	for _, c := range []struct {
		name   string
		cands  []Candidate
		link   string
		review []string
	}{
		{"one exact", []Candidate{cand("1", conflicting), cand("2", exact)}, "2", []string{"2", "1"}},
		{"one corroborated", []Candidate{cand("1", Evaluation{Title: 0.95, Score: 0.95}), cand("2", corroborated)}, "2", []string{"2", "1"}},
		{"one of each branch", []Candidate{cand("1", exact), cand("2", corroborated)}, "", []string{"2", "1"}},
		{"none eligible", []Candidate{cand("1", Evaluation{Title: 0.5}), cand("2", Evaluation{Title: 0.7, Score: 0.7}), cand("3", conflicting)}, "", []string{"3", "2"}},
		{"implausible", []Candidate{cand("1", Evaluation{Title: 0.59, Score: 0.59})}, "", nil},
		{"top five", slices.Repeat([]Candidate{cand("1", exact)}, 6), "", slices.Repeat([]string{"1"}, 5)},
	} {
		d := decide(c.cands)
		link := ""
		if d.Link != nil {
			link = d.Link.ID
		}
		if ids := fp.Map(d.Candidates, func(c Candidate) string { return c.Key.ID }); link != c.link || !slices.Equal(ids, c.review) {
			t.Errorf("%s: link %q, candidates %v; want %q, %v", c.name, link, ids, c.link, c.review)
		}
	}
}

// matchFixtures serves MangaBaka match results captured once, by query.
type matchFixtures struct {
	*mangabaka.Provider
	t       *testing.T
	results map[string][]string // query -> testdata files
}

func (p matchFixtures) Match(_ context.Context, _, title string, _ int) ([]providers.Entry, error) {
	var out []providers.Entry
	for _, name := range p.results[title] {
		data, err := os.ReadFile(filepath.Join("testdata", "match_"+name+".json"))
		if err != nil {
			p.t.Fatal(err)
		}
		var raws []json.RawMessage
		if err := json.Unmarshal(data, &raws); err != nil {
			p.t.Fatal(err)
		}
		for _, raw := range raws {
			var s struct{ ID int }
			_ = json.Unmarshal(raw, &s)
			out = append(out, providers.Entry{Key: metadata.EntryKey{Provider: p.Name(), ID: strconv.Itoa(s.ID)}, Raw: raw})
		}
	}
	return out, nil
}

func TestMatchFixtures(t *testing.T) {
	const (
		fly      = "Fly Me to the Moon"
		angel    = "The Angel Next Door Spoils Me Rotten"
		bookworm = "Ascendance of a Bookworm: Part 2"
	)
	p := matchFixtures{mangabaka.New(), t, map[string][]string{
		fly: {"fly"}, "HELLSING": {"hellsing"}, "HELLSING (1997)": nil,
		angel: {"angel_comic", "angel_novel"}, bookworm: {"bookworm_comic", "bookworm_novel"},
	}}
	for _, c := range []struct {
		name   string
		q      MatchQuery
		link   string
		review int // candidates for review
	}{
		// Five entries carry the title exactly, two of them by Kenjirou Hata.
		{"title alone", MatchQuery{ContentType: "comic_series", Titles: []string{fly}}, "", 5},
		{"staff and volumes", MatchQuery{ContentType: "comic_series", Titles: []string{fly}, Staff: []string{"Kenjiro Hata"},
			MaxVolume: new(12)}, "477", 0},
		// Upstream duplicates, a year apart.
		{"duplicates", MatchQuery{ContentType: "comic_series", Titles: []string{"HELLSING", "HELLSING (1997)"}, Year: new(1997)}, "", 2},
		{"novel", MatchQuery{ContentType: "book_series", Titles: []string{angel}}, "84791", 0},
		{"manga", MatchQuery{ContentType: "comic_series", Titles: []string{angel}}, "46", 0},
		// Parts one edit apart are other works.
		{"part of a novel", MatchQuery{ContentType: "book_series", Titles: []string{bookworm}, Staff: []string{"Miya Kazuki"}}, "100477", 0},
		{"part of a manga", MatchQuery{ContentType: "comic_series", Titles: []string{bookworm}}, "5012", 0},
	} {
		found, err := retrieve(context.Background(), p, p.Match, c.q, true)
		if err != nil {
			t.Fatal(err)
		}
		var cands []Candidate
		for _, f := range found {
			if !slices.ContainsFunc(cands, func(o Candidate) bool { return o.Key == f.Entry.Key }) {
				cands = append(cands, Candidate{summarize(f.Entry.Key, f.Record), evaluate(c.q, f.Record.Fields)})
			}
		}
		d := decide(cands)
		link, review := "", len(d.Candidates)
		if d.Link != nil {
			link, review = d.Link.ID, 0
		}
		if link != c.link || review != c.review {
			t.Errorf("%s: link %q, %d candidates; want %q, %d\n%+v", c.name, link, review, c.link, c.review, d.Candidates)
		}
	}
}
