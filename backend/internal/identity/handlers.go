package identity

import (
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
)

// Controller wires the identity service to HTTP routes.
type Controller struct {
	svc    *Service
	logger logging.Logger
}

// NewController creates a ready-to-boot identity Controller.
func NewController(svc *Service, logger logging.Logger) *Controller {
	return &Controller{svc: svc, logger: logger}
}

// GroupedBy returns the route prefix shared by all identity endpoints.
func (c *Controller) GroupedBy() string { return "/identity" }

// Middleware returns nil — gateway-level enforcement is handled by Envoy + OPA.
func (c *Controller) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers all identity routes with the Mux.
//
// Route map:
//
//	POST   /identity/device                 Initiate a Device Authorization Grant flow.
//	POST   /identity/session                Complete device flow poll OR exchange a refresh token.
//	POST   /identity/exchange               Exchange a SPIFFE SVID for a platform session token.
//	POST   /identity/revoke                 Revoke an access token or refresh token (RFC 7009).
//	GET    /identity/.well-known/jwks.json  Public key set for downstream token verification.
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("POST /device", c.handleInitiateDeviceAuth)
	mux.Handle("POST /session", c.handleSession)
	mux.Handle("POST /exchange", c.handleExchange)
	mux.Handle("POST /revoke", c.handleRevoke)
	mux.Handle("GET /.well-known/jwks.json", c.handleJWKS)
}

// ─── Handlers ─────────────────────────────────────────────────────────────────

// handleInitiateDeviceAuth starts a Device Authorization Grant flow.
// The caller receives a user_code and verification_uri to present to the user,
// then polls POST /identity/session with grant_type=device_code.
func (c *Controller) handleInitiateDeviceAuth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req DeviceAuthRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.TenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required", map[string]interface{}{
			"field": "tenant_id",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	resp, err := c.svc.InitiateDeviceAuth(ctx, req)
	if err != nil {
		if errors.Is(err, ErrOIDCNotConfigured) {
			apiErr := api.BadRequestError("oidc_not_configured",
				"federated authentication is not configured for this environment", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to initiate device auth", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	c.logger.Info(ctx, "device auth flow initiated",
		logging.Field{Key: "tenant_id", Value: req.TenantID},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleSession handles both Device Authorization Grant polling and refresh token exchange.
//
// Supported grant_type values:
//   - "urn:ietf:params:oauth:grant-type:device_code" — poll the upstream OIDC provider
//   - "refresh_token"                                — rotate a human session refresh token
func (c *Controller) handleSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req SessionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.TenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required", map[string]interface{}{
			"field": "tenant_id",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	var (
		resp *SessionResponse
		err  error
	)

	switch req.GrantType {
	case "urn:ietf:params:oauth:grant-type:device_code":
		if req.DeviceCode == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "device_code is required for device_code grant", map[string]interface{}{
				"field": "device_code",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		resp, err = c.svc.PollDeviceAuth(ctx, req)

	case "refresh_token":
		if req.RefreshToken == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "refresh_token is required for refresh_token grant", map[string]interface{}{
				"field": "refresh_token",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		resp, err = c.svc.RefreshSession(ctx, req)

	case "":
		apiErr := api.ValidationError(api.ErrCodeMissingField, "grant_type is required", map[string]interface{}{
			"field":           "grant_type",
			"supported_types": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return

	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "unsupported grant_type", map[string]interface{}{
			"grant_type":      req.GrantType,
			"supported_types": []string{"urn:ietf:params:oauth:grant-type:device_code", "refresh_token"},
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err != nil {
		c.handleSessionError(w, r, err)
		return
	}

	c.logger.Info(ctx, "session issued",
		logging.Field{Key: "grant_type", Value: req.GrantType},
		logging.Field{Key: "tenant_id", Value: req.TenantID},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleExchange extracts the caller's SPIFFE identity from the Envoy-injected
// x-forwarded-client-cert (XFCC) header and issues a platform session token.
// The agent workload is completely identity-unaware — it makes a plain HTTP
// POST and the sidecar infrastructure supplies the cryptographic identity
// (ADR-0010, ADR-0011, ADR-0015).
//
// The request body carries only tenant_id, which is required so the platform
// can scope the issued token to the correct tenant context.
func (c *Controller) handleExchange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req ExchangeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.TenantID == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required", map[string]interface{}{
			"field": "tenant_id",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	resp, err := c.svc.ExchangeSVID(ctx, r, req.TenantID)
	if err != nil {
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "identity extraction failed", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	c.logger.Info(ctx, "SPIFFE identity exchanged for platform token",
		logging.Field{Key: "tenant_id", Value: req.TenantID},
	)

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// handleRevoke revokes an access token or refresh token.
// Uses POST per RFC 7009 §2.1.
func (c *Controller) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req RevokeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	if req.Token == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "token is required", map[string]interface{}{
			"field": "token",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	if req.TokenType == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "token_type is required", map[string]interface{}{
			"field":           "token_type",
			"supported_types": []string{"access_token", "refresh_token"},
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.svc.RevokeToken(ctx, req); err != nil {
		switch {
		case errors.Is(err, ErrInvalidToken), errors.Is(err, ErrRefreshTokenNotFound):
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "token not found or already revoked", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		default:
			apiErr := api.InternalError(api.ErrCodeInternalError, "failed to revoke token", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		}
		return
	}

	c.logger.Info(ctx, "token revoked",
		logging.Field{Key: "token_type", Value: req.TokenType},
	)

	w.WriteHeader(http.StatusNoContent)
}

// handleJWKS returns the public key set used to verify platform tokens.
// Downstream services and the gateway can cache this response.
func (c *Controller) handleJWKS(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(c.svc.JWKSJSON()); err != nil {
		c.logger.Error(ctx, "failed to write JWKS response",
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
}

// ─── Error mapping helpers ────────────────────────────────────────────────────

// handleSessionError maps service-layer session errors to structured HTTP responses.
func (c *Controller) handleSessionError(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()

	switch {
	// Device flow — the client must keep polling; surface these as 202 Accepted
	// with a machine-readable code so CLI tooling can act on them.
	case errors.Is(err, ErrAuthorizationPending):
		api.JSON(ctx, w, c.logger, http.StatusAccepted, map[string]string{
			"code":    "authorization_pending",
			"message": "the user has not yet completed authorisation — continue polling",
		})

	case errors.Is(err, ErrSlowDown):
		api.JSON(ctx, w, c.logger, http.StatusAccepted, map[string]string{
			"code":    "slow_down",
			"message": "polling too frequently — increase the interval",
		})

	// Terminal device flow errors.
	case errors.Is(err, ErrAccessDenied):
		apiErr := api.ForbiddenError(api.ErrCodeForbidden, "the user denied the authorisation request", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrDeviceCodeExpired):
		apiErr := api.BadRequestError(api.ErrCodeInvalidField, "device code has expired — start a new device flow", map[string]interface{}{
			"field": "device_code",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrOIDCNotConfigured):
		apiErr := api.BadRequestError("oidc_not_configured",
			"federated authentication is not configured for this environment", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	// Refresh token errors.
	case errors.Is(err, ErrRefreshTokenNotFound), errors.Is(err, ErrRefreshTokenRevoked):
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "refresh token is invalid or has been revoked", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrRefreshTokenExpired):
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "refresh token has expired — please sign in again", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrTokenRevoked):
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "session has been revoked", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrSessionNotFound):
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "session not found", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	default:
		apiErr := api.InternalError(api.ErrCodeInternalError, "failed to create session", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
	}
}
