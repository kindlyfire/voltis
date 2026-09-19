package tasks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Snapshot struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Status    int             `json:"status"`
	Input     json.RawMessage `json:"input"`
	Output    json.RawMessage `json:"output"`
	Progress  json.RawMessage `json:"progress" db:"-"`
	LogLen    int             `json:"log_len"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type TaskDef struct {
	Name             string
	Process          func(any, *TaskContext) (any, error)
	UnmarshalInput   func(json.RawMessage) (any, error)
	IsCompatibleWith func(self any, other RunningInfo) bool
}

type Pending struct {
	ID    string
	Input any
}

type entry struct {
	def    *TaskDef
	input  any
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
	result any
	err    error
	mu     sync.Mutex
	snap   Snapshot
	prog   any
	logs   strings.Builder
	dirty  bool
}

type TaskHandle struct{ e *entry }

type TaskContext struct {
	ctx  context.Context
	pool *pgxpool.Pool
	e    *entry
}

func (e *entry) snapshotLocked() Snapshot {
	s := e.snap
	s.Input = bytes.Clone(e.snap.Input)
	s.Output = bytes.Clone(e.snap.Output)
	if e.prog != nil {
		data, err := json.Marshal(e.prog)
		if err != nil {
			slog.Error("[tasks] failed to marshal progress", "id", s.ID, "err", err)
		} else {
			s.Progress = data
		}
	}
	return s
}

func (e *entry) snapshot() Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.snapshotLocked()
}

func (e *entry) setProgress(v any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.prog = v
	e.snap.UpdatedAt = time.Now().UTC()
	e.dirty = true
}

func (e *entry) appendLog(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	cur := e.logs.String()
	if len(cur) > 0 && cur[len(cur)-1] != '\n' {
		e.logs.WriteString("\n")
	}
	e.logs.WriteString(s)
	e.snap.LogLen = e.logs.Len()
	e.snap.UpdatedAt = time.Now().UTC()
	e.dirty = true
}

func (e *entry) readLogs() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.logs.String()
}

func (h *TaskHandle) ID() string { return h.e.snap.ID }

func (h *TaskHandle) Wait() (any, error) {
	<-h.e.done
	return h.e.result, h.e.err
}

func (c *TaskContext) Context() context.Context { return c.ctx }
func (c *TaskContext) Pool() *pgxpool.Pool      { return c.pool }
func (c *TaskContext) ID() string               { return c.e.snap.ID }
func (c *TaskContext) Progress(v any)           { c.e.setProgress(v) }

func (c *TaskContext) Log(format string, args ...any) {
	c.e.appendLog(fmt.Sprintf(format, args...))
}
