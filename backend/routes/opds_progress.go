package routes

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"voltis/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// servedPage is what the PSE handler read along with the image. A rescan since then makes the
// write stale.
type servedPage struct {
	Pages     int
	FileMtime *time.Time
}

// recordComicPage records a PSE page fetch as reading progress, forward only, through the reading
// service: the last page finishes the comic, any other is a position. Page 0 of a multi-page comic
// is skipped, since clients fetch it as a preview.
//
// Lock order: user data advisory locks → users → app_keys → content → user_to_content. A user
// delete locks users and then cascades, and a revoke locks only the key row, so neither can
// deadlock with this. The key is re-checked because appKeyAuth ran before the image was served.
func recordComicPage(
	ctx context.Context, pool *pgxpool.Pool, userID, keyID, contentID string, page int, served servedPage,
) (recorded bool, err error) {
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := lockUserData(ctx, tx, userID, contentID); err != nil {
			return err
		}
		// FOR KEY SHARE conflicts with DELETE, so a revoke either commits first or waits for us.
		var one int
		if err := tx.QueryRow(ctx, "SELECT 1 FROM users WHERE id = $1 FOR KEY SHARE", userID).Scan(&one); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, "SELECT 1 FROM app_keys WHERE id = $1 FOR KEY SHARE", keyID).Scan(&one); err != nil {
			return err
		}

		var mtime *time.Time
		var pages int
		var current *float64
		if err := tx.QueryRow(ctx, `
			SELECT c.file_mtime, COALESCE(jsonb_array_length(c.file_data->'pages'), 0),
				CASE WHEN jsonb_typeof(utc.progress->'current_page') = 'number'
					THEN (utc.progress->>'current_page')::float8 END
			FROM content c`+utcJoin+`
			WHERE c.id = @id
		`, pgx.NamedArgs{"id": contentID, "user_id": userID}).Scan(&mtime, &pages, &current); err != nil {
			return err
		}
		sameMtime := (mtime == nil) == (served.FileMtime == nil) && (mtime == nil || mtime.Equal(*served.FileMtime))
		if !sameMtime || pages != served.Pages || page >= pages || (page == 0 && pages > 1) ||
			current != nil && *current >= float64(page) {
			return nil
		}

		req := readingRequest{Op: "finish", revision: "opds-" + keyID + ":" + strings.ToLower(rand.Text())}
		if page < pages-1 {
			req.Op = "position"
			req.Progress, _ = json.Marshal(map[string]any{
				"current_page":     page,
				"progress_percent": pagePercent(page, pages),
			})
		}
		res, err := applyReading(ctx, tx, userID, contentID, req, time.Now().UTC())
		recorded = res.Outcome != "none"
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // user, key or content gone
	}
	return recorded, err
}

// pagePercent is the share of a comic read before page, to 0.1. Only a finish reaches 100.
func pagePercent(page, pages int) float64 {
	return min(math.Round(float64(page)/float64(pages)*1000)/10, 99.9)
}
