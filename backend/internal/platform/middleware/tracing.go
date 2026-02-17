package middleware

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates middleware that handles OpenTelemetry distributed tracing.
// It extracts trace context from incoming request headers, creates a span for the request,
// and injects the span into the request context for downstream handlers and logging.
//
// This middleware should be applied BEFORE LoggingMiddleware so that logs automatically
// include trace_id and span_id for correlation.
//
// Trace context propagation follows W3C TraceContext standard (configured in observability.Setup).
func TracingMiddleware(serviceName string) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get the global tracer for this service
			tracer := otel.Tracer(serviceName)

			// Extract the route pattern from the request context
			// Go 1.22+ ServeMux stores the matched pattern in the request context
			routePattern := r.Pattern
			if routePattern == "" {
				// Fallback to URL path if pattern is not available
				routePattern = r.URL.Path
			}

			// Extract trace context from incoming request headers (W3C TraceContext format)
			// Then start a new span for this HTTP request
			// The span name follows OpenTelemetry semantic conventions: "HTTP {method}"
			propagatedCtx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			tracedCtx, span := tracer.Start(propagatedCtx, r.Method+" "+routePattern,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.HTTPRouteKey.String(routePattern),
					semconv.URLScheme(schemeFromRequest(r)),
					semconv.URLPath(r.URL.Path),
					semconv.ServerAddress(r.Host),
					semconv.UserAgentOriginal(r.UserAgent()),
				),
			)
			defer span.End()

			// Inject the span into the request context
			// This makes trace_id and span_id available to:
			// - Logging middleware (automatic correlation)
			// - Business logic (manual span creation)
			// - Downstream HTTP clients (propagation)
			r = r.WithContext(tracedCtx)

			// Wrap response writer to capture status code for span attributes
			wrapped := &tracingResponseWriter{
				ResponseWriter: w,
			}

			// Call next handler with traced context
			next.ServeHTTP(wrapped, r)

			// Record response status in span
			span.SetAttributes(semconv.HTTPResponseStatusCode(wrapped.statusCode))

			// Mark span as error only for server errors (5xx), not client errors (4xx).
			// Client errors indicate issues with the request, not failures in our service.
			// This aligns with OpenTelemetry semantic conventions for HTTP server spans.
			if wrapped.statusCode >= 500 {
				span.SetStatus(codes.Error, "HTTP server error")
			}
		})
	})
}

// tracingResponseWriter wraps http.ResponseWriter to capture status code for tracing
type tracingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *tracingResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *tracingResponseWriter) Write(b []byte) (int, error) {
	// If WriteHeader hasn't been called, default to 200
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

// schemeFromRequest determines the URL scheme from the request
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
