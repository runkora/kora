package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/runkora/kora/backend/internal/platform/metrics/buckets"
)

// instrumentServiceName strips a leading "orbit-" from the configured
// observability service name before it's used to build an instrument name.
// Some services configure "orbit-<name>" (e.g. "orbit-identity"), others just
// "<name>" (e.g. "policy-gateway"); without normalizing, the former doubles up
// into "orbit.orbit-identity...", breaking the PRD's uniform
// orbit_<service>_... naming contract.
func instrumentServiceName(serviceName string) string {
	return strings.TrimPrefix(serviceName, "orbit-")
}

// MetricsMiddleware records one counter and one histogram per request:
// orbit.<service>.http.requests and orbit.<service>.http.request.duration.
// route comes from RoutePatternFromContext — the same low-cardinality pattern
// TracingMiddleware uses for span names — never the raw path, so this cannot
// be driven to unbounded cardinality by request-smuggled path segments (e.g.
// PAT IDs).
//
// Must run INSIDE the per-route chain, after RoutePattern has set the route
// on the request context — api.Mux applies it there automatically. Placing
// this in the global MiddlewareChain instead (outside the Mux's own dispatch)
// means the context carrying the route is a copy local to the Mux's inner
// call stack and never visible here, so every request would record
// route="unmatched".
func MetricsMiddleware(meter metric.Meter, serviceName string) (Middleware, error) {
	name := instrumentServiceName(serviceName)
	requests, err := meter.Int64Counter(
		fmt.Sprintf("orbit.%s.http.requests", name),
		metric.WithDescription("HTTP requests served"),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return Middleware{}, fmt.Errorf("create http request counter: %w", err)
	}
	duration, err := meter.Float64Histogram(
		fmt.Sprintf("orbit.%s.http.request.duration", name),
		metric.WithDescription("HTTP request wall time"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(buckets.Default...),
	)
	if err != nil {
		return Middleware{}, fmt.Errorf("create http request duration histogram: %w", err)
	}

	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			wrapped := &metricsResponseWriter{ResponseWriter: w}
			next.ServeHTTP(wrapped, r)

			route, ok := RoutePatternFromContext(r.Context())
			if !ok {
				route = "unmatched"
			}

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
// for the request counter/histogram labels. Only the first WriteHeader call
// is honored, matching net/http's own behavior: a handler that writes a
// final status and then (incorrectly) calls WriteHeader again would otherwise
// have this metric report the later, superfluous status instead of the one
// actually sent to the client.
type metricsResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *metricsResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
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
