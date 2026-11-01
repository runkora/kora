package token

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

func TestWithClaimsAttribution(t *testing.T) {
	claims := &PlatformClaims{Kora: KoraClaims{TenantID: "tenant-1", Claims: map[string]string{"run_id": "run-caller"}}}
	tests := []struct {
		name     string
		routeRun string
		wantRun  string
	}{
		{"caller's run claim with no route run", "", "run-caller"},
		{"route run wins over the caller's run claim", "run-route", "run-route"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := tracetest.NewSpanRecorder()
			tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
			ctx, span := tp.Tracer("test").Start(observability.WithRun(context.Background(), tt.routeRun), "GET /")

			ctx = WithClaims(ctx, claims)
			span.End()

			if got := observability.RunFromContext(ctx); got != tt.wantRun {
				t.Errorf("context run = %q, want %q", got, tt.wantRun)
			}
			ended := rec.Ended()
			if len(ended) != 1 {
				t.Fatalf("got %d spans, want 1", len(ended))
			}
			var tenant string
			for _, kv := range ended[0].Attributes() {
				if kv.Key == observability.TenantIDKey {
					tenant = kv.Value.AsString()
				}
			}
			if tenant != "tenant-1" {
				t.Errorf("server span tenant = %q, want tenant-1", tenant)
			}
		})
	}
}
