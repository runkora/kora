package middleware

import (
	"net/http"

	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// SpanRouteName names the request's span after the matched route template, with mountPath and a stripped run prefix added back.
// TracingMiddleware runs before routing and sees only the catch-all pattern.
func SpanRouteName(mountPath string) MiddlewareHandler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pattern, ok := RoutePatternFromContext(r.Context()); ok {
				route := mountPath + pattern
				if _, ok := r.Context().Value(runIDContextKey{}).(string); ok {
					route = "/run/{run_id}" + route
				}
				span := trace.SpanFromContext(r.Context())
				span.SetName(r.Method + " " + route)
				span.SetAttributes(semconv.HTTPRoute(route))
			}
			next.ServeHTTP(w, r)
		})
	}
}
