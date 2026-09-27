package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// serve answers requests in turn with the given statuses, then with a JSON body.
func serve(t *testing.T, statuses ...int) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(hits.Add(1)) - 1
		if n < len(statuses) {
			if statuses[n] == http.StatusTooManyRequests {
				w.Header().Set("Retry-After", "30")
			}
			w.WriteHeader(statuses[n])
			return
		}
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestGetJSONRetries(t *testing.T) {
	for _, c := range []struct {
		name     string
		statuses []int
		waits    []time.Duration // before each retry is granted a token
	}{
		{"server errors back off", []int{503, 500}, []time.Duration{retryDelay, 2 * retryDelay}},
		{"429 waits for Retry-After", []int{429}, []time.Duration{30 * time.Second}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv, hits := serve(t, c.statuses...)
			l, clk := newTestLimiter(600, 1)
			var out struct{ OK bool }
			errc := make(chan error, 1)
			go func() { errc <- GetJSON(context.Background(), srv.Client(), l, srv.URL, &out) }()
			for _, d := range c.waits {
				waitQueued(t, l, 1)
				clk.Advance(d - time.Millisecond)
				waitQueued(t, l, 1)
				clk.Advance(time.Millisecond)
			}
			if err := <-errc; err != nil || !out.OK {
				t.Fatalf("err = %v, out = %+v", err, out)
			}
			if got := int(hits.Load()); got != len(c.waits)+1 {
				t.Fatalf("attempts = %d", got)
			}
		})
	}
}

func TestGetJSONRetriesAStalledBody(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			_, _ = w.Write([]byte(`{"ok":`))
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	t.Cleanup(srv.Close)
	hc := srv.Client()
	hc.Timeout = 50 * time.Millisecond
	l, clk := newTestLimiter(600, 1)
	var out struct{ OK bool }
	errc := make(chan error, 1)
	go func() { errc <- GetJSON(context.Background(), hc, l, srv.URL, &out) }()
	waitQueued(t, l, 1)
	clk.Advance(retryDelay)
	if err := <-errc; err != nil || !out.OK || hits.Load() != 2 {
		t.Fatalf("err = %v, out = %+v after %d attempts", err, out, hits.Load())
	}
}

func TestGetJSONGivesUp(t *testing.T) {
	srv, hits := serve(t, 404)
	l, _ := newTestLimiter(600, 1)
	err := GetJSON(context.Background(), srv.Client(), l, srv.URL, &struct{}{})
	if he, ok := errors.AsType[*HTTPError](err); !ok || he.Status != 404 || hits.Load() != 1 {
		t.Fatalf("err = %v after %d attempts", err, hits.Load())
	}
}

func TestGetJSONDoesNotPauseAfterTheLastAttempt(t *testing.T) {
	srv, hits := serve(t, 500, 500, 500)
	l, clk := newTestLimiter(600, 1)
	errc := make(chan error, 1)
	go func() { errc <- GetJSON(context.Background(), srv.Client(), l, srv.URL, &struct{}{}) }()
	for _, d := range []time.Duration{retryDelay, 2 * retryDelay} {
		waitQueued(t, l, 1)
		clk.Advance(d)
	}
	if err := <-errc; err == nil || hits.Load() != maxAttempts {
		t.Fatalf("err = %v after %d attempts", err, hits.Load())
	}
	// Nothing retries, so the next caller only waits out the rate.
	clk.Advance(100 * time.Millisecond)
	select {
	case err := <-goWait(context.Background(), l):
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the limiter stayed paused after the last attempt")
	}
}

func TestGetJSONKeepsARetryAfterOnTheLastAttempt(t *testing.T) {
	srv, _ := serve(t, 500, 500, 429)
	l, clk := newTestLimiter(600, 1)
	errc := make(chan error, 1)
	go func() { errc <- GetJSON(context.Background(), srv.Client(), l, srv.URL, &struct{}{}) }()
	for _, d := range []time.Duration{retryDelay, 2 * retryDelay} {
		waitQueued(t, l, 1)
		clk.Advance(d)
	}
	if err := <-errc; err == nil {
		t.Fatal("no error after the last attempt")
	}
	next := goWait(context.Background(), l)
	waitQueued(t, l, 1)
	clk.Advance(30*time.Second - time.Millisecond)
	waitQueued(t, l, 1)
	clk.Advance(time.Millisecond)
	if err := <-next; err != nil {
		t.Fatal(err)
	}
}
