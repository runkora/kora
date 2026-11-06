package observability

import (
	"errors"

	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// RecordFailure marks span failed. The sentinel is the static status description and error.type;
// err carries the dynamic detail as an exception event, skipped when err is in the sentinel's chain.
func RecordFailure(span trace.Span, err, sentinel error) {
	if !errors.Is(sentinel, err) {
		span.RecordError(err)
	}
	span.SetStatus(codes.Error, sentinel.Error())
	span.SetAttributes(semconv.ErrorTypeKey.String(sentinel.Error()))
}
