package identity

import (
	"context"
	"errors"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// bffAuthorizePath is the browser-facing OAuth authorization endpoint, served by
// the Next.js BFF (which owns the kora session) at the same public host as this
// service. It is a consent page: after the SSO session check it asks the human to
// approve the client before a code is minted. The AS metadata advertises it as
// authorization_endpoint.
const bffAuthorizePath = "/mcp/authorize"

// authorizationServerMetadata is the RFC 8414 OAuth 2.0 Authorization Server
// Metadata document a spec-compliant MCP client fetches to discover kora's
// authorization endpoints. It advertises the OAuth 2.1 profile the MCP
// authorization spec requires: authorization-code + PKCE (S256), public clients.
//
// The /authorize, /token, and /register endpoints it names are built in the
// later MCP-OAuth phases; this metadata (and the gateway's protected-resource
// metadata) wires discovery end-to-end first.
type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	RegistrationEndpoint              string   `json:"registration_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

// handleAuthorizationServerMetadata serves GET /.well-known/oauth-authorization-server
// (RFC 8414). The issuer and every endpoint are derived from the request's public
// host (via the ingress X-Forwarded-* headers) plus this service's fixed base
// path, so the document is self-consistent at whatever host the service is
// exposed — no hardcoded host.
func (c *Controller) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	base := api.RequestBaseURL(r)
	// The issuer is the API host root (RFC 8414) — the metadata is served at the
	// root /.well-known/oauth-authorization-server (ADR 2026-07-04). Endpoints are
	// absolute under the identity service's path; authorization_endpoint is the
	// browser-facing BFF consent page on the app host (strip the api. label).
	idPath := base + c.basePath + c.GroupedBy()
	meta := authorizationServerMetadata{
		Issuer:                            base,
		AuthorizationEndpoint:             api.AppBaseURL(base) + bffAuthorizePath,
		TokenEndpoint:                     idPath + "/oauth/token",
		RegistrationEndpoint:              idPath + "/oauth/register",
		JWKSURI:                           idPath + "/.well-known/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	}
	// URLs are derived from the request host, so this must never be shared-cached
	// across hosts — a poisoned cached copy would point clients at an attacker's
	// authorization/token endpoints. no-store + Vary close that vector.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Vary", "X-Forwarded-Host, X-Forwarded-Proto")
	api.JSON(ctx, w, c.logger, http.StatusOK, meta)
}

// ── OAuth authorization-code + PKCE (MCP profile) ─────────────────────────────

type createCodeRequest struct {
	ClientID            string `json:"client_id"`
	RedirectURI         string `json:"redirect_uri"`
	CodeChallenge       string `json:"code_challenge"`
	CodeChallengeMethod string `json:"code_challenge_method"`
	Scope               string `json:"scope"`
}

// handleCreateOAuthCode handles POST /identity/oauth/code — called by the BFF
// /authorize after its session check, authenticated by the human's platform
// token. It mints a single-use authorization code bound to that human.
func (c *Controller) handleCreateOAuthCode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	claims, ok := token.ClaimsFromContext(ctx)
	if !ok {
		api.ErrorResponse(ctx, w, c.logger,
			api.UnauthorizedError(api.ErrCodeUnauthorized, "unauthorized", nil), nil)
		return
	}
	var req createCodeRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	human := AuthCode{
		Subject:     claims.Subject,
		TenantID:    claims.Orbit.TenantID,
		DisplayName: claims.Orbit.DisplayName,
		// Carry the human's verified claims (roles, etc.) so the minted MCP token
		// has the same permissions — the token acts as this human.
		Claims: claims.Orbit.Claims,
	}
	code, err := c.svc.CreateAuthCode(ctx, human, AuthorizeParams(req))
	if err != nil {
		api.ErrorResponse(ctx, w, c.logger,
			api.ValidationError(api.ErrCodeInvalidField, err.Error(), nil), nil)
		return
	}
	writeOAuthJSON(ctx, w, c.logger, http.StatusOK, map[string]string{"code": code})
}

// handleOAuthToken handles POST /identity/oauth/token (public, RFC 6749
// authorization_code grant, form-encoded). Validates the code + PKCE and mints
// the human's ordinary aud=orbit-platform token.
func (c *Controller) handleOAuthToken(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024) // bound the token-endpoint body
	if err := r.ParseForm(); err != nil {
		writeOAuthError(ctx, w, c.logger, http.StatusBadRequest, "invalid_request", "malformed form body")
		return
	}
	var (
		resp *TokenResponse
		err  error
	)
	switch r.PostForm.Get("grant_type") {
	case "refresh_token":
		// Public-client refresh: the refresh token is the credential (rotated on use).
		resp, err = c.svc.RefreshOAuthToken(ctx, r.PostForm.Get("refresh_token"))
	default:
		resp, err = c.svc.ExchangeAuthCode(ctx, TokenRequest{
			GrantType:    r.PostForm.Get("grant_type"),
			Code:         r.PostForm.Get("code"),
			CodeVerifier: r.PostForm.Get("code_verifier"),
			RedirectURI:  r.PostForm.Get("redirect_uri"),
			ClientID:     r.PostForm.Get("client_id"),
		})
	}
	switch {
	case errors.Is(err, ErrInvalidOAuthRequest):
		writeOAuthError(ctx, w, c.logger, http.StatusBadRequest, "invalid_request", err.Error())
		return
	case errors.Is(err, ErrInvalidOAuthGrant):
		writeOAuthError(ctx, w, c.logger, http.StatusBadRequest, "invalid_grant", err.Error())
		return
	case err != nil:
		c.logger.Error(ctx, "oauth token exchange failed", logging.Field{Key: "error", Value: err.Error()})
		writeOAuthError(ctx, w, c.logger, http.StatusInternalServerError, "server_error", "token exchange failed")
		return
	}
	writeOAuthJSON(ctx, w, c.logger, http.StatusOK, resp)
}

// writeOAuthJSON writes a no-store JSON response (token/code endpoints must not cache).
// CORS headers are set on all responses so remote MCP clients (e.g. Claude Desktop)
// can call the registration and token endpoints cross-origin.
func writeOAuthJSON(ctx context.Context, w http.ResponseWriter, logger logging.Logger, status int, body any) {
	w.Header().Set("Cache-Control", "no-store")
	setOAuthCORSHeaders(w)
	api.JSON(ctx, w, logger, status, body)
}

// writeOAuthError writes an RFC 6749 {error, error_description} response.
func writeOAuthError(ctx context.Context, w http.ResponseWriter, logger logging.Logger, status int, code, desc string) {
	writeOAuthJSON(ctx, w, logger, status, map[string]string{"error": code, "error_description": desc})
}

// setOAuthCORSHeaders sets CORS headers on the public OAuth back-channel
// endpoints (/oauth/register, /oauth/token). These endpoints are called
// cross-origin by remote MCP clients such as Claude Desktop.
func setOAuthCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// handleOAuthCORSPreflight handles OPTIONS preflight requests for the public
// OAuth back-channel endpoints (/oauth/register, /oauth/token).
func (c *Controller) handleOAuthCORSPreflight(w http.ResponseWriter, r *http.Request) {
	setOAuthCORSHeaders(w)
	w.Header().Set("Access-Control-Max-Age", "86400")
	api.JSON(r.Context(), w, c.logger, http.StatusNoContent, nil)
}

// clientRegistrationResponse is the RFC 7591 registration response.
type clientRegistrationResponse struct {
	ClientID                string   `json:"client_id"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
}

// handleRegisterClient handles POST /identity/oauth/register (public, RFC 7591
// dynamic client registration for public PKCE clients).
func (c *Controller) handleRegisterClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var req ClientRegistrationRequest
	if !api.DecodeJSON(ctx, r, w, c.logger, &req) {
		return
	}
	client, err := c.svc.RegisterClient(ctx, req)
	switch {
	case errors.Is(err, ErrDCRDisabled):
		writeOAuthError(ctx, w, c.logger, http.StatusForbidden, "access_denied", "dynamic client registration is disabled")
		return
	case errors.Is(err, ErrInvalidClientMetadata):
		writeOAuthError(ctx, w, c.logger, http.StatusBadRequest, "invalid_client_metadata", err.Error())
		return
	case err != nil:
		c.logger.Error(ctx, "client registration failed", logging.Field{Key: "error", Value: err.Error()})
		writeOAuthError(ctx, w, c.logger, http.StatusInternalServerError, "server_error", "registration failed")
		return
	}
	writeOAuthJSON(ctx, w, c.logger, http.StatusCreated, clientRegistrationResponse{
		ClientID:                client.ClientID,
		RedirectURIs:            client.RedirectURIs,
		TokenEndpointAuthMethod: "none",
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		ClientName:              client.ClientName,
		ClientIDIssuedAt:        client.CreatedAt.Unix(),
	})
}

// consentClientInfo is a registered client's public metadata, shown on the
// consent screen so the human knows which application is requesting access.
type consentClientInfo struct {
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
}

// handleGetClient handles GET /identity/oauth/clients/{id} (authenticated) —
// the BFF consent page fetches it (with the human's session token) to name the
// application requesting access.
func (c *Controller) handleGetClient(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	client, err := c.svc.GetClient(ctx, r.PathValue("id"))
	if errors.Is(err, ErrClientNotFound) {
		writeOAuthError(ctx, w, c.logger, http.StatusNotFound, "not_found", "client not registered")
		return
	}
	if err != nil {
		c.logger.Error(ctx, "get client failed", logging.Field{Key: "error", Value: err.Error()})
		writeOAuthError(ctx, w, c.logger, http.StatusInternalServerError, "server_error", "failed to load client")
		return
	}
	writeOAuthJSON(ctx, w, c.logger, http.StatusOK, consentClientInfo{
		ClientID:     client.ClientID,
		ClientName:   client.ClientName,
		RedirectURIs: client.RedirectURIs,
	})
}
