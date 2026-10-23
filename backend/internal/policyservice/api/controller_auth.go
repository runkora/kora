package api

import (
	"context"
	"net/http"
	"strings"

	platformapi "github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
)

// claimsFromContext retrieves the verified platform claims stored by AuthMiddleware.
// AuthMiddleware is applied at controller level so claims are always present —
// this is a safety net against misconfiguration.
func (c *Controller) claimsFromContext(w http.ResponseWriter, r *http.Request) (*token.PlatformClaims, bool) {
	ctx := r.Context()
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.UnauthorizedError(platformapi.ErrCodeUnauthorized, "platform token required", nil), nil)
		return nil, false
	}
	return claims, true
}

// resolveTenantID returns the effective tenant ID for a request.
//
// SYSTEM principals are trusted to supply any tenant ID (cross-tenant admin
// operations). All other principals are always scoped to the tenant in their
// token — the supplied value is ignored and a 403 is returned if the caller
// explicitly passes a different tenant ID.
func (c *Controller) resolveTenantID(w http.ResponseWriter, r *http.Request, claims *token.PlatformClaims, supplied string) (string, bool) {
	ctx := r.Context()
	if claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return supplied, true
	}
	if supplied != "" && supplied != claims.Kora.TenantID {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"tenant_id does not match your token", nil), nil)
		return "", false
	}
	return claims.Kora.TenantID, true
}

// enforceScopeCeiling implements the grant-administration anti-escalation invariant
// (2026-06-30 amendment to the authorisation model): a non-SYSTEM caller may assign,
// in a grant or role, only scopes that are covered by its own effective scopes.
// SYSTEM principals (e.g. kora-agents provisioning agent grants) are exempt.
// Returns true if the request may proceed; otherwise it has written the error
// response (403 naming the rejected scopes, or 500 if the caller's scopes can't be
// resolved) and the caller must return.
//
// A delegated run is refused outright. Its ceiling would be the agent's own grant,
// which can exceed the person's access and would outlive the run.
func (c *Controller) enforceScopeCeiling(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, scopes []string) bool {
	if claims.Kora.PrincipalType == token.PrincipalTypeSystem {
		return true
	}
	if c.refuseDelegated(ctx, w) {
		return false
	}

	callerScopes, err := c.callerEffectiveScopes(ctx, claims)
	if err != nil {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.InternalError(platformapi.ErrCodeInternalError, "failed to resolve caller scopes", nil), err)
		return false
	}

	if rejected := uncoveredScopes(callerScopes, scopes); len(rejected) > 0 {
		platformapi.ErrorResponse(ctx, w, c.logger,
			platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
				"cannot grant scopes you do not hold: "+strings.Join(rejected, ", "), nil), nil)
		return false
	}
	return true
}

// callerEffectiveScopes resolves the effective scopes of the principal in claims.
// Roles count for HUMAN callers only; EffectiveScopes ignores them otherwise.
func (c *Controller) callerEffectiveScopes(ctx context.Context, claims *token.PlatformClaims) ([]string, error) {
	var roles []string
	if raw := claims.Kora.Claims["roles"]; raw != "" {
		for role := range strings.SplitSeq(raw, ",") {
			if t := strings.TrimSpace(role); t != "" {
				roles = append(roles, t)
			}
		}
	}
	return c.svc.EffectiveScopes(ctx, claims.Kora.TenantID, string(claims.Kora.PrincipalType), grantPrincipalID(claims), roles)
}

// grantPrincipalID is the principal ID grants are keyed on. A PAT's sub is the
// issuing user, kept for attribution; its grant is keyed on the PAT ID. A PAT
// without one resolves to no grant, as at the edge.
func grantPrincipalID(claims *token.PlatformClaims) string {
	if claims.Kora.PrincipalType == token.PrincipalTypePAT {
		return claims.Kora.PatID
	}
	return claims.Subject
}

// uncoveredScopes returns the scopes in `requested` that are not covered by any
// scope in `held` (per the kora scope hierarchy). Empty result means every
// requested scope is within the holder's authority.
func uncoveredScopes(held, requested []string) []string {
	var out []string
	for _, s := range requested {
		if !policy.AnyScopeCovers(held, s) {
			out = append(out, s)
		}
	}
	return out
}

// refuseDelegated writes 403 for a delegated run, which cannot make any policy
// write: grants, roles or budgets. Returns true when it refused.
func (c *Controller) refuseDelegated(ctx context.Context, w http.ResponseWriter) bool {
	if policy.DelegationFromContext(ctx) == nil {
		return false
	}
	platformapi.ErrorResponse(ctx, w, c.logger,
		platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
			"a delegated run cannot change policy", nil), nil)
	return true
}

// refuseNonHumanGrantWrite writes 403 when a caller that is not SYSTEM writes
// (creates, deletes or sets the budget of) a grant whose principal type is not
// HUMAN. AGENT, SERVICE and PAT grants belong to the platform components that
// own those identities (kora-agents, kora-triggers, identity), which present
// SYSTEM; a tenant admin or a SERVICE principal is refused. Returns true when
// it refused and has written the response.
//
// It fails closed: an unrecognized principal type from a non-SYSTEM caller gets
// 403 as well, not 400, so the check never depends on the type being valid.
func (c *Controller) refuseNonHumanGrantWrite(ctx context.Context, w http.ResponseWriter, claims *token.PlatformClaims, pt grants.PrincipalType) bool {
	if claims.Kora.PrincipalType == token.PrincipalTypeSystem || pt == grants.PrincipalTypeHuman {
		return false
	}
	platformapi.ErrorResponse(ctx, w, c.logger,
		platformapi.ForbiddenError(platformapi.ErrCodeForbidden,
			"grants for AGENT, SERVICE and PAT principals can only be written by a SYSTEM caller", nil), nil)
	return true
}
