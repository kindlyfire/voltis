package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"voltis/db"
	"voltis/lib/bufchan"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DefineOpts[I, O any] struct {
	Name     string
	Process  func(I, *TaskContext[O]) error
	OnUpdate func(task *models.Task, progress json.RawMessage)
}

type TaskDef[I, O any] struct {
	name    string
	process func(I, *TaskContext[O]) error

	OnUpdate func(task *models.Task, progress json.RawMessage)

	mu      sync.Mutex
	running []*RunningEntry[I, O]
}

type RunningEntry[I, O any] struct {
	Input  I
	Handle *TaskHandle[O]
}

type TaskHandle[O any] struct {
	task   *models.Task
	done   chan struct{}
	mu     sync.Mutex
	result O
	err    error
}

type TaskContext[O any] struct {
	ctx         context.Context
	pool        *pgxpool.Pool
	task        *models.Task
	runtimeData map[string]any
	bc          *bufchan.BufChan[updateOpts]
	output      any
	resultValue O
}

func Define[I, O any](opts DefineOpts[I, O]) *TaskDef[I, O] {
	return &TaskDef[I, O]{
		name:     opts.Name,
		process:  opts.Process,
		OnUpdate: opts.OnUpdate,
	}
}

func (d *TaskDef[I, O]) Start(ctx context.Context, pool *pgxpool.Pool, input I, runtimeData map[string]any) (*TaskHandle[O], error) {
	now := time.Now().UTC()
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal task input: %w", err)
	}

	task := &models.Task{
		ID:        models.MakeTaskID(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      d.name,
		Status:    models.TaskStatusInProgress,
		Input:     inputJSON,
		Output:    json.RawMessage("{}"),
	}
	if err := db.TaskCreate(ctx, pool, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	handle := &TaskHandle[O]{
		task: task,
		done: make(chan struct{}),
	}

	entry := &RunningEntry[I, O]{Input: input, Handle: handle}
	fp.WithMutex(&d.mu, func() {
		d.running = append(d.running, entry)
	})

	tc := &TaskContext[O]{
		ctx:         ctx,
		pool:        pool,
		task:        task,
		runtimeData: runtimeData,
		bc: bufchan.New(mergeUpdateOpts, 200*time.Millisecond, func(opts updateOpts) error {
			return d.flushUpdate(ctx, pool, task, opts)
		}),
	}

	go func() {
		defer fp.WithMutex(&d.mu, func() {
			d.running = fp.Remove(d.running, entry)
		})

		processErr := d.process(input, tc)

		if processErr != nil {
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

	return handle, nil
}

func (d *TaskDef[I, O]) Running() []RunningEntry[I, O] {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]RunningEntry[I, O], len(d.running))
	for i, e := range d.running {
		result[i] = *e
	}
	return result
}

func (d *TaskDef[I, O]) flushUpdate(ctx context.Context, pool *pgxpool.Pool, task *models.Task, opts updateOpts) error {
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

func (h *TaskHandle[O]) Wait() (O, error) {
	<-h.done
	return h.result, h.err
}

func (h *TaskHandle[O]) Task() *models.Task {
	return h.task
}

// TaskContext

func (c *TaskContext[O]) Context() context.Context { return c.ctx }
func (c *TaskContext[O]) Pool() *pgxpool.Pool      { return c.pool }
func (c *TaskContext[O]) Task() *models.Task       { return c.task }

func (c *TaskContext[O]) Get(key string) any {
	if c.runtimeData == nil {
		return nil
	}
	return c.runtimeData[key]
}

func (c *TaskContext[O]) Result(output O) {
	c.resultValue = output
	c.output = output
}

func (c *TaskContext[O]) Progress(v any) {
	c.bc.Send(updateOpts{progress: v})
}

func (c *TaskContext[O]) Log(format string, args ...any) {
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
