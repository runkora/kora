package observability

import (
	"errors"
	"fmt"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

func TestRecordFailure(t *testing.T) {
	sentinel := errors.New("load widget")
	detail := fmt.Errorf("%w: host db-7 refused", sentinel)

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	_, span := tp.Tracer("test").Start(t.Context(), "op")
	RecordFailure(span, detail, sentinel)
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("ended spans = %d, want 1", len(spans))
	}
	got := spans[0]

	if got.Status().Code != codes.Error {
		t.Errorf("status code = %v, want Error", got.Status().Code)
	}
	if got.Status().Description != sentinel.Error() {
		t.Errorf("status description = %q, want %q", got.Status().Description, sentinel.Error())
	}

	var errorType string
	for _, kv := range got.Attributes() {
		if kv.Key == semconv.ErrorTypeKey {
			errorType = kv.Value.AsString()
		}
	}
	if errorType != sentinel.Error() {
		t.Errorf("error.type = %q, want %q", errorType, sentinel.Error())
	}

	if len(got.Events()) != 1 || got.Events()[0].Name != "exception" {
		t.Fatalf("events = %v, want one exception event", got.Events())
	}
	var message string
	for _, kv := range got.Events()[0].Attributes {
		if kv.Key == semconv.ExceptionMessageKey {
			message = kv.Value.AsString()
		}
	}
	if message != detail.Error() {
		t.Errorf("exception.message = %q, want %q", message, detail.Error())
	}
}
