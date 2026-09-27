package cmd

import (
	"context"
	"fmt"
	"strings"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/linking"
	"voltis/metadata"
	"voltis/models"
	"voltis/scanner"

	"github.com/jackc/pgx/v5/pgxpool"
)

// MatchLibrary matches every pending series of a library, those backing off included, whether or
// not it matches automatically, and prints the counts; a dry run writes nothing and prints each
// decision. Like the worker, it skips series while their library is scanned.
func MatchLibrary(ctx context.Context, pool *pgxpool.Pool, links *linking.Service, libraryID string, dryRun bool) error {
	var report func(metadata.Target, linking.MatchQuery, linking.MatchDecision, error)
	scanning := scanRunning(ctx, pool)
	if dryRun {
		scanning = func(string) bool { return false }
		report = func(t metadata.Target, q linking.MatchQuery, d linking.MatchDecision, err error) {
			title := t.URI
			if len(q.Titles) > 0 {
				title = q.Titles[0]
			}
			switch {
			case err != nil:
				fmt.Printf("failed     %s: %v\n", title, err)
			case d.Link != nil:
				fmt.Printf("linked     %s → %s\n", title, describeCandidate(d.Candidates[0]))
			case len(d.Candidates) > 0:
				fmt.Printf("review     %s → %s\n", title, strings.Join(fp.Map(d.Candidates, describeCandidate), "; "))
			default:
				fmt.Printf("unmatched  %s\n", title)
			}
		}
	}
	res, err := links.MatchLibrary(ctx, libraryID, scanning, report)
	fmt.Printf("\nlinked %d, review %d, unmatched %d, failed %d, skipped %d\n",
		res.Linked, res.Review, res.Unmatched, res.Failed, res.Skipped)
	return err
}

// scanRunning reports whether the tasks table has a scan of the library pending or in progress,
// which the server runs; failing to tell counts as one.
func scanRunning(ctx context.Context, pool *pgxpool.Pool) func(libraryID string) bool {
	return func(libraryID string) bool {
		running, err := db.SelectScalar[bool](ctx, pool, `SELECT EXISTS (SELECT 1 FROM tasks
			WHERE name = $1 AND status IN ($2, $3) AND input->>'library_id' = $4)`,
			scanner.TaskName, models.TaskStatusPending, models.TaskStatusInProgress, libraryID)
		return err != nil || running
	}
}

func describeCandidate(c linking.Candidate) string {
	s := fmt.Sprintf("#%s %s", c.Key.ID, c.Title)
	if c.Year != nil {
		s += fmt.Sprintf(" (%d)", *c.Year)
	}
	e := c.Evaluation
	s += fmt.Sprintf(" [title %.2f", e.Title)
	for _, f := range [][2]string{{"year", e.Year}, {"staff", e.Staff}, {"volumes", e.Volumes}} {
		if f[1] != "" {
			s += ", " + f[0] + " " + f[1]
		}
	}
	if e.Eligible {
		s += ", eligible"
	}
	return s + "]"
}
