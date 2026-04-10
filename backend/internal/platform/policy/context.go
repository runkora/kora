package policy

import "context"

type delegationContextKey struct{}

// WithDelegation returns a child context carrying parsed delegation claims.
// Called by DelegationMiddleware after extracting the X-Orbit-Delegation-Token header.
func WithDelegation(ctx context.Context, d *DelegationClaims) context.Context {
	return context.WithValue(ctx, delegationContextKey{}, d)
}

// DelegationFromContext retrieves delegation claims stored by DelegationMiddleware.
// Returns nil if no delegation token was present on the request.
func DelegationFromContext(ctx context.Context) *DelegationClaims {
	d, _ := ctx.Value(delegationContextKey{}).(*DelegationClaims)
	return d
}
