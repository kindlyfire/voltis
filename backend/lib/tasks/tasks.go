package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"time"

	"voltis/lib/bufchan"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskDef struct {
	Name             string
	Process          func(any, *TaskContext) error
	UnmarshalInput   func(json.RawMessage) (any, error)
	IsCompatibleWith func(self any, other RunningInfo) bool
	OnUpdate         func(task *models.Task, progress json.RawMessage)
}

type TaskHandle struct {
	task     *models.Task
	done     chan struct{}
	ctx      context.Context
	cancelFn context.CancelFunc
	mu       sync.Mutex
	result   any
	err      error
}

type TaskContext struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	task        *models.Task
	runtimeData map[string]any
	bc          *bufchan.BufChan[updateOpts]
	output      any
	resultValue any
}

func (d *TaskDef) execute(pool *pgxpool.Pool, input any, task *models.Task, handle *TaskHandle, runtimeData map[string]any, onDone func()) {
	tc := &TaskContext{
		ctx:         handle.ctx,
		pool:        pool,
		task:        task,
		runtimeData: runtimeData,
		bc: bufchan.New(mergeUpdateOpts, 200*time.Millisecond, func(opts updateOpts) error {
			return d.flushUpdate(context.Background(), pool, task, opts)
		}),
	}

	go func() {
		defer onDone()

		// Transition to InProgress
		inProgress := models.TaskStatusInProgress
		tc.bc.SendNow(updateOpts{status: &inProgress})

		processErr := d.Process(input, tc)

		if handle.ctx.Err() != nil {
			tc.bc.SendNow(updateOpts{status: new(models.TaskStatusCancelled)})
		} else if processErr != nil {
			failed := models.TaskStatusFailed
			err := tc.bc.SendNow(updateOpts{
				status: &failed,
				logs:   new(processErr.Error()),
			})
			if err != nil {
				slog.Error("[tasks] failed to send failure update", "err", err)
			}
		} else {
			completed := models.TaskStatusCompleted
			opts := updateOpts{status: &completed}
			if tc.output != nil {
				opts.output = tc.output
			}
			err := tc.bc.SendNow(opts)
			if err != nil {
				slog.Error("[tasks] failed to send completion update", "err", err)
			}
		}

		tc.bc.Close()

		fp.WithMutex(&handle.mu, func() {
			handle.result = tc.resultValue
			handle.err = processErr
		})
		close(handle.done)
	}()
}

func (d *TaskDef) flushUpdate(ctx context.Context, pool *pgxpool.Pool, task *models.Task, opts updateOpts) error {
	if opts.status != nil {
		task.Status = *opts.status
	}
	if opts.output != nil {
		data, err := json.Marshal(opts.output)
		if err != nil {
			return err
		}
		task.Output = data
	}
	if opts.logs != nil {
		if task.Logs == nil {
			task.Logs = opts.logs
		} else {
			s := *task.Logs
			if len(s) > 0 && s[len(s)-1] != '\n' {
				s += "\n"
			}
			s += *opts.logs
			task.Logs = &s
		}
	}

	task.UpdatedAt = time.Now().UTC()

	_, err := pool.Exec(ctx, `
		UPDATE tasks SET status = $1, output = $2, logs = $3, updated_at = $4
		WHERE id = $5
	`, task.Status, task.Output, task.Logs, task.UpdatedAt, task.ID)
	if err != nil {
		return err
	}

	if d.OnUpdate != nil {
		var progress json.RawMessage
		if opts.progress != nil {
			progress, _ = json.Marshal(opts.progress)
		}
		d.OnUpdate(task, progress)
	}

	return nil
}

// TaskHandle

func (h *TaskHandle) Wait() (any, error) {
	<-h.done
	return h.result, h.err
}

func (h *TaskHandle) Task() *models.Task {
	return h.task
}

// TaskContext

func (c *TaskContext) Context() context.Context { return c.ctx }
func (c *TaskContext) Pool() *pgxpool.Pool      { return c.pool }
func (c *TaskContext) Task() *models.Task       { return c.task }

func (c *TaskContext) Get(key string) any {
	if c.runtimeData == nil {
		return nil
	}
	return c.runtimeData[key]
}

func (c *TaskContext) Result(output any) {
	c.resultValue = output
	c.output = output
}

func (c *TaskContext) Progress(v any) {
	c.bc.Send(updateOpts{progress: v})
}

func (c *TaskContext) Log(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	c.bc.Send(updateOpts{logs: &s})
}

// Internal update types

type updateOpts struct {
	status   *int
	output   any
	logs     *string
	progress any
}

func mergeUpdateOpts(a, b updateOpts) updateOpts {
	if b.status != nil {
		a.status = b.status
	}
	if b.output != nil {
		a.output = b.output
	}
	if b.logs != nil {
		if a.logs == nil {
			a.logs = b.logs
		} else {
			s := *a.logs
			if len(s) > 0 && s[len(s)-1] != '\n' {
				s += "\n"
			}
			s += *b.logs
			a.logs = &s
		}
	}
	if b.progress != nil {
		a.progress = b.progress
	}
	return a
}
