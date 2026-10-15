// Package notify writes and reads the per-user notification feed.
package notify

import (
	"context"
	"time"

	"voltis/db"
	"voltis/models"
)

// unread is a SQL fragment over a notifications row n.
const unread = `n.alerted_at IS NOT NULL AND n.seen_at IS DISTINCT FROM n.alerted_at`

// Counts mirrors scanner.Counts, which this package can't import.
type Counts struct {
	Added   int `json:"added"`
	Updated int `json:"updated"`
	Removed int `json:"removed"`
}

type ScanData struct {
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	Files       int    `json:"files,omitempty"`
}

// CreateScanItems gives every admin without a scan item for the task one, and returns their ids.
func CreateScanItems(ctx context.Context, tx db.Querier, taskID string, d ScanData) ([]string, error) {
	admins, err := db.SelectScalars[string](ctx, tx, `
		SELECT u.id FROM users u
		WHERE u.permissions @> ARRAY['ADMIN']
		  AND NOT EXISTS (SELECT 1 FROM notifications n
		                  WHERE n.task_id = $1 AND n.kind = 'scan' AND n.user_id = u.id)
	`, taskID)
	if err != nil || len(admins) == 0 {
		return nil, err
	}
	ids := make([]string, len(admins))
	for i := range ids {
		ids[i] = models.MakeNotificationID()
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO notifications (id, user_id, kind, task_id, data, created_at, updated_at)
		SELECT r.id, r.user_id, 'scan', $3, $4::jsonb, $5, $5
		FROM unnest($1::text[], $2::text[]) AS r(id, user_id)
	`, ids, admins, taskID, d, time.Now().UTC())
	return admins, err
}

// RebindScanItems points the scan items of oldTaskID at newTaskID as a fresh, quiet item. It can
// match nothing; callers follow it with CreateScanItems.
func RebindScanItems(ctx context.Context, tx db.Querier, oldTaskID, newTaskID string, d ScanData) ([]string, error) {
	return db.SelectScalars[string](ctx, tx, `
		UPDATE notifications SET task_id = $2, data = $3::jsonb, alerted_at = NULL, updated_at = $4
		WHERE task_id = $1 AND kind = 'scan'
		RETURNING user_id
	`, oldTaskID, newTaskID, d, time.Now().UTC())
}

func DropScanItems(ctx context.Context, q db.Querier, taskID string) ([]string, error) {
	return db.SelectScalars[string](ctx, q,
		"DELETE FROM notifications WHERE task_id = $1 AND kind = 'scan' RETURNING user_id", taskID)
}

// CheckpointScan saves a running scan's counts. It leaves updated_at alone, so items don't reorder.
func CheckpointScan(ctx context.Context, tx db.Querier, taskID string, saved Counts, failed int) error {
	_, err := tx.Exec(ctx, `
		UPDATE notifications
		SET data = data || jsonb_build_object('saved', $2::jsonb, 'failed', $3::int)
		WHERE task_id = $1 AND kind = 'scan'
	`, taskID, saved, failed)
	return err
}

type ScanTotals struct {
	Saved              Counts
	Failed             int
	ReadFailures       int
	RemovalsSuppressed string
}

type ScanSummary struct {
	// "completed", "failed" or "cancelled".
	Outcome string
	// Nil without a live run: the counts stay as last checkpointed.
	Totals *ScanTotals
}

// FinishScan writes the final summary, and alerts unless the scan completed cleanly.
func FinishScan(ctx context.Context, tx db.Querier, taskID string, s ScanSummary) ([]string, error) {
	data := map[string]any{"outcome": s.Outcome}
	alert := s.Outcome != "completed"
	if t := s.Totals; t != nil {
		data["saved"], data["failed"], data["read_failures"] = t.Saved, t.Failed, t.ReadFailures
		if t.RemovalsSuppressed != "" {
			data["removals_suppressed"] = t.RemovalsSuppressed
		}
		alert = alert || t.Failed > 0 || t.ReadFailures > 0 || t.RemovalsSuppressed != ""
	}
	return db.SelectScalars[string](ctx, tx, `
		UPDATE notifications
		SET data = data || $2::jsonb, updated_at = $3,
		    alerted_at = CASE WHEN $4::bool THEN $3 ELSE alerted_at END
		WHERE task_id = $1 AND kind = 'scan'
		RETURNING user_id
	`, taskID, data, time.Now().UTC(), alert)
}
