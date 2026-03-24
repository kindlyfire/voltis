package fp

import (
	"sync"
	"time"
)

func Map[T any, R any](in []T, fn func(T) R) []R {
	out := make([]R, len(in))
	for i, v := range in {
		out[i] = fn(v)
	}
	return out
}

func Filter[T any](in []T, fn func(T) bool) []T {
	out := make([]T, 0, len(in))
	for _, v := range in {
		if fn(v) {
			out = append(out, v)
		}
	}
	return out
}

// Remove removes the first occurrence of v from in, if it exists. It returns
// the updated slice.
func Remove[T comparable](in []T, v T) []T {
	for i, item := range in {
		if item == v {
			return append(in[:i], in[i+1:]...)
		}
	}
	return in
}

func Dedup[T comparable](in []T) []T {
	seen := make(map[T]struct{})
	out := make([]T, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; !ok {
			seen[v] = struct{}{}
			out = append(out, v)
		}
	}
	return out
}

func MapConcurrently[T any](in []T, concurrency int, fn func(T)) {
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	for i, v := range in {
		wg.Add(1)
		go func(i int, v T) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fn(v)
		}(i, v)
	}
	wg.Wait()
}

func NewTicker(ms int, fn func()) func() {
	ticker := time.NewTicker(time.Duration(ms) * time.Millisecond)
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-ticker.C:
				fn()
			case <-done:
				return
			}
		}
	}()

	return func() {
		ticker.Stop()
		close(done)
	}
}

func WithMutex(mu *sync.Mutex, fn func()) {
	mu.Lock()
	defer mu.Unlock()
	fn()
}

func DerefString(s *string) string {
	if s == nil {
		return "<nil>"
	}
	return *s
}
