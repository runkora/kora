package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestLoggingMiddleware verifies basic logging middleware functionality
func TestLoggingMiddleware(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("response"))
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test/path", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Verify handler was called
	if rec.Code != http.StatusOK {
		t.Errorf("Response code = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify logs were written
	if buf.Len() == 0 {
		t.Fatal("No logs written by middleware")
	}

	// Parse log entries (should be 2: incoming request + completed request)
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	if len(logs) < 2 {
		t.Fatalf("Expected at least 2 log entries, got %d", len(logs))
	}
}

// TestLoggingMiddlewareIncomingRequest verifies incoming request logging
func TestLoggingMiddlewareIncomingRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("POST", "/api/users", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse first log entry (incoming request)
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	var logEntry map[string]any
	if err := json.Unmarshal(logs[0], &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify incoming request log content
	if msg, ok := logEntry["message"].(string); !ok || msg != "Incoming request" {
		t.Errorf("message = %v, want 'Incoming request'", logEntry["message"])
	}

	if method, ok := logEntry["method"].(string); !ok || method != "POST" {
		t.Errorf("method = %v, want 'POST'", logEntry["method"])
	}

	if path, ok := logEntry["path"].(string); !ok || path != "/api/users" {
		t.Errorf("path = %v, want '/api/users'", logEntry["path"])
	}

	if remoteAddr, ok := logEntry["remote_addr"].(string); !ok || remoteAddr != "192.168.1.1:12345" {
		t.Errorf("remote_addr = %v, want '192.168.1.1:12345'", logEntry["remote_addr"])
	}
}

// TestLoggingMiddlewareCompletedRequest verifies completed request logging
func TestLoggingMiddlewareCompletedRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond) // Ensure measurable duration
		w.WriteHeader(http.StatusCreated)
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("POST", "/api/items", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse second log entry (completed request)
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	if len(logs) < 2 {
		t.Fatal("Expected at least 2 log entries")
	}

	var logEntry map[string]any
	if err := json.Unmarshal(logs[1], &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Verify completed request log content
	if msg, ok := logEntry["message"].(string); !ok || msg != "Request completed" {
		t.Errorf("message = %v, want 'Request completed'", logEntry["message"])
	}

	if method, ok := logEntry["method"].(string); !ok || method != "POST" {
		t.Errorf("method = %v, want 'POST'", logEntry["method"])
	}

	if path, ok := logEntry["path"].(string); !ok || path != "/api/items" {
		t.Errorf("path = %v, want '/api/items'", logEntry["path"])
	}

	if status, ok := logEntry["status"].(float64); !ok || int(status) != http.StatusCreated {
		t.Errorf("status = %v, want %d", logEntry["status"], http.StatusCreated)
	}

	// Verify duration was logged and is positive
	if durationMs, ok := logEntry["duration_ms"].(float64); !ok || durationMs <= 0 {
		t.Errorf("duration_ms = %v, want positive value", logEntry["duration_ms"])
	}
}

// TestLoggingMiddlewareWithTraceContext verifies trace context propagation to logs
func TestLoggingMiddlewareWithTraceContext(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	// Setup OTEL tracer
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.next(handler)

	// Create request with trace context
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-request")
	defer span.End()

	req := httptest.NewRequest("GET", "/traced", nil)
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse log entries and verify trace_id is present
	logs := bytes.SplitSeq(buf.Bytes(), []byte("\n"))
	for logBytes := range logs {
		if len(logBytes) == 0 {
			continue
		}

		var logEntry map[string]any
		if err := json.Unmarshal(logBytes, &logEntry); err != nil {
			continue
		}

		// Both incoming and completed logs should have trace_id
		if _, ok := logEntry["trace_id"]; !ok {
			t.Error("trace_id not found in log entry with trace context")
		}

		if _, ok := logEntry["span_id"]; !ok {
			t.Error("span_id not found in log entry with trace context")
		}
	}
}

// TestResponseWriterStatusCapture verifies status code capture
func TestResponseWriterStatusCapture(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{"200 OK", http.StatusOK},
		{"201 Created", http.StatusCreated},
		{"400 Bad Request", http.StatusBadRequest},
		{"404 Not Found", http.StatusNotFound},
		{"500 Internal Server Error", http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := logging.NewLoggerWithOutput(&buf)

			mw := LoggingMiddleware(logger)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrapped := mw.next(handler)
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()

			wrapped.ServeHTTP(rec, req)

			// Parse completed request log (second entry)
			logs := bytes.Split(buf.Bytes(), []byte("\n"))
			if len(logs) < 2 {
				t.Fatal("Expected at least 2 log entries")
			}

			var logEntry map[string]any
			if err := json.Unmarshal(logs[1], &logEntry); err != nil {
				t.Fatalf("Failed to parse log JSON: %v", err)
			}

			if status, ok := logEntry["status"].(float64); !ok || int(status) != tt.statusCode {
				t.Errorf("status = %v, want %d", logEntry["status"], tt.statusCode)
			}
		})
	}
}

// TestResponseWriterImplicit200 verifies implicit 200 status when WriteHeader not called
func TestResponseWriterImplicit200(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't call WriteHeader - should default to 200
		w.Write([]byte("response"))
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse completed request log
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	var logEntry map[string]any
	if err := json.Unmarshal(logs[1], &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	if status, ok := logEntry["status"].(float64); !ok || int(status) != http.StatusOK {
		t.Errorf("status = %v, want %d", logEntry["status"], http.StatusOK)
	}
}

// TestResponseWriterMultipleWriteHeader verifies only first WriteHeader is captured
func TestResponseWriterMultipleWriteHeader(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.WriteHeader(http.StatusCreated) // Should be ignored
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse completed request log
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	var logEntry map[string]any
	if err := json.Unmarshal(logs[1], &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	// Should capture first WriteHeader (200), not second (201)
	if status, ok := logEntry["status"].(float64); !ok || int(status) != http.StatusOK {
		t.Errorf("status = %v, want %d", logEntry["status"], http.StatusOK)
	}
}

// TestResponseWriterWritePassthrough verifies Write passes through correctly
func TestResponseWriterWritePassthrough(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	expectedBody := "test response body"
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(expectedBody))
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	if rec.Body.String() != expectedBody {
		t.Errorf("Response body = %s, want %s", rec.Body.String(), expectedBody)
	}
}

// TestLoggingMiddlewareDurationAccuracy verifies duration tracking
func TestLoggingMiddlewareDurationAccuracy(t *testing.T) {
	var buf bytes.Buffer
	logger := logging.NewLoggerWithOutput(&buf)

	mw := LoggingMiddleware(logger)

	sleepDuration := 50 * time.Millisecond
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(sleepDuration)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	wrapped.ServeHTTP(rec, req)

	// Parse completed request log
	logs := bytes.Split(buf.Bytes(), []byte("\n"))
	var logEntry map[string]any
	if err := json.Unmarshal(logs[1], &logEntry); err != nil {
		t.Fatalf("Failed to parse log JSON: %v", err)
	}

	durationMs, ok := logEntry["duration_ms"].(float64)
	if !ok {
		t.Fatal("duration_ms not found or wrong type")
	}

	// Duration should be close to the sleep duration, allowing for variance in both directions
	expectedMs := sleepDuration.Milliseconds()
	minExpected := expectedMs - 5  // 5ms lower tolerance
	maxExpected := expectedMs + 10 // 10ms upper tolerance
	if int64(durationMs) < minExpected || int64(durationMs) > maxExpected {
		t.Errorf("duration_ms = %v, want between %v and %v", durationMs, minExpected, maxExpected)
	}
}
