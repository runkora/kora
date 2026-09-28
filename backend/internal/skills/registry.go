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

// sourcesFile is the wire shape of marketplace-sources.json.
type sourcesFile struct {
	Sources []MarketplaceSource `json:"sources"`
}

// Registry is the in-memory, hot-reloadable view of marketplace-sources.json.
// Safe for concurrent use.
type Registry struct {
	path   string
	logger logging.Logger

	mu      sync.RWMutex
	sources []MarketplaceSource
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
	logger.Info(context.Background(), "marketplace registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "sources", Value: len(sources)},
	)
	return r, nil
}

// ListSources returns a copy of the current marketplace source list.
func (r *Registry) ListSources() []MarketplaceSource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]MarketplaceSource, len(r.sources))
	copy(out, r.sources)
	return out
}

// FindByName returns the MarketplaceSource whose Name field matches name
// exactly (case-sensitive). Returns false if no match.
func (r *Registry) FindByName(name string) (MarketplaceSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.sources {
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

	r.mu.Lock()
	r.sources = sources
	r.mu.Unlock()

	r.logger.Info(ctx, "marketplace registry reloaded",
		logging.Field{Key: "sources", Value: len(sources)})
}

// loadSources reads and parses marketplace-sources.json. An absent file
// returns an empty slice (not an error).
func loadSources(path string) ([]MarketplaceSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []MarketplaceSource{}, nil
		}
		return nil, err
	}

	var f sourcesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Sources == nil {
		return []MarketplaceSource{}, nil
	}
	return f.Sources, nil
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
