package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOtelQueryTracer_ErrorStatusIsStatic(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	tracer := &otelQueryTracer{tracer: provider.Tracer("test")}
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT 1"})
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
