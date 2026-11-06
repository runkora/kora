package observability

import (
	"context"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// traceparentHeader is the W3C Trace Context header and the key a stored traceparent is read under.
const traceparentHeader = "traceparent"

// TraceParent is the W3C traceparent of the span in ctx, or empty when ctx holds no valid span
// (docs/adr/2026-10-31-one-trace-per-run.md).
func TraceParent(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get(traceparentHeader)
}

// ContextWithTraceParent returns ctx carrying traceparent as its remote parent. An empty or
// malformed traceparent leaves ctx unchanged.
func ContextWithTraceParent(ctx context.Context, traceparent string) context.Context {
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{traceparentHeader: traceparent})
}

// LinkToTraceParent adds a link from the span in ctx to the span traceparent names, when it parses.
func LinkToTraceParent(ctx context.Context, traceparent string) {
	if link, ok := TraceParentLink(traceparent); ok {
		trace.SpanFromContext(ctx).AddLink(link)
	}
}

// TraceParentLink is a span link to the span traceparent names, and false when it does not parse.
// Pass it to trace.WithLinks: the sampler sees links only at span start.
func TraceParentLink(traceparent string) (trace.Link, bool) {
	sc := spanContextOf(traceparent)
	return trace.Link{SpanContext: sc}, sc.IsValid()
}

// SampledTraceID is the trace ID in traceparent, or nil when it does not parse or was not sampled.
func SampledTraceID(traceparent string) *string {
	sc := spanContextOf(traceparent)
	if !sc.IsValid() || !sc.IsSampled() {
		return nil
	}
	id := sc.TraceID().String()
	return &id
}

func spanContextOf(traceparent string) trace.SpanContext {
	return trace.SpanContextFromContext(ContextWithTraceParent(context.Background(), traceparent))
}
