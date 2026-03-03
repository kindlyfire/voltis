package bufchan

import (
	"sync"
	"time"
)

// Sort of debounced function caller that can merge call data, and has an option
// to send priority values right away.
type BufChan[T any] struct {
	mu           sync.Mutex
	next         *T
	merge        func(T, T) T
	fn           func(T) error
	debounceTime time.Duration
	signal       chan struct{}
	sendNow      chan struct{}
	done         chan struct{}
	closed       chan struct{}

	errMu sync.Mutex
	err   error
}

func NewBufChan[T any](merge func(T, T) T, debounceTime time.Duration, fn func(T) error) *BufChan[T] {
	b := &BufChan[T]{
		merge:        merge,
		debounceTime: debounceTime,
		fn:           fn,
		signal:       make(chan struct{}, 1),
		sendNow:      make(chan struct{}, 1),
		done:         make(chan struct{}),
		closed:       make(chan struct{}),
	}
	b.start()
	return b
}

func (b *BufChan[T]) Send(v T) error {
	err := b.takeErr()

	b.mu.Lock()
	if b.next == nil {
		b.next = &v
	} else {
		*b.next = b.merge(*b.next, v)
	}
	b.mu.Unlock()

	select {
	case b.signal <- struct{}{}:
	default:
	}

	return err
}

func (b *BufChan[T]) SendNow(v T) error {
	err := b.takeErr()

	b.mu.Lock()
	if b.next == nil {
		b.next = &v
	} else {
		*b.next = b.merge(*b.next, v)
	}
	b.mu.Unlock()

	select {
	case b.signal <- struct{}{}:
	default:
	}
	select {
	case b.sendNow <- struct{}{}:
	default:
	}

	return err
}

func (b *BufChan[T]) Close() error {
	close(b.done)
	<-b.closed
	return b.takeErr()
}

func (b *BufChan[T]) start() {
	go func() {
		defer close(b.closed)

		for {
			select {
			case <-b.signal:
			case <-b.done:
				b.flush()
				return
			}

			b.flush()

			timer := time.NewTimer(b.debounceTime)
		debouncing:
			for {
				select {
				case <-timer.C:
					b.mu.Lock()
					hasNext := b.next != nil
					b.mu.Unlock()
					if hasNext {
						b.flush()
						timer.Reset(b.debounceTime)
						continue
					}
					break debouncing

				case <-b.sendNow:
					if !timer.Stop() {
						<-timer.C
					}
					b.flush()
					timer.Reset(b.debounceTime)

				case <-b.done:
					timer.Stop()
					b.flush()
					return
				}
			}
		}
	}()
}

func (b *BufChan[T]) flush() {
	b.mu.Lock()
	v := b.next
	b.next = nil
	b.mu.Unlock()

	if v == nil {
		return
	}

	if err := b.fn(*v); err != nil {
		b.errMu.Lock()
		b.err = err
		b.errMu.Unlock()
	}
}

func (b *BufChan[T]) takeErr() error {
	b.errMu.Lock()
	err := b.err
	b.err = nil
	b.errMu.Unlock()
	return err
}
