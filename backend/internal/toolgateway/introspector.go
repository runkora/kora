// MCP introspection worker.
//
// For each server in the mounted registry the worker opens an MCP session
// (initialize + tools/list over Streamable HTTP) and records the discovered
// tools. On failure the server is flagged Degraded and its prior tools are
// RETAINED in the snapshot (never silently dropped), so a transient outage does
// not revoke live scopes. The worker holds the per-server snapshots the scope
// publisher (Phase 3.3) and catalog writer (Phase 3.4) consume.
//
// Introspection carries no caller. UpstreamCredentials decides what each
// server receives.
package toolgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// introspectClientName identifies the gateway in the MCP initialize handshake.
const introspectClientName = "kora-tool-gateway"

// defaultIntrospectTimeout bounds a single server's initialize + tools/list.
const defaultIntrospectTimeout = 15 * time.Second

// Backoff for a server that has never been reached, independent of
// ReintrospectIntervalSeconds.
const (
	retryBaseDelay = 2 * time.Second
	retryMaxDelay  = time.Minute
)

// Introspector introspects MCP servers and holds their snapshots. It is
// concurrency-safe: Sync mutates snapshots under a write lock; Snapshot and the
// prune-guard predicate read under a read lock.
type Introspector struct {
	creds   UpstreamCredentials
	version string
	timeout time.Duration
	logger  logging.Logger

	mu        sync.RWMutex
	snapshots map[string]*ServerSnapshot // key: serverKey(tenantID, name)
	retries   map[string]*retrySchedule  // key: serverKey, only while unreached
}

// NewIntrospector constructs an Introspector.
func NewIntrospector(creds UpstreamCredentials, version string, logger logging.Logger) *Introspector {
	return &Introspector{
		creds:     creds,
		version:   version,
		timeout:   defaultIntrospectTimeout,
		logger:    logger,
		snapshots: make(map[string]*ServerSnapshot),
		retries:   make(map[string]*retrySchedule),
	}
}

// retrySchedule is when a never-reached server is next due.
type retrySchedule struct {
	failures int
	at       time.Time
}

// serverKey identifies a server uniquely. Two tenants' servers may share a name
// (and thus scope names) but are distinct endpoints, so the snapshot is keyed by
// (tenant_id, name).
func serverKey(tenantID, name string) string { return tenantID + "|" + name }

// Sync introspects servers in the list, updating snapshots. Servers no longer
// in the list are dropped from the snapshot map (a removed server's tools must
// leave the keep-list). Each server is introspected independently; one failure
// never aborts the batch.
//
// Returns whether a consumer-visible value changed.
//
// force controls whether a server's own ReintrospectIntervalSeconds is
// honored: force=true (registry reload, and the initial boot cycle)
// introspects every server unconditionally; force=false (a periodic timer
// tick) introspects only servers that are due — see due.
func (i *Introspector) Sync(ctx context.Context, servers []Server, force bool) bool {
	current := make(map[string]struct{}, len(servers))
	dirty := false

	for _, srv := range servers {
		key := serverKey(srv.TenantID, srv.Name)
		current[key] = struct{}{}

		if !force && !i.due(key, srv) {
			continue
		}

		tools, err := i.introspectOne(ctx, srv)
		if i.recordResult(ctx, srv, key, tools, err) {
			dirty = true
		}
	}

	// Drop snapshots for servers no longer registered.
	i.mu.Lock()
	for key := range i.snapshots {
		if _, ok := current[key]; !ok {
			delete(i.snapshots, key)
			delete(i.retries, key)
			dirty = true
		}
	}
	i.mu.Unlock()
	return dirty
}

// due reports whether a server is due for re-introspection on the periodic
// timer. A server never introspected this lifetime is always due, and one never
// reached is due once its backoff elapses. Otherwise a
// server with ReintrospectIntervalSeconds <= 0 is reload-only — never due on
// the timer — and one with a positive interval is due once that many seconds
// have passed since it was last introspected.
func (i *Introspector) due(key string, srv Server) bool {
	i.mu.RLock()
	snap, seen := i.snapshots[key]
	var last, retryAt time.Time
	neverReached := false
	if seen {
		last = snap.LastIntrospectedAt
		neverReached = !snap.Reached
		if r := i.retries[key]; r != nil {
			retryAt = r.at
		}
	}
	i.mu.RUnlock()

	if !seen {
		return true
	}
	if neverReached {
		return !time.Now().Before(retryAt)
	}
	if srv.ReintrospectIntervalSeconds <= 0 {
		return false
	}
	return time.Since(last) >= time.Duration(srv.ReintrospectIntervalSeconds)*time.Second
}

// recordResult updates one server's snapshot and reports whether a
// consumer-visible value changed. A failure retains the prior tools.
func (i *Introspector) recordResult(ctx context.Context, srv Server, key string, tools []Tool, err error) bool {
	i.mu.Lock()
	defer i.mu.Unlock()

	snap := i.snapshots[key]
	if snap == nil {
		snap = &ServerSnapshot{Server: srv, State: StatePending}
		i.snapshots[key] = snap
	}
	before := *snap
	snap.Server = srv
	snap.LastIntrospectedAt = time.Now()

	if err != nil {
		snap.State = StateDegraded
		snap.LastErr = err.Error()

		// One incident per outage, not per retry.
		first := before.State != StateDegraded
		if !snap.Reached {
			r := i.retries[key]
			if r == nil {
				r = &retrySchedule{}
				i.retries[key] = r
			}
			r.failures++
			r.at = time.Now().Add(backoffFor(r.failures))
		}

		msg := "MCP server introspection failed"
		fields := []logging.Field{
			{Key: "server", Value: srv.Name},
			{Key: "tenant_id", Value: srv.TenantID},
			{Key: "endpoint", Value: srv.Endpoint},
			{Key: "reason", Value: failureReason(err)},
			{Key: "error", Value: err.Error()},
		}
		if len(snap.Tools) > 0 {
			msg += " — retaining prior tools"
			fields = append(fields, logging.Field{Key: "retained_tools", Value: len(snap.Tools)})
		}
		if first {
			i.logger.Error(ctx, msg, fields...)
		} else {
			i.logger.Warn(ctx, msg, fields...)
		}
		return changed(before, *snap)
	}

	sort.Slice(tools, func(a, b int) bool { return tools[a].Name < tools[b].Name })
	snap.Tools = tools
	snap.State = StateOK
	snap.LastErr = ""
	snap.Reached = true
	delete(i.retries, key)
	i.logger.Info(ctx, "MCP server introspected",
		logging.Field{Key: "server", Value: srv.Name},
		logging.Field{Key: "tenant_id", Value: srv.TenantID},
		logging.Field{Key: "tools", Value: len(tools)})
	return changed(before, *snap)
}

// changed reports whether a difference is one BuildScopes or
// CatalogToolsFromSnapshots reads. LastIntrospectedAt and LastErr are excluded:
// both move between otherwise identical failures.
func changed(before, after ServerSnapshot) bool {
	if before.State != after.State ||
		before.Reached != after.Reached || len(before.Tools) != len(after.Tools) {
		return true
	}
	for n := range after.Tools {
		a, b := after.Tools[n], before.Tools[n]
		if a.Name != b.Name || a.Description != b.Description ||
			!bytes.Equal(a.InputSchema, b.InputSchema) ||
			!slices.Equal(a.RequiresScopes, b.RequiresScopes) {
			return true
		}
	}
	return false
}

// introspectOne opens an MCP session to the server and lists its tools
// (paginating). Returns an error on any connect/list failure.
func (i *Introspector) introspectOne(ctx context.Context, srv Server) ([]Tool, error) {
	cctx, cancel := context.WithTimeout(ctx, i.timeout)
	defer cancel()

	headers, err := i.creds.Headers(ctx, srv, nil)
	if err != nil {
		return nil, fmt.Errorf("upstream credentials: %w", err)
	}
	client := sdk.NewClient(&sdk.Implementation{Name: introspectClientName, Version: i.version}, nil)
	httpClient, upstream := upstreamClient(i.timeout, headers)
	transport := &sdk.StreamableClientTransport{Endpoint: srv.Endpoint, HTTPClient: httpClient}

	session, err := client.Connect(cctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", upstream.upstreamError(err))
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			i.logger.Debug(ctx, "closing introspection session",
				logging.Field{Key: "server", Value: srv.Name},
				logging.Field{Key: "error", Value: upstream.upstreamError(cerr).Error()})
		}
	}()

	var tools []Tool
	for tool, terr := range session.Tools(cctx, nil) {
		if terr != nil {
			return nil, fmt.Errorf("list tools: %w", upstream.upstreamError(terr))
		}
		tools = append(tools, Tool{
			Name:           tool.Name,
			Description:    tool.Description,
			InputSchema:    marshalSchema(tool.InputSchema),
			RequiresScopes: toolRequires(srv, tool),
		})
	}
	return tools, nil
}

// metaRequiresKey is the tool _meta field a global MCP server uses to
// declare the platform scopes its tool calls upstream (see Tool.RequiresScopes).
const metaRequiresKey = "kora/requires"

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

// toolRequires returns the scopes a tool declares in _meta. Requires confer
// authority to every holder of the tool through vocabulary every tenant reads,
// so a tenant server declares none.
func toolRequires(srv Server, tool *sdk.Tool) []string {
	if srv.TenantID != "" {
		return nil
	}
	return requiresFromMeta(tool.Meta)
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

// backoffFor doubles retryBaseDelay per consecutive failure, capped.
func backoffFor(failures int) time.Duration {
	d := retryBaseDelay
	for range failures - 1 {
		d *= 2
		if d >= retryMaxDelay {
			return retryMaxDelay
		}
	}
	return d
}

// NextRetryIn reports how long until the earliest listed server is due, and
// whether any is waiting. Zero means due now.
func (i *Introspector) NextRetryIn(servers []Server) (time.Duration, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	var earliest time.Time
	for _, srv := range servers {
		key := serverKey(srv.TenantID, srv.Name)
		snap := i.snapshots[key]
		if snap == nil {
			return 0, true // never introspected: due now
		}
		if snap.Reached {
			continue
		}
		// At the cap, it would otherwise hold the whole cycle at retryMaxDelay.
		r := i.retries[key]
		if r == nil || backoffFor(r.failures) >= retryMaxDelay {
			continue
		}
		if earliest.IsZero() || r.at.Before(earliest) {
			earliest = r.at
		}
	}
	if earliest.IsZero() {
		return 0, false
	}
	return max(0, time.Until(earliest)), true
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
