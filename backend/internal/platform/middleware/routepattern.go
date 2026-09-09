package middleware

import (
	"context"
	"net/http"
)

type routePatternContextKey struct{}

// RoutePattern puts the route a request matched on its context, e.g.
// "/api/v1/pats/{id}". The Mux installs it outermost on every route it
// registers, so middleware can read the route instead of re-deriving it from
// the URL.
func RoutePattern(pattern string) MiddlewareHandler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(withRoutePattern(r.Context(), pattern)))
		})
	}
}

// RoutePatternFromContext returns the matched route pattern, and false when the
// request was served outside the Mux (the health endpoint, file servers).
func RoutePatternFromContext(ctx context.Context) (string, bool) {
	pattern, ok := ctx.Value(routePatternContextKey{}).(string)
	return pattern, ok
}

func withRoutePattern(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, routePatternContextKey{}, pattern)
}
