package db

import (
	"context"

	"voltis/models"
)

// TaskCreate inserts a new task row.
func TaskCreate(ctx context.Context, q Querier, task *models.Task) error {
	_, err := q.Exec(ctx, `
		INSERT INTO tasks (id, created_at, updated_at, name, status, input, output, logs, user_id, library_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, task.ID, task.CreatedAt, task.UpdatedAt, task.Name, task.Status,
		task.Input, task.Output, task.Logs, task.UserID, task.LibraryID)
	return err
}
