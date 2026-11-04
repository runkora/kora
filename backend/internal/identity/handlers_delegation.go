package identity

import (
	"errors"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// Delegation issuer endpoints (docs/adr/2026-07-23-delegated-agent-identity.md,
// Decision 3). POST / GET / DELETE authenticate with a platform bearer token
// (PATs allowed — a PAT counts as the person). GET /delegation/run/{run_id}
// authenticates like POST /identity/exchange: by the caller's mesh-attested
// SPIFFE identity from the XFCC header, because its caller (the
// identity-proxy) holds no bearer token of its own.

// errCodeDelegationNotFound is the distinct error code for "no active
// delegation issuance for this run" — the identity-proxy treats it as
// "autonomous, none" rather than a failure.
const errCodeDelegationNotFound = "delegation_not_found"

// handleCreateDelegation handles POST /identity/delegation.
// Mints a delegation issuance for a run. The response carries the issuance
// record, never a token.
func (c *Controller) handleCreateDelegation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	var req CreateDelegationRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.RunID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "run_id is required", map[string]any{"field": "run_id"}), nil)
		return
	}
	if req.AgentPrincipalID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "agent_principal_id is required", map[string]any{"field": "agent_principal_id"}), nil)
		return
	}
	if !strings.HasPrefix(req.AgentPrincipalID, "spiffe://") {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, "agent_principal_id must be a SPIFFE URI", map[string]any{"field": "agent_principal_id"}), nil)
		return
	}

	issuance, err := c.svc.MintDelegation(ctx, claims, req)
	if err != nil {
		switch {
		case errors.Is(err, ErrDelegationPrincipalType):
			api.ErrorResponse(ctx, w, c.logger,
				api.ForbiddenError(api.ErrCodeForbidden,
					"delegations can only be minted by HUMAN or PAT principals; AGENT, SERVICE, and SYSTEM callers cannot delegate",
					map[string]any{"principal_type": string(claims.Kora.PrincipalType)}), nil)
		case errors.Is(err, ErrDelegationSubjectToken):
			api.ErrorResponse(ctx, w, c.logger,
				api.UnauthorizedError(api.ErrCodeUnauthorized,
					"invalid or expired subject_token", nil), nil)
		case errors.Is(err, ErrDelegationRunActive):
			api.ErrorResponse(ctx, w, c.logger,
				api.ConflictError(api.ErrCodeResourceExists,
					"an active delegation already exists for this run",
					map[string]any{"run_id": req.RunID}), nil)
		default:
			api.ErrorResponse(ctx, w, c.logger,
				api.InternalError(api.ErrCodeInternalError, "failed to mint delegation", nil), err)
		}
		return
	}

	c.logger.Info(ctx, "delegation issuance created",
		logging.Field{Key: "jti", Value: issuance.JTI},
		logging.Field{Key: "run_id", Value: issuance.RunID},
		logging.Field{Key: "tenant_id", Value: issuance.TenantID},
		logging.Field{Key: "agent_principal_id", Value: issuance.AgentPrincipalID},
	)

	api.JSON(ctx, w, c.logger, http.StatusCreated, newDelegationIssuanceResponse(issuance))
}

// handleRevokeDelegation handles DELETE /identity/delegation/{jti}.
// Tombstones the issuance and blocklists the JTI. Allowed for the delegator
// themselves or a SERVICE / SYSTEM principal.
func (c *Controller) handleRevokeDelegation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	jti := r.PathValue("jti")
	if jti == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "jti path parameter is required", map[string]any{"field": "jti"}), nil)
		return
	}

	if err := c.svc.RevokeDelegation(ctx, claims, jti); err != nil {
		switch {
		case errors.Is(err, ErrDelegationNotFound):
			api.ErrorResponse(ctx, w, c.logger,
				api.NotFoundError(api.ErrCodeResourceNotFound, "delegation issuance not found", nil), nil)
		case errors.Is(err, ErrDelegationRevokeForbidden):
			api.ErrorResponse(ctx, w, c.logger,
				api.ForbiddenError(api.ErrCodeForbidden,
					"only the delegator or a platform SERVICE/SYSTEM principal may revoke a delegation", nil), nil)
		default:
			api.ErrorResponse(ctx, w, c.logger,
				api.InternalError(api.ErrCodeInternalError, "failed to revoke delegation", nil), err)
		}
		return
	}

	c.logger.Info(ctx, "delegation revoked",
		logging.Field{Key: "jti", Value: jti},
		logging.Field{Key: "principal.type", Value: string(claims.Kora.PrincipalType)},
	)

	api.JSON(ctx, w, c.logger, http.StatusNoContent, nil)
}

// handleGetDelegationForRun handles GET /identity/delegation/run/{run_id}.
// Releases a freshly re-signed delegation token to a caller whose
// mesh-attested SPIFFE identity matches the issuance's agent principal. A 404
// with code "delegation_not_found" means the run is autonomous (or the
// issuance was revoked) — the identity-proxy attaches no delegation header.
func (c *Controller) handleGetDelegationForRun(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	runID := r.PathValue("run_id")
	if runID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeMissingField, "run_id path parameter is required", map[string]any{"field": "run_id"}), nil)
		return
	}

	resp, err := c.svc.DelegationTokenForRun(ctx, r, runID)
	if err != nil {
		switch {
		case errors.Is(err, ErrDelegationNotFound):
			api.ErrorResponse(ctx, w, c.logger,
				api.NotFoundError(errCodeDelegationNotFound,
					"no active delegation issuance for this run", map[string]any{"run_id": runID}), nil)
		case errors.Is(err, ErrDelegationAgentMismatch):
			api.ErrorResponse(ctx, w, c.logger,
				api.ForbiddenError(api.ErrCodeForbidden,
					"caller identity does not match the delegation's agent principal", nil), nil)
		default:
			// Includes XFCC extraction failures — mirror handleExchange.
			api.ErrorResponse(ctx, w, c.logger,
				api.UnauthorizedError(api.ErrCodeUnauthorized, "identity extraction failed", nil), err)
		}
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleListDelegations handles GET /identity/delegation.
// Returns the authenticated principal's active issuances — the delegator view.
func (c *Controller) handleListDelegations(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}

	issuances, err := c.svc.ListDelegations(ctx, claims)
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to list delegations", nil), err)
		return
	}

	resp := ListDelegationsResponse{Delegations: make([]DelegationIssuanceResponse, 0, len(issuances))}
	for i := range issuances {
		resp.Delegations = append(resp.Delegations, newDelegationIssuanceResponse(&issuances[i]))
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}
