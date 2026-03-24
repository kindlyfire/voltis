package bufchan

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type collector struct {
	mu     sync.Mutex
	values []int
}

func (c *collector) collect(v int) error {
	c.mu.Lock()
	c.values = append(c.values, v)
	c.mu.Unlock()
	return nil
}

func (c *collector) get() []int {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]int, len(c.values))
	copy(out, c.values)
	return out
}

func (c *collector) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.values)
}

func mergeInts(a, b int) int { return a + b }

func TestSingleSendImmediate(t *testing.T) {
	c := &collector{}
	bc := New(mergeInts, 100*time.Millisecond, c.collect)
	defer bc.Close()

	bc.Send(42)
	time.Sleep(20 * time.Millisecond)

	vals := c.get()
	if len(vals) != 1 || vals[0] != 42 {
		t.Fatalf("expected [42], got %v", vals)
	}
}

func TestRapidSendsMerge(t *testing.T) {
	c := &collector{}
	bc := New(mergeInts, 100*time.Millisecond, c.collect)
	defer bc.Close()

	bc.Send(1)
	time.Sleep(20 * time.Millisecond) // let first flush happen

	bc.Send(2)
	bc.Send(3)

	// Should not have flushed yet (debouncing).
	time.Sleep(20 * time.Millisecond)
	if c.len() != 1 {
		t.Fatalf("expected 1 flush during debounce, got %d", c.len())
	}

	// Wait for debounce to fire.
	time.Sleep(120 * time.Millisecond)

	vals := c.get()
	if len(vals) != 2 || vals[1] != 5 {
		t.Fatalf("expected [1, 5], got %v", vals)
	}
}

func TestSendNowInterruptsDebounce(t *testing.T) {
	c := &collector{}
	bc := New(mergeInts, 500*time.Millisecond, c.collect)
	defer bc.Close()

	bc.Send(1)
	time.Sleep(20 * time.Millisecond)

	bc.SendNow(10)
	time.Sleep(20 * time.Millisecond)

	vals := c.get()
	if len(vals) != 2 || vals[1] != 10 {
		t.Fatalf("expected [1, 10], got %v", vals)
	}
}

func TestIdleAfterDebounce(t *testing.T) {
	c := &collector{}
	bc := New(mergeInts, 50*time.Millisecond, c.collect)
	defer bc.Close()

	bc.Send(1)
	time.Sleep(20 * time.Millisecond)

	// Wait for debounce to fully elapse.
	time.Sleep(80 * time.Millisecond)

	bc.Send(7)
	time.Sleep(20 * time.Millisecond)

	vals := c.get()
	if len(vals) != 2 || vals[1] != 7 {
		t.Fatalf("expected [1, 7], got %v", vals)
	}
}

func TestCloseFlushes(t *testing.T) {
	c := &collector{}
	bc := New(mergeInts, 500*time.Millisecond, c.collect)

	bc.Send(1)
	time.Sleep(20 * time.Millisecond)

	// Queue during debounce, then close immediately.
	bc.Send(99)
	bc.Close()

	vals := c.get()
	if len(vals) != 2 || vals[1] != 99 {
		t.Fatalf("expected [1, 99], got %v", vals)
	}
}

func TestErrorPropagation(t *testing.T) {
	errBoom := errors.New("boom")
	calls := 0

	bc := New(mergeInts, 50*time.Millisecond, func(v int) error {
		calls++
		if calls == 1 {
			return errBoom
		}
		return nil
	})
	defer bc.Close()

	bc.Send(1)
	time.Sleep(20 * time.Millisecond)

	// Wait for debounce so goroutine is idle again.
	time.Sleep(80 * time.Millisecond)

	// Next Send should return the error from the first flush.
	err := bc.Send(2)
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom, got %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	// Subsequent Send should be clear.
	err = bc.Send(3)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestCloseReturnsError(t *testing.T) {
	errBoom := errors.New("boom")

	bc := New(mergeInts, 500*time.Millisecond, func(v int) error {
		return errBoom
	})

	bc.Send(1)
	time.Sleep(20 * time.Millisecond)

	err := bc.Close()
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected errBoom from Close, got %v", err)
	}
}
