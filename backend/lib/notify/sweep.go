package notify

import (
	"context"
	"log/slog"
	"time"

	"voltis/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Seen is the version of an item a client saw.
type Seen struct {
	ID        string    `json:"id"         validate:"required"`
	AlertedAt time.Time `json:"alerted_at" validate:"required"`
}

// MarkSeen marks items seen, those only whose alerted_at is still the one the client saw. Rows are
// locked in the order a scan flush locks them (scan rows, then grouped rows), so the two can't
// deadlock.
func MarkSeen(ctx context.Context, q db.Querier, userID string, items []Seen) error {
	ids, ats := make([]string, len(items)), make([]time.Time, len(items))
	for i, it := range items {
		ids[i], ats[i] = it.ID, it.AlertedAt
	}
	_, err := q.Exec(ctx, `
		WITH r AS (SELECT * FROM unnest($2::text[], $3::timestamptz[]) r(id, at)),
		l AS (SELECT n.id, r.at FROM notifications n JOIN r ON r.id = n.id
		       WHERE n.user_id = $1 AND n.alerted_at = r.at
		       ORDER BY n.group_key NULLS FIRST, n.id FOR UPDATE OF n)
		UPDATE notifications n SET seen_at = l.at FROM l WHERE n.id = l.id
	`, userID, ids, ats)
	return err
}

func Dismiss(ctx context.Context, q db.Querier, userID, id string) (bool, error) {
	tag, err := q.Exec(ctx, "DELETE FROM notifications WHERE id = $1 AND user_id = $2", id, userID)
	return tag.RowsAffected() > 0, err
}

// Clear locks the user's rows in MarkSeen's order before deleting.
func Clear(ctx context.Context, q db.Querier, userID string) error {
	_, err := q.Exec(ctx, `
		WITH l AS (SELECT id FROM notifications WHERE user_id = $1
		            ORDER BY group_key NULLS FIRST, id FOR UPDATE)
		DELETE FROM notifications n USING l WHERE n.id = l.id
	`, userID)
	return err
}

// Sweep deletes items seen and untouched for 30 days. Unread items and active scans stay.
func Sweep(ctx context.Context, q db.Querier) (int64, error) {
	tag, err := q.Exec(ctx, `
		DELETE FROM notifications n
		WHERE n.updated_at < now() - interval '30 days' AND NOT (`+unread+`)
		  AND NOT EXISTS (SELECT 1 FROM tasks t WHERE t.id = n.task_id AND t.status IN (0, 1))
	`)
	return tag.RowsAffected(), err
}

// RunSweep sweeps at start and then hourly until ctx ends.
func RunSweep(ctx context.Context, pool *pgxpool.Pool) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		if _, err := Sweep(ctx, pool); err != nil && ctx.Err() == nil {
			slog.Error("[notify] sweep failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
