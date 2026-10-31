package middleware

import (
	"cmp"
	"net/http"
	"strings"

	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// SpanRouteName names the request's span after the matched route template, with mountPath and a stripped run prefix added back.
// TracingMiddleware runs before routing and sees only the catch-all pattern.
func SpanRouteName(mountPath string) MiddlewareHandler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if pattern, ok := RoutePatternFromContext(r.Context()); ok {
				route := mountPath + pattern
				runID, ok := r.Context().Value(runIDContextKey{}).(string)
				if ok {
					route = "/run/{run_id}" + route
				}
				span := trace.SpanFromContext(r.Context())
				span.SetName(r.Method + " " + route)
				span.SetAttributes(semconv.HTTPRoute(route))
				if runID = cmp.Or(runID, routeRunID(r)); runID != "" {
					span.SetAttributes(observability.RunIDKey.String(runID))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// routeRunID reads the run from the route's path parameter, which the routes
// name runID, run_id, or id under /runs/.
func routeRunID(r *http.Request) string {
	if id := cmp.Or(r.PathValue("runID"), r.PathValue("run_id")); id != "" {
		return id
	}
	if strings.Contains(r.Pattern, "/runs/{id}") {
		return r.PathValue("id")
	}
	return ""
}
