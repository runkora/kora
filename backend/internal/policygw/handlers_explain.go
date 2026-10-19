package policygw

import (
	"context"
	"net/http"
	"time"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// inspectOthersScope gates reading a principal other than the caller.
// docs/adr/2026-04-15-policy-administration-dashboard.md requires it on both
// cross-principal routes: a caller's own scopes stay readable without it, a
// second principal's do not.
const inspectOthersScope = "platform:grants:read"

// authorizeInspectOthers reports whether the caller may read another
// principal's authorization state. SYSTEM principals pass: they are platform
// services authenticated by SPIFFE at the mesh, carry no tenant, and would be
// denied by a per-tenant bundle lookup. This is the same carve-out
// ScopeEnforcementMiddleware makes.
func (c *Controller) authorizeInspectOthers(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims) bool {
	if claims.Orbit.PrincipalType == token.PrincipalTypeSystem {
		return true
	}

	result, err := c.svc.Check(ctx, claims, nil, "platform", []string{"grants"}, "read", nil)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return false
	}
	if !result.Allow {
		c.logger.Warn(ctx, "inspecting another principal denied",
			logging.Field{Key: "principal_id", Value: claims.Subject},
			logging.Field{Key: "required_scope", Value: inspectOthersScope},
			logging.Field{Key: "reason", Value: result.Reason},
		)
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"inspecting another principal requires "+inspectOthersScope, nil), nil)
		return false
	}
	return true
}

// handleScopesExplain handles GET /v1/policy/scopes/explain.
// Returns the principal's effective scopes broken down by source (grants vs roles).
// Accepts optional principal_type and principal_id query parameters to inspect
// another principal; when omitted, uses the authenticated caller's own identity.
func (c *Controller) handleScopesExplain(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "missing claims", nil), nil)
		return
	}

	// Build effective claims for the target principal.
	targetClaims := claims
	q := r.URL.Query()
	principalType := q.Get("principal_type")
	principalID := q.Get("principal_id")

	if principalType != "" {
		switch token.PrincipalType(principalType) {
		case token.PrincipalTypeAgent, token.PrincipalTypeHuman, token.PrincipalTypeService:
		default:
			platformapi.ErrorResponse(ctx, w, c.logger,
				platformapi.ValidationError(platformapi.ErrCodeInvalidField,
					"principal_type must be AGENT, HUMAN, or SERVICE", nil), nil)
			return
		}
	}

	if principalType != "" && principalID != "" {
		// If looking up yourself, use original claims (preserves OIDC roles etc).
		isSelf := principalID == claims.Subject &&
			token.PrincipalType(principalType) == claims.Orbit.PrincipalType
		if !isSelf {
			if !c.authorizeInspectOthers(ctx, w, claims) {
				return
			}
			// Inspecting another principal — build synthetic claims.
			// Note: synthetic claims cannot carry OIDC roles, so role-based
			// scopes will only resolve for grant-based principals.
			targetClaims = &token.PlatformClaims{}
			targetClaims.Subject = principalID
			targetClaims.Orbit.PrincipalType = token.PrincipalType(principalType)
			targetClaims.Orbit.TenantID = claims.Orbit.TenantID
		}
	}

	result, err := c.svc.ScopesExplain(ctx, targetClaims)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, ScopesExplainResponse{
		PrincipalType:   string(targetClaims.Orbit.PrincipalType),
		PrincipalID:     targetClaims.Subject,
		TenantID:        targetClaims.Orbit.TenantID,
		FromRoles:       result.FromRoles,
		FromGrants:      result.FromGrants,
		EffectiveScopes: result.EffectiveScopes,
		EvaluatedAt:     time.Now().UTC().Format(time.RFC3339),
	})
}

// handleCheckSimulate handles POST /v1/policy/check/simulate.
// Evaluates a policy decision for a specified principal without requiring
// their JWT. The caller must be authenticated and the simulated principal
// is evaluated within the caller's tenant.
func (c *Controller) handleCheckSimulate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "missing claims", nil), nil)
		return
	}

	var req CheckSimulateRequest
	if !platformapi.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.PrincipalType == "" || req.PrincipalID == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"principal_type and principal_id are required", nil), nil)
		return
	}

	if req.Resource.Type == "" || req.Action == "" {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ValidationError(platformapi.ErrCodeMissingField,
				"resource.type and action are required", nil), nil)
		return
	}

	// Simulation always names a principal and a role set, so there is no
	// self case to exempt.
	if !c.authorizeInspectOthers(ctx, w, claims) {
		return
	}

	result, err := c.svc.CheckSimulate(ctx, claims,
		req.PrincipalType, req.PrincipalID, req.Roles,
		req.Resource.Type, req.Resource.PathSegments, req.Action, req.Resource.Attributes)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, CheckResponse{Allow: result.Allow, Reason: result.Reason})
}
