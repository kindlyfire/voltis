package providers

import (
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
	Status int
	Body   string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Body) }

// GetJSON decodes url into out, retrying 429s, 5xx responses, and timeouts. Every attempt takes a
// token from lim, and a backoff or a 429's Retry-After pauses lim for everyone using it.
func GetJSON(ctx context.Context, hc *http.Client, lim *Limiter, url string, out any) error {
	for attempt := 1; ; attempt++ {
		if err := lim.Wait(ctx); err != nil {
			return err
		}
		retry, after, err := getOnce(ctx, hc, url, out)
		last := !retry || attempt == maxAttempts
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

// getOnce reports whether a failure is worth retrying, and after how long if the server said.
func getOnce(ctx context.Context, hc *http.Client, url string, out any) (retry bool, after time.Duration, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, 0, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Voltis/"+config.AppVersion)
	resp, err := hc.Do(req)
	if err != nil {
		return timedOut(ctx, err), 0, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		err := &HTTPError{Status: resp.StatusCode, Body: string(body)}
		if resp.StatusCode == http.StatusTooManyRequests {
			return true, retryAfter(resp.Header.Get("Retry-After")), err
		}
		return resp.StatusCode >= 500, 0, err
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return timedOut(ctx, err), 0, fmt.Errorf("decode %s: %w", url, err)
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
