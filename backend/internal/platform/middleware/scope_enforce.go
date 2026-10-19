package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ScopeEnforcementMiddleware enforces route → required-scope bindings declared
// by the service. Behavior mirrors `_route_match` in bundle/authz.rego and
// bundle/authz_gateway.rego:
//
//  1. Find routes whose PathPrefix covers the request path on a segment boundary.
//  2. Prefer a matching route whose Method equals the request method exactly.
//  3. Otherwise fall through to a matching route whose Method is "*".
//     Within either kind the longest PathPrefix wins.
//  4. If no route matches, deny — mirrors OPA's `deny:no_route_match`.
//  5. If a route matches with a non-empty RequiredScope, call
//     policy.Evaluator.Check with the resolved scope and deny on a non-allow
//     result.
//  6. If a route matches with an empty RequiredScope, pass through —
//     authentication is sufficient; no scope check is needed. Use this for
//     "any authenticated caller" routes (e.g. personal data endpoints).
//
// basePath is the server's base path (e.g. "/v1"). The middleware prepends it
// to r.URL.Path before route lookup so the route table can declare absolute
// paths consistent with what is registered with policy-service for Layer 1
// OPA enforcement. Pass "" for services that mount controllers at the root.
//
// This middleware MUST run after AuthMiddleware (which puts the verified
// platform claims in context) and DelegationMiddlewareWithBlocklist (which puts parsed
// delegation claims in context). See docs/adr/2026-06-04-platform-scope-enforcement-middleware.md for the design rationale.
func ScopeEnforcementMiddleware(eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// http.StripPrefix has already removed basePath from r.URL.Path by
			// the time this middleware runs (it is wrapped inside controllerMux,
			// which sits behind StripPrefix in platform/api.Server). Prepend
			// basePath so the route table can use the same absolute paths that
			// are registered with policy-service for Layer 1 enforcement.
			fullPath := basePath + r.URL.Path

			requiredScope, ok := ResolveRequiredScope(routes, r.Method, fullPath)
			if !ok {
				logger.Warn(ctx, "scope enforcement: no route match — deny",
					logging.Field{Key: "method", Value: r.Method},
					logging.Field{Key: "path", Value: fullPath},
				)
				writeForbidden(w, "no route match")
				return
			}
			if requiredScope == "" {
				// Route matched but is open to any authenticated caller.
				// AuthMiddleware has already run; no further check needed.
				next.ServeHTTP(w, r)
				return
			}

			claims, _ := token.ClaimsFromContext(ctx)

			// SYSTEM principals are platform services (orbit-agents, identity,
			// etc.) authenticated by SPIFFE/mTLS at the Istio layer. Their
			// platform tokens carry empty tenant_id since they are infrastructure
			// rather than tenant-scoped actors, so the per-tenant policy bundle
			// lookup would deny every request with "no policy configured for
			// tenant". The Istio AuthorizationPolicy + mTLS already restrict
			// which SPIFFE identities can reach each endpoint; in-process scope
			// enforcement is for human/agent/PAT principals, not platform infra.
			if claims != nil && claims.Kora.PrincipalType == token.PrincipalTypeSystem {
				next.ServeHTTP(w, r)
				return
			}

			resourceType, pathSegments, action := splitScope(requiredScope)
			result, err := eval.Check(ctx, policy.CheckInput{
				Claims:       claims,
				Delegation:   policy.DelegationFromContext(ctx),
				ResourceType: resourceType,
				PathSegments: pathSegments,
				Action:       action,
				// The stripped /run/{run_id} prefix (AGENT principals only) —
				// lets the engine deny a delegation token bound to another run
				// (delegated-agent-identity ADR, Decision 4).
				RunID: RunIDFromContext(ctx),
			})
			if err != nil {
				logger.Error(ctx, "scope enforcement: evaluator failed",
					logging.Field{Key: "error", Value: err.Error()},
					logging.Field{Key: "required_scope", Value: requiredScope},
				)
				writeForbidden(w, "policy unavailable")
				return
			}
			if !result.Allow {
				logger.Warn(ctx, "scope enforcement: denied",
					logging.Field{Key: "required_scope", Value: requiredScope},
					logging.Field{Key: "reason", Value: result.Reason},
				)
				writeForbidden(w, result.Reason)
				return
			}

			next.ServeHTTP(w, r)
		})
	})
}

// splitScope reverses policy.DeriveRequiredScope: it splits a scope string
// (e.g. "agents:types:write") into the resource-type, path-segments, and
// action that policy.CheckInput expects. The path-segments slice may be
// empty for short scopes like "inference:invoke".
//
// This is the inverse of DeriveRequiredScope and is here because the
// middleware receives a pre-built scope from the route table rather than
// resource attributes — policy.CheckInput's API is shaped for the resource
// caller (which can also feed ABAC attributes), so we split to fit it.
func splitScope(scope string) (resourceType string, pathSegments []string, action string) {
	parts := strings.Split(scope, ":")
	switch len(parts) {
	case 0:
		return "", nil, ""
	case 1:
		return parts[0], nil, ""
	case 2:
		return parts[0], nil, parts[1]
	default:
		return parts[0], parts[1 : len(parts)-1], parts[len(parts)-1]
	}
}

// pathMatchesPrefix reports whether path is covered by prefix, anchored to a path
// segment boundary. A bare strings.HasPrefix would let "/api/v1/runs" match an
// unrelated sibling like "/api/v1/runs-export"; requiring an exact match, a
// trailing-slash prefix, or a '/' at the boundary prevents that.
func pathMatchesPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if len(path) == len(prefix) || strings.HasSuffix(prefix, "/") {
		return true
	}
	return path[len(prefix)] == '/'
}

// ResolveRequiredScope mirrors _route_match in authz.rego and authz_gateway.rego.
// Exact-method matches beat wildcard ("*") matches, and among matches of the same
// kind the longest (most specific) PathPrefix wins — so resolution does not depend
// on route-table ordering. Returns the matched scope and true; returns "", false
// if no route matches the request.
//
// Exported so a binary can assert what its own route table actually resolves to
// without restating this rule. A test that reimplements it can agree with itself
// while disagreeing with enforcement, which is the failure mode that let a route
// silently inherit a broader prefix's scope in the first place.
func ResolveRequiredScope(routes []routescopes.RouteScope, method, path string) (string, bool) {
	var (
		bestScope   string
		bestLen     = -1
		bestMatched bool
		wildScope   string
		wildLen     = -1
		wildMatched bool
	)
	for _, r := range routes {
		if !pathMatchesPrefix(path, r.PathPrefix) {
			continue
		}
		switch {
		case r.Method == method && len(r.PathPrefix) > bestLen:
			bestScope, bestLen, bestMatched = r.RequiredScope, len(r.PathPrefix), true
		case r.Method == "*" && len(r.PathPrefix) > wildLen:
			wildScope, wildLen, wildMatched = r.RequiredScope, len(r.PathPrefix), true
		}
	}
	if bestMatched {
		return bestScope, true
	}
	return wildScope, wildMatched
}

// writeForbidden writes a minimal JSON 403 response. Does not import
// platform/api to avoid the api → middleware import cycle.
func writeForbidden(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, `{"code":"forbidden","message":%q}`, reason)
}
