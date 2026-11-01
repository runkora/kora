package observability

import (
	"context"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type runIDContextKey struct{}

type tenantIDContextKey struct{}

// WithRun returns ctx carrying runID, which every span started from it records as RunIDKey.
func WithRun(ctx context.Context, runID string) context.Context {
	if runID == "" {
		return ctx
	}
	return context.WithValue(ctx, runIDContextKey{}, runID)
}

// WithTenant returns ctx carrying tenantID, which every span started from it records as TenantIDKey.
func WithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantIDContextKey{}, tenantID)
}

// attributionProcessor copies the run and tenant in a span's start context onto the span.
// OpenTelemetry child spans do not inherit their parent's attributes.
type attributionProcessor struct{}

func (attributionProcessor) OnStart(ctx context.Context, s sdktrace.ReadWriteSpan) {
	if id, ok := ctx.Value(runIDContextKey{}).(string); ok {
		s.SetAttributes(RunIDKey.String(id))
	}
	if id, ok := ctx.Value(tenantIDContextKey{}).(string); ok {
		s.SetAttributes(TenantIDKey.String(id))
	}
}

func (attributionProcessor) OnEnd(sdktrace.ReadOnlySpan)      {}
func (attributionProcessor) Shutdown(context.Context) error   { return nil }
func (attributionProcessor) ForceFlush(context.Context) error { return nil }
