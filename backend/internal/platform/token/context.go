package token

import (
	"cmp"
	"context"

	"go.opentelemetry.io/otel/trace"

	"github.com/runkora/kora/backend/internal/platform/observability"
)

// claimsContextKey is the unexported context key under which verified
// PlatformClaims are stored by AuthMiddleware. Unexported prevents collisions
// across packages.
type claimsContextKey struct{}

// WithClaims returns a child context carrying the verified platform claims, set by AuthMiddleware.
// A run already in ctx is the route's run and wins over the caller's run claim.
func WithClaims(ctx context.Context, claims *PlatformClaims) context.Context {
	if claims != nil {
		runID := cmp.Or(observability.RunFromContext(ctx), claims.Kora.Claims["run_id"])
		if claims.Kora.TenantID != "" {
			trace.SpanFromContext(ctx).SetAttributes(observability.TenantIDKey.String(claims.Kora.TenantID))
		}
		ctx = observability.WithTenant(observability.WithRun(ctx, runID), claims.Kora.TenantID)
	}
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext retrieves the verified PlatformClaims stored by AuthMiddleware.
// Returns nil, false if no claims have been stored (e.g. unauthenticated path).
func ClaimsFromContext(ctx context.Context) (*PlatformClaims, bool) {
	c, ok := ctx.Value(claimsContextKey{}).(*PlatformClaims)
	return c, ok
}
