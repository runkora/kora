package identity

import (
	"encoding/json"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// authorizationServerMetadata is the RFC 8414 OAuth 2.0 Authorization Server
// Metadata document a spec-compliant MCP client fetches to discover Orbit's
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
	iss := api.RequestBaseURL(r) + c.basePath + c.GroupedBy()
	meta := authorizationServerMetadata{
		Issuer:                            iss,
		AuthorizationEndpoint:             iss + "/authorize",
		TokenEndpoint:                     iss + "/token",
		RegistrationEndpoint:              iss + "/register",
		JWKSURI:                           iss + "/.well-known/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		CodeChallengeMethodsSupported:     []string{"S256"},
		TokenEndpointAuthMethodsSupported: []string{"none"},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		c.logger.Error(ctx, "failed to write AS metadata response",
			logging.Field{Key: "error", Value: err.Error()})
	}
}
