package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestAttributionProcessor(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(attributionProcessor{}), sdktrace.WithSpanProcessor(rec))
	tracer := tp.Tracer("test")

	ctx := WithTenant(WithRun(context.Background(), "run-1"), "tenant-1")
	_, attributed := tracer.Start(ctx, "attributed")
	attributed.End()
	_, bare := tracer.Start(context.Background(), "bare")
	bare.End()

	spans := rec.Ended()
	if len(spans) != 2 {
		t.Fatalf("got %d spans, want 2", len(spans))
	}
	got := attrMap(spans[0].Attributes())
	if got[RunIDKey] != "run-1" || got[TenantIDKey] != "tenant-1" {
		t.Errorf("attributed span attributes = %v, want run-1 and tenant-1", got)
	}
	if got := attrMap(spans[1].Attributes()); len(got) != 0 {
		t.Errorf("bare span attributes = %v, want none", got)
	}
}

func TestWithRunTagsTheCurrentSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	ctx, span := tp.Tracer("test").Start(context.Background(), "started-first")
	WithTenant(WithRun(ctx, "run-1"), "tenant-1")
	span.End()

	got := attrMap(rec.Ended()[0].Attributes())
	if got[RunIDKey] != "run-1" || got[TenantIDKey] != "tenant-1" {
		t.Errorf("span attributes = %v, want run-1 and tenant-1", got)
	}
}

func attrMap(kvs []attribute.KeyValue) map[attribute.Key]string {
	m := map[attribute.Key]string{}
	for _, kv := range kvs {
		m[kv.Key] = kv.Value.Emit()
	}
	return m
}
