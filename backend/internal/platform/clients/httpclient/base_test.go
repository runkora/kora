package httpclient_test

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/clear-route/orbit/internal/platform/clients/httpclient"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// countingBody wraps an io.ReadCloser and counts how many times Close is called.
type countingBody struct {
	io.ReadCloser
	closes atomic.Int32
}

func (c *countingBody) Close() error {
	c.closes.Add(1)
	return c.ReadCloser.Close()
}

func newTestBase() *httpclient.Base {
	logger := logging.NewLogger()
	return httpclient.New("test", logger, 0)
}

func newTestResponse(statusCode int, body string) (*http.Response, *countingBody) {
	cb := &countingBody{ReadCloser: io.NopCloser(strings.NewReader(body))}
	return &http.Response{
		StatusCode: statusCode,
		Body:       cb,
	}, cb
}

// TestReadErrorBodyDoesNotCloseBody verifies that ReadErrorBody reads the body
// content but does NOT close it — the caller owns close via defer.
func TestReadErrorBodyDoesNotCloseBody(t *testing.T) {
	base := newTestBase()

	tracer := noop.NewTracerProvider().Tracer("test")
	_, noopSpan := tracer.Start(t.Context(), "test")

	resp, cb := newTestResponse(http.StatusInternalServerError, `{"error":"something went wrong"}`)

	upstream := base.ReadErrorBody(noopSpan, resp)

	if upstream == "" {
		t.Fatal("expected non-empty upstream body")
	}
	if got := cb.closes.Load(); got != 0 {
		t.Errorf("ReadErrorBody closed the body %d time(s); want 0 (caller owns Close)", got)
	}

	// Caller closes once — no panic, no double-close.
	_ = resp.Body.Close()
	if got := cb.closes.Load(); got != 1 {
		t.Errorf("after caller Close: close count = %d; want 1", got)
	}
}

// TestHandleErrorResponseDoesNotCloseBody verifies that HandleErrorResponse
// (which calls ReadErrorBody) does not close resp.Body. The caller's single
// defer resp.Body.Close() must be the only close.
func TestHandleErrorResponseDoesNotCloseBody(t *testing.T) {
	base := newTestBase()
	sentinel := errors.New("upstream error")

	tracer := noop.NewTracerProvider().Tracer("test")
	_, noopSpan := tracer.Start(t.Context(), "test")

	resp, cb := newTestResponse(http.StatusForbidden, `{"code":"forbidden"}`)

	err := base.HandleErrorResponse(noopSpan, resp, sentinel)
	if err == nil {
		t.Fatal("expected non-nil error from HandleErrorResponse")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("error does not wrap sentinel: %v", err)
	}
	if got := cb.closes.Load(); got != 0 {
		t.Errorf("HandleErrorResponse closed body %d time(s); want 0 (caller owns Close)", got)
	}

	// Simulate the caller's deferred close.
	_ = resp.Body.Close()
	if got := cb.closes.Load(); got != 1 {
		t.Errorf("after caller Close: close count = %d; want 1", got)
	}
}
