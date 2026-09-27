package providers

import (
	"context"
	"slices"
	"sync"
	"time"
)

type clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func())
}

type realClock struct{}

func (realClock) Now() time.Time                      { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) { time.AfterFunc(d, f) }

// Limiter spaces requests to one API bucket. Waiters queue FIFO per priority, and interactive
// waiters are served first.
type Limiter struct {
	clock    clock
	interval time.Duration
	slack    time.Duration // burst-1 intervals
	mu       sync.Mutex
	tat      time.Time          // theoretical arrival time of the next request (GCRA)
	queues   [2][]chan struct{} // interactive, background
	due      time.Time          // the armed wake-up, if any
}

func NewLimiter(perMinute, burst int) *Limiter { return newLimiter(realClock{}, perMinute, burst) }

func newLimiter(c clock, perMinute, burst int) *Limiter {
	interval := time.Minute / time.Duration(perMinute)
	return &Limiter{clock: c, interval: interval, slack: time.Duration(burst-1) * interval}
}

type interactiveKey struct{}

// Interactive marks a context as serving an admin, whose requests go before background work.
func Interactive(ctx context.Context) context.Context {
	return context.WithValue(ctx, interactiveKey{}, true)
}

// Wait takes a token, or returns ctx's error once it is done.
func (l *Limiter) Wait(ctx context.Context) error {
	p := 1
	if ctx.Value(interactiveKey{}) != nil {
		p = 0
	}
	ready := make(chan struct{})
	l.mu.Lock()
	l.queues[p] = append(l.queues[p], ready)
	l.dispatch()
	l.mu.Unlock()

	select {
	case <-ready:
		return nil
	case <-ctx.Done():
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-ready:
		return nil
	default:
	}
	l.queues[p] = slices.DeleteFunc(l.queues[p], func(c chan struct{}) bool { return c == ready })
	return ctx.Err()
}

// PauseUntil grants nothing before t, and then no burst.
func (l *Limiter) PauseUntil(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if at := t.Add(l.slack); at.After(l.tat) {
		l.tat = at
	}
}

func (l *Limiter) backoff(d time.Duration) { l.PauseUntil(l.clock.Now().Add(d)) }

// dispatch grants tokens to queue heads, then arms a wake-up for the next one. Called with mu held.
func (l *Limiter) dispatch() {
	for {
		q := &l.queues[0]
		if len(*q) == 0 {
			q = &l.queues[1]
		}
		if len(*q) == 0 {
			return
		}
		now := l.clock.Now()
		start := l.tat
		if now.After(start) {
			start = now
		}
		if at := start.Add(-l.slack); now.Before(at) {
			l.arm(at)
			return
		}
		l.tat = start.Add(l.interval)
		close((*q)[0])
		*q = (*q)[1:]
	}
}

func (l *Limiter) arm(at time.Time) {
	if !l.due.IsZero() && !l.due.After(at) {
		return
	}
	l.due = at
	l.clock.AfterFunc(at.Sub(l.clock.Now()), func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.due.Equal(at) {
			l.due = time.Time{}
		}
		l.dispatch()
	})
}
