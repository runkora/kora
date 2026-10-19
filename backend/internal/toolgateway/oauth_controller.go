package toolgateway

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// protectedResourceMetadata is the RFC 9728 document that tells a spec-compliant
// MCP client which authorization server protects this gateway. The client
// fetches it after the 401 challenge, then discovers the AS's own metadata.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	ScopesSupported        []string `json:"scopes_supported"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// OAuthMetadataController serves the gateway's protected-resource metadata
// (RFC 9728) unauthenticated — the MCP client fetches it before it has a token.
// URLs are derived from the request host so no host is hardcoded.
type OAuthMetadataController struct {
	basePath string // the gateway's base path (e.g. /v1)
	logger   logging.Logger
}

// NewOAuthMetadataController builds the PRM controller. basePath is the gateway's
// configured base path, used to form the (host-derived) resource and metadata URLs.
func NewOAuthMetadataController(basePath string, logger logging.Logger) *OAuthMetadataController {
	return &OAuthMetadataController{basePath: basePath, logger: logger}
}

// GroupedBy mounts the metadata at the base path root (no group).
func (c *OAuthMetadataController) GroupedBy() string { return "" }

// Middleware returns nil — protected-resource metadata is public discovery.
func (c *OAuthMetadataController) Middleware() mw.MiddlewareHandler { return nil }

// Boot registers the RFC 9728 metadata endpoint.
func (c *OAuthMetadataController) Boot(mux *api.Mux) {
	mux.Handle("GET /.well-known/oauth-protected-resource", c.handleProtectedResourceMetadata)
	// Per-server metadata under the server's own path (RFC 9728 resource_metadata
	// pointer target). Routed to the gateway by the existing /v1/mcp prefix + covered
	// by the self-enforced OPA bypass — no VS/policy change needed.
	mux.Handle("GET /mcp/{server}/.well-known/oauth-protected-resource", c.handleProtectedResourceMetadata)
}

func (c *OAuthMetadataController) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := api.RequestBaseURL(r)
	// The per-server route sets {server} (…/v1/mcp/<server>/.well-known/…); the base
	// route is the generic gateway resource. The server comes from the trusted URL
	// path, never a client-supplied value.
	resource := base + c.basePath + "/mcp"
	if server := r.PathValue("server"); server != "" {
		resource += "/" + server
	}
	meta := protectedResourceMetadata{
		Resource: resource,
		// The authorization server is the API host root (RFC 8414 issuer); the
		// client fetches its metadata from <base>/.well-known/oauth-authorization-server.
		// Ref ADR 2026-07-04 (north-south ingress + external service exposure).
		AuthorizationServers:   []string{base},
		ScopesSupported:        []string{mcpInvokeScope},
		BearerMethodsSupported: []string{"header"},
	}
	// The URLs are derived from the request host, so this document must never be
	// shared-cached across hosts: a cached copy poisoned with an attacker's host
	// would redirect other clients' OAuth flow. no-store + Vary close that.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Vary", "X-Forwarded-Host, X-Forwarded-Proto")
	api.JSON(r.Context(), w, c.logger, http.StatusOK, meta)
}

// challengeMiddleware adds the RFC 9728 `WWW-Authenticate` challenge to the MCP
// route's 401 responses, pointing the client at the protected-resource metadata
// so an unauthenticated MCP client can begin the OAuth flow. The metadata URL is
// derived from each request's host (no hardcoded host) and lives at the host root
// per RFC 8615/9728 (ADR 2026-07-04); the edge routes root /.well-known/* to the
// gateway. The specific per-server resource (this endpoint, /v1/mcp/<server>) is
// carried in the metadata URL's `resource` query so the client sends it as the
// RFC 8707 resource indicator — which the consent screen uses to show exactly
// which server's tools are being granted.
func challengeMiddleware(basePath string) mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Point at the per-server protected-resource metadata, served under the
			// server's own path so the client fetches the URL verbatim (RFC 9728
			// resource_metadata pointer — no query for the client to strip). The
			// client then sends resource=<base>/v1/mcp/<server>, which the consent
			// screen uses to show exactly that server's tools.
			prmURL := api.RequestBaseURL(r) + basePath + "/mcp/" + r.PathValue("server") +
				"/.well-known/oauth-protected-resource"
			challenge := `Bearer resource_metadata="` + prmURL + `"`
			next.ServeHTTP(&challengeWriter{ResponseWriter: w, challenge: challenge}, r)
		})
	})
}
