package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ConnectionCatalogEntry is one tenant-scoped connection in the read-only
// catalog the frontend connector picker consumes. actions is derived from the
// adapter type via adapterActions; it serializes as [] (never null) for any
// adapter without a declared vocabulary.
type ConnectionCatalogEntry struct {
	Name    string   `json:"name"`
	Adapter string   `json:"adapter"`
	Actions []string `json:"actions"`
}

// connectionRegistryEntry is the wire shape of one element of connections.json,
// the registry the operator's ConnectionAggregator renders into the
// orbit-tenant-connections ConfigMap. Only the fields the catalog needs are
// modeled; the rest (base_host, secret_ref, config) are intentionally ignored
// so this stays a non-sensitive read.
type connectionRegistryEntry struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Adapter  string `json:"adapter"`
}

// adapterActions maps an adapter type to the grantable action vocabulary the
// connector picker offers. It is a deliberate static table rather than a
// cross-feature lookup into internal/credentials (banned by depguard): the
// catalog is a read-only UI affordance and the mapping is tiny and stable.
var adapterActions = map[string][]string{
	"github_app":  {"repo:read", "repo:write"},
	"passthrough": {},
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
		out = append(out, ConnectionCatalogEntry{
			Name:    e.Name,
			Adapter: e.Adapter,
			Actions: actionsForAdapter(e.Adapter),
		})
	}
	return out, nil
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

// actionsForAdapter returns the static action vocabulary for an adapter type.
// An unknown adapter yields an empty (non-nil) slice so the JSON field
// serializes as [] rather than null.
func actionsForAdapter(adapter string) []string {
	if actions, ok := adapterActions[adapter]; ok {
		out := make([]string, len(actions))
		copy(out, actions)
		return out
	}
	return []string{}
}
