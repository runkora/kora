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

			// Extract trace context from incoming request headers (W3C TraceContext format)
			// Then start a new span for this HTTP request
			// The span name follows OpenTelemetry semantic conventions: "HTTP {method}"
			propagatedCtx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			tracedCtx, span := tracer.Start(propagatedCtx, r.Method+" "+r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.HTTPRouteKey.String(r.URL.Path),
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
				statusCode:     http.StatusOK,
			}

			// Call next handler with traced context
			next.ServeHTTP(wrapped, r)

			// Record response status in span
			span.SetAttributes(semconv.HTTPResponseStatusCode(wrapped.statusCode))

			// Mark span as error if status code indicates failure
			if wrapped.statusCode >= 400 {
				span.SetStatus(codes.Error, "HTTP error")
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

// schemeFromRequest determines the scheme (http/https) from the request
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}
