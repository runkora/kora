package middleware

import (
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// LoggingMiddleware creates middleware that logs HTTP requests with OTEL trace correlation.
// It extracts the request context which may contain an active OTEL span, enabling
// automatic trace_id and span_id attachment to log entries (per ADR-0004).
//
// Logs both the incoming request and the response status/duration.
func LoggingMiddleware(logger logging.Logger) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Extract request context - may contain OTEL span for trace correlation
			ctx := r.Context()

			// Log incoming request with trace context
			logger.Info(ctx, "Incoming request",
				logging.Field{Key: "method", Value: r.Method},
				logging.Field{Key: "path", Value: r.URL.Path},
				logging.Field{Key: "remote_addr", Value: r.RemoteAddr},
			)

			// Wrap response writer to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
			}

			// Call next handler
			next.ServeHTTP(wrapped, r)

			// Log response with duration and trace context
			duration := time.Since(start)
			logger.Info(ctx, "Request completed",
				logging.Field{Key: "method", Value: r.Method},
				logging.Field{Key: "path", Value: r.URL.Path},
				logging.Field{Key: "status", Value: wrapped.statusCode},
				logging.Field{Key: "duration_ms", Value: duration.Milliseconds()},
			)
		})
	})
}

// responseWriter wraps http.ResponseWriter to capture the status code
type responseWriter struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

// WriteHeader captures the status code before calling the underlying WriteHeader
func (rw *responseWriter) WriteHeader(code int) {
	if rw.wroteHeader {
		return
	}
	rw.statusCode = code
	rw.wroteHeader = true
	rw.ResponseWriter.WriteHeader(code)
}

// Write captures the implicit 200 status code when WriteHeader is not called explicitly
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.wroteHeader {
		// WriteHeader hasn't been called - Write will trigger implicit 200
		rw.WriteHeader(http.StatusOK)
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher by delegating to the underlying writer if it supports it.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
