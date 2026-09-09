package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

func TestSpanRouteName(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		hasRoute  bool
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

			handler := SpanRouteName(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// Stand in for TracingMiddleware, which starts the span before routing.
			ctx, span := provider.Tracer("test").Start(context.Background(), "GET /")
			if tt.hasRoute {
				ctx = context.WithValue(ctx, routePatternContextKey{}, tt.pattern)
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

			var gotRoute string
			for _, attr := range ended[0].Attributes() {
				if attr.Key == semconv.HTTPRouteKey {
					gotRoute = attr.Value.AsString()
				}
			}
			if gotRoute != tt.wantRoute {
				t.Errorf("http.route = %q, want %q", gotRoute, tt.wantRoute)
			}
		})
	}
}
