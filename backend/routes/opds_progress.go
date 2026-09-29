package routes

import (
	"context"
	"errors"
	"math"
	"time"

	"voltis/db"
	"voltis/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// servedPage is what the PSE handler read along with the image. A rescan since then makes the
// write stale.
type servedPage struct {
	Pages     int
	FileMtime *time.Time
}

// recordComicPage records a PSE page fetch as reading progress, forward only. Page 0 of a
// multi-page comic is skipped, since clients fetch it as a preview.
//
// The status and percent rule mirrors the web reader's (createComicState.ts updateProgress).
//
// Lock order: metadata advisory lock → users → app_keys → content → user_to_content. A user
// delete locks users and then cascades, and a revoke locks only the key row, so neither can
// deadlock with this. The key is re-checked because appKeyAuth ran before the image was served.
func recordComicPage(
	ctx context.Context, pool *pgxpool.Pool, userID, keyID, contentID string, page int, served servedPage,
) (recorded bool, err error) {
	err = db.WithTx(ctx, pool, func(tx pgx.Tx) error {
		if err := lockContentLibraries(ctx, tx, contentID); err != nil {
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

		var libraryID, uri string
		var mtime *time.Time
		var pages int
		if err := tx.QueryRow(ctx, `
			SELECT library_id, uri, file_mtime, COALESCE(jsonb_array_length(file_data->'pages'), 0)
			FROM content WHERE id = $1
		`, contentID).Scan(&libraryID, &uri, &mtime, &pages); err != nil {
			return err
		}
		sameMtime := (mtime == nil) == (served.FileMtime == nil) && (mtime == nil || mtime.Equal(*served.FileMtime))
		if !sameMtime || pages != served.Pages || page >= pages || (page == 0 && pages > 1) {
			return nil
		}

		status := "reading"
		if page == pages-1 {
			status = "completed"
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO user_to_content (id, user_id, library_id, uri, status, status_updated_at, progress,
				progress_updated_at)
			VALUES (@id, @user_id, @library_id, @uri, @status, @now, @progress, @now)
			ON CONFLICT (user_id, library_id, uri) DO UPDATE SET
				progress = user_to_content.progress || EXCLUDED.progress,
				progress_updated_at = EXCLUDED.progress_updated_at,
				status = CASE WHEN user_to_content.status IS NULL OR user_to_content.status = 'reading'
					THEN EXCLUDED.status ELSE user_to_content.status END,
				status_updated_at = CASE WHEN user_to_content.status IS NULL OR user_to_content.status = 'reading'
					THEN EXCLUDED.status_updated_at ELSE user_to_content.status_updated_at END
			WHERE CASE WHEN jsonb_typeof(user_to_content.progress->'current_page') = 'number'
				THEN (user_to_content.progress->>'current_page')::numeric ELSE -1 END < @page
		`, pgx.NamedArgs{
			"id":         models.MakeUserToContentID(),
			"user_id":    userID,
			"library_id": libraryID,
			"uri":        uri,
			"status":     status,
			"now":        time.Now().UTC(),
			"progress": map[string]any{
				"current_page":     page,
				"progress_percent": math.Round(float64(page+1)/float64(pages)*1000) / 10,
			},
			"page": page,
		})
		recorded = tag.RowsAffected() == 1
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil // user, key or content gone
	}
	return recorded, err
}
