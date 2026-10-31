package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

func TestSpanRouteName(t *testing.T) {
	tests := []struct {
		name      string
		mountPath string
		pattern   string
		hasRoute  bool
		runPrefix bool
		wantName  string
		wantRoute string
	}{
		{
			name:      "names the span after the matched route",
			pattern:   "/api/v1/pats/{id}",
			hasRoute:  true,
			wantName:  "GET /api/v1/pats/{id}",
			wantRoute: "/api/v1/pats/{id}",
		},
		{
			name:      "adds back the stripped server base path",
			mountPath: "/api/v1",
			pattern:   "/policy/bundles/{tenant_id}",
			hasRoute:  true,
			wantName:  "GET /api/v1/policy/bundles/{tenant_id}",
			wantRoute: "/api/v1/policy/bundles/{tenant_id}",
		},
		{
			name:      "adds back a stripped run prefix as a template",
			mountPath: "/v1",
			pattern:   "/messages",
			hasRoute:  true,
			runPrefix: true,
			wantName:  "GET /run/{run_id}/v1/messages",
			wantRoute: "/run/{run_id}/v1/messages",
		},
		{
			name:     "leaves the span alone when no route matched",
			hasRoute: false,
			wantName: "GET /",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			handler := SpanRouteName(tt.mountPath)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// Stand in for TracingMiddleware, which starts the span before routing.
			ctx, span := provider.Tracer("test").Start(context.Background(), "GET /")
			if tt.hasRoute {
				ctx = context.WithValue(ctx, routePatternContextKey{}, tt.pattern)
			}
			if tt.runPrefix {
				ctx = context.WithValue(ctx, runIDContextKey{}, "run-123")
			}
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/pats/abc123", nil).WithContext(ctx))
			span.End()

			ended := recorder.Ended()
			if len(ended) != 1 {
				t.Fatalf("expected 1 span, got %d", len(ended))
			}
			if got := ended[0].Name(); got != tt.wantName {
				t.Errorf("span name = %q, want %q", got, tt.wantName)
			}

			var gotRoute, gotRunID string
			for _, attr := range ended[0].Attributes() {
				switch attr.Key {
				case semconv.HTTPRouteKey:
					gotRoute = attr.Value.AsString()
				case observability.RunIDKey:
					gotRunID = attr.Value.AsString()
				}
			}
			wantRunID := ""
			if tt.runPrefix {
				wantRunID = "run-123"
			}
			if gotRunID != wantRunID {
				t.Errorf("kora.run.id = %q, want %q", gotRunID, wantRunID)
			}
			if gotRoute != tt.wantRoute {
				t.Errorf("http.route = %q, want %q", gotRoute, tt.wantRoute)
			}
		})
	}
}
