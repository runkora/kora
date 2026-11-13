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

// ErrEmptySecret is returned by Mint when the mounted secret is blank.
var ErrEmptySecret = errors.New("apikey secret is empty")

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

func parseConfig(raw json.RawMessage) ([]platformconns.APIKeyHeader, error) {
	cfg, err := platformconns.ParseAPIKeyConfig(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", credentials.ErrInvalidConfig, err)
	}
	return cfg.Headers, nil
}

// connector serves one apikey connection.
type connector struct {
	adapter *Adapter
	conn    credentials.Connection
	headers []platformconns.APIKeyHeader
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
		return credentials.Credential{}, ErrEmptySecret
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
