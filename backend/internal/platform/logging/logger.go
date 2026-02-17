package logging

import "context"

// Field represents a structured key-value pair to attach to a log message.
// Use Field to add contextual information, e.g., Field{Key: "userID", Value: userID}.
type Field struct {
	Key   string      // The key or name of the field, e.g., "userID"
	Value interface{} // The value associated with the key
}

// Logger defines the project-wide logging abstraction interface.
// It supports structured logging at multiple severity levels with context awareness.
// All methods accept context.Context as the first parameter to enable automatic
// OpenTelemetry trace/span ID extraction when tracing is active.
//
// Implementations should be concurrency-safe and provide zero-allocation
// fast paths when trace context is not present.
type Logger interface {
	// Debug logs a debug-level message with optional key-value fields.
	// Debug messages are typically disabled in production.
	Debug(ctx context.Context, msg string, fields ...Field)

	// Info logs an info-level message with optional key-value fields.
	// Info level is used for routine application events.
	Info(ctx context.Context, msg string, fields ...Field)

	// Warn logs a warning-level message with optional key-value fields.
	// Warn level indicates a potential issue or unexpected situation.
	Warn(ctx context.Context, msg string, fields ...Field)

	// Error logs an error-level message with optional key-value fields.
	// Error level indicates a failure the system could recover from.
	Error(ctx context.Context, msg string, fields ...Field)
}
