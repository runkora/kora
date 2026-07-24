package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	platformconns "github.com/clear-route/orbit/internal/platform/connections"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// ConnectionCatalogEntry is one tenant-scoped connection in the read-only
// catalog the frontend connector picker consumes. actions is derived from the
// adapter type via the shared internal/platform/connections metadata table; it
// serializes as [] (never null) for any adapter without a declared vocabulary.
// qualifier carries the adapter's grant-time resource dimension (nil when the
// adapter has none) so the attachment picker can render the resource-scope
// block; see docs/adr/2026-07-22-connection-scope-qualifiers.md.
type ConnectionCatalogEntry struct {
	Name      string                    `json:"name"`
	Adapter   string                    `json:"adapter"`
	Actions   []string                  `json:"actions"`
	Baseline  []string                  `json:"baseline"`
	Qualifier *ConnectionQualifierEntry `json:"qualifier,omitempty"`
}

// ConnectionQualifierEntry describes a connection's resource-qualifier
// dimension for the picker: the dimension's name ("repository", "namespace")
// and the connection's optional config-declared ceiling allowlist.
type ConnectionQualifierEntry struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Ceiling     []string `json:"ceiling"`
}

// connectionRegistryEntry is the wire shape of one element of connections.json,
// the registry the operator's ConnectionAggregator renders into the
// orbit-tenant-connections ConfigMap. secret_ref is intentionally ignored.
// base_host IS read: it is the upstream authority (not a secret), and the grant
// compiler needs it to derive a connector's auto-egress hosts via the shared
// adapter metadata table. config is peeked only for the adapter's declared
// qualifier-ceiling key (an allowlist of resource names — not secret material).
type connectionRegistryEntry struct {
	TenantID string          `json:"tenant_id"`
	Name     string          `json:"name"`
	Adapter  string          `json:"adapter"`
	BaseHost string          `json:"base_host"`
	Config   json.RawMessage `json:"config,omitempty"`
}

// ConnectionCatalog serves the read-only, tenant-filtered connection catalog
// from the mounted connections.json ConfigMap. It reads the file fresh on every
// request (os.ReadFile + json.Unmarshal): the registry is small and reads are
// infrequent (admin UI only), so a per-request read is the simplest correct
// design and avoids a watch goroutine. A missing or empty file yields an empty
// catalog, never an error — the operator may not have rendered the ConfigMap yet.
type ConnectionCatalog struct {
	path   string
	logger logging.Logger
}

// NewConnectionCatalog constructs a catalog reader over the connections.json at
// path. An empty path is permitted and yields an always-empty catalog.
func NewConnectionCatalog(path string, logger logging.Logger) *ConnectionCatalog {
	return &ConnectionCatalog{path: path, logger: logger}
}

// ListByTenant returns the catalog entries for tenantID. It reads and parses
// connections.json on each call, filters to the tenant, and derives actions from
// the adapter type. An empty path or absent file returns an empty (non-nil)
// slice; a parse error is returned wrapped.
func (c *ConnectionCatalog) ListByTenant(ctx context.Context, tenantID string) ([]ConnectionCatalogEntry, error) {
	entries, err := c.load(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ConnectionCatalogEntry, 0, len(entries))
	for _, e := range entries {
		if e.TenantID != tenantID {
			continue
		}
		entry := ConnectionCatalogEntry{
			Name:     e.Name,
			Adapter:  e.Adapter,
			Actions:  platformconns.ActionNames(e.Adapter),
			Baseline: platformconns.BaselineActions(e.Adapter),
		}
		if qm := platformconns.Qualifier(e.Adapter); qm != nil {
			entry.Qualifier = &ConnectionQualifierEntry{
				Name:        qm.Name,
				Description: qm.Description,
				Ceiling:     qualifierCeiling(e.Config, qm.ConfigKey),
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// qualifierCeiling extracts the adapter's qualifier-ceiling allowlist from a
// connection's config. Empty (non-nil) when the config declares none.
func qualifierCeiling(config json.RawMessage, key string) []string {
	out := []string{}
	if len(config) == 0 || key == "" {
		return out
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(config, &raw); err != nil {
		return out
	}
	var list []string
	if err := json.Unmarshal(raw[key], &list); err != nil {
		return out
	}
	for _, v := range list {
		if v = strings.ToLower(strings.TrimSpace(v)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// ResolveConnection returns the adapter type and base host for the named
// connection within tenantID, reading connections.json fresh. ok is false when
// no such connection is registered (not yet rendered, or a typo) — callers
// derive auto-egress only for connections that resolve, never failing grant
// reconciliation on a missing one. A parse error is returned wrapped.
func (c *ConnectionCatalog) ResolveConnection(ctx context.Context, tenantID, name string) (adapter, baseHost string, ok bool, err error) {
	entries, err := c.load(ctx)
	if err != nil {
		return "", "", false, err
	}
	for _, e := range entries {
		if e.TenantID == tenantID && e.Name == name {
			return e.Adapter, e.BaseHost, true, nil
		}
	}
	return "", "", false, nil
}

// load reads and unmarshals connections.json. An empty path or a missing file is
// tolerated as an empty registry (the ConfigMap may not be rendered yet).
func (c *ConnectionCatalog) load(ctx context.Context) ([]connectionRegistryEntry, error) {
	if c.path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(c.path)
	if err != nil {
		if os.IsNotExist(err) {
			c.logger.Warn(ctx, "connection registry file does not exist — empty catalog until it is mounted",
				logging.Field{Key: "path", Value: c.path},
			)
			return nil, nil
		}
		return nil, fmt.Errorf("read connection registry %q: %w", c.path, err)
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var entries []connectionRegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse connection registry %q: %w", c.path, err)
	}
	return entries, nil
}
