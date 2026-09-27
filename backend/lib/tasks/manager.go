package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

var PublishInterval = 250 * time.Millisecond

type RunningInfo struct {
	Name  string
	Input any
}

type Manager struct {
	pool    *pgxpool.Pool
	publish func(Snapshot)
	mu      sync.Mutex
	queue   []*entry
	running []*entry
	live    map[string]*entry
	defs    map[string]*TaskDef
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func NewManager(pool *pgxpool.Pool, publish func(Snapshot)) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		pool:    pool,
		publish: publish,
		live:    map[string]*entry{},
		defs:    map[string]*TaskDef{},
		cancel:  cancel,
	}
	m.wg.Go(func() { m.publishLoop(ctx) })
	return m
}

func (m *Manager) Close() {
	m.cancel()
	m.wg.Wait()
}

func (m *Manager) Register(def *TaskDef) {
	m.defs[def.Name] = def
}

func newEntry(def *TaskDef, input any, task *models.Task) *entry {
	ctx, cancel := context.WithCancel(context.Background())
	e := &entry{
		def:    def,
		input:  input,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
		dirty:  true,
		snap: Snapshot{
			ID:        task.ID,
			Name:      task.Name,
			Status:    task.Status,
			Input:     task.Input,
			Output:    task.Output,
			CreatedAt: task.CreatedAt,
			UpdatedAt: task.UpdatedAt,
		},
	}
	if e.snap.Output == nil {
		e.snap.Output = json.RawMessage("{}")
	}
	if task.Logs != nil {
		e.logs.WriteString(*task.Logs)
		e.snap.LogLen = e.logs.Len()
	}
	return e
}

func (m *Manager) Push(def *TaskDef, input any) (*TaskHandle, error) {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, fmt.Errorf("marshal task input: %w", err)
	}

	now := time.Now().UTC()
	task := &models.Task{
		ID:        models.MakeTaskID(),
		CreatedAt: now,
		UpdatedAt: now,
		Name:      def.Name,
		Status:    models.TaskStatusPending,
		Input:     inputJSON,
		Output:    json.RawMessage("{}"),
	}
	if err := db.TaskCreate(context.Background(), m.pool, task); err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	e := newEntry(def, input, task)
	m.mu.Lock()
	m.live[e.snap.ID] = e
	m.queue = append(m.queue, e)
	m.scheduleUnlocked()
	m.mu.Unlock()

	return &TaskHandle{e: e}, nil
}

func (m *Manager) Load(ctx context.Context) error {
	_, err := m.pool.Exec(ctx, "UPDATE tasks SET status = $1, updated_at = $2 WHERE status = $3",
		models.TaskStatusCancelled, time.Now().UTC(), models.TaskStatusInProgress)
	if err != nil {
		return fmt.Errorf("cancel stale tasks: %w", err)
	}

	pending, err := db.Select[models.Task](ctx, m.pool,
		"SELECT * FROM tasks WHERE status = $1 ORDER BY created_at", models.TaskStatusPending)
	if err != nil {
		return fmt.Errorf("load pending tasks: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range pending {
		task := &pending[i]
		def, ok := m.defs[task.Name]
		if !ok {
			slog.Warn("[tasks] no registered def for pending task", "name", task.Name, "id", task.ID)
			continue
		}
		if def.UnmarshalInput == nil {
			slog.Warn("[tasks] no UnmarshalInput for pending task", "name", task.Name, "id", task.ID)
			continue
		}
		input, unmarshalErr := def.UnmarshalInput(task.Input)
		if unmarshalErr != nil {
			slog.Error("[tasks] failed to unmarshal pending task input", "name", task.Name, "id", task.ID, "err", unmarshalErr)
			continue
		}
		if input == nil {
			continue
		}
		e := newEntry(def, input, task)
		m.live[e.snap.ID] = e
		m.queue = append(m.queue, e)
	}
	m.scheduleUnlocked()

	return nil
}

func (m *Manager) Pending(name string) []Pending {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Pending{}
	for _, e := range slices.Concat(m.queue, m.running) {
		if e.def.Name == name {
			out = append(out, Pending{ID: e.snap.ID, Input: e.input})
		}
	}
	return out
}

func (m *Manager) Live() []Snapshot {
	m.mu.Lock()
	entries := slices.Collect(maps.Values(m.live))
	m.mu.Unlock()
	return fp.Map(entries, func(e *entry) Snapshot { return e.snapshot() })
}

func (m *Manager) Logs(id string) (string, bool) {
	m.mu.Lock()
	e := m.live[id]
	m.mu.Unlock()
	if e == nil {
		return "", false
	}
	return e.readLogs(), true
}

func (m *Manager) Cancel(id string) error {
	var e *entry
	queued := false

	byID := func(x *entry) bool { return x.snap.ID == id }
	m.mu.Lock()
	if i := slices.IndexFunc(m.queue, byID); i >= 0 {
		e, queued = m.queue[i], true
		m.queue = slices.Delete(m.queue, i, i+1)
	} else if i := slices.IndexFunc(m.running, byID); i >= 0 {
		e = m.running[i]
	}
	m.mu.Unlock()

	if e == nil {
		return fmt.Errorf("task not found: %s", id)
	}

	e.cancel()
	if queued {
		m.finish(e, models.TaskStatusCancelled, nil, nil, context.Canceled)
	}
	return nil
}

func (m *Manager) scheduleUnlocked() {
	var remaining []*entry
	for _, e := range m.queue {
		if m.canRunUnlocked(e) {
			m.running = append(m.running, e)
			go m.run(e)
		} else {
			remaining = append(remaining, e)
		}
	}
	m.queue = remaining
}

func (m *Manager) canRunUnlocked(e *entry) bool {
	if len(m.running) == 0 {
		return true
	}
	if e.def.IsCompatibleWith == nil {
		return false
	}
	for _, r := range m.running {
		if !e.def.IsCompatibleWith(e.input, RunningInfo{Name: r.def.Name, Input: r.input}) {
			return false
		}
	}
	return true
}

func (m *Manager) run(e *entry) {
	m.setStatus(e, models.TaskStatusInProgress)

	result, err := e.def.Process(e.input, &TaskContext{ctx: e.ctx, pool: m.pool, e: e})

	status := models.TaskStatusCompleted
	var output any
	switch {
	case e.ctx.Err() != nil:
		status = models.TaskStatusCancelled
	case err != nil:
		status = models.TaskStatusFailed
		e.appendLog(err.Error())
	default:
		output = result
	}
	m.finish(e, status, output, result, err)
}

func (m *Manager) setStatus(e *entry, status int) {
	e.mu.Lock()
	e.snap.Status = status
	e.snap.UpdatedAt = time.Now().UTC()
	e.dirty = true
	output, updated := e.snap.Output, e.snap.UpdatedAt
	e.mu.Unlock()
	_ = m.persist(context.Background(), e, status, output, updated)
}

func (m *Manager) finish(e *entry, status int, output, result any, err error) {
	var data json.RawMessage
	if output != nil {
		encoded, marshalErr := json.Marshal(output)
		if marshalErr != nil {
			err = errors.Join(err, fmt.Errorf("marshal task output: %w", marshalErr))
		} else {
			data = encoded
		}
	}

	e.mu.Lock()
	term := e.snap
	term.Status = status
	if data != nil {
		term.Output = data
	}
	term.UpdatedAt = time.Now().UTC()
	e.mu.Unlock()

	persistErr := m.persist(context.Background(), e, term.Status, term.Output, term.UpdatedAt)

	// Off the running list before publishing, so that whoever the terminal snapshot wakes no longer
	// sees the task pending; still live, so that its snapshot and logs stay readable until then.
	m.mu.Lock()
	m.running = fp.Remove(m.running, e)
	m.scheduleUnlocked()
	m.mu.Unlock()

	if persistErr != nil {
		err = errors.Join(err, fmt.Errorf("persist terminal task state: %w", persistErr))
	} else {
		e.mu.Lock()
		e.snap.Status, e.snap.Output, e.snap.UpdatedAt = term.Status, term.Output, term.UpdatedAt
		e.dirty = false
		snap := e.snapshotLocked()
		e.mu.Unlock()
		if m.publish != nil {
			m.publish(snap)
		}
	}

	m.mu.Lock()
	delete(m.live, term.ID)
	m.mu.Unlock()

	e.result, e.err = result, err
	close(e.done)
}

func (m *Manager) publishLoop(ctx context.Context) {
	ticker := time.NewTicker(PublishInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		m.mu.Lock()
		entries := slices.Collect(maps.Values(m.live))
		m.mu.Unlock()

		var dirty []Snapshot
		for _, e := range entries {
			e.mu.Lock()
			if e.dirty {
				e.dirty = false
				dirty = append(dirty, e.snapshotLocked())
			}
			e.mu.Unlock()
		}

		if m.publish == nil {
			continue
		}
		for _, s := range dirty {
			m.publish(s)
		}
	}
}

func (m *Manager) persist(ctx context.Context, e *entry, status int, output json.RawMessage, updated time.Time) error {
	e.mu.Lock()
	id, logs := e.snap.ID, e.logs.String()
	e.mu.Unlock()

	var logsPtr *string
	if logs != "" {
		logsPtr = &logs
	}

	writeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	tag, err := m.pool.Exec(writeCtx, `
		UPDATE tasks SET status = $1, output = $2, logs = $3, updated_at = $4
		WHERE id = $5
	`, status, output, logsPtr, updated, id)
	if err == nil && tag.RowsAffected() != 1 {
		err = fmt.Errorf("persist task %s: %d rows affected", id, tag.RowsAffected())
	}
	if err != nil {
		slog.Error("[tasks] failed to persist task", "id", id, "err", err)
	}
	return err
}
