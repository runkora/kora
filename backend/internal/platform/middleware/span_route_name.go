package middleware

import (
	"net/http"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// SpanRouteNameMiddleware names the request's span after the route it matched.
//
// TracingMiddleware runs before any routing, where the only pattern available
// is the outer mux's catch-all, so without this every span is named "GET /".
// The pattern is a template rather than a URL, so span names stay
// low-cardinality regardless of path parameters.
func SpanRouteNameMiddleware() Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			pattern, ok := RoutePatternFromContext(r.Context())
			if ok {
				span := trace.SpanFromContext(r.Context())
				span.SetName(r.Method + " " + pattern)
				span.SetAttributes(semconv.HTTPRoute(pattern))
			}
			next.ServeHTTP(w, r)
		})
	})
}
