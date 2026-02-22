package db

import (
	"context"
	"encoding/json"
	"time"

	"voltis/models"
)

// TaskEventBroadcaster is implemented by the WebSocket hub.
type TaskEventBroadcaster interface {
	BroadcastTaskEvent(task *models.Task, progress json.RawMessage)
}

type TaskUpdateOpts struct {
	Status   *int
	Output   any
	Logs     *string
	Progress any
}

// TaskUpdate updates a task row in the DB and broadcasts it via the hub.
func TaskUpdate(ctx context.Context, q Querier, hub TaskEventBroadcaster, task *models.Task, opts TaskUpdateOpts) error {
	if opts.Status != nil {
		task.Status = *opts.Status
	}
	if opts.Output != nil {
		data, err := json.Marshal(opts.Output)
		if err != nil {
			return err
		}
		task.Output = data
	}
	if opts.Logs != nil {
		if task.Logs == nil {
			task.Logs = opts.Logs
		} else {
			s := *task.Logs
			if len(s) > 0 && s[len(s)-1] != '\n' {
				s += "\n"
			}
			s += *opts.Logs
			task.Logs = &s
		}
	}

	task.UpdatedAt = time.Now().UTC()

	_, err := q.Exec(ctx, `
		UPDATE tasks SET status = $1, output = $2, logs = $3, updated_at = $4
		WHERE id = $5
	`, task.Status, task.Output, task.Logs, task.UpdatedAt, task.ID)
	if err != nil {
		return err
	}

	var progress json.RawMessage
	if opts.Progress != nil {
		progress, _ = json.Marshal(opts.Progress)
	}
	hub.BroadcastTaskEvent(task, progress)

	return nil
}

// TaskCreate inserts a new task row.
func TaskCreate(ctx context.Context, q Querier, task *models.Task) error {
	_, err := q.Exec(ctx, `
		INSERT INTO tasks (id, created_at, updated_at, name, status, input, output, logs, user_id, library_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, task.ID, task.CreatedAt, task.UpdatedAt, task.Name, task.Status,
		task.Input, task.Output, task.Logs, task.UserID, task.LibraryID)
	return err
}
