package linking

import (
	"cmp"
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/metadata"
	"voltis/providers"
	"voltis/scanner/keys"
)

// MatchQuery is what a series says about itself. It is built from local layers only, so a wrong
// link cannot feed itself back.
type MatchQuery struct {
	ContentType string
	Titles      []string // resolved title, folder name, children's series
	Year        *int     // the folder's year; exact
	EditionYear *int     // the earliest child's date; a lower bound
	Staff       []string
	MaxVolume   *int
}

func buildQuery(ctx context.Context, q db.Querier, t metadata.Target) (MatchQuery, error) {
	type row struct {
		FileURI *string      `db:"file_uri"`
		Doc     metadata.Doc `db:"doc"`
	}
	rows, err := db.Select[row](ctx, q, `
		SELECT c.file_uri, COALESCE(m.data_raw, '{}') AS doc
		FROM content c LEFT JOIN content_metadata m ON m.library_id = c.library_id AND m.uri = c.uri
		WHERE c.id = $1 OR c.parent_id = $1
		ORDER BY c.id = $1 DESC, c."order"
	`, t.ContentID)
	if err != nil || len(rows) == 0 {
		return MatchQuery{}, err
	}

	mq := MatchQuery{ContentType: t.Type}
	series := rows[0]
	local := series.Doc.Local()
	mq.Titles = append([]string{local.Title.V}, series.Doc.File.Normalize().AltTitles.V...)
	mq.Staff = staffNames(local)
	if series.FileURI != nil {
		_, mq.Year = keys.ParseSeriesName(filepath.Base(*series.FileURI))
	}
	for _, child := range rows[1:] {
		f := child.Doc.Local()
		mq.Titles = append(mq.Titles, f.Series.V)
		mq.Staff = append(mq.Staff, staffNames(f)...)
		if y := year(f); y != nil && (mq.EditionYear == nil || *y < *mq.EditionYear) {
			mq.EditionYear = y
		}
		if v, err := strconv.ParseFloat(f.Volume.V, 64); err == nil && (mq.MaxVolume == nil || int(v) > *mq.MaxVolume) {
			mq.MaxVolume = new(int(v))
		}
	}
	mq.Titles = slices.DeleteFunc(dedupeFolded(mq.Titles), func(s string) bool { return s == "" })
	mq.Staff = dedupeFolded(mq.Staff)
	return mq, nil
}

// Fingerprint identifies the inputs a match was decided on, so that writing it can tell they changed.
func (q MatchQuery) Fingerprint() string {
	b, _ := json.Marshal(q)
	return string(b)
}

func dedupeFolded(vs []string) []string {
	seen := map[string]bool{}
	return slices.DeleteFunc(vs, func(s string) bool {
		k := metadata.NormalizeTitle(s)
		dup := seen[k]
		seen[k] = true
		return dup
	})
}

func staffNames(f metadata.Fields) []string {
	return fp.Map(f.Staff.V, func(s metadata.Staff) string { return s.Name })
}

// year reads the year of a partial ISO date.
func year(f metadata.Fields) *int {
	d, ok := f.PublicationDate.Get()
	if !ok {
		return nil
	}
	y, err := strconv.Atoi(d[:4])
	if err != nil {
		return nil
	}
	return &y
}

type EntrySummary struct {
	Key      metadata.EntryKey `json:"key"`
	Title    string            `json:"title"`
	Year     *int              `json:"year"`
	Kind     string            `json:"kind"`
	Status   string            `json:"status"`
	Staff    []string          `json:"staff"`
	CoverURL string            `json:"cover_url"`
	URL      string            `json:"url"`
}

func summarize(key metadata.EntryKey, rec providers.Record) EntrySummary {
	f := rec.Fields
	return EntrySummary{
		Key: key, Title: f.Title.V, Year: year(f), Kind: string(f.Kind.V), Status: string(f.Status.V),
		Staff: fp.Dedup(staffNames(f)), CoverURL: rec.CoverURL, URL: rec.URL,
	}
}

type Evaluation struct {
	Title    float64 `json:"title"`
	Exact    bool    `json:"exact"`
	Year     string  `json:"year,omitempty"`    // match | conflict
	Staff    string  `json:"staff,omitempty"`   // match | conflict
	Volumes  string  `json:"volumes,omitempty"` // conflict
	Score    float64 `json:"score"`
	Eligible bool    `json:"eligible"`
}

type Candidate struct {
	EntrySummary
	Evaluation Evaluation `json:"evaluation"`
}

const (
	match    = "match"
	conflict = "conflict"
)

func evaluate(q MatchQuery, f metadata.Fields) Evaluation {
	var e Evaluation
	e.Title, e.Exact = titleScore(q.Titles, append([]string{f.Title.V}, f.AltTitles.V...))

	if y := year(f); y != nil {
		if q.Year != nil {
			switch d := abs(*q.Year - *y); {
			case d <= 1:
				e.Year = match
			case d >= 3:
				e.Year = conflict
			}
		}
		if q.EditionYear != nil && e.Year != conflict {
			switch d := *q.EditionYear - *y; {
			case d < -1:
				e.Year = conflict
			case d <= 1 && d >= 0:
				e.Year = match
			}
		}
	}

	local, remote := nameSet(q.Staff), nameSet(staffNames(f))
	overlap := false
	for n := range local {
		overlap = overlap || remote[n]
	}
	switch {
	case overlap:
		e.Staff = match
	case len(local) > 0 && len(remote) > 0:
		e.Staff = conflict
	}

	status, known := f.Status.Get()
	if count, ok := f.Count.Get(); known && ok && !status.Active() && q.MaxVolume != nil && *q.MaxVolume > count {
		e.Volumes = conflict
	}

	e.Score = e.Title
	for _, s := range []string{e.Year, e.Staff} {
		if s == match {
			e.Score += 0.3
		}
	}
	noConflict := e.Year != conflict && e.Staff != conflict && e.Volumes != conflict
	e.Eligible = noConflict && (e.Exact || (e.Title >= 0.9 && (e.Year == match || e.Staff == match)))
	return e
}

type MatchDecision struct {
	Link       *metadata.EntryKey
	Candidates []Candidate // the plausible ones, eligible and best first; reviewed unless Link is set
}

// decide links the only eligible candidate; otherwise the plausible ones wait for review.
func decide(cands []Candidate) MatchDecision {
	cands = slices.DeleteFunc(slices.Clone(cands), func(c Candidate) bool { return c.Evaluation.Title < 0.6 })
	slices.SortStableFunc(cands, func(a, b Candidate) int {
		return cmp.Or(rank(b)-rank(a), cmp.Compare(b.Evaluation.Score, a.Evaluation.Score))
	})
	eligible := 0
	for _, c := range cands {
		if c.Evaluation.Eligible {
			eligible++
		}
	}
	d := MatchDecision{Candidates: cands[:min(len(cands), 5)]}
	if eligible == 1 {
		d.Link = &cands[0].Key
	}
	return d
}

func rank(c Candidate) int {
	if c.Evaluation.Eligible {
		return 1
	}
	return 0
}

// titleScore is the best pairing's similarity; equal normalized titles are exact. Titles one edit
// apart score high but are not exact, so only a year or staff match makes them eligible. Titles
// with other numbers are other works: numbered parts or sequels.
func titleScore(local, remote []string) (float64, bool) {
	best := 0.0
	for _, a := range local {
		na := metadata.NormalizeTitle(a)
		for _, b := range remote {
			nb := metadata.NormalizeTitle(b)
			if na == "" || nb == "" || !slices.Equal(numbers(na), numbers(nb)) {
				continue
			}
			if na == nb {
				return 1, true
			}
			best = math.Max(best, similarity(sortTokens(na), sortTokens(nb)))
		}
	}
	return best, false
}

// numbers lists the numbers in a title, however written: "7SEEDS", "3x3", "Vol 01".
func numbers(s string) []string {
	return fp.Map(digitRuns.FindAllString(s, -1), func(n string) string { return cmp.Or(strings.TrimLeft(n, "0"), "0") })
}

var digitRuns = regexp.MustCompile(`\d+`)

func sortTokens(s string) string {
	t := strings.Fields(s)
	slices.Sort(t)
	return strings.Join(t, " ")
}

func similarity(a, b string) float64 {
	n := max(len([]rune(a)), len([]rune(b)))
	if n == 0 {
		return 0
	}
	return 1 - float64(levenshtein(a, b))/float64(n)
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := range ra {
		cur := make([]int, len(rb)+1)
		cur[0] = i + 1
		for j := range rb {
			cost := 1
			if ra[i] == rb[j] {
				cost = 0
			}
			cur[j+1] = min(prev[j+1]+1, cur[j]+1, prev[j]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// nameSet folds names so that word order, spacing, long vowels written out or not ("Ryou", "Ryo"),
// and credits joined with "and" do not matter.
func nameSet(names []string) map[string]bool {
	set := map[string]bool{}
	for _, n := range names {
		for _, name := range strings.Split(" "+metadata.NormalizeTitle(n)+" ", " and ") {
			tokens := strings.Fields(longVowels.Replace(name))
			if len(tokens) == 0 {
				continue
			}
			set[strings.Join(tokens, "")] = true
			slices.Sort(tokens)
			set[strings.Join(tokens, "")] = true
		}
	}
	return set
}

var longVowels = strings.NewReplacer("ou", "o", "uu", "u", "oo", "o")

func abs(n int) int { return max(n, -n) }
