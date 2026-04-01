package token

import "context"

// claimsContextKey is the unexported context key under which verified
// PlatformClaims are stored by AuthMiddleware. Unexported prevents collisions
// across packages.
type claimsContextKey struct{}

// WithClaims returns a child context carrying the verified platform claims.
// Called by AuthMiddleware after successful JWT verification.
func WithClaims(ctx context.Context, claims *PlatformClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext retrieves the verified PlatformClaims stored by AuthMiddleware.
// Returns nil, false if no claims have been stored (e.g. unauthenticated path).
func ClaimsFromContext(ctx context.Context) (*PlatformClaims, bool) {
	c, ok := ctx.Value(claimsContextKey{}).(*PlatformClaims)
	return c, ok
}
