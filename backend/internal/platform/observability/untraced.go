package observability

import (
	"context"
	"crypto/rand"

	"go.opentelemetry.io/otel/trace"
)

// Untraced returns ctx under an unsampled parent, so no span started under it
// is recorded, here or in a service it calls. Periodic loops run a tick under
// it and record a span afterwards only when the tick changed something or failed.
func Untraced(ctx context.Context) context.Context {
	var cfg trace.SpanContextConfig
	_, _ = rand.Read(cfg.TraceID[:])
	_, _ = rand.Read(cfg.SpanID[:])
	return trace.ContextWithSpanContext(ctx, trace.NewSpanContext(cfg))
}
