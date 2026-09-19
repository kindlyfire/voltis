package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"voltis/db"
	"voltis/models"
)

type recorder struct {
	mu   sync.Mutex
	seen []Snapshot
}

func (r *recorder) publish(s Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, s)
}

func (r *recorder) forID(id string) []Snapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Snapshot
	for _, s := range r.seen {
		if s.ID == id {
			out = append(out, s)
		}
	}
	return out
}

func exclusiveDef(release chan struct{}) *TaskDef {
	return &TaskDef{
		Name: "exclusive",
		Process: func(_ any, _ *TaskContext) (any, error) {
			<-release
			return nil, nil
		},
		IsCompatibleWith: func(any, RunningInfo) bool { return false },
	}
}

func compatibleDef(name string, process func(any, *TaskContext) (any, error)) *TaskDef {
	return &TaskDef{
		Name:             name,
		Process:          process,
		IsCompatibleWith: func(any, RunningInfo) bool { return true },
	}
}

func TestSnapshotsFlowWhilePersistenceIsStalled(t *testing.T) {
	rec := &recorder{}
	m, pool := newManager(t, rec.publish)
	ctx := context.Background()

	release := make(chan struct{})
	stallDef := compatibleDef("stall", func(_ any, _ *TaskContext) (any, error) {
		<-release
		return map[string]int{"ok": 1}, nil
	})

	stop := make(chan struct{})
	tickDef := compatibleDef("tick", func(_ any, tc *TaskContext) (any, error) {
		for i := 0; ; i++ {
			tc.Progress(map[string]int{"n": i})
			tc.Log("tick %d", i)
			select {
			case <-stop:
				return nil, nil
			case <-time.After(10 * time.Millisecond):
			}
		}
	})

	stalled := pushTask(t, m, stallDef, map[string]string{})
	waitUntil(t, "the stalled task to be running", func() bool {
		return dbStatus(t, pool, stalled.ID()) == models.TaskStatusInProgress
	})

	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := tx.Exec(ctx, "SELECT id FROM tasks WHERE id = $1 FOR UPDATE", stalled.ID()); err != nil {
		t.Fatalf("lock row: %v", err)
	}

	close(release)

	ticking := pushTask(t, m, tickDef, map[string]string{})

	waitUntil(t, "snapshots of the second task to be published", func() bool {
		return len(rec.forID(ticking.ID())) >= 3
	})

	published := rec.forID(ticking.ID())
	tick := func(s Snapshot) int {
		var p struct {
			N int `json:"n"`
		}
		if err := json.Unmarshal(s.Progress, &p); err != nil {
			t.Fatalf("published progress %q: %v", s.Progress, err)
		}
		return p.N
	}
	prevTick, prevLen := -1, -1
	for _, s := range published {
		n := tick(s)
		if n < prevTick {
			t.Fatalf("published tick went backwards: %d after %d", n, prevTick)
		}
		if s.LogLen < prevLen {
			t.Fatalf("published log_len went backwards: %d after %d", s.LogLen, prevLen)
		}
		prevTick, prevLen = n, s.LogLen
	}
	first := published[0]
	if firstTick := tick(first); prevTick <= firstTick || prevLen <= first.LogLen {
		t.Fatalf("published content did not advance: ticks %d -> %d, log_len %d -> %d",
			firstTick, prevTick, first.LogLen, prevLen)
	}
	if logs, _ := m.Logs(ticking.ID()); len(logs) < prevLen {
		t.Fatalf("published log_len %d exceeds the live logs (%d bytes)", prevLen, len(logs))
	}

	if got := dbStatus(t, pool, stalled.ID()); got != models.TaskStatusInProgress {
		t.Fatalf("stalled task persisted status %d while its write was blocked", got)
	}
	for _, s := range rec.forID(stalled.ID()) {
		if s.Status >= models.TaskStatusCompleted {
			t.Fatal("stalled task published a terminal snapshot before its write landed")
		}
	}

	live := m.Live()
	if len(live) != 2 {
		t.Fatalf("Live() returned %d entries while persistence was stalled, want 2", len(live))
	}

	close(stop)
	waitTask(t, ticking)

	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	waitTask(t, stalled)
	if got := dbStatus(t, pool, stalled.ID()); got != models.TaskStatusCompleted {
		t.Fatalf("stalled task status = %d after release, want completed", got)
	}
}

func TestTerminalOrderIsPersistPublishRetire(t *testing.T) {
	pool := newTestPool(t)
	var mp atomic.Pointer[Manager]

	type observation struct {
		status int
		live   bool
	}
	var mu sync.Mutex
	var seen []observation

	m := NewManager(pool, func(s Snapshot) {
		if s.Status != models.TaskStatusCompleted {
			return
		}
		status := dbStatus(t, pool, s.ID)
		live := liveHas(mp.Load(), s.ID)
		mu.Lock()
		seen = append(seen, observation{status: status, live: live})
		mu.Unlock()
	})
	mp.Store(m)
	t.Cleanup(m.Close)

	def := compatibleDef("terminal", func(_ any, _ *TaskContext) (any, error) {
		return map[string]int{"added": 2}, nil
	})
	handle := pushTask(t, m, def, map[string]string{})
	waitTask(t, handle)

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 {
		t.Fatalf("terminal snapshot published %d times, want 1", len(seen))
	}
	if seen[0].status != models.TaskStatusCompleted {
		t.Fatalf("db status at publish time = %d, want completed: persist must run first", seen[0].status)
	}
	if !seen[0].live {
		t.Fatal("task was retired before its terminal snapshot was published")
	}
	if live := m.Live(); len(live) != 0 {
		t.Fatalf("Live() returned %d entries after the task finished, want 0", len(live))
	}
	if _, ok := m.Logs(handle.ID()); ok {
		t.Fatal("logs still served from the live map after the task finished")
	}
}

func TestQueuedCancelTakesTheTerminalPath(t *testing.T) {
	rec := &recorder{}
	m, pool := newManager(t, rec.publish)

	release := make(chan struct{})
	def := exclusiveDef(release)

	running := pushTask(t, m, def, map[string]string{})
	queued := pushTask(t, m, def, map[string]string{})

	if err := m.Cancel(queued.ID()); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := queued.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued task err = %v, want context.Canceled", err)
	}
	if got := dbStatus(t, pool, queued.ID()); got != models.TaskStatusCancelled {
		t.Fatalf("queued task status = %d, want cancelled", got)
	}

	if !slices.ContainsFunc(rec.forID(queued.ID()), func(s Snapshot) bool {
		return s.Status == models.TaskStatusCancelled
	}) {
		t.Fatal("cancelling a queued task published no terminal snapshot")
	}
	if liveHas(m, queued.ID()) {
		t.Fatal("cancelled task stayed in the live map")
	}
	if p := m.Pending("exclusive"); len(p) != 1 || p[0].ID != running.ID() {
		t.Fatalf("Pending = %v, want only the running task", p)
	}

	close(release)
	waitTask(t, running)
}

func TestPendingCopiesTheSliceAndAliasesInput(t *testing.T) {
	m, _ := newManager(t, nil)

	release := make(chan struct{})
	def := exclusiveDef(release)

	running := pushTask(t, m, def, map[string]string{"n": "1"})
	queued := pushTask(t, m, def, map[string]string{"n": "2"})

	first := m.Pending("exclusive")
	if len(first) != 2 {
		t.Fatalf("Pending returned %d entries, want 2", len(first))
	}
	for i := range first {
		first[i] = Pending{ID: "clobbered"}
	}
	first = append(first, Pending{ID: "extra"})
	_ = first

	second := m.Pending("exclusive")
	if len(second) != 2 {
		t.Fatalf("Pending returned %d entries after the caller mutated its copy, want 2", len(second))
	}
	ids := map[string]bool{second[0].ID: true, second[1].ID: true}
	if !ids[running.ID()] || !ids[queued.ID()] {
		t.Fatalf("Pending = %v, want the pushed task IDs", second)
	}

	for _, p := range second {
		input, ok := p.Input.(map[string]string)
		if !ok {
			t.Fatalf("Input = %T, want the pushed value", p.Input)
		}
		input["mutated"] = "yes"
	}
	for _, p := range m.Pending("exclusive") {
		if p.Input.(map[string]string)["mutated"] != "yes" {
			t.Fatal("Pending hands out the pushed Input itself; callers must treat it as read-only")
		}
	}

	close(release)
	waitTask(t, running)
	waitTask(t, queued)
}

func TestLogNewlineRuleAndByteLength(t *testing.T) {
	m, _ := newManager(t, nil)

	logged := make(chan struct{})
	release := make(chan struct{})
	def := compatibleDef("logs", func(_ any, tc *TaskContext) (any, error) {
		tc.Log("first")
		tc.Log("second")
		tc.Log("third\n")
		tc.Log("fourth")
		tc.Log("é")
		close(logged)
		<-release
		return nil, nil
	})

	handle := pushTask(t, m, def, map[string]string{})
	<-logged

	want := "first\nsecond\nthird\nfourth\né"
	waitUntil(t, "logs to be written", func() bool {
		logs, _ := m.Logs(handle.ID())
		return logs == want
	})

	for _, s := range m.Live() {
		if s.ID == handle.ID() && s.LogLen != len(want) {
			t.Fatalf("log_len = %d, want %d UTF-8 bytes", s.LogLen, len(want))
		}
	}

	close(release)
	waitTask(t, handle)
}

func TestSnapshotBuffersAreNotShared(t *testing.T) {
	rec := &recorder{}
	m, _ := newManager(t, rec.publish)

	running := make(chan struct{})
	release := make(chan struct{})
	def := compatibleDef("buffers", func(_ any, tc *TaskContext) (any, error) {
		close(running)
		<-release
		return map[string]string{"out": "kept"}, nil
	})

	handle := pushTask(t, m, def, map[string]string{"library_id": "l_1"})
	<-running

	live := m.Live()
	if len(live) != 1 {
		t.Fatalf("Live() returned %d entries, want 1", len(live))
	}
	first := live[0]
	wantInput := string(first.Input)
	for i := range first.Input {
		first.Input[i] = 'x'
	}
	for i := range first.Output {
		first.Output[i] = 'x'
	}

	second := m.Live()[0]
	if string(second.Input) != wantInput {
		t.Fatalf("Input = %s after a caller mutated an earlier snapshot, want %s", second.Input, wantInput)
	}
	if string(second.Output) != "{}" {
		t.Fatalf("Output = %s after a caller mutated an earlier snapshot, want {}", second.Output)
	}

	close(release)
	waitTask(t, handle)

	published := rec.forID(handle.ID())
	terminal := published[len(published)-1]
	if string(terminal.Output) != `{"out":"kept"}` {
		t.Fatalf("published output = %s, want the task result", terminal.Output)
	}
	for _, s := range published {
		if len(s.Input) > 0 && string(s.Input) != wantInput {
			t.Fatalf("published input = %s, want %s", s.Input, wantInput)
		}
	}
}

func TestLoadCancelsRunningRowsAndRequeuesPending(t *testing.T) {
	m, pool := newManager(t, nil)
	ctx := context.Background()

	insert := func(name string, status int, input string) string {
		t.Helper()
		id := models.MakeTaskID()
		now := time.Now().UTC()
		task := &models.Task{
			ID: id, CreatedAt: now, UpdatedAt: now, Name: name, Status: status,
			Input: json.RawMessage(input), Output: json.RawMessage("{}"),
		}
		if err := db.TaskCreate(ctx, pool, task); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
		return id
	}

	stale := insert("restartable", models.TaskStatusInProgress, `{"n":0}`)
	resumed := insert("restartable", models.TaskStatusPending, `{"n":1}`)
	unknown := insert("not_registered", models.TaskStatusPending, `{"n":2}`)
	unparsable := insert("restartable", models.TaskStatusPending, `"nope"`)
	skipped := insert("skippable", models.TaskStatusPending, `{"n":3}`)

	ran := make(chan int, 4)
	m.Register(&TaskDef{
		Name: "restartable",
		Process: func(input any, _ *TaskContext) (any, error) {
			ran <- input.(map[string]int)["n"]
			return nil, nil
		},
		UnmarshalInput: func(data json.RawMessage) (any, error) {
			var v map[string]int
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, err
			}
			return v, nil
		},
		IsCompatibleWith: func(any, RunningInfo) bool { return true },
	})
	m.Register(&TaskDef{
		Name:           "skippable",
		Process:        func(any, *TaskContext) (any, error) { return nil, nil },
		UnmarshalInput: func(json.RawMessage) (any, error) { return nil, nil },
	})

	if err := m.Load(ctx); err != nil {
		t.Fatalf("load: %v", err)
	}

	if got := dbStatus(t, pool, stale); got != models.TaskStatusCancelled {
		t.Fatalf("stale running task status = %d, want cancelled", got)
	}

	select {
	case n := <-ran:
		if n != 1 {
			t.Fatalf("restarted task input n = %d, want 1", n)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("pending task was not restarted by Load")
	}

	waitUntil(t, "the restarted task to finish", func() bool {
		return dbStatus(t, pool, resumed) == models.TaskStatusCompleted
	})

	select {
	case n := <-ran:
		t.Fatalf("a task that should not restart ran with n = %d", n)
	case <-time.After(200 * time.Millisecond):
	}

	for _, id := range []string{unknown, unparsable, skipped} {
		if got := dbStatus(t, pool, id); got != models.TaskStatusPending {
			t.Fatalf("skipped task %s status = %d, want it left pending", id, got)
		}
		if liveHas(m, id) {
			t.Fatalf("skipped task %s entered the live map", id)
		}
	}
	if _, ok := m.Logs(stale); ok {
		t.Fatal("a stale running row was adopted into the live map")
	}
}
