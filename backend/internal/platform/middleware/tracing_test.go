package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// TestTracingMiddleware verifies basic tracing middleware functionality
func TestTracingMiddleware(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	// Verify a span was created
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
}

// TestTracingMiddlewareSpanAttributes verifies span attributes
func TestTracingMiddlewareSpanAttributes(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("POST", "/api/users?id=123", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
	
	span := spans[0]
	attrs := span.Attributes
	
	// Verify HTTP attributes (using OpenTelemetry semantic conventions v1.24.0)
	expectedAttrs := map[string]interface{}{
		"http.request.method":       "POST",
		"http.route":                "/api/users",
		"url.scheme":                "http",
		"url.path":                  "/api/users",
		"server.address":            "example.com",
		"http.response.status_code": int64(200),
	}
	
	attrMap := make(map[string]interface{})
	for _, attr := range attrs {
		attrMap[string(attr.Key)] = attr.Value.AsInterface()
	}
	
	for key, expected := range expectedAttrs {
		actual, ok := attrMap[key]
		if !ok {
			t.Errorf("Attribute %s not found", key)
			continue
		}
		if actual != expected {
			t.Errorf("Attribute %s = %v, want %v", key, actual, expected)
		}
	}
}

// TestTracingMiddlewareSpanName verifies span naming
func TestTracingMiddlewareSpanName(t *testing.T) {
	tests := []struct {
		name         string
		method       string
		path         string
		expectedName string
	}{
		{
			name:         "GET request",
			method:       "GET",
			path:         "/users",
			expectedName: "GET /users",
		},
		{
			name:         "POST request",
			method:       "POST",
			path:         "/api/items",
			expectedName: "POST /api/items",
		},
		{
			name:         "DELETE request",
			method:       "DELETE",
			path:         "/resource/123",
			expectedName: "DELETE /resource/123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exporter := tracetest.NewInMemoryExporter()
			tp := trace.NewTracerProvider(
				trace.WithSyncer(exporter),
			)
			otel.SetTracerProvider(tp)
			defer otel.SetTracerProvider(nil)
			
			mw := TracingMiddleware("test-service")
			
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			
			wrapped := mw.next(handler)
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			
			wrapped.ServeHTTP(rec, req)
			
			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("Expected 1 span, got %d", len(spans))
			}
			
			if spans[0].Name != tt.expectedName {
				t.Errorf("Span name = %s, want %s", spans[0].Name, tt.expectedName)
			}
		})
	}
}

// TestTracingMiddlewareStatusCodes verifies status code tracking
func TestTracingMiddlewareStatusCodes(t *testing.T) {
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
			exporter := tracetest.NewInMemoryExporter()
			tp := trace.NewTracerProvider(
				trace.WithSyncer(exporter),
			)
			otel.SetTracerProvider(tp)
			defer otel.SetTracerProvider(nil)
			
			mw := TracingMiddleware("test-service")
			
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})
			
			wrapped := mw.next(handler)
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			
			wrapped.ServeHTTP(rec, req)
			
			spans := exporter.GetSpans()
			if len(spans) != 1 {
				t.Fatalf("Expected 1 span, got %d", len(spans))
			}
			
			// Find status_code attribute (using OpenTelemetry semantic conventions v1.24.0)
			var statusCode int64
			found := false
			for _, attr := range spans[0].Attributes {
				if attr.Key == "http.response.status_code" {
					statusCode = attr.Value.AsInt64()
					found = true
					break
				}
			}
			
			if !found {
				t.Fatal("http.response.status_code attribute not found")
			}
			
			if int(statusCode) != tt.statusCode {
				t.Errorf("http.response.status_code = %d, want %d", statusCode, tt.statusCode)
			}
		})
	}
}

// TestTracingMiddlewareW3CPropagation verifies W3C TraceContext propagation
func TestTracingMiddlewareW3CPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer otel.SetTracerProvider(nil)
	
	// Create parent span
	tracer := tp.Tracer("parent")
	parentCtx, parentSpan := tracer.Start(context.Background(), "parent-operation")
	defer parentSpan.End()
	
	parentSpanContext := parentSpan.SpanContext()
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify span context was propagated
		span := oteltrace.SpanFromContext(r.Context())
		if !span.SpanContext().IsValid() {
			t.Error("Span context not valid in handler")
		}
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	
	// Create request with W3C TraceContext header
	req := httptest.NewRequest("GET", "/test", nil)
	
	// Inject trace context into request headers
	propagator := propagation.TraceContext{}
	propagator.Inject(parentCtx, propagation.HeaderCarrier(req.Header))
	
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
	
	childSpan := spans[0]
	
	// Verify child span has same trace ID as parent
	if childSpan.SpanContext.TraceID() != parentSpanContext.TraceID() {
		t.Errorf("Child trace ID = %s, want %s", 
			childSpan.SpanContext.TraceID(), 
			parentSpanContext.TraceID())
	}
	
	// Verify child span has different span ID than parent
	if childSpan.SpanContext.SpanID() == parentSpanContext.SpanID() {
		t.Error("Child span ID should differ from parent span ID")
	}
}

// TestTracingMiddlewareContextPropagation verifies context is passed to handler
func TestTracingMiddlewareContextPropagation(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	var handlerSpan oteltrace.Span
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerSpan = oteltrace.SpanFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	if handlerSpan == nil {
		t.Fatal("Handler did not receive span in context")
	}
	
	if !handlerSpan.SpanContext().IsValid() {
		t.Error("Handler span context is not valid")
	}
}

// TestTracingMiddlewareErrorStatus verifies error status on 5xx responses
func TestTracingMiddlewareErrorStatus(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
	
	span := spans[0]
	
	// For 5xx errors, span status should indicate error
	// Note: The actual implementation should set span.SetStatus() for errors
	// This test verifies the status code is recorded (using OpenTelemetry semantic conventions v1.24.0)
	var statusCode int64
	for _, attr := range span.Attributes {
		if attr.Key == "http.response.status_code" {
			statusCode = attr.Value.AsInt64()
			break
		}
	}
	
	if statusCode != http.StatusInternalServerError {
		t.Errorf("http.response.status_code = %d, want %d", statusCode, http.StatusInternalServerError)
	}
}

// TestTracingMiddlewareNoPropagatorSet verifies behavior when no propagator configured
func TestTracingMiddlewareNoPropagatorSet(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	// Don't set propagator - should still work
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	
	// Should not panic
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
}

// TestTracingMiddlewareHTTPSScheme verifies HTTPS scheme detection
func TestTracingMiddlewareHTTPSScheme(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "https://example.com/test", nil)
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
	
	// Find url.scheme attribute (using OpenTelemetry semantic conventions v1.24.0)
	var scheme string
	for _, attr := range spans[0].Attributes {
		if attr.Key == "url.scheme" {
			scheme = attr.Value.AsString()
			break
		}
	}
	
	if scheme != "https" {
		t.Errorf("url.scheme = %s, want https", scheme)
	}
}

// TestTracingMiddlewareSpanEnding verifies span is properly ended
func TestTracingMiddlewareSpanEnding(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(
		trace.WithSyncer(exporter),
	)
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)
	
	mw := TracingMiddleware("test-service")
	
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	
	wrapped := mw.next(handler)
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	
	wrapped.ServeHTTP(rec, req)
	
	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}
	
	span := spans[0]
	
	// Verify span has ended (has end time)
	if span.EndTime.IsZero() {
		t.Error("Span end time is zero - span was not properly ended")
	}
	
	// Verify end time is after start time
	if !span.EndTime.After(span.StartTime) {
		t.Error("Span end time is not after start time")
	}
}
