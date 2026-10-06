package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// MetricsMiddleware records one counter and one histogram per request:
// orbit.<service>.http.requests and orbit.<service>.http.request.duration.
// route comes from RoutePatternFromContext — the same low-cardinality pattern
// TracingMiddleware uses for span names — never the raw path, so this cannot
// be driven to unbounded cardinality by request-smuggled path segments (e.g.
// PAT IDs).
//
// Must run after the Mux's RoutePattern middleware. Place alongside
// TracingMiddleware/LoggingMiddleware in MiddlewareChain.
func MetricsMiddleware(meter metric.Meter, serviceName string) (Middleware, error) {
	requests, err := meter.Int64Counter(
		fmt.Sprintf("orbit.%s.http.requests", serviceName),
		metric.WithDescription("HTTP requests served"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return Middleware{}, fmt.Errorf("create http request counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		fmt.Sprintf("orbit.%s.http.request.duration", serviceName),
		metric.WithDescription("HTTP request wall time"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return Middleware{}, fmt.Errorf("create http request duration histogram: %w", err)
	}

	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			route, ok := RoutePatternFromContext(r.Context())
			if !ok {
				route = "unmatched"
			}
			wrapped := &metricsResponseWriter{ResponseWriter: w}
			next.ServeHTTP(wrapped, r)

			statusCode := wrapped.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}

			attrs := metric.WithAttributes(
				attribute.String("method", r.Method),
				attribute.String("route", route),
				attribute.String("status", strconv.Itoa(statusCode)),
			)
			requests.Add(r.Context(), 1, attrs)
			duration.Record(r.Context(), time.Since(start).Seconds(), attrs)
		})
	}), nil
}

// metricsResponseWriter wraps http.ResponseWriter to capture the status code
// for the request counter/histogram labels.
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

// Flush implements http.Flusher by delegating to the underlying writer if it
// supports it. Required so SSE/streaming routes can flush through this
// middleware.
func (w *metricsResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
