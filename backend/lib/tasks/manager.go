package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"voltis/db"
	"voltis/lib/fp"
	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RunningInfo struct {
	Name  string
	Input any
}

type queueEntry struct {
	def         *TaskDef
	task        *models.Task
	input       any
	runtimeData map[string]any
	handle      *TaskHandle
}

type Manager struct {
	pool    *pgxpool.Pool
	mu      sync.Mutex
	queue   []*queueEntry
	running []*queueEntry
	defs    map[string]*TaskDef
}

func NewManager(pool *pgxpool.Pool) *Manager {
	return &Manager{
		pool: pool,
		defs: map[string]*TaskDef{},
	}
}

func (m *Manager) Register(def *TaskDef) {
	m.defs[def.Name] = def
}

func (m *Manager) Push(def *TaskDef, input any, runtimeData map[string]any) (*TaskHandle, error) {
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

	ctx, cancel := context.WithCancel(context.Background())
	handle := &TaskHandle{task: task, done: make(chan struct{}), ctx: ctx, cancelFn: cancel}
	entry := &queueEntry{
		def:         def,
		task:        task,
		input:       input,
		runtimeData: runtimeData,
		handle:      handle,
	}

	fp.WithMutex(&m.mu, func() {
		m.queue = append(m.queue, entry)
		m.scheduleUnlocked()
	})

	return handle, nil
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

	fp.WithMutex(&m.mu, func() {
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
			entryCtx, entryCancel := context.WithCancel(context.Background())
			m.queue = append(m.queue, &queueEntry{
				def:    def,
				task:   task,
				input:  input,
				handle: &TaskHandle{task: task, done: make(chan struct{}), ctx: entryCtx, cancelFn: entryCancel},
			})
		}
		m.scheduleUnlocked()
	})

	return nil
}

// Pending returns the inputs of all queued and running tasks with the given name.
func (m *Manager) Pending(name string) []any {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := append(m.queue, m.running...)
	return fp.Map(
		fp.Filter(all, func(e *queueEntry) bool { return e.def.Name == name }),
		func(e *queueEntry) any { return e.input },
	)
}

func (m *Manager) Cancel(id string) error {
	var entry *queueEntry
	var queued bool

	fp.WithMutex(&m.mu, func() {
		for _, e := range m.queue {
			if e.task.ID == id {
				entry = e
				queued = true
				m.queue = fp.Remove(m.queue, e)
				return
			}
		}
		for _, e := range m.running {
			if e.task.ID == id {
				entry = e
				return
			}
		}
	})

	if entry == nil {
		return fmt.Errorf("task not found: %s", id)
	}

	entry.handle.cancelFn()

	if queued {
		entry.task.Status = models.TaskStatusCancelled
		entry.task.UpdatedAt = time.Now().UTC()
		_, _ = m.pool.Exec(context.Background(),
			"UPDATE tasks SET status = $1, updated_at = $2 WHERE id = $3",
			entry.task.Status, entry.task.UpdatedAt, entry.task.ID)

		fp.WithMutex(&entry.handle.mu, func() {
			entry.handle.err = context.Canceled
		})
		close(entry.handle.done)
	}

	return nil
}

func (m *Manager) scheduleUnlocked() {
	var remaining []*queueEntry
	for _, e := range m.queue {
		if m.canRunUnlocked(e) {
			m.running = append(m.running, e)
			go e.def.execute(m.pool, e.input, e.task, e.handle, e.runtimeData, func() {
				m.taskDone(e)
			})
		} else {
			remaining = append(remaining, e)
		}
	}
	m.queue = remaining
}

func (m *Manager) taskDone(e *queueEntry) {
	fp.WithMutex(&m.mu, func() {
		m.running = fp.Remove(m.running, e)
		m.scheduleUnlocked()
	})
}

func (m *Manager) canRunUnlocked(e *queueEntry) bool {
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
