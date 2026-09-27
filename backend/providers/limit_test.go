package providers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []fakeTimer
}

type fakeTimer struct {
	at time.Time
	f  func()
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.timers = append(c.timers, fakeTimer{c.now.Add(d), f})
}

// Advance moves time and runs the timers that came due, outside the lock since they re-arm.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []func()
	pending := c.timers[:0]
	for _, t := range c.timers {
		if t.at.After(c.now) {
			pending = append(pending, t)
		} else {
			due = append(due, t.f)
		}
	}
	c.timers = pending
	c.mu.Unlock()
	for _, f := range due {
		f()
	}
}

func newTestLimiter(perMinute, burst int) (*Limiter, *fakeClock) {
	clk := &fakeClock{now: time.Unix(1700000000, 0)}
	return newLimiter(clk, perMinute, burst), clk
}

func queued(l *Limiter) (interactive, background int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.queues[0]), len(l.queues[1])
}

func waitQueued(t *testing.T, l *Limiter, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		if i, b := queued(l); i+b == n {
			return
		}
	}
	t.Fatalf("timed out waiting for %d queued waiters", n)
}

func goWait(ctx context.Context, l *Limiter) <-chan error {
	errc := make(chan error, 1)
	go func() { errc <- l.Wait(ctx) }()
	return errc
}

func TestLimiterConfigs(t *testing.T) {
	for _, c := range []struct {
		name             string
		perMinute, burst int
		spacing          time.Duration
	}{
		{"mangabaka search", 20, 5, 3 * time.Second},
		{"mangabaka batch", 100, 20, 600 * time.Millisecond},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, clk := newTestLimiter(c.perMinute, c.burst)
			for range c.burst {
				if err := l.Wait(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				errc := goWait(context.Background(), l)
				waitQueued(t, l, 1)
				clk.Advance(c.spacing - time.Millisecond)
				waitQueued(t, l, 1)
				clk.Advance(time.Millisecond)
				if err := <-errc; err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestLimiterServesInteractiveFirst(t *testing.T) {
	l, clk := newTestLimiter(60, 1)
	_ = l.Wait(context.Background())
	background := goWait(context.Background(), l)
	waitQueued(t, l, 1)
	interactive := goWait(Interactive(context.Background()), l)
	waitQueued(t, l, 2)

	clk.Advance(time.Second)
	if err := <-interactive; err != nil {
		t.Fatal(err)
	}
	if i, b := queued(l); i != 0 || b != 1 {
		t.Fatalf("queued = %d interactive, %d background", i, b)
	}
	clk.Advance(time.Second)
	if err := <-background; err != nil {
		t.Fatal(err)
	}
}

func TestLimiterDropsCancelledWaiters(t *testing.T) {
	l, clk := newTestLimiter(60, 1)
	_ = l.Wait(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	first := goWait(ctx, l)
	waitQueued(t, l, 1)
	second := goWait(context.Background(), l)
	waitQueued(t, l, 2)

	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: err = %v", err)
	}
	waitQueued(t, l, 1)
	clk.Advance(time.Second)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
}

func TestLimiterPauseUntilDropsTheBurst(t *testing.T) {
	l, clk := newTestLimiter(60, 5)
	l.PauseUntil(clk.Now().Add(10 * time.Second))
	errc := goWait(context.Background(), l)
	waitQueued(t, l, 1)
	clk.Advance(10*time.Second - time.Millisecond)
	waitQueued(t, l, 1)
	clk.Advance(time.Millisecond)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	errc = goWait(context.Background(), l)
	waitQueued(t, l, 1)
	clk.Advance(time.Second)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}
