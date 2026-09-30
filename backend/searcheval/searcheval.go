// Package searcheval ranks a hand-judged fixture of title searches against a restored database and
// reports relevance metrics, so ranking changes can be compared on real data.
package searcheval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"slices"
	"time"

	"voltis/db"
	"voltis/metadata"
	"voltis/routes"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fetched is how deep each query's ranks are read; metrics look at the top 10.
const fetched, top = 50, 10

type Options struct {
	Cases, Out, Baseline string
}

type ref struct {
	LibraryID string `json:"library_id"`
	URI       string `json:"uri"`
}

// testCase is one judged query. No expected rows marks a negative case, which passes without hits.
type testCase struct {
	Query     string   `json:"query"`
	Surface   string   `json:"surface"` // header: all libraries; grid: library_id only
	LibraryID string   `json:"library_id"`
	Expected  []ref    `json:"expected"`
	MaxRank   int      `json:"max_rank"`
	Tags      []string `json:"tags"`
}

func (c testCase) key() string { return c.Surface + "|" + c.LibraryID + "|" + c.Query }

// label names the case in output: repeated queries differ by surface and library.
func (c testCase) label() string {
	if c.Surface == "grid" {
		return fmt.Sprintf("%q (grid %s)", c.Query, c.LibraryID)
	}
	return fmt.Sprintf("%q", c.Query)
}

type hit struct {
	ref
	Title string   `json:"title"`
	Score *float64 `json:"score,omitempty"`
}

type caseResult struct {
	testCase
	Rank          int     `json:"rank"`           // best expected rank within the first 50, 0 if none
	ExpectedRanks []int   `json:"expected_ranks"` // per expected row, 0 if absent
	Pass          bool    `json:"pass"`
	Total         int     `json:"total"`
	PageMS        float64 `json:"page_ms"`
	CountMS       float64 `json:"count_ms"`
	Hits          []hit   `json:"hits"` // the top 10
	Error         string  `json:"error,omitempty"`
}

type metrics struct {
	Cases       int      `json:"cases"`
	Pass        float64  `json:"pass"`
	Top1        float64  `json:"top1"`
	MRR10       float64  `json:"mrr10"`
	Recall10    float64  `json:"recall10"`
	ZeroResults float64  `json:"zero_results"`
	TieRate     *float64 `json:"tie_rate,omitempty"` // top-10 rows scored like a neighbor
}

type latency struct {
	PageP50  float64 `json:"page_p50"`
	PageP95  float64 `json:"page_p95"`
	CountP50 float64 `json:"count_p50"`
	CountP95 float64 `json:"count_p95"`
}

type results struct {
	Overall  metrics            `json:"overall"`
	Tags     map[string]metrics `json:"tags"`
	Negative metrics            `json:"negative"` // Pass is the share without hits
	Latency  latency            `json:"latency_ms"`
	Cases    []caseResult       `json:"cases"`
}

// Run evaluates opts.Cases under generic plans, as the app's prepared statements run, writes the
// results to opts.Out and prints them with the rank changes against opts.Baseline. Failed cases
// stay out of the metrics, and fail the run once reported.
func Run(ctx context.Context, pool *pgxpool.Pool, opts Options) error {
	cases, err := readCases(opts.Cases)
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET plan_cache_mode = force_generic_plan"); err != nil {
		return err
	}

	var res results
	failed := 0
	for _, c := range cases {
		r := caseResult{testCase: c}
		if err := evalCase(ctx, conn, &r); err != nil {
			r.Error = err.Error()
			failed++
		}
		res.Cases = append(res.Cases, r)
	}
	summarize(&res)

	var base *results // read first: --out may name the baseline
	if opts.Baseline != "" {
		b, err := os.ReadFile(opts.Baseline)
		if err != nil {
			return err
		}
		base = &results{}
		if err := json.Unmarshal(b, base); err != nil {
			return fmt.Errorf("baseline: %w", err)
		}
	}
	out, err := json.MarshalIndent(res, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(opts.Out, out, 0o644); err != nil {
		return err
	}
	report(res, base)
	if failed > 0 {
		return fmt.Errorf("%d of %d cases failed", failed, len(cases))
	}
	return nil
}

func readCases(path string) ([]testCase, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cases []testCase
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		if len(sc.Bytes()) == 0 {
			continue
		}
		var c testCase
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if c.Surface == "grid" && c.LibraryID == "" || c.Surface != "grid" && c.Surface != "header" {
			return nil, fmt.Errorf("%s:%d: surface must be header, or grid with a library_id", path, n)
		}
		if c.Surface == "header" {
			c.LibraryID = ""
		}
		c.MaxRank = max(c.MaxRank, 1)
		cases = append(cases, c)
	}
	return cases, sc.Err()
}

func evalCase(ctx context.Context, q db.Querier, r *caseResult) error {
	page, err := routes.SearchEvalQuery(ctx, q, r.Query, r.LibraryID, fetched)
	if err != nil {
		return err
	}
	r.Total = page.Total
	r.PageMS = ms(page.PageTime)
	r.CountMS = ms(page.CountTime)

	// Const scores are row-local and BM25 statistics index-wide, so scoring the page's rows alone
	// leaves them as the search computed them. The scores join rather than filter: a filter on
	// c2.id would be pushed into the index query and add its own score.
	hits, err := db.Select[hit](ctx, q, `
		SELECT c.library_id, c.uri, coalesce(c.data->>'title', '') AS title, s.score
		FROM unnest(@ids::text[]) WITH ORDINALITY AS p(id, n)
		JOIN content c ON c.id = p.id
		LEFT JOIN (SELECT c2.id, `+metadata.TitleScore("c2")+`::float8 AS score FROM content c2
			WHERE `+metadata.TitleMatch("c2", true)+`) s ON s.id = c.id
		ORDER BY p.n`, pgx.NamedArgs{"search": r.Query, "ids": page.IDs})
	if err != nil {
		return err
	}

	for _, e := range r.Expected {
		rank := slices.IndexFunc(hits, func(h hit) bool { return h.ref == e }) + 1
		r.ExpectedRanks = append(r.ExpectedRanks, rank)
		if rank > 0 && (r.Rank == 0 || rank < r.Rank) {
			r.Rank = rank
		}
	}
	if len(r.Expected) == 0 {
		r.Pass = r.Total == 0
	} else {
		r.Pass = r.Rank > 0 && r.Rank <= r.MaxRank
	}
	r.Hits = hits[:min(len(hits), top)]
	return nil
}

func summarize(res *results) {
	res.Tags = map[string]metrics{}
	var pos, neg []caseResult
	byTag := map[string][]caseResult{}
	var page, count []float64
	for _, r := range res.Cases {
		if r.Error != "" { // reported on its own; its zero results and timings would skew these
			continue
		}
		page, count = append(page, r.PageMS), append(count, r.CountMS)
		if len(r.Expected) == 0 {
			neg = append(neg, r)
			continue
		}
		pos = append(pos, r)
		for _, t := range r.Tags {
			byTag[t] = append(byTag[t], r)
		}
	}
	res.Overall = measure(pos)
	for t, rs := range byTag {
		res.Tags[t] = measure(rs)
	}
	res.Negative = measure(neg)
	res.Latency = latency{pct(page, 50), pct(page, 95), pct(count, 50), pct(count, 95)}
}

func measure(rs []caseResult) metrics {
	m := metrics{Cases: len(rs)}
	if len(rs) == 0 {
		return m
	}
	var rows, ties int
	for _, r := range rs {
		m.Pass += b2f(r.Pass)
		m.Top1 += b2f(r.Rank == 1)
		if r.Rank > 0 && r.Rank <= top {
			m.MRR10 += 1 / float64(r.Rank)
		}
		if len(r.Expected) > 0 {
			found := 0
			for _, rank := range r.ExpectedRanks {
				if rank > 0 && rank <= top {
					found++
				}
			}
			m.Recall10 += float64(found) / float64(len(r.Expected))
		}
		m.ZeroResults += b2f(r.Total == 0)
		for i, h := range r.Hits {
			if h.Score == nil {
				continue
			}
			rows++
			if i > 0 && sameScore(r.Hits[i-1], h) || i+1 < len(r.Hits) && sameScore(r.Hits[i+1], h) {
				ties++
			}
		}
	}
	n := float64(len(rs))
	m.Pass, m.Top1, m.MRR10, m.Recall10, m.ZeroResults = m.Pass/n, m.Top1/n, m.MRR10/n, m.Recall10/n, m.ZeroResults/n
	if rows > 0 {
		m.TieRate = new(float64(ties) / float64(rows))
	}
	return m
}

func report(res results, base *results) {
	fmt.Printf("%-24s %5s %6s %6s %6s %6s %6s %6s\n", "", "cases", "pass", "top1", "mrr10", "rec10", "zero", "ties")
	line := func(name string, m metrics) {
		ties := "-"
		if m.TieRate != nil {
			ties = fmt.Sprintf("%.3f", *m.TieRate)
		}
		fmt.Printf("%-24s %5d %6.3f %6.3f %6.3f %6.3f %6.3f %6s\n",
			name, m.Cases, m.Pass, m.Top1, m.MRR10, m.Recall10, m.ZeroResults, ties)
	}
	line("overall", res.Overall)
	for _, t := range slices.Sorted(maps.Keys(res.Tags)) {
		line("  "+t, res.Tags[t])
	}
	line("negative (pass=no hits)", res.Negative)
	l := res.Latency
	fmt.Printf("latency ms: page p50 %.1f p95 %.1f, count p50 %.1f p95 %.1f\n", l.PageP50, l.PageP95, l.CountP50, l.CountP95)

	for _, r := range res.Cases {
		if r.Error != "" {
			fmt.Printf("ERROR %s: %s\n", r.label(), r.Error)
		}
	}
	if base == nil {
		return
	}
	old := map[string]caseResult{}
	for _, r := range base.Cases {
		old[r.key()] = r
	}
	fmt.Println("\nrank changes (baseline → now; - is absent from the first 50; negatives show totals):")
	for _, r := range res.Cases {
		o, ok := old[r.key()]
		switch {
		case r.Error != "" || o.Error != "": // reported as errors, not ranks
		case !ok:
			fmt.Printf("  new      %s: %s\n", r.label(), rank(r.Rank))
		case len(r.Expected) == 0 && o.Total != r.Total:
			fmt.Printf("  negative %s: %d → %d\n", r.label(), o.Total, r.Total)
		case len(r.Expected) > 0 && o.Rank != r.Rank:
			fmt.Printf("  %-8s %s: %s → %s\n", verdict(o.Rank, r.Rank), r.label(), rank(o.Rank), rank(r.Rank))
		}
	}
}

func verdict(old, now int) string {
	if now != 0 && (old == 0 || now < old) {
		return "better"
	}
	return "worse"
}

func rank(r int) string {
	if r == 0 {
		return "-"
	}
	return fmt.Sprint(r)
}

func sameScore(a, b hit) bool { return b.Score != nil && a.Score != nil && *a.Score == *b.Score }

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func pct(xs []float64, p int) float64 {
	if len(xs) == 0 {
		return 0
	}
	s := slices.Sorted(slices.Values(xs))
	return s[(len(s)-1)*p/100]
}
