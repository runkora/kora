package identity

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// The internal service-token endpoint exists so that a platform component which
// starts work on behalf of a tenant principal (kora-triggers for trigger runs,
// kora-agents for grading) can obtain that principal's short-lived token without
// holding any signing key. identity remains the sole token authority.
//
// It authenticates exactly like POST /identity/exchange: by the caller's
// mesh-attested SPIFFE identity in the Envoy-injected XFCC header. No shared
// secret, no bearer token, nothing this process could leak.
//
// See docs/adr/2026-06-20-extended-trigger-types.md, Decision 1.

// mintServiceTokenCallers is the allowlist of system components permitted to
// mint a SERVICE token, each with the rule for which principals it may name.
//
// This endpoint issues a token for a principal the caller names, so it is only
// as safe as this list is short and these rules are narrow. Adding an entry or
// widening a rule is a security decision, not a wiring detail.
var mintServiceTokenCallers = map[string]func(MintServiceTokenRequest) error{
	// kora-triggers mints any SERVICE principal in any tenant, bounded only by that principal's grants.
	"kora-triggers": func(MintServiceTokenRequest) error { return nil },
	// kora-agents mints only the request tenant's grading principal (docs/adr/2026-11-19-graders.md).
	"kora-agents": func(req MintServiceTokenRequest) error {
		if req.PrincipalID != token.GraderPrincipalID(req.TenantID) {
			return fmt.Errorf("%w: kora-agents may only mint grader:<tenant_id>, not %q", ErrCallerNotPermitted, req.PrincipalID)
		}
		return nil
	},
}

// maxServiceTokenTTL caps the lifetime a caller may request. Trigger tokens are
// used for a single dispatch and are dead long before this.
const maxServiceTokenTTL = 15 * time.Minute

// ErrCallerNotPermitted is returned when a mesh-attested caller is not on the
// mint allowlist.
var ErrCallerNotPermitted = errors.New("caller not permitted to mint service tokens")

// MintServiceTokenRequest is the body for POST /identity/internal/service-token.
//
// TenantID and PrincipalID name the principal the token is minted for. They are
// caller-supplied on purpose: the caller is a trusted platform component acting
// for a tenant principal it manages, and the resulting token's access is bounded
// by that principal's own grants rather than by the caller's.
type MintServiceTokenRequest struct {
	TenantID    string `json:"tenant_id"`
	PrincipalID string `json:"principal_id"`
	DisplayName string `json:"display_name"`
	TTLSeconds  int    `json:"ttl_seconds"`
}

// MintServiceTokenResponse carries the minted token and its lifetime.
type MintServiceTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// handleMintServiceToken issues a SERVICE token for the principal named in the
// request, but only to a mesh-attested system component on the allowlist.
//
// Authorization is the caller's SPIFFE identity, not a bearer token: the request
// must arrive through the mesh with an XFCC header naming a permitted system
// component. A request without XFCC did not traverse an mTLS sidecar and is
// rejected.
func (c *Controller) handleMintServiceToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	caller, err := c.svc.extractor.Extract(r)
	if err != nil {
		c.logger.Warn(ctx, "service-token mint rejected: no mesh identity",
			logging.Field{Key: "error", Value: err.Error()})
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "mesh identity required", nil), err)
		return
	}

	if permErr := assertMintCallerPermitted(caller); permErr != nil {
		c.logger.Warn(ctx, "service-token mint rejected: caller not permitted",
			logging.Field{Key: "caller", Value: caller.DisplayName},
			logging.Field{Key: "caller_type", Value: string(caller.Type)})
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "caller not permitted to mint service tokens", nil), permErr)
		return
	}

	var req MintServiceTokenRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.TenantID == "" || req.PrincipalID == "" {
		api.ErrorResponse(ctx, w, c.logger,
			api.BadRequestError(api.ErrCodeValidationFailed, "tenant_id and principal_id are required", nil), nil)
		return
	}

	if permErr := mintServiceTokenCallers[caller.DisplayName](req); permErr != nil {
		c.logger.Warn(ctx, "service-token mint rejected: principal not permitted for caller",
			logging.Field{Key: "caller", Value: caller.DisplayName},
			logging.Field{Key: "tenant_id", Value: req.TenantID},
			logging.Field{Key: "principal_id", Value: req.PrincipalID})
		api.ErrorResponse(ctx, w, c.logger,
			api.ForbiddenError(api.ErrCodeForbidden, "caller not permitted to mint this principal", nil), permErr)
		return
	}

	ttl := time.Duration(req.TTLSeconds) * time.Second
	if ttl <= 0 || ttl > maxServiceTokenTTL {
		ttl = maxServiceTokenTTL
	}

	tok, err := c.svc.MintServiceToken(ctx, req.TenantID, req.PrincipalID, req.DisplayName, ttl)
	if err != nil {
		c.logger.Error(ctx, "service-token mint failed",
			logging.Field{Key: "error", Value: err.Error()})
		api.ErrorResponse(ctx, w, c.logger,
			api.InternalError(api.ErrCodeInternalError, "failed to mint service token", nil), err)
		return
	}

	c.logger.Info(ctx, "service token minted for platform component",
		logging.Field{Key: "caller", Value: caller.DisplayName},
		logging.Field{Key: "tenant_id", Value: req.TenantID},
		logging.Field{Key: "principal_id", Value: req.PrincipalID},
		logging.Field{Key: "ttl_seconds", Value: int(ttl.Seconds())})

	api.JSON(ctx, w, c.logger, http.StatusOK, MintServiceTokenResponse{
		AccessToken: tok,
		TokenType:   "Bearer",
		ExpiresIn:   int(ttl.Seconds()),
	})
}

// assertMintCallerPermitted fails closed unless the attested caller is a SYSTEM
// principal named on the allowlist. Anything else — an agent, a tenant service,
// an unknown component — is refused.
func assertMintCallerPermitted(caller *Principal) error {
	if caller.Type != token.PrincipalTypeSystem {
		return fmt.Errorf("%w: principal type %q", ErrCallerNotPermitted, caller.Type)
	}
	if _, ok := mintServiceTokenCallers[caller.DisplayName]; !ok {
		return fmt.Errorf("%w: component %q", ErrCallerNotPermitted, caller.DisplayName)
	}
	return nil
}
