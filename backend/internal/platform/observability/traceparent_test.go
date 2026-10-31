package observability

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestTraceParentRoundTrip(t *testing.T) {
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("test").Start(context.Background(), "parent")
	defer span.End()

	stored := TraceParent(ctx)
	if stored == "" {
		t.Fatal("TraceParent is empty for a recording span")
	}
	got := trace.SpanContextFromContext(ContextWithTraceParent(context.Background(), stored))
	if got.TraceID() != span.SpanContext().TraceID() || got.SpanID() != span.SpanContext().SpanID() || !got.IsRemote() {
		t.Fatalf("restored span context = %v, want the parent's as remote", got)
	}
	if id := SampledTraceID(stored); id == nil || *id != span.SpanContext().TraceID().String() {
		t.Fatalf("SampledTraceID = %v, want %s", id, span.SpanContext().TraceID())
	}
}

func TestTraceParentWithoutSpan(t *testing.T) {
	if got := TraceParent(context.Background()); got != "" {
		t.Fatalf("TraceParent = %q, want empty", got)
	}
	ctx := ContextWithTraceParent(context.Background(), "")
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Fatal("an empty traceparent produced a span context")
	}
}

func TestSampledTraceIDUnsampled(t *testing.T) {
	if id := SampledTraceID("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-00"); id != nil {
		t.Fatalf("SampledTraceID = %q for an unsampled traceparent, want nil", *id)
	}
	if id := SampledTraceID("garbage"); id != nil {
		t.Fatalf("SampledTraceID = %q for a malformed traceparent, want nil", *id)
	}
}

func TestLinkToTraceParent(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	ctx, span := tp.Tracer("test").Start(context.Background(), "action")
	LinkToTraceParent(ctx, "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	LinkToTraceParent(ctx, "")
	span.End()

	links := rec.Ended()[0].Links()
	if len(links) != 1 || links[0].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("links = %v, want one link to the stored trace", links)
	}
}
