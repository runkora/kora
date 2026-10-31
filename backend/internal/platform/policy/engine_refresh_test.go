package policy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-policy-agent/opa/v1/bundle"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func testBundle(t *testing.T, version int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := bundle.NewWriter(&buf).Write(bundle.Bundle{Data: map[string]any{"version": version}}); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	return buf.Bytes()
}

func TestRefresh_SpansOnlyOnChangeOrFailure(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	v1, v2 := testBundle(t, 1), testBundle(t, 2)
	var body atomic.Pointer[[]byte]
	var status atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if code := int(status.Load()); code != http.StatusOK {
			w.WriteHeader(code)
			return
		}
		_, _ = w.Write(*body.Load())
	}))
	t.Cleanup(srv.Close)

	e := newEngine(srv.URL, time.Hour, logging.NewLoggerWithOutput(io.Discard))
	body.Store(&v1)
	status.Store(http.StatusOK)
	if _, err := e.loadBundle(context.Background(), "tenant-1"); err != nil {
		t.Fatalf("initial load: %v", err)
	}

	tests := []struct {
		name       string
		body       []byte
		status     int
		wantSpans  int
		wantStatus codes.Code
	}{
		{name: "unchanged bundle records no span", body: v1, status: http.StatusOK, wantSpans: 0},
		{name: "changed bundle records a span", body: v2, status: http.StatusOK, wantSpans: 1, wantStatus: codes.Unset},
		{name: "failed poll records an error span", body: v2, status: http.StatusInternalServerError, wantSpans: 1, wantStatus: codes.Error},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body.Store(&tt.body)
			status.Store(int32(tt.status))
			before := len(recorder.Ended())

			e.refreshAll(context.Background())

			spans := recorder.Ended()[before:]
			if len(spans) != tt.wantSpans {
				t.Fatalf("spans = %d, want %d", len(spans), tt.wantSpans)
			}
			if tt.wantSpans == 1 && spans[0].Status().Code != tt.wantStatus {
				t.Errorf("status = %v, want %v", spans[0].Status().Code, tt.wantStatus)
			}
		})
	}
}
