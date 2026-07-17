package identity

import (
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Controller wires the identity service to HTTP routes.
type Controller struct {
	svc      *Service
	eval     policy.Evaluator
	routes   []routescopes.RouteScope
	basePath string
	logger   logging.Logger
}

// NewController creates a ready-to-boot identity Controller.
//
// eval and routes are passed to ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md). Identity
// has unauthenticated routes (device, session, exchange, jwks, etc.) that the
// middleware never sees because they are registered with mux.Handle directly
// rather than mux.HandleWithMiddleware. Only the PAT and user routes — which
// use the per-route auth chain — go through the scope middleware. basePath
// must match the server's BasePath so the middleware can reconstruct absolute
// paths after http.StripPrefix.
func NewController(svc *Service, eval policy.Evaluator, routes []routescopes.RouteScope, basePath string, logger logging.Logger) *Controller {
	return &Controller{svc: svc, eval: eval, routes: routes, basePath: basePath, logger: logger}
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
//	POST   /identity/session                Complete device flow poll, present id_token, or refresh token.
//	POST   /identity/exchange               Exchange a SPIFFE SVID for a platform session token.
//	GET    /identity/health                 Liveness check.
//	POST   /identity/revoke                 Revoke an access token or refresh token (RFC 7009).
//	GET    /identity/.well-known/jwks.json  Public key set for downstream token verification.
//	GET    /identity/tenants/discover       Home Realm Discovery — resolve tenant by email domain.
//
//	POST   /identity/tokens                    Create a Personal Access Token (requires auth).
//	GET    /identity/tokens                    List the caller's active PATs (requires auth).
//	GET    /identity/tokens/attribution        Resolve PAT UUIDs → owning user profiles (requires auth).
//	PATCH  /identity/tokens/{id}/scopes        Update PAT scopes (requires auth).
//	DELETE /identity/tokens/{id}               Revoke a PAT by ID (requires auth).
//
//	GET    /identity/users                     List users in the caller's tenant (requires auth).
//	GET    /identity/users/{id}                Get a single user profile (requires auth).
//
//	GET    /identity/preferences               Get the caller's preferences document (requires auth).
//	PUT    /identity/preferences               Replace the caller's preferences document (requires auth).
//	DELETE /identity/preferences               Delete the caller's preferences document (requires auth).
func (c *Controller) Boot(mux *api.Mux) {
	mux.Handle("GET /health", c.handleHealth)
	mux.Handle("POST /device", c.handleInitiateDeviceAuth)
	mux.Handle("POST /session", c.handleSession)
	mux.Handle("POST /exchange", c.handleExchange)
	mux.Handle("POST /revoke", c.handleRevoke)
	mux.Handle("GET /.well-known/jwks.json", c.handleJWKS)
	mux.Handle("GET /.well-known/oauth-authorization-server", c.handleAuthorizationServerMetadata)
	mux.Handle("GET /tenants/discover", c.handleDiscoverTenant)
	// OAuth token endpoint (MCP profile) is public: the MCP client authenticates
	// with the authorization code + PKCE verifier, not a session.
	mux.Handle("POST /oauth/token", c.handleOAuthToken)
	mux.Handle("POST /oauth/register", c.handleRegisterClient)

	// PAT management routes: require a non-PAT authenticated caller.
	// AuthMiddleware verifies the JWT; rejectPATMiddleware then ensures the
	// token is not itself a PAT — PATs are scoped exclusively to the inference
	// gateway and must not be used to manage identity resources.
	// ScopeEnforcementMiddleware (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md) then checks the route's required
	// scope against the principal's grants.
	auth := mw.MiddlewareChain(
		c.svc.AuthMiddleware(c.logger),
		rejectPATMiddleware(c.logger),
		mw.ScopeEnforcementMiddleware(c.eval, c.routes, c.basePath, c.logger),
	)
	// OAuth authorization code creation: called by the BFF /authorize with the
	// human's session token. Any authenticated (non-PAT) human may start their own
	// MCP OAuth flow, so it authenticates but requires no specific scope.
	authNoScope := mw.MiddlewareChain(
		c.svc.AuthMiddleware(c.logger),
		rejectPATMiddleware(c.logger),
	)
	mux.HandleWithMiddleware("POST /oauth/code", authNoScope, c.handleCreateOAuthCode)
	mux.HandleWithMiddleware("GET /oauth/clients/{id}", authNoScope, c.handleGetClient)

	mux.HandleWithMiddleware("POST /tokens", auth, c.handleCreatePAT)
	mux.HandleWithMiddleware("GET /tokens", auth, c.handleListPATs)
	// /tokens/attribution must be registered before /tokens/{id} so the literal
	// path segment "attribution" is not captured as the {id} wildcard.
	mux.HandleWithMiddleware("GET /tokens/attribution", auth, c.handleGetPATAttribution)
	mux.HandleWithMiddleware("PATCH /tokens/{id}/scopes", auth, c.handleUpdatePATScopes)
	mux.HandleWithMiddleware("DELETE /tokens/{id}", auth, c.handleRevokePAT)

	// User profile resolution: requires a human session token.
	mux.HandleWithMiddleware("GET /users", auth, c.handleListUsers)
	mux.HandleWithMiddleware("GET /users/{id}", auth, c.handleGetUser)

	// User preferences: requires a human session token.
	mux.HandleWithMiddleware("GET /preferences", auth, c.handleGetPreferences)
	mux.HandleWithMiddleware("PUT /preferences", auth, c.handlePutPreferences)
	mux.HandleWithMiddleware("DELETE /preferences", auth, c.handleDeletePreferences)
}

// rejectPATMiddleware rejects requests that carry a PAT token with 403
// Forbidden. PATs are scoped exclusively to the inference gateway — they must
// not authenticate against identity endpoints (including PAT management routes,
// to prevent a PAT from minting or revoking other PATs).
//
// This middleware is a no-op when no claims are present in the context (i.e.
// on unauthenticated routes where AuthMiddleware has not run).
func rejectPATMiddleware(logger logging.Logger) mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if claims, ok := token.ClaimsFromContext(r.Context()); ok && claims != nil {
				if claims.Orbit.PrincipalType == token.PrincipalTypePAT {
					api.ErrorResponse(r.Context(), w, logger,
						api.ForbiddenError(api.ErrCodeForbidden,
							"personal access tokens are not permitted on this endpoint", nil), nil)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	})
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
		apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required", map[string]any{
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
		if errors.Is(err, ErrDeviceFlowNotSupported) {
			apiErr := api.BadRequestError("device_flow_not_supported",
				"this tenant's OIDC provider does not support the Device Authorization Grant", nil)
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

// handleSession handles Device Authorization Grant polling, OIDC id_token
// handoff, and refresh token rotation.
//
// Supported grant_type values:
//   - "urn:ietf:params:oauth:grant-type:device_code" — poll the upstream OIDC provider
//   - "oidc_id_token"                                — browser presents a verified id_token; backend issues an Orbit session
//   - "refresh_token"                                — rotate a human session refresh token
func (c *Controller) handleSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req SessionRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}

	var (
		resp *SessionResponse
		err  error
	)

	switch req.GrantType {
	case "urn:ietf:params:oauth:grant-type:device_code":
		if req.TenantID == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required for device_code grant", map[string]any{
				"field": "tenant_id",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if req.DeviceCode == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "device_code is required for device_code grant", map[string]any{
				"field": "device_code",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		resp, err = c.svc.PollDeviceAuth(ctx, req)

	case "oidc_id_token":
		if req.IDToken == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "id_token is required for oidc_id_token grant", map[string]any{
				"field": "id_token",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if req.TenantID == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "tenant_id is required for oidc_id_token grant", map[string]any{
				"field": "tenant_id",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		resp, err = c.svc.ExchangeIDToken(ctx, req)

	case "refresh_token":
		if req.RefreshToken == "" {
			apiErr := api.ValidationError(api.ErrCodeMissingField, "refresh_token is required for refresh_token grant", map[string]any{
				"field": "refresh_token",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		resp, err = c.svc.RefreshSession(ctx, req)

	case "":
		apiErr := api.ValidationError(api.ErrCodeMissingField, "grant_type is required", map[string]any{
			"field":           "grant_type",
			"supported_types": []string{"urn:ietf:params:oauth:grant-type:device_code", "oidc_id_token", "refresh_token"},
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return

	default:
		apiErr := api.ValidationError(api.ErrCodeInvalidField, "unsupported grant_type", map[string]any{
			"grant_type":      req.GrantType,
			"supported_types": []string{"urn:ietf:params:oauth:grant-type:device_code", "oidc_id_token", "refresh_token"},
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
// (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md, docs/adr/2026-03-05-universal-gateway-policy-enforcement.md, docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md).
//
// No request body is needed: tenant and principal identity are derived entirely
// from the SPIFFE URI that SPIRE attests and Envoy injects via XFCC.
func (c *Controller) handleExchange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	resp, err := c.svc.ExchangeSVID(ctx, r)
	if err != nil {
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "identity extraction failed", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	c.logger.Info(ctx, "SPIFFE identity exchanged for platform token")

	api.JSON(ctx, w, c.logger, http.StatusOK, resp)
}

// maxRevokeBodyBytes caps the request body read during RFC 7009 token revocation
// to prevent memory exhaustion from oversized form payloads.
const maxRevokeBodyBytes = 1 << 20 // 1 MiB

// handleRevoke revokes an access token or refresh token.
// Accepts application/x-www-form-urlencoded per RFC 7009 §2.1.
// Fields: token (required), token_type_hint (required).
func (c *Controller) handleRevoke(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, maxRevokeBodyBytes)
	if err := r.ParseForm(); err != nil {
		apiErr := api.ValidationError(api.ErrCodeInvalidJSON, "could not parse form body", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	req := RevokeRequest{
		Token:     r.FormValue("token"),
		TokenType: r.FormValue("token_type_hint"),
	}

	if req.Token == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "token is required", map[string]any{
			"field": "token",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}
	if req.TokenType == "" {
		apiErr := api.ValidationError(api.ErrCodeMissingField, "token_type_hint is required", map[string]any{
			"field":           "token_type_hint",
			"supported_types": []string{"access_token", "refresh_token"},
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	if err := c.svc.RevokeToken(ctx, req); err != nil {
		switch {
		case errors.Is(err, ErrUnsupportedTokenType):
			apiErr := api.ValidationError(api.ErrCodeInvalidField, "unsupported token_type_hint: expected access_token or refresh_token", map[string]any{
				"field":           "token_type_hint",
				"supported_types": []string{"access_token", "refresh_token"},
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		case errors.Is(err, ErrInvalidToken):
			// RFC 7009 §2.1: token revocation endpoints MUST return 200/204 even for
			// invalid tokens. The client cannot distinguish "never existed" from
			// "already revoked" — both are success states.
			c.logger.Info(ctx, "revoke called on invalid/unknown token (treated as success per RFC 7009)")
			w.WriteHeader(http.StatusNoContent)
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

// handleHealth returns a simple liveness response for the identity service.
func (c *Controller) handleHealth(w http.ResponseWriter, r *http.Request) {
	api.JSON(r.Context(), w, c.logger, http.StatusOK, map[string]string{
		"status":  "healthy",
		"service": "orbit-identity",
	})
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

// handleDiscoverTenant implements GET /identity/tenants/discover?email=user@example.com
// It resolves the OIDC tenant for an email address via Home Realm Discovery (docs/adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md).
// The response contains only public OIDC config — never the client secret.
// Returns 404 if no tenant claims the email domain, 400 if the email is missing/malformed.
func (c *Controller) handleDiscoverTenant(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	email := r.URL.Query().Get("email")
	if email == "" {
		apiErr := api.BadRequestError(api.ErrCodeInvalidField, "email query parameter is required", map[string]any{
			"field": "email",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
		return
	}

	discovery, err := c.svc.DiscoverTenantByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrInvalidEmailFormat) {
			apiErr := api.BadRequestError(api.ErrCodeInvalidField, "malformed email address", map[string]any{
				"field": "email",
			})
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		if errors.Is(err, ErrTenantNotFound) {
			apiErr := api.NotFoundError(api.ErrCodeResourceNotFound, "no tenant found for the provided email domain", nil)
			api.ErrorResponse(ctx, w, c.logger, apiErr, nil)
			return
		}
		apiErr := api.InternalError(api.ErrCodeInternalError, "tenant discovery failed", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)
		return
	}

	api.JSON(ctx, w, c.logger, http.StatusOK, discovery)
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
		apiErr := api.BadRequestError(api.ErrCodeInvalidField, "device code has expired — start a new device flow", map[string]any{
			"field": "device_code",
		})
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	case errors.Is(err, ErrOIDCNotConfigured):
		apiErr := api.BadRequestError("oidc_not_configured",
			"federated authentication is not configured for this environment", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, nil)

	// OIDC id_token grant errors.
	case errors.Is(err, ErrIDTokenInvalid):
		apiErr := api.UnauthorizedError(api.ErrCodeUnauthorized, "id_token verification failed — token may be expired, tampered with, or issued by an unexpected provider", nil)
		api.ErrorResponse(ctx, w, c.logger, apiErr, err)

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
