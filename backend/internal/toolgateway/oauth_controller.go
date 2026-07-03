package toolgateway

import (
	"encoding/json"
	"net/http"
	"strings"

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
type OAuthMetadataController struct {
	resource   string
	authServer string
	logger     logging.Logger
}

// NewOAuthMetadataController builds the PRM controller. resource is this gateway's
// protected-resource identifier (the MCP endpoint URL); authServer is the identity
// service's OAuth issuer.
func NewOAuthMetadataController(resource, authServer string, logger logging.Logger) *OAuthMetadataController {
	return &OAuthMetadataController{resource: resource, authServer: authServer, logger: logger}
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
	meta := protectedResourceMetadata{
		Resource:               c.resource,
		AuthorizationServers:   []string{c.authServer},
		ScopesSupported:        []string{mcpInvokeScope},
		BearerMethodsSupported: []string{"header"},
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(meta); err != nil {
		c.logger.Error(r.Context(), "failed to write protected-resource metadata",
			logging.Field{Key: "error", Value: err.Error()})
	}
}

// ProtectedResourceMetadataURL derives where the PRM is served from the resource
// (the MCP endpoint): the resource with its trailing "/mcp" replaced by the RFC
// 9728 well-known path. This is the URL advertised in the 401 challenge and it
// matches where OAuthMetadataController mounts under the same base path.
func ProtectedResourceMetadataURL(resource string) string {
	return strings.TrimSuffix(resource, "/mcp") + "/.well-known/oauth-protected-resource"
}

// challengeMiddleware adds the RFC 9728 `WWW-Authenticate` challenge to the MCP
// route's 401 responses, pointing the client at the protected-resource metadata
// so an unauthenticated MCP client can begin the OAuth flow. It wraps the writer
// so the header is set exactly when the auth middleware writes a 401.
func challengeMiddleware(resourceMetadataURL string) mw.Middleware {
	challenge := `Bearer resource_metadata="` + resourceMetadataURL + `"`
	return mw.NewMiddleware(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
