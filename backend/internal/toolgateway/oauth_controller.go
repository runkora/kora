package toolgateway

import (
	"encoding/json"
	"net/http"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
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
}

func (c *OAuthMetadataController) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := api.RequestBaseURL(r)
	meta := protectedResourceMetadata{
		Resource: base + c.basePath + "/mcp",
		// The authorization server is the API host root (RFC 8414 issuer); the
		// client fetches its metadata from <base>/.well-known/oauth-authorization-server.
		// Ref ADR 2026-07-04 (north-south ingress + external service exposure).
		AuthorizationServers:   []string{base},
		ScopesSupported:        []string{mcpInvokeScope},
		BearerMethodsSupported: []string{"header"},
	}
	w.Header().Set("Content-Type", "application/json")
	// The URLs are derived from the request host, so this document must never be
	// shared-cached across hosts — a cached response poisoned with an attacker's
	// host would redirect other clients' OAuth flow. no-store + Vary close that.
	w.Header().Set("Cache-Control", "no-store, private")
	w.Header().Set("Vary", "X-Forwarded-Host, X-Forwarded-Proto")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		c.logger.Error(r.Context(), "failed to write protected-resource metadata",
			logging.Field{Key: "error", Value: err.Error()})
	}
}

// challengeMiddleware adds the RFC 9728 `WWW-Authenticate` challenge to the MCP
// route's 401 responses, pointing the client at the protected-resource metadata
// so an unauthenticated MCP client can begin the OAuth flow. The metadata URL is
// derived from each request's host (no hardcoded host) and lives at the host root
// per RFC 8615/9728 (ADR 2026-07-04); the edge routes root /.well-known/* to the
// gateway.
func challengeMiddleware() mw.Middleware {
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			prmURL := api.RequestBaseURL(r) + "/.well-known/oauth-protected-resource"
			challenge := `Bearer resource_metadata="` + prmURL + `"`
			next.ServeHTTP(&challengeWriter{ResponseWriter: w, challenge: challenge}, r)
		})
	})
}

// challengeWriter injects the WWW-Authenticate header iff the wrapped handler
// writes a 401, without disturbing any other status.
type challengeWriter struct {
	http.ResponseWriter
	challenge string
	wrote     bool
}

func (w *challengeWriter) WriteHeader(code int) {
	if !w.wrote {
		w.wrote = true
		if code == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", w.challenge)
		}
	}
	w.ResponseWriter.WriteHeader(code)
}
