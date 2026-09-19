package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"voltis/models"

	"github.com/jackc/pgx/v5/pgxpool"
)

func unreachablePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://voltis:voltis@127.0.0.1:1/voltis?connect_timeout=1")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestFlushUpdateReturnsDeltaWhenWriteFails(t *testing.T) {
	pool := unreachablePool(t)
	d := &TaskDef{}
	task := &models.Task{ID: "task_1"}

	for _, tc := range []struct {
		chunk string
		delta string
		logs  string
	}{
		{"first", "first", "first"},
		{"second", "\nsecond", "first\nsecond"},
		{"third\n", "\nthird\n", "first\nsecond\nthird\n"},
		{"fourth", "fourth", "first\nsecond\nthird\nfourth"},
	} {
		delta, err := d.flushUpdate(context.Background(), pool, task, updateOpts{logs: &tc.chunk})
		if err == nil {
			t.Fatal("expected the write to an unreachable database to fail")
		}
		if delta != tc.delta {
			t.Fatalf("delta = %q, want %q", delta, tc.delta)
		}
		if task.Logs == nil || *task.Logs != tc.logs {
			t.Fatalf("logs = %v, want %q", task.Logs, tc.logs)
		}
	}
}

func TestFlushUpdateReturnsDeltaWhenOutputMarshalFails(t *testing.T) {
	pool := unreachablePool(t)
	d := &TaskDef{}
	task := &models.Task{ID: "task_1"}
	chunk := "chunk"

	delta, err := d.flushUpdate(context.Background(), pool, task, updateOpts{logs: &chunk, output: func() {}})
	if err == nil {
		t.Fatal("expected marshalling a func to fail")
	}
	if delta != "chunk" {
		t.Fatalf("delta = %q, want %q", delta, "chunk")
	}
}

func TestUpdaterCarriesDeltaOverFailedFlush(t *testing.T) {
	var got []*string
	d := &TaskDef{OnUpdate: func(_ *models.Task, _ json.RawMessage, logDelta *string) {
		got = append(got, logDelta)
	}}

	boom := errors.New("boom")
	rounds := []struct {
		delta string
		err   error
	}{{"a", boom}, {"\nb", boom}, {"\nc", nil}, {"", nil}}
	i := 0
	update := d.updater(&models.Task{ID: "task_1"}, func(updateOpts) (string, error) {
		r := rounds[i]
		i++
		return r.delta, r.err
	})

	for n := range rounds {
		err := update(updateOpts{})
		if want := rounds[n].err; !errors.Is(err, want) {
			t.Fatalf("round %d: err = %v, want %v", n, err, want)
		}
	}

	if len(got) != 2 {
		t.Fatalf("OnUpdate called %d times, want 2", len(got))
	}
	if got[0] == nil {
		t.Fatalf("first delta = nil, want %q", "a\nb\nc")
	}
	if *got[0] != "a\nb\nc" {
		t.Fatalf("first delta = %q, want %q", *got[0], "a\nb\nc")
	}
	if got[1] != nil {
		t.Fatalf("second delta = %q, want nil", *got[1])
	}
}
