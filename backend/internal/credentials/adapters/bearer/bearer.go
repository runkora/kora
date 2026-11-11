// Package bearer implements the credential adapter for a connection whose
// credential is a static, admin-supplied token that the upstream expects in a
// request header, for example the GitHub Packages npm registry. The egress
// gateway calls Mint to read the token from the mounted secret and inject it
// on the re-originated leg; the sandbox only ever holds a placeholder.
//
// Authorization is coarse: GET and HEAD are "read", POST, PUT, PATCH and
// DELETE are "write". There is no path categorization and no qualifier. See
// docs/adr/2026-11-11-bearer-connection-adapter.md.
//
// Rotation is a Secret update, picked up on the next resolve, as for
// k8sbearer.
package bearer

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
	ActionRead  = platformconns.ActionBearerRead
	ActionWrite = platformconns.ActionBearerWrite
)

const (
	defaultHeader = "Authorization"
	defaultPrefix = "Bearer "
)

// ErrInvalidConfig is returned by Open when the connection's config is
// malformed.
var ErrInvalidConfig = errors.New("invalid bearer connection config")

// forbiddenHeaders are headers the adapter must never be configured to inject:
// the ones the proxy owns and the hop-by-hop set (RFC 9110 section 7.6.1).
var forbiddenHeaders = map[string]bool{
	"Host":                true,
	"Content-Length":      true,
	"Connection":          true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Proxy-Connection":    true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

// config is the connection's adapter configuration. Every key is optional.
type config struct {
	// Header is the header to inject. Default "Authorization".
	Header string `json:"header"`
	// Prefix is prepended to the token. Default "Bearer "; an explicit empty
	// string injects the raw token.
	Prefix *string `json:"prefix"`
	// PlaceholderEnv names a sandbox env var set to the placeholder. It is
	// read by the agents service through the connection catalog and ignored by
	// this adapter.
	PlaceholderEnv string `json:"placeholderEnv"`
}

// Adapter resolves the static token mounted for a bearer connection. Adapter
// implements credentials.Adapter.
type Adapter struct {
	secrets credentials.SecretResolver
	logger  logging.Logger
}

// New constructs an Adapter backed by the given secret resolver.
func New(secrets credentials.SecretResolver, logger logging.Logger) *Adapter {
	return &Adapter{secrets: secrets, logger: logger}
}

// Type reports the adapter type served.
func (a *Adapter) Type() credentials.AdapterType { return credentials.AdapterBearer }

// Actions enumerates every action Classify can return, delegating to the
// shared metadata table.
func (a *Adapter) Actions() []credentials.ActionDeclaration {
	return credentials.DeclaredActions(platformconns.AdapterBearer)
}

// Open binds the adapter to one connection, parsing and validating conn.Config.
// Unknown keys are rejected to catch typos.
func (a *Adapter) Open(conn credentials.Connection) (credentials.Connector, error) {
	header, prefix, err := parseConfig(conn.Config)
	if err != nil {
		return nil, err
	}
	return &connector{adapter: a, conn: conn, header: header, prefix: prefix}, nil
}

func parseConfig(raw json.RawMessage) (header, prefix string, err error) {
	header, prefix = defaultHeader, defaultPrefix
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return header, prefix, nil
	}
	var cfg config
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return "", "", fmt.Errorf("%w: %w", ErrInvalidConfig, err)
	}
	if dec.More() {
		return "", "", fmt.Errorf("%w: trailing data", ErrInvalidConfig)
	}
	if cfg.Header != "" {
		if err := validateHeader(cfg.Header); err != nil {
			return "", "", err
		}
		header = cfg.Header
	}
	if cfg.Prefix != nil {
		if strings.ContainsAny(*cfg.Prefix, "\r\n") {
			return "", "", fmt.Errorf("%w: prefix contains a line break", ErrInvalidConfig)
		}
		prefix = *cfg.Prefix
	}
	return header, prefix, nil
}

// validateHeader requires an RFC 9110 token that is not a proxy-owned or
// hop-by-hop header.
func validateHeader(name string) error {
	for i := 0; i < len(name); i++ {
		if !isTokenChar(name[i]) {
			return fmt.Errorf("%w: header %q is not a valid HTTP header name", ErrInvalidConfig, name)
		}
	}
	if forbiddenHeaders[http.CanonicalHeaderKey(name)] {
		return fmt.Errorf("%w: header %q cannot be injected", ErrInvalidConfig, name)
	}
	return nil
}

func isTokenChar(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	}
	return strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}

// connector serves one bearer connection.
type connector struct {
	adapter *Adapter
	conn    credentials.Connection
	header  string
	prefix  string
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

// Mint reads the static token from the mounted secret and returns it with the
// configured header and prefix. ExpiresAt is left zero: the token is static.
// The token is never logged.
func (c *connector) Mint(ctx context.Context, id credentials.CallerIdentity) (credentials.Credential, error) {
	tok, err := c.adapter.secrets.Resolve(ctx, c.conn.SecretRef)
	if err != nil {
		return credentials.Credential{}, fmt.Errorf("resolve bearer secret: %w", err)
	}
	token := strings.TrimSpace(string(tok))

	c.adapter.logger.Debug(ctx, "resolved bearer credential",
		logging.Field{Key: "connection", Value: c.conn.Name},
		logging.Field{Key: "tenant_id", Value: id.TenantID},
		logging.Field{Key: "run_id", Value: id.RunID},
	)

	return credentials.Credential{
		Token:  token,
		Header: c.header,
		Value:  c.prefix + token,
	}, nil
}
