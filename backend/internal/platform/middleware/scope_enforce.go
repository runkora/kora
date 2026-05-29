package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ScopeEnforcementMiddleware enforces route → required-scope bindings declared
// by the service. Behavior mirrors the OPA `_required_scope` rule in
// bundle/authz.rego:
//
//  1. Find routes whose PathPrefix matches the request path (prefix match).
//  2. Prefer a matching route whose Method equals the request method exactly.
//  3. Otherwise fall through to a matching route whose Method is "*".
//  4. If no route matches, deny — mirrors OPA's `deny:no_route_match`.
//  5. If a route matches, call policy.Evaluator.Check with the resolved scope
//     and deny on a non-allow result.
//
// This middleware MUST run after AuthMiddleware (which puts the verified
// platform claims in context) and DelegationMiddleware (which puts parsed
// delegation claims in context). See ADR-0064 for the design rationale.
func ScopeEnforcementMiddleware(eval policy.Evaluator, routes []routescopes.RouteScope, logger logging.Logger) Middleware {
	return NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			requiredScope, ok := resolveRequiredScope(routes, r.Method, r.URL.Path)
			if !ok {
				logger.Warn(ctx, "scope enforcement: no route match — deny",
					logging.Field{Key: "method", Value: r.Method},
					logging.Field{Key: "path", Value: r.URL.Path},
				)
				writeForbidden(w, "no route match")
				return
			}

			claims, _ := token.ClaimsFromContext(ctx)
			resourceType, pathSegments, action := splitScope(requiredScope)
			result, err := eval.Check(ctx, policy.CheckInput{
				Claims:       claims,
				Delegation:   policy.DelegationFromContext(ctx),
				ResourceType: resourceType,
				PathSegments: pathSegments,
				Action:       action,
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

// resolveRequiredScope mirrors authz.rego's _required_scope derivation.
// Exact-method matches beat wildcard ("*") matches. Returns the matched scope
// and true; returns "", false if no route matches the request.
func resolveRequiredScope(routes []routescopes.RouteScope, method, path string) (string, bool) {
	var (
		wildcardScope   string
		wildcardMatched bool
	)
	for _, r := range routes {
		if !strings.HasPrefix(path, r.PathPrefix) {
			continue
		}
		if r.Method == method {
			return r.RequiredScope, true
		}
		if r.Method == "*" && !wildcardMatched {
			wildcardScope = r.RequiredScope
			wildcardMatched = true
		}
	}
	return wildcardScope, wildcardMatched
}

// writeForbidden writes a minimal JSON 403 response. Does not import
// platform/api to avoid the api → middleware import cycle.
func writeForbidden(w http.ResponseWriter, reason string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, _ = fmt.Fprintf(w, `{"code":"forbidden","message":%q}`, reason)
}
