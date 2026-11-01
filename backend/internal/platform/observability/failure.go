package observability

import (
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// RecordFailure marks span failed. The sentinel is the static status description and error.type;
// err carries the dynamic detail as an exception event, skipped when err is the sentinel.
func RecordFailure(span trace.Span, err, sentinel error) {
	if err != sentinel { //nolint:errorlint // identity check: a wrapped sentinel still carries detail
		span.RecordError(err)
	}
	span.SetStatus(codes.Error, sentinel.Error())
	span.SetAttributes(semconv.ErrorTypeKey.String(sentinel.Error()))
}
