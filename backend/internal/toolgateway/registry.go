// File-backed, hot-reloadable MCP server registry.
//
// The operator's MCPServerAggregator renders the orbit-mcp-servers ConfigMap
// (one key, servers.json — a JSON array of Server entries) into orbit-system;
// the publisher mounts it as a file. This reader parses servers.json and holds
// the parsed servers in memory, hot-reloading on change. It mirrors the egress
// gateway's connection ConfigMapRegistry (internal/credentials/configmap.go):
// same fsnotify directory-watch (Kubernetes rotates the "..data" symlink
// atomically on a ConfigMap update), same debounce, same OnReload hook the
// introspection worker uses to re-sync.
package toolgateway

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// registryReloadDebounce coalesces a burst of fsnotify events into one reload.
const registryReloadDebounce = 200 * time.Millisecond

// Registry is the in-memory, hot-reloadable view of servers.json.
type Registry struct {
	path   string
	logger logging.Logger

	mu       sync.RWMutex
	servers  []Server
	loaded   bool
	onReload func(ctx context.Context)
}

// LoadRegistry reads servers.json from path and returns a Registry holding the
// parsed servers. An empty path or absent file is permitted (the operator may
// not have rendered the ConfigMap yet) and yields an empty registry that is NOT
// marked loaded — the prune-guard relies on Loaded() to avoid revoking scopes
// before the registry has ever been mounted.
func LoadRegistry(ctx context.Context, path string, logger logging.Logger) (*Registry, error) {
	r := &Registry{path: path, logger: logger}
	servers, mounted, err := loadServers(ctx, path, logger)
	if err != nil {
		return nil, err
	}
	r.servers = servers
	r.loaded = mounted
	logger.Info(ctx, "MCP server registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "servers", Value: len(servers)},
		logging.Field{Key: "mounted", Value: mounted},
	)
	return r, nil
}

// OnReload registers a callback invoked after every successful reload (not at
// initial load). The introspection worker uses it to re-introspect + republish.
func (r *Registry) OnReload(fn func(ctx context.Context)) {
	r.mu.Lock()
	r.onReload = fn
	r.mu.Unlock()
}

// Servers returns a copy of the current server list.
func (r *Registry) Servers() []Server {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Server, len(r.servers))
	copy(out, r.servers)
	return out
}

// lookup returns the registered server with the given name (any tenant). The
// gateway resolves the caller's server by slug; for the MVP a slug is unique in
// the registry. Returns false if no such server.
func (r *Registry) lookup(name string) (Server, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.servers {
		if s.Name == name {
			return s, true
		}
	}
	return Server{}, false
}

// Loaded reports whether the registry file has been successfully read at least
// once (the ConfigMap is mounted). The prune-guard requires this before any
// destructive scope prune.
func (r *Registry) Loaded() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.loaded
}

// Watch watches the directory containing servers.json and reloads on change,
// mirroring the connection registry's watch. A debounce coalesces event bursts.
// The goroutine exits when ctx is canceled; an empty path disables the watch.
func (r *Registry) Watch(ctx context.Context) {
	if r.path == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.logger.Error(ctx, "failed to create fsnotify watcher for MCP server registry — hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	defer func() {
		if cerr := watcher.Close(); cerr != nil {
			r.logger.Error(ctx, "failed to close MCP server registry fsnotify watcher",
				logging.Field{Key: "error", Value: cerr.Error()})
		}
	}()

	dir := filepath.Dir(r.path)
	base := filepath.Base(r.path)
	if addErr := watcher.Add(dir); addErr != nil {
		r.logger.Error(ctx, "failed to watch MCP server registry directory — hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()})
		return
	}

	r.logger.Info(ctx, "watching MCP server registry for changes",
		logging.Field{Key: "path", Value: r.path})

	var debounce *time.Timer
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
			debounce = time.AfterFunc(registryReloadDebounce, func() { r.reload(ctx) })

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.logger.Error(ctx, "MCP server registry watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()})
		}
	}
}

// reload re-reads servers.json and swaps in the new set under a write lock, then
// fires the OnReload hook. A read error keeps the current set — a transient
// mount glitch must not blank the registry mid-flight.
func (r *Registry) reload(ctx context.Context) {
	servers, mounted, err := loadServers(ctx, r.path, r.logger)
	if err != nil {
		r.logger.Error(ctx, "MCP server registry reload failed — keeping current servers",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}

	r.mu.Lock()
	r.servers = servers
	if mounted {
		r.loaded = true
	}
	hook := r.onReload
	r.mu.Unlock()

	r.logger.Info(ctx, "MCP server registry reloaded",
		logging.Field{Key: "servers", Value: len(servers)})
	if hook != nil {
		hook(ctx)
	}
}

// loadServers reads and parses servers.json. The bool reports whether the file
// was actually present and read (mounted) — distinct from "parsed to zero
// servers". An empty path or absent file yields (nil, false, nil): not an error,
// but not mounted, so the prune-guard holds.
func loadServers(ctx context.Context, path string, logger logging.Logger) ([]Server, bool, error) {
	if path == "" {
		logger.Warn(ctx, "MCP server registry path is empty — no servers will be introspected")
		return nil, false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn(ctx, "MCP server registry file does not exist — no servers until it is mounted",
				logging.Field{Key: "path", Value: path})
			return nil, false, nil
		}
		return nil, false, err
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return nil, true, nil
	}

	var servers []Server
	if err := json.Unmarshal([]byte(trimmed), &servers); err != nil {
		return nil, false, err
	}
	return servers, true, nil
}

// isRelevantRegistryEvent reports whether an fsnotify event should trigger a
// reload — a direct write to the target file (local dev) or the "..data" symlink
// swap Kubernetes uses for ConfigMap-volume updates.
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
