package routes

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
)

// A request its client gave up on is neither logged as an error nor answered; an error that only
// wraps a cancellation, on a live request, is the server's.
func TestHandleErrorSkipsCanceledRequests(t *testing.T) {
	var logs bytes.Buffer
	defer func(l *slog.Logger) { slog.SetDefault(l) }(slog.Default())
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	serve := func(ctx context.Context, err error) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handleError(err, echo.New().NewContext(httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil), rec))
		return rec
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if rec := serve(canceled, fmt.Errorf("query: %w", context.Canceled)); rec.Body.Len() != 0 || logs.Len() != 0 {
		t.Fatalf("answered %d %s, logged %q", rec.Code, rec.Body, &logs)
	}
	if rec := serve(context.Background(), fmt.Errorf("query: %w", context.Canceled)); rec.Code != http.StatusInternalServerError ||
		logs.Len() == 0 {
		t.Fatalf("answered %d, logged %q", rec.Code, &logs)
	}
}
