package logging

import (
	"context"
	"io"
	"os"

	"github.com/rs/zerolog"
	"go.opentelemetry.io/otel/trace"
)

// zerologLogger is an implementation of Logger interface backed by zerolog.
// This provides efficient structured logging with JSON output by default
// and automatic OpenTelemetry trace context extraction.
//
// TODO: Add Fatal() method for unrecoverable errors
// TODO: Add WithFields() pattern for pre-attaching fields to logger context
// TODO: Add field construction helpers (String, Int, Bool, etc.) for better ergonomics
type zerologLogger struct {
	logger zerolog.Logger
}

// NewLogger creates a new zerolog-backed Logger with production defaults.
// The logger writes JSON logs to stdout, includes timestamps, and automatically
// extracts OTEL trace/span IDs from context when present.
//
// This is the primary logger constructor for production deployments.
func NewLogger() Logger {
	return NewLoggerWithOutput(os.Stdout)
}

// NewLoggerWithOutput creates a logger writing to the specified output.
// Useful for testing or custom output destinations.
func NewLoggerWithOutput(w io.Writer) Logger {
	zlog := zerolog.New(w).With().Timestamp().Logger()
	return &zerologLogger{logger: zlog}
}

// NewDevelopmentLogger creates a human-friendly console logger for local development.
// Output is colorized and formatted for readability rather than machine parsing.
func NewDevelopmentLogger() Logger {
	output := zerolog.ConsoleWriter{Out: os.Stdout}
	zlog := zerolog.New(output).With().Timestamp().Logger()
	return &zerologLogger{logger: zlog}
}

// Debug logs a debug-level message with optional structured fields.
// Debug messages are typically disabled in production.
func (l *zerologLogger) Debug(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Debug(), msg, fields)
}

// Info logs an informational message with optional structured fields.
// Info level is used for routine application events.
func (l *zerologLogger) Info(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Info(), msg, fields)
}

// Warn logs a warning message with optional structured fields.
// Warn level indicates a potential issue or unexpected situation.
func (l *zerologLogger) Warn(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Warn(), msg, fields)
}

// Error logs an error message with optional structured fields.
// Error level indicates a failure the system could recover from.
//
// TODO: Add ErrorWithErr(ctx, msg, err, fields...) helper for structured error logging
func (l *zerologLogger) Error(ctx context.Context, msg string, fields ...Field) {
	l.logWithContext(ctx, l.logger.Error(), msg, fields)
}

// logWithContext is the core logging implementation that extracts OTEL trace context
// and attaches it to log entries. This enables automatic log-trace correlation when
// distributed tracing is active.
//
// Performance characteristics:
//   - Zero allocations when no trace context is present
//   - Single span context extraction per log call when tracing active
//   - Field conversion only allocates map when fields are provided
func (l *zerologLogger) logWithContext(
	ctx context.Context,
	event *zerolog.Event,
	msg string,
	fields []Field,
) {
	// Defensive nil check - prevents panics from incorrect usage
	if ctx == nil {
		ctx = context.Background()
	}

	// Extract OTEL span from context (if present)
	// This is a fast operation - just a context value lookup
	span := trace.SpanFromContext(ctx)
	spanCtx := span.SpanContext()

	// Only add trace fields if we have a valid span context
	// IsValid() checks are inlined and fast
	if spanCtx.IsValid() {
		event = event.
			Str("trace_id", spanCtx.TraceID().String()).
			Str("span_id", spanCtx.SpanID().String())

		// Include trace flags if trace is sampled
		// This helps backends filter sampled vs unsampled traces
		if spanCtx.TraceFlags().IsSampled() {
			event = event.Bool("trace_sampled", true)
		}
	}

	// Add custom fields if provided
	// convertFields only allocates if fields slice is non-empty
	if len(fields) > 0 {
		event = event.Fields(convertFields(fields))
	}

	// Emit the log message
	event.Msg(msg)
}

// convertFields converts a slice of Field into a map compatible with zerolog
// structured logging. This enables attaching arbitrary key-value pairs to log entries.
//
// The map is sized exactly to the number of fields to minimize allocations.
//
// TODO: Consider using sync.Pool for map allocation in high-throughput scenarios
func convertFields(fields []Field) map[string]any {
	m := make(map[string]any, len(fields))
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	return m
}
