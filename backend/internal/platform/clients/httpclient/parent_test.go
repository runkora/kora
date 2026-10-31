package httpclient_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/runkora/kora/backend/internal/platform/clients/httpclient"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

func TestClientSpans_OnlyUnderAParent(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	var traceparent string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
	}))
	t.Cleanup(srv.Close)

	base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0)
	sdk := httpclient.NewHTTPClient(0, httpclient.NewTracingTransport("test", nil))
	calls := map[string]func(ctx context.Context) error{
		"Base.Do": func(ctx context.Context) error {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			_, span, resp, err := base.Do(ctx, "test.Call", req)
			defer span.End()
			if err == nil {
				_ = resp.Body.Close()
			}
			return err
		},
		"tracing transport": func(ctx context.Context) error {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			resp, err := sdk.Do(req)
			if err == nil {
				_ = resp.Body.Close()
			}
			return err
		},
	}

	tests := []struct {
		client    string
		hasParent bool
		wantSpans int
	}{
		{client: "Base.Do", hasParent: false, wantSpans: 0},
		{client: "Base.Do", hasParent: true, wantSpans: 1},
		{client: "tracing transport", hasParent: false, wantSpans: 0},
		{client: "tracing transport", hasParent: true, wantSpans: 1},
	}
	for _, tt := range tests {
		t.Run(tt.client, func(t *testing.T) {
			ctx := context.Background()
			if tt.hasParent {
				c, parent := provider.Tracer("test").Start(ctx, "parent")
				t.Cleanup(func() { parent.End() })
				ctx = c
			}
			traceparent = ""
			before := len(recorder.Ended())

			if err := calls[tt.client](ctx); err != nil {
				t.Fatalf("call: %v", err)
			}

			if got := len(recorder.Ended()) - before; got != tt.wantSpans {
				t.Errorf("client spans = %d, want %d", got, tt.wantSpans)
			}
			if gotHeader := traceparent != ""; gotHeader != tt.hasParent {
				t.Errorf("traceparent = %q, want injected = %v", traceparent, tt.hasParent)
			}
		})
	}
}

func TestSpanSince_StartsARootWithoutAParent(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	base := httpclient.New("test", logging.NewLoggerWithOutput(io.Discard), 0)
	base.SpanSince(context.Background(), "test.Tick", time.Now(), nil, nil)

	spans := recorder.Ended()
	if len(spans) != 1 || spans[0].Parent().IsValid() {
		t.Fatalf("want one root span, got %d spans", len(spans))
	}
}
