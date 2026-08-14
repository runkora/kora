package routescopes

// FilterScopedRoutes returns the subset of routes that require a non-empty
// scope.
//
// Routes with an empty RequiredScope are "authenticated-only": the local
// ScopeEnforcementMiddleware passes them through after auth, and the service
// layer holds the real check (typically because the governing scope depends on
// request data a static route table cannot see). Registering them with the
// policy service would compile an empty required scope into the OPA bundle and
// deny everyone once Layer 1 sidecar enforcement is on, so they are dropped
// before registration.
//
// Shared by every binary that registers a route table, so the two enforcement
// layers cannot disagree about which routes are policy-governed.
func FilterScopedRoutes(in []RouteScope) []RouteScope {
	out := make([]RouteScope, 0, len(in))
	for _, r := range in {
		if r.RequiredScope != "" {
			out = append(out, r)
		}
	}
	return out
}
