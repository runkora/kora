package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// reloadDebounce coalesces a burst of fsnotify events into one reload.
// Kubernetes rotates the ConfigMap "..data" symlink atomically but may fire
// several events in quick succession.
const reloadDebounce = 200 * time.Millisecond

// tenantSources is the per-tenant subtree inside marketplace-sources.json.
type tenantSources struct {
	Sources []MarketplaceSource `json:"sources"`
}

// sourcesFile is the wire shape of marketplace-sources.json.
// Keys are tenant IDs; individual source entries carry no tenantId field.
type sourcesFile struct {
	Tenants map[string]tenantSources `json:"tenants"`
}

// Registry is the in-memory, hot-reloadable view of marketplace-sources.json.
// Safe for concurrent use.
type Registry struct {
	path   string
	logger logging.Logger

	mu      sync.RWMutex
	sources map[string][]MarketplaceSource // key: tenantID
}

// NewRegistry constructs a Registry for the given file path and reads the
// initial contents. An absent file is not an error — the ConfigMap may not
// be mounted yet; ListSources returns empty until the file appears and Watch
// picks up the change.
func NewRegistry(path string, logger logging.Logger) (*Registry, error) {
	r := &Registry{path: path, logger: logger}
	sources, err := loadSources(path)
	if err != nil {
		return nil, err
	}
	r.sources = sources
	total := 0
	for _, ss := range sources {
		total += len(ss)
	}
	logger.Info(context.Background(), "marketplace registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: total},
	)
	return r, nil
}

// ListSources returns a copy of the marketplace source list for tenantID.
// Returns an empty slice if the tenant has no declared marketplaces.
func (r *Registry) ListSources(tenantID string) []MarketplaceSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ss := r.sources[tenantID]
	if len(ss) == 0 {
		return []MarketplaceSource{}
	}
	out := make([]MarketplaceSource, len(ss))
	copy(out, ss)
	return out
}

// FindByName returns the MarketplaceSource for tenantID whose Name field
// matches name exactly (case-sensitive). Returns false if no match.
func (r *Registry) FindByName(tenantID, name string) (MarketplaceSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sources[tenantID] {
		if s.Name == name {
			return s, true
		}
	}
	return MarketplaceSource{}, false
}

// Watch watches the directory containing marketplace-sources.json and reloads
// on change. Kubernetes rotates the "..data" symlink atomically on a ConfigMap
// update — watching the directory (not the file) catches it. A debounce
// coalesces event bursts. The goroutine exits when ctx is canceled.
func (r *Registry) Watch(ctx context.Context) {
	if r.path == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.logger.Error(ctx, "failed to create fsnotify watcher for marketplace registry — hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}
	defer func() {
		if cerr := watcher.Close(); cerr != nil {
			r.logger.Error(ctx, "failed to close marketplace registry watcher",
				logging.Field{Key: "error", Value: cerr.Error()})
		}
	}()

	dir := filepath.Dir(r.path)
	base := filepath.Base(r.path)
	if addErr := watcher.Add(dir); addErr != nil {
		r.logger.Error(ctx, "failed to watch marketplace registry directory — hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()})
		return
	}

	r.logger.Info(ctx, "watching marketplace registry for changes",
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
			if !isRelevantEvent(event, base) {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(reloadDebounce, func() { r.reload(ctx) })

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.logger.Error(ctx, "marketplace registry watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()})
		}
	}
}

// reload re-reads marketplace-sources.json and swaps in the new set under a
// write lock. A parse error keeps the current set — a transient mount glitch
// must not blank the registry mid-flight.
func (r *Registry) reload(ctx context.Context) {
	sources, err := loadSources(r.path)
	if err != nil {
		r.logger.Error(ctx, "marketplace registry reload failed — keeping current sources",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}

	total := 0
	for _, ss := range sources {
		total += len(ss)
	}

	r.mu.Lock()
	r.sources = sources
	r.mu.Unlock()

	r.logger.Info(ctx, "marketplace registry reloaded",
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: total})
}

// loadSources reads and parses marketplace-sources.json. An absent file
// returns an empty map (not an error).
func loadSources(path string) (map[string][]MarketplaceSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]MarketplaceSource{}, nil
		}
		return nil, err
	}

	var f sourcesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	out := make(map[string][]MarketplaceSource, len(f.Tenants))
	for tenantID, ts := range f.Tenants {
		if ts.Sources == nil {
			out[tenantID] = []MarketplaceSource{}
		} else {
			out[tenantID] = ts.Sources
		}
	}
	return out, nil
}

// isRelevantEvent reports whether an fsnotify event should trigger a reload:
// either a direct write to the target file (local dev) or the "..data"
// symlink swap Kubernetes uses for ConfigMap-volume updates.
func isRelevantEvent(event fsnotify.Event, base string) bool {
	name := filepath.Base(event.Name)
	if name != base && name != "..data" {
		return false
	}
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Rename) ||
		event.Has(fsnotify.Remove)
}
