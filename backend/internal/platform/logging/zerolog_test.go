package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestNewLogger verifies production logger creation
func TestNewLogger(t *testing.T) {
	logger := NewLogger()
	if logger == nil {
		t.Fatal("NewLogger() returned nil")
	}

	// Verify it's the correct implementation
	if _, ok := logger.(*zerologLogger); !ok {
		t.Errorf("NewLogger() returned %T, want *zerologLogger", logger)
	}
}

// TestNewLoggerWithOutput verifies custom output logger creation
func TestNewLoggerWithOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)

	if logger == nil {
		t.Fatal("NewLoggerWithOutput() returned nil")
	}

	// Verify it writes to our buffer
	ctx := context.Background()
	logger.Info(ctx, "test message")

	if buf.Len() == 0 {
		t.Error("Logger did not write to provided output")
	}
}

// TestNewDevelopmentLogger verifies development logger creation
func TestNewDevelopmentLogger(t *testing.T) {
	logger := NewDevelopmentLogger()
	if logger == nil {
		t.Fatal("NewDevelopmentLogger() returned nil")
	}

	if _, ok := logger.(*zerologLogger); !ok {
		t.Errorf("NewDevelopmentLogger() returned %T, want *zerologLogger", logger)
	}
}

// TestZerologLevels verifies all log levels work correctly
func TestZerologLevels(t *testing.T) {
	tests := []struct {
		name      string
		logFunc   func(Logger, context.Context, string, ...Field)
		wantLevel string
	}{
		{
			name:      "debug level",
			logFunc:   func(l Logger, ctx context.Context, msg string, fields ...Field) { l.Debug(ctx, msg, fields...) },
			wantLevel: "debug",
		},
		{
			name:      "info level",
			logFunc:   func(l Logger, ctx context.Context, msg string, fields ...Field) { l.Info(ctx, msg, fields...) },
			wantLevel: "info",
		},
		{
			name:      "warn level",
			logFunc:   func(l Logger, ctx context.Context, msg string, fields ...Field) { l.Warn(ctx, msg, fields...) },
			wantLevel: "warn",
		},
		{
			name:      "error level",
			logFunc:   func(l Logger, ctx context.Context, msg string, fields ...Field) { l.Error(ctx, msg, fields...) },
			wantLevel: "error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLoggerWithOutput(&buf)
			ctx := context.Background()

			tt.logFunc(logger, ctx, "test message")

			// Parse JSON output
			var logEntry map[string]any
			if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
				t.Fatalf("Failed to parse log JSON: %v", err)
			}

			// Verify level
			if level, ok := logEntry["level"].(string); !ok || level != tt.wantLevel {
				t.Errorf("Log level = %v, want %v", logEntry["level"], tt.wantLevel)
			}

			// Verify message
			if msg, ok := logEntry["message"].(string); !ok || msg != "test message" {
				t.Errorf("Log message = %v, want 'test message'", logEntry["message"])
			}
		})
	}
}

// TestZerologWithFields verifies structured field logging
func TestZerologWithFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)
	ctx := context.Background()

	logger.Info(ctx, "user action",
		Field{Key: "user_id", Value: "12345"},
		Field{Key: "action", Value: "login"},
		Field{Key: "success", Value: true},
	)

	// Parse JSON output
	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify fields
	if userID, ok := logEntry["user_id"].(string); !ok || userID != "12345" {
		t.Errorf("user_id = %v, want '12345'", logEntry["user_id"])
	}

	if action, ok := logEntry["action"].(string); !ok || action != "login" {
		t.Errorf("action = %v, want 'login'", logEntry["action"])
	}

	if success, ok := logEntry["success"].(bool); !ok || !success {
		t.Errorf("success = %v, want true", logEntry["success"])
	}
}

// TestZerologWithNoFields verifies logging without fields
func TestZerologWithNoFields(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)
	ctx := context.Background()

	logger.Info(ctx, "simple message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	if msg, ok := logEntry["message"].(string); !ok || msg != "simple message" {
		t.Errorf("message = %v, want 'simple message'", logEntry["message"])
	}
}

// TestZerologWithTraceContext verifies OpenTelemetry trace extraction
func TestZerologWithTraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)

	// Setup OTEL tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	// Create a span
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-operation")
	defer span.End()

	// Log with trace context
	logger.Info(ctx, "traced message")

	// Parse log output
	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify trace_id and span_id are present
	if _, ok := logEntry["trace_id"].(string); !ok {
		t.Error("trace_id not found in log entry")
	}

	if _, ok := logEntry["span_id"].(string); !ok {
		t.Error("span_id not found in log entry")
	}

	// Verify trace_id matches the span's trace ID
	spanContext := span.SpanContext()
	expectedTraceID := spanContext.TraceID().String()
	if traceID, ok := logEntry["trace_id"].(string); ok && traceID != expectedTraceID {
		t.Errorf("trace_id = %s, want %s", traceID, expectedTraceID)
	}
}

// TestZerologWithoutTraceContext verifies behavior when no trace context exists
func TestZerologWithoutTraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)
	ctx := context.Background()

	logger.Info(ctx, "untraced message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify trace fields are NOT present
	if _, ok := logEntry["trace_id"]; ok {
		t.Error("trace_id should not be present without trace context")
	}

	if _, ok := logEntry["span_id"]; ok {
		t.Error("span_id should not be present without trace context")
	}
}

// TestZerologWithNilContext verifies defensive nil context handling
func TestZerologWithNilContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)

	// Should not panic with nil context
	logger.Info(nil, "message with nil context")

	if buf.Len() == 0 {
		t.Error("Logger did not write with nil context")
	}
}

// TestConvertFields verifies field conversion to map
func TestConvertFields(t *testing.T) {
	tests := []struct {
		name   string
		fields []Field
		want   map[string]any
	}{
		{
			name:   "empty fields",
			fields: []Field{},
			want:   map[string]any{},
		},
		{
			name: "single field",
			fields: []Field{
				{Key: "key1", Value: "value1"},
			},
			want: map[string]any{
				"key1": "value1",
			},
		},
		{
			name: "multiple fields",
			fields: []Field{
				{Key: "string", Value: "text"},
				{Key: "number", Value: 42},
				{Key: "bool", Value: true},
			},
			want: map[string]any{
				"string": "text",
				"number": 42,
				"bool":   true,
			},
		},
		{
			name: "nil value",
			fields: []Field{
				{Key: "nullable", Value: nil},
			},
			want: map[string]any{
				"nullable": nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertFields(tt.fields)

			if len(got) != len(tt.want) {
				t.Errorf("convertFields() map length = %d, want %d", len(got), len(tt.want))
			}

			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("convertFields() missing key %s", k)
					continue
				}
				if gotV != wantV {
					t.Errorf("convertFields()[%s] = %v, want %v", k, gotV, wantV)
				}
			}
		})
	}
}

// TestZerologTimestamps verifies that timestamps are included in logs
func TestZerologTimestamps(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)
	ctx := context.Background()

	logger.Info(ctx, "timestamped message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	if _, ok := logEntry["time"]; !ok {
		t.Error("timestamp (time) not found in log entry")
	}
}

// TestZerologSampledTraceFlag verifies trace_sampled flag is added when appropriate
func TestZerologSampledTraceFlag(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithOutput(&buf)

	// Setup OTEL tracer with sampling
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
		trace.WithSampler(trace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	// Create a sampled span
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "sampled-operation")
	defer span.End()

	logger.Info(ctx, "sampled trace")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify trace_sampled is present and true
	if sampled, ok := logEntry["trace_sampled"].(bool); !ok || !sampled {
		t.Errorf("trace_sampled = %v, want true", logEntry["trace_sampled"])
	}
}

// TestZerologConcurrentWrites verifies thread safety (smoke test)
func TestZerologConcurrentWrites(t *testing.T) {
	// Use thread-safe logger with os.Stdout instead of shared buffer
	// bytes.Buffer is not safe for concurrent writes
	logger := NewLogger()
	ctx := context.Background()

	// Write concurrently from multiple goroutines
	done := make(chan bool, 10)
	for i := range 10 {
		go func(id int) {
			logger.Info(ctx, "concurrent write", Field{Key: "id", Value: id})
			done <- true
		}(i)
	}

	// Wait for all writes
	for range 10 {
		<-done
	}

	// If we get here without panic or race detection, the test passes
	// We can't easily verify output when using stdout, but that's okay
	// The goal is to verify no data races occur
}
