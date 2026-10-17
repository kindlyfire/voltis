package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"voltis/config"
)

const (
	maxAttempts = 3
	retryDelay  = time.Second // doubled per retry
)

type HTTPError struct {
	Status     int
	Body       string
	RetryAfter time.Duration // from the response's Retry-After header
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body[:min(len(e.Body), 512)])
}

// Request is a JSON request. A non-nil Body is sent as JSON. Ready, when set, runs after the
// limiter wait, right before sending, and its error aborts the request.
type Request struct {
	Method, URL string
	Header      http.Header
	Body        any
	Ready       func() error
}

// GetJSON decodes url into out, retrying 429s, 5xx responses, and timeouts. Every attempt takes a
// token from lim, and a backoff or a 429's Retry-After pauses lim for everyone using it.
func GetJSON(ctx context.Context, hc *http.Client, lim *Limiter, url string, out any) error {
	return DoJSON(ctx, hc, lim, Request{Method: http.MethodGet, URL: url}, out)
}

// DoJSON is GetJSON for any method. Only a GET is retried: a write is attempted once, since its
// outcome may be unknown. A nil out discards the response body.
func DoJSON(ctx context.Context, hc *http.Client, lim *Limiter, r Request, out any) error {
	attempts := 1
	if r.Method == http.MethodGet {
		attempts = maxAttempts
	}
	var body []byte
	if r.Body != nil {
		var err error
		if body, err = json.Marshal(r.Body); err != nil {
			return err
		}
	}
	for attempt := 1; ; attempt++ {
		if err := lim.Wait(ctx); err != nil {
			return err
		}
		if r.Ready != nil {
			if err := r.Ready(); err != nil {
				return err
			}
		}
		retry, after, err := doOnce(ctx, hc, r, body, out)
		last := !retry || attempt == attempts
		if !last {
			after = max(after, retryDelay<<(attempt-1))
		}
		// A server-requested pause holds for every caller, even when this one gives up.
		if after > 0 {
			lim.backoff(after)
		}
		if last {
			return err
		}
	}
}

// doOnce reports whether a failure is worth retrying, and after how long if the server said.
func doOnce(ctx context.Context, hc *http.Client, r Request, body []byte, out any) (retry bool, after time.Duration, err error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, reader)
	if err != nil {
		return false, 0, err
	}
	for k, v := range r.Header {
		req.Header[k] = v
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Voltis/"+config.AppVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return timedOut(ctx, err), 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		err := &HTTPError{Status: resp.StatusCode, Body: string(body), RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
		if resp.StatusCode == http.StatusTooManyRequests {
			return true, err.RetryAfter, err
		}
		return resp.StatusCode >= 500, 0, err
	}
	if out == nil {
		return false, 0, nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return timedOut(ctx, err), 0, fmt.Errorf("decode %s: %w", r.URL, err)
	}
	return false, 0, nil
}

// timedOut reports a timeout the caller did not cause by cancelling.
func timedOut(ctx context.Context, err error) bool {
	ne, ok := errors.AsType[net.Error](err)
	return ok && ne.Timeout() && ctx.Err() == nil
}

// retryAfter reads a Retry-After header in seconds or as a date.
func retryAfter(h string) time.Duration {
	if s, err := strconv.Atoi(h); err == nil {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		return time.Until(t)
	}
	return 0
}
