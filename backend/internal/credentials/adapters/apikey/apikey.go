// Package apikey implements the credential adapter for a connection whose
// credential is a static, admin-supplied secret that the upstream expects in
// one or more custom request headers, for example the GitHub Packages npm
// registry (Authorization: Bearer <token>) or an API that wants x-api-key. The
// egress gateway calls Mint to read the secret from the mounted Secret and
// inject the headers on the re-originated leg; the sandbox only ever holds a
// placeholder.
//
// Authorization is coarse: GET and HEAD are "read", POST, PUT, PATCH and
// DELETE are "write". There is no path categorization and no qualifier. See
// docs/adr/2026-11-11-apikey-connection-adapter.md.
//
// Rotation is a Secret update, picked up on the next resolve, as for
// k8sbearer.
package apikey

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Action constants returned by Classify, sourced from the shared adapter
// metadata table so the classifier and the published vocabulary cannot drift.
const (
	ActionRead  = platformconns.ActionAPIKeyRead
	ActionWrite = platformconns.ActionAPIKeyWrite
)

// Header limits. They match the MCPServer spec.auth.headers limits in
// operator/mcpserver_auth.go; a test in the operator package keeps the two in
// step.
const (
	MaxHeaders          = 8
	MaxHeaderNameLength = 64
	MaxPrefixLength     = 256
)

const (
	defaultHeader = "Authorization"
	defaultPrefix = "Bearer "
)

// ErrInvalidConfig is returned by Open when the connection's config is
// malformed.
var ErrInvalidConfig = errors.New("invalid apikey connection config")

// reservedHeaders are set by HTTP, the proxy, tracing or the platform. A
// credential header must not replace them. This copies the MCPServer list in
// operator/mcpserver_auth.go, which the adapter cannot import.
var reservedHeaders = map[string]bool{
	"host": true, "content-length": true, "content-type": true, "accept": true,
	"transfer-encoding": true, "connection": true, "keep-alive": true,
	"proxy-connection": true, "proxy-authenticate": true, "proxy-authorization": true,
	"te": true, "trailer": true, "upgrade": true,
	"mcp-session-id": true, "mcp-protocol-version": true, "last-event-id": true,
	"traceparent": true, "tracestate": true,
	"x-kora-delegation-token": true, "x-kora-run-id": true,
}

// reservedHeaderPrefixes cover proxy and Envoy control headers.
var reservedHeaderPrefixes = []string{"x-forwarded-", "x-envoy-"}

// headerConfig is one configured header.
type headerConfig struct {
	Name string `json:"name"`
	// Prefix is prepended to the secret. An omitted prefix is empty: a raw
	// value, as for x-api-key.
	Prefix string `json:"prefix"`
}

// config is the connection's adapter configuration. Every key is optional.
type config struct {
	// Headers lists the headers to inject, 1 to 8. When omitted the default is
	// Authorization: Bearer <secret>.
	Headers []headerConfig `json:"headers"`
	// PlaceholderEnv names a sandbox env var set to the placeholder. It is
	// read by the agents service through the connection catalog and ignored by
	// this adapter.
	PlaceholderEnv string `json:"placeholderEnv"`
}

// Adapter resolves the static secret mounted for an apikey connection.
// Adapter implements credentials.Adapter.
type Adapter struct {
	secrets credentials.SecretResolver
	logger  logging.Logger
}

// New constructs an Adapter backed by the given secret resolver.
func New(secrets credentials.SecretResolver, logger logging.Logger) *Adapter {
	return &Adapter{secrets: secrets, logger: logger}
}

// Type reports the adapter type served.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterAPIKey }

// Actions enumerates every action Classify can return, delegating to the
// shared metadata table.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return credentials.DeclaredActions(platformconns.AdapterAPIKey)
}

// Open binds the adapter to one connection, parsing and validating conn.Config.
// Unknown keys are rejected to catch typos.
func (a *Adapter) Open(conn credentials.Connection) (credentials.Connector, error) {
	headers, err := parseConfig(conn.Config)
	if err != nil {
		return nil, err
	}
	return &connector{adapter: a, conn: conn, headers: headers}, nil
}

// ValidateConfig reports whether raw is a valid apikey connection config. The
// operator webhook calls it so admission and the gateway agree.
func ValidateConfig(raw json.RawMessage) error {
	_, err := parseConfig(raw)
	return err
}

func parseConfig(raw json.RawMessage) ([]headerConfig, error) {
	defaults := []headerConfig{{Name: defaultHeader, Prefix: defaultPrefix}}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return defaults, nil
	}
	var cfg config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("%w: trailing data", ErrInvalidConfig)
	}
	if cfg.Headers == nil {
		return defaults, nil
	}
	if n := len(cfg.Headers); n == 0 || n > MaxHeaders {
		return nil, fmt.Errorf("%w: headers must list 1 to %d entries", ErrInvalidConfig, MaxHeaders)
	}
	seen := make(map[string]bool, len(cfg.Headers))
	for _, h := range cfg.Headers {
		lower := strings.ToLower(h.Name)
		switch {
		case len(h.Name) > MaxHeaderNameLength:
			return nil, fmt.Errorf("%w: a header name is over %d characters", ErrInvalidConfig, MaxHeaderNameLength)
		case !isHeaderName(h.Name):
			return nil, fmt.Errorf("%w: %q must use only letters, digits and hyphens", ErrInvalidConfig, h.Name)
		case isReservedHeader(lower):
			return nil, fmt.Errorf("%w: header %q is reserved", ErrInvalidConfig, h.Name)
		case seen[lower]:
			return nil, fmt.Errorf("%w: header %q is listed twice", ErrInvalidConfig, h.Name)
		case len(h.Prefix) > MaxPrefixLength:
			return nil, fmt.Errorf("%w: the prefix of %q is over %d characters", ErrInvalidConfig, h.Name, MaxPrefixLength)
		case !isHeaderValue(h.Prefix):
			return nil, fmt.Errorf("%w: the prefix of %q contains a control character", ErrInvalidConfig, h.Name)
		}
		seen[lower] = true
	}
	return cfg.Headers, nil
}

func isReservedHeader(lower string) bool {
	if reservedHeaders[lower] {
		return true
	}
	for _, p := range reservedHeaderPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// isHeaderName reports whether name uses only letters, digits and hyphens.
func isHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
		if !letter && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// isHeaderValue rejects control characters other than tab, as net/http does.
func isHeaderValue(v string) bool {
	for i := 0; i < len(v); i++ {
		if c := v[i]; (c < 0x20 && c != '\t') || c == 0x7f {
			return false
		}
	}
	return true
}

// connector serves one apikey connection.
type connector struct {
	adapter *Adapter
	conn    credentials.Connection
	headers []headerConfig
}

// Classify maps the HTTP method to a coarse read or write action. Anything
// else, including OPTIONS and protocol upgrades, is unsupported.
func (c *connector) Classify(req *http.Request) (credentials.Classification, error) {
	if req == nil {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	if isUpgradeRequest(req) {
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
	switch strings.ToUpper(req.Method) {
	case http.MethodGet, http.MethodHead:
		return credentials.Classification{Action: ActionRead}, nil
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return credentials.Classification{Action: ActionWrite}, nil
	default:
		return credentials.Classification{}, credentials.ErrUnsupportedAction
	}
}

// isUpgradeRequest reports whether req asks the server to switch protocols.
func isUpgradeRequest(req *http.Request) bool {
	if req.Header.Get("Upgrade") == "" {
		return false
	}
	for part := range strings.SplitSeq(req.Header.Get("Connection"), ",") {
		if strings.EqualFold(strings.TrimSpace(part), "upgrade") {
			return true
		}
	}
	return false
}

// Mint reads the static secret from the mounted Secret and returns one header
// per configured entry, each prefix + secret. ExpiresAt is left zero: the
// secret is static. The secret is never logged.
func (c *connector) Mint(ctx context.Context, id credentials.CallerIdentity) (credentials.Credential, error) {
	tok, err := c.adapter.secrets.Resolve(ctx, c.conn.SecretRef)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("resolve apikey secret: %w", err)
	}
	token := strings.TrimSpace(string(tok))
	if token == "" {
		return credentials.Credential{}, errors.New("apikey secret is empty")
	}

	c.adapter.logger.Debug(ctx, "resolved apikey credential",
		logging.Field{Key: "connection", Value: c.conn.Name},
		logging.Field{Key: "tenant_id", Value: id.TenantID},
		logging.Field{Key: "run_id", Value: id.RunID},
	)

	headers := make([]credentials.HeaderValue, 0, len(c.headers))
	for _, h := range c.headers {
		headers = append(headers, credentials.HeaderValue{Name: h.Name, Value: h.Prefix + token})
	}
	return credentials.Credential{Token: token, Headers: headers}, nil
}
