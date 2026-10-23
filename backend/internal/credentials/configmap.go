package credentials

// ConfigMapRegistry is the gateway's connection registry, read from the file
// the operator renders into kora-system. The ConnectionAggregator writes one
// ConfigMap key, connections.json, holding a JSON array of registry entries;
// the gateway mounts it and parses it into Connection values, serving the
// ListByHost and ListByTenant lookups the broker and ScopePublisher use.
//
// Hot-reload watches the directory with fsnotify, since Kubernetes rotates the
// "..data" symlink atomically on a ConfigMap-volume update. Reload fires the
// OnReload hook, which republishes the connection scope vocabulary.
//
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md,
// Amendment 3.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	platformconns "github.com/runkora/kora/backend/internal/platform/connections"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// registryEntry is the wire shape of one element of connections.json,
// runtime contract the operator's ConnectionAggregator renders. tenant_id equals
// claims.Kora.TenantID (the KoraTenant CR name); secret_ref is a flat key the
// k8sfile resolver reads under the connection-secret root.
type registryEntry struct {
	TenantID  string          `json:"tenant_id"`
	Name      string          `json:"name"`
	Adapter   string          `json:"adapter"`
	BaseHost  string          `json:"base_host"`
	SecretRef string          `json:"secret_ref"`
	Config    json.RawMessage `json:"config,omitempty"`
	CACert    string          `json:"ca_cert,omitempty"`
}

// ConfigMapRegistry is the gateway's connection registry: file-backed and
// hot-reloadable. It loads connections.json from the mounted ConfigMap path and
// holds the parsed connections in memory behind an RWMutex. Components take the
// narrow interface they call, so this concrete type is what binaries wire.
type ConfigMapRegistry struct {
	path   string
	logger logging.Logger

	mu       sync.RWMutex
	conns    []*Connection
	onReload func(ctx context.Context)
}

// LoadConfigMapRegistry reads connections.json from path and returns a registry
// holding the parsed connections. An empty path or an absent file is permitted
// (the operator may not have rendered the ConfigMap yet) and yields an empty
// registry that returns ErrConnectionNotFound for every host until a reload
// populates it, mirroring cabundle.LoadEager's tolerance of a missing mount.
func LoadConfigMapRegistry(ctx context.Context, path string, logger logging.Logger) (*ConfigMapRegistry, error) {
	r := &ConfigMapRegistry{path: path, logger: logger}
	conns, err := loadConnections(ctx, path, logger)
	if err != nil {
		return nil, err
	}
	r.conns = conns
	logger.Info(ctx, "connection registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "connections", Value: len(conns)},
	)
	return r, nil
}

// OnReload registers a callback invoked after every successful reload (and not
// at initial load). The gateway uses it to republish the connection scope
// vocabulary to the policy service.
func (r *ConfigMapRegistry) OnReload(fn func(ctx context.Context)) {
	r.mu.Lock()
	r.onReload = fn
	r.mu.Unlock()
}

// ListByHost returns every connection serving host within tenantID, in load
// order. A connection serves its base_host and the family of hosts its adapter
// declares. Comparison is case-insensitive and ignores any :port suffix.
// Returns ErrConnectionNotFound when no connection serves host.
func (r *ConfigMapRegistry) ListByHost(_ context.Context, tenantID, host string) ([]*Connection, error) {
	want := normalizeHost(host)
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []*Connection
	for _, c := range r.conns {
		if c.TenantID == tenantID && serves(c, want) {
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, ErrConnectionNotFound
	}
	return out, nil
}

// serves reports whether c serves the normalized host.
func serves(c *Connection, host string) bool {
	if normalizeHost(c.BaseHost) == host {
		return true
	}
	for _, h := range platformconns.EgressHosts(string(c.Adapter), c.BaseHost) {
		if hostPatternMatches(normalizeHost(h), host) {
			return true
		}
	}
	return false
}

// hostPatternMatches reports whether the (already normalized) host matches the
// (already normalized) pattern: either an exact host or a "*.suffix" wildcard
// covering any subdomain of suffix.
func hostPatternMatches(pattern, host string) bool {
	if pattern == host {
		return true
	}
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".githubusercontent.com"
		return strings.HasSuffix(host, suffix) && len(host) > len(suffix)
	}
	return false
}

// ListByTenant returns connections for tenantID. An empty tenantID returns ALL
// connections across every tenant. The ScopePublisher relies on this to
// publish the full connection:<name>:<action> vocabulary on reload.
func (r *ConfigMapRegistry) ListByTenant(_ context.Context, tenantID string) ([]*Connection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Connection, 0, len(r.conns))
	for _, c := range r.conns {
		if tenantID == "" || c.TenantID == tenantID {
			out = append(out, c)
		}
	}
	return out, nil
}

// Watch watches the directory containing connections.json and reloads the
// registry on change, mirroring the identity service's Secret/ConfigMap
// directory-watch (Kubernetes rotates the "..data" symlink atomically). A 200ms
// debounce coalesces event bursts. The goroutine exits when ctx is canceled.
// Run it in a goroutine; an empty path disables the watch.
func (r *ConfigMapRegistry) Watch(ctx context.Context) {
	if r.path == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.logger.Error(ctx, "failed to create fsnotify watcher for connection registry; hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	defer func() {
		if cerr := watcher.Close(); cerr != nil {
			r.logger.Error(ctx, "failed to close connection registry fsnotify watcher",
				logging.Field{Key: "error", Value: cerr.Error()},
			)
		}
	}()

	dir := filepath.Dir(r.path)
	base := filepath.Base(r.path)
	if addErr := watcher.Add(dir); addErr != nil {
		r.logger.Error(ctx, "failed to watch connection registry directory; hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()},
		)
		return
	}

	r.logger.Info(ctx, "watching connection registry for changes",
		logging.Field{Key: "path", Value: r.path},
	)

	var debounce *time.Timer
	const debounceDelay = 200 * time.Millisecond

	for {
		select {
		case <-ctx.Done():
			if debounce != nil {
				debounce.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if !isRelevantRegistryEvent(event, base) {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(debounceDelay, func() { r.reload(ctx) })

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.logger.Error(ctx, "connection registry watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()},
			)
		}
	}
}

// reload re-reads connections.json and swaps in the new set under a write lock,
// then fires the OnReload hook. A read error keeps the current set. A transient
// mount glitch must not blank the registry mid-flight.
func (r *ConfigMapRegistry) reload(ctx context.Context) {
	conns, err := loadConnections(ctx, r.path, r.logger)
	if err != nil {
		r.logger.Error(ctx, "connection registry reload failed; keeping current connections",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	r.mu.Lock()
	r.conns = conns
	hook := r.onReload
	r.mu.Unlock()

	r.logger.Info(ctx, "connection registry reloaded",
		logging.Field{Key: "connections", Value: len(conns)},
	)
	if hook != nil {
		hook(ctx)
	}
}

// loadConnections reads and parses connections.json into Connection values. An
// empty path or absent file yields an empty slice (not an error), matching
// cabundle's tolerance of an unmounted secret.
func loadConnections(ctx context.Context, path string, logger logging.Logger) ([]*Connection, error) {
	if path == "" {
		logger.Warn(ctx, "connection registry path is empty; no connections will be brokered")
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn(ctx, "connection registry file does not exist; no connections until it is mounted",
				logging.Field{Key: "path", Value: path})
			return nil, nil
		}
		return nil, fmt.Errorf("read connection registry %q: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, nil
	}

	var entries []registryEntry
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		return nil, fmt.Errorf("parse connection registry %q: %w", path, err)
	}

	conns := make([]*Connection, 0, len(entries))
	for _, e := range entries {
		conns = append(conns, &Connection{
			// ID is derived (tenant|name) so the github_app adapter's token
			// cache key, which is conn.ID, stays stable across reloads.
			ID:        e.TenantID + "|" + e.Name,
			TenantID:  e.TenantID,
			Name:      e.Name,
			Adapter:   AdapterType(e.Adapter),
			BaseHost:  e.BaseHost,
			SecretRef: e.SecretRef,
			Config:    e.Config,
			CACert:    e.CACert,
		})
	}
	return conns, nil
}

// normalizeHost lowercases a host and strips any :port suffix so base_host
// "github.com" matches a request authority "github.com:443".
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if stripped, _, err := net.SplitHostPort(h); err == nil {
		h = stripped
	}
	// net/http accepts a fully-qualified "github.com." and passes it through as
	// the request Host. Left in place it misses every registered connection,
	// and the broker falls back to passthrough: no action classification and no
	// connection-scope check on a host that has both.
	if len(h) > 1 {
		h = strings.TrimSuffix(h, ".")
	}
	return h
}

// isRelevantRegistryEvent reports whether an fsnotify event on a watched
// directory should trigger a reload. It matches either a direct write to the
// target file (local dev) or the "..data" symlink swap Kubernetes uses for
// ConfigMap-volume updates.
func isRelevantRegistryEvent(event fsnotify.Event, base string) bool {
	name := filepath.Base(event.Name)
	if name != base && name != "..data" {
		return false
	}
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Rename) ||
		event.Has(fsnotify.Remove)
}
