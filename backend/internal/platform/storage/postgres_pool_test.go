package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestOtelQueryTracer_ErrorStatusIsStatic(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	tracer := &otelQueryTracer{tracer: provider.Tracer("test")}
	parent, _ := provider.Tracer("test").Start(context.Background(), "parent")
	ctx := tracer.TraceQueryStart(parent, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{Err: errors.New(`relation "runs" does not exist`)})

	ended := recorder.Ended()
	if len(ended) != 1 {
		t.Fatalf("expected 1 span, got %d", len(ended))
	}
	status := ended[0].Status()
	if status.Code != codes.Error || status.Description != errQueryFailed.Error() {
		t.Errorf("status = %v %q, want Error %q", status.Code, status.Description, errQueryFailed.Error())
	}
	if len(ended[0].Events()) != 1 {
		t.Errorf("expected the driver error recorded as one event, got %d", len(ended[0].Events()))
	}
}

func TestOtelQueryTracer_SpansOnlyUnderAParent(t *testing.T) {
	tests := []struct {
		name      string
		hasParent bool
		wantSpans int
	}{
		{name: "no parent records no span", hasParent: false, wantSpans: 0},
		{name: "parent records a child span", hasParent: true, wantSpans: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
			t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

			ctx := context.Background()
			var parent trace.Span
			if tt.hasParent {
				ctx, parent = provider.Tracer("test").Start(ctx, "parent")
			}
			tracer := &otelQueryTracer{tracer: provider.Tracer("test")}
			qctx := tracer.TraceQueryStart(ctx, nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
			tracer.TraceQueryEnd(qctx, nil, pgx.TraceQueryEndData{})

			if got := len(recorder.Ended()); got != tt.wantSpans {
				t.Fatalf("ended spans = %d, want %d", got, tt.wantSpans)
			}
			if parent != nil && !parent.IsRecording() {
				t.Error("query end ended the parent span")
			}
		})
	}
}
