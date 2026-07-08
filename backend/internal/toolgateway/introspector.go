// MCP introspection worker.
//
// For each server in the mounted registry the worker opens an MCP session
// (initialize + tools/list over Streamable HTTP) and records the discovered
// tools. On failure the server is flagged Degraded and its prior tools are
// RETAINED in the snapshot (never silently dropped), so a transient outage does
// not revoke live scopes. The worker holds the per-server snapshots the scope
// publisher (Phase 3.3) and catalog writer (Phase 3.4) consume.
//
// In-cluster, outbound calls to a server endpoint flow through the gateway's
// identity-proxy sidecar, which injects the gateway's platform token — so a
// first-party Orbit MCP whose tools/list is auth-gated is reachable without the
// worker handling tokens itself (the same way the routescopes client reaches
// policy-service). A standalone/unauthenticated server is reached directly.
package toolgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// introspectClientName identifies the gateway in the MCP initialize handshake.
const introspectClientName = "orbit-tool-gateway"

// defaultIntrospectTimeout bounds a single server's initialize + tools/list.
const defaultIntrospectTimeout = 15 * time.Second

// Introspector introspects MCP servers and holds their snapshots. It is
// concurrency-safe: Sync mutates snapshots under a write lock; Snapshot and the
// prune-guard predicate read under a read lock.
type Introspector struct {
	httpClient *http.Client
	version    string
	timeout    time.Duration
	logger     logging.Logger

	mu        sync.RWMutex
	snapshots map[string]*ServerSnapshot // key: serverKey(tenantID, name)
}

// NewIntrospector constructs an Introspector. A nil httpClient defaults to a
// client with the introspection timeout.
func NewIntrospector(httpClient *http.Client, version string, logger logging.Logger) *Introspector {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultIntrospectTimeout}
	}
	return &Introspector{
		httpClient: httpClient,
		version:    version,
		timeout:    defaultIntrospectTimeout,
		logger:     logger,
		snapshots:  make(map[string]*ServerSnapshot),
	}
}

// serverKey identifies a server uniquely. Two tenants' servers may share a name
// (and thus scope names) but are distinct endpoints, so the snapshot is keyed by
// (tenant_id, name).
func serverKey(tenantID, name string) string { return tenantID + "|" + name }

// Sync introspects every server in the list, updating snapshots. Servers no
// longer in the list are dropped from the snapshot map (a removed server's tools
// must leave the keep-list). Each server is introspected independently; one
// failure never aborts the batch.
func (i *Introspector) Sync(ctx context.Context, servers []Server) {
	current := make(map[string]struct{}, len(servers))

	for _, srv := range servers {
		key := serverKey(srv.TenantID, srv.Name)
		current[key] = struct{}{}

		tools, err := i.introspectOne(ctx, srv)
		i.recordResult(ctx, srv, key, tools, err)
	}

	// Drop snapshots for servers no longer registered.
	i.mu.Lock()
	for key := range i.snapshots {
		if _, ok := current[key]; !ok {
			delete(i.snapshots, key)
		}
	}
	i.mu.Unlock()
}

// recordResult updates the snapshot for one server. On success the tools are
// replaced and state set OK. On failure the prior tools are retained and state
// set Degraded — never dropped.
func (i *Introspector) recordResult(ctx context.Context, srv Server, key string, tools []Tool, err error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	snap := i.snapshots[key]
	if snap == nil {
		snap = &ServerSnapshot{Server: srv, State: StatePending}
		i.snapshots[key] = snap
	}
	snap.Server = srv
	snap.LastIntrospectedAt = time.Now()

	if err != nil {
		snap.State = StateDegraded
		snap.LastErr = err.Error()
		i.logger.Error(ctx, "MCP server introspection failed — retaining prior tools",
			logging.Field{Key: "server", Value: srv.Name},
			logging.Field{Key: "tenant_id", Value: srv.TenantID},
			logging.Field{Key: "endpoint", Value: srv.Endpoint},
			logging.Field{Key: "retained_tools", Value: len(snap.Tools)},
			logging.Field{Key: "error", Value: err.Error()})
		return
	}

	sort.Slice(tools, func(a, b int) bool { return tools[a].Name < tools[b].Name })
	snap.Tools = tools
	snap.State = StateOK
	snap.LastErr = ""
	i.logger.Info(ctx, "MCP server introspected",
		logging.Field{Key: "server", Value: srv.Name},
		logging.Field{Key: "tenant_id", Value: srv.TenantID},
		logging.Field{Key: "tools", Value: len(tools)})
}

// introspectOne opens an MCP session to the server and lists its tools
// (paginating). Returns an error on any connect/list failure.
func (i *Introspector) introspectOne(ctx context.Context, srv Server) ([]Tool, error) {
	cctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	client := sdk.NewClient(&sdk.Implementation{Name: introspectClientName, Version: i.version}, nil)
	transport := &sdk.StreamableClientTransport{Endpoint: srv.Endpoint, HTTPClient: i.httpClient}

	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			i.logger.Debug(ctx, "closing introspection session",
				logging.Field{Key: "server", Value: srv.Name},
				logging.Field{Key: "error", Value: cerr.Error()})
		}
	}()

	var tools []Tool
	for tool, terr := range session.Tools(cctx, nil) {
		if terr != nil {
			return nil, fmt.Errorf("list tools: %w", terr)
		}
		tools = append(tools, Tool{
			Name:           tool.Name,
			Description:    tool.Description,
			InputSchema:    marshalSchema(tool.InputSchema),
			RequiresScopes: requiresFromMeta(tool.Meta),
		})
	}
	return tools, nil
}

// metaRequiresKey is the tool _meta field a first-party MCP server uses to
// declare the platform scopes its tool calls upstream (see Tool.RequiresScopes).
const metaRequiresKey = "orbit/requires"

// requiresFromMeta extracts the declared upstream scopes from a tool's _meta.
// It accepts a JSON array of strings under metaRequiresKey; anything else
// (absent, wrong type, non-string elements) yields nil — external servers that
// set no such meta contribute no requires.
func requiresFromMeta(meta sdk.Meta) []string {
	arr, ok := meta[metaRequiresKey].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FlagDegraded marks a server's snapshot Degraded with a reason — used to flag
// a scope collision (the ADR's "server Degraded" on a leaf/prefix collision)
// without dropping its retained tools. A no-op if the server has no snapshot.
func (i *Introspector) FlagDegraded(tenantID, name, reason string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if snap := i.snapshots[serverKey(tenantID, name)]; snap != nil {
		snap.State = StateDegraded
		snap.LastErr = reason
	}
}

// Snapshot returns a copy of the current per-server snapshots.
func (i *Introspector) Snapshot() []ServerSnapshot {
	i.mu.RLock()
	defer i.mu.RUnlock()
	out := make([]ServerSnapshot, 0, len(i.snapshots))
	for _, s := range i.snapshots {
		out = append(out, *s)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Server.TenantID != out[b].Server.TenantID {
			return out[a].Server.TenantID < out[b].Server.TenantID
		}
		return out[a].Server.Name < out[b].Server.Name
	})
	return out
}

// AllSeenOrRetained reports whether every server in the list has either been
// successfully introspected this lifetime (StateOK) or is Degraded with prior
// tools to retain. A server that has NEVER been reachable since boot (Pending,
// or Degraded with no tools) returns false — the prune-guard then defers the
// destructive prune so a cold/partial start cannot revoke live scopes.
func (i *Introspector) AllSeenOrRetained(servers []Server) bool {
	i.mu.RLock()
	defer i.mu.RUnlock()
	for _, srv := range servers {
		snap := i.snapshots[serverKey(srv.TenantID, srv.Name)]
		if snap == nil {
			return false
		}
		if snap.State == StateOK {
			continue
		}
		if snap.State == StateDegraded && len(snap.Tools) > 0 {
			continue
		}
		return false
	}
	return true
}

// marshalSchema renders a tool's input schema (a map[string]any from the client)
// to JSON, defaulting an empty schema to "{}".
func marshalSchema(schema any) json.RawMessage {
	if schema == nil {
		return json.RawMessage("{}")
	}
	raw, err := json.Marshal(schema)
	if err != nil || len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}
