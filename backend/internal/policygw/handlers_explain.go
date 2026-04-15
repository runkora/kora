package policygw

import (
	"net/http"
	"time"

	platformapi "github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/token"
)

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

	if principalType != "" && principalID != "" {
		// If looking up yourself, use original claims (preserves OIDC roles etc).
		isSelf := principalID == claims.Subject &&
			token.PrincipalType(principalType) == claims.Orbit.PrincipalType
		if !isSelf {
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

	result, err := c.svc.CheckSimulate(ctx, claims,
		req.PrincipalType, req.PrincipalID, req.Roles,
		req.Resource.Type, req.Resource.PathSegments, req.Action, req.Resource.Attributes)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger, evaluationErrorResponse(err), err)
		return
	}

	platformapi.JSON(ctx, w, c.logger, http.StatusOK, CheckResponse{Allow: result.Allow, Reason: result.Reason})
}
