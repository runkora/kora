package middleware

import (
	"net"
	"net/http"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
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
//
// A request any filter rejects is served without a span, like otelhttp.WithFilter.
// Services use it to drop high-rate polls that carry no signal, the same way
// health checks skip tracing.
func TracingMiddleware(filters ...TraceFilter) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, traced := range filters {
				if !traced(r) {
					next.ServeHTTP(w, r)
					return
				}
			}
			// Get the global tracer for this service
			tracer := otel.Tracer("github.com/runkora/kora/backend/internal/platform/middleware")

			// Extract the route pattern from the request context
			// Go 1.22+ ServeMux stores the matched pattern in the request context
			routePattern := r.Pattern
			if routePattern == "" {
				// Fallback to URL path if pattern is not available
				routePattern = r.URL.Path
			}

			// Extract trace context from incoming request headers (W3C TraceContext format)
			// Then start a new span for this HTTP request, named "{method} {route}"
			// per the HTTP semantic conventions; SpanRouteName renames it once a route matches.
			propagatedCtx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Build span attributes following OpenTelemetry semantic conventions
			attrs := []trace.SpanStartOption{
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					semconv.HTTPRequestMethodKey.String(r.Method),
					semconv.HTTPRouteKey.String(routePattern),
					semconv.URLScheme(schemeFromRequest(r)),
					semconv.URLPath(r.URL.Path),
					semconv.UserAgentOriginal(r.UserAgent()),
				),
			}

			// Split server.address and server.port per semantic conventions.
			// Only emit server.port when parsing succeeds to avoid zero-value pollution.
			if host, port, err := net.SplitHostPort(r.Host); err == nil {
				attrs = append(attrs, trace.WithAttributes(semconv.ServerAddress(host)))
				if p := mustParsePort(port); p != -1 {
					attrs = append(attrs, trace.WithAttributes(semconv.ServerPort(p)))
				}
			} else {
				// No port in Host header, use default based on scheme
				attrs = append(attrs, trace.WithAttributes(semconv.ServerAddress(r.Host)))
			}

			// Add client peer attributes from RemoteAddr.
			// Only emit client.port when parsing succeeds to avoid zero-value pollution.
			if clientHost, clientPort, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				attrs = append(attrs, trace.WithAttributes(semconv.ClientAddress(clientHost)))
				if p := mustParsePort(clientPort); p != -1 {
					attrs = append(attrs, trace.WithAttributes(semconv.ClientPort(p)))
				}
			}

			tracedCtx, span := tracer.Start(propagatedCtx, r.Method+" "+routePattern, attrs...)
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
			// If no status was set, default to 200 OK
			statusCode := wrapped.statusCode
			if statusCode == 0 {
				statusCode = http.StatusOK
			}
			span.SetAttributes(semconv.HTTPResponseStatusCode(statusCode))

			// Mark span as error only for server errors (5xx), not client errors (4xx).
			// Client errors indicate issues with the request, not failures in our service.
			// This aligns with OpenTelemetry semantic conventions for HTTP server spans.
			if statusCode >= 500 {
				span.SetStatus(codes.Error, "HTTP server error")
			}
		})
	})
}

// TraceFilter reports whether TracingMiddleware should trace a request.
type TraceFilter func(*http.Request) bool

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

// Flush implements http.Flusher by delegating to the underlying writer if it supports it.
// Required so that SSE and streaming responses can flush data through the middleware chain.
func (w *tracingResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// schemeFromRequest determines the URL scheme from the request
func schemeFromRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

// mustParsePort converts a port string to int.
// Returns -1 if parsing fails so callers can omit the attribute entirely,
// avoiding zero-value pollution in traces.
func mustParsePort(portStr string) int {
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return -1
	}
	return port
}
