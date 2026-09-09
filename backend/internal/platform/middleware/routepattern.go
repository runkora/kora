package middleware

import "context"

type routePatternKey struct{}

// WithRoutePattern carries the route pattern a request matched, e.g.
// "/api/v1/pats/{id}". The Mux sets it once the pattern is known; middleware
// registered on the route can then read it instead of re-deriving the route
// from the URL.
func WithRoutePattern(ctx context.Context, pattern string) context.Context {
	return context.WithValue(ctx, routePatternKey{}, pattern)
}

// RoutePatternFromContext returns the matched route pattern, and false when the
// request was served outside the Mux (the health endpoint, file servers).
func RoutePatternFromContext(ctx context.Context) (string, bool) {
	pattern, ok := ctx.Value(routePatternKey{}).(string)
	return pattern, ok
}
