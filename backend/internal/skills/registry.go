package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/marketplace"
)

// reloadDebounce coalesces a burst of fsnotify events into one reload.
// Kubernetes swaps a ConfigMap volume's "..data" symlink atomically but may
// fire several events in quick succession.
const reloadDebounce = 200 * time.Millisecond

// Registry is the in-memory view of marketplace-sources.json, which the
// operator renders from SkillMarketplaceSource CRs. Watch reloads it when the
// mounted ConfigMap changes. Safe for concurrent use.
type Registry struct {
	path   string
	logger logging.Logger

	mu      sync.RWMutex
	sources map[string][]MarketplaceSource // key: tenantID
}

// NewRegistry reads marketplace-sources.json at path. An absent file is not an
// error: the operator may not have rendered the ConfigMap yet, so ListSources
// returns empty until Watch sees the file appear.
func NewRegistry(path string, logger logging.Logger) (*Registry, error) {
	sources, err := loadSources(path, logger)
	if err != nil {
		return nil, err
	}
	logger.Info(context.Background(), "marketplace registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: countSources(sources)},
	)
	return &Registry{path: path, logger: logger, sources: sources}, nil
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

// Watch watches the directory holding marketplace-sources.json and reloads on
// change. Kubernetes swaps the "..data" symlink atomically on a ConfigMap
// update, so the directory is watched rather than the file. A debounce
// coalesces event bursts. Watch returns when ctx is canceled.
func (r *Registry) Watch(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.logger.Error(ctx, "failed to create fsnotify watcher for marketplace registry; hot-reload disabled",
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
		r.logger.Error(ctx, "failed to watch marketplace registry directory; hot-reload disabled",
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

// reload re-reads marketplace-sources.json and swaps in the new set. A parse
// error keeps the current set: a transient mount glitch must not blank the
// registry.
func (r *Registry) reload(ctx context.Context) {
	sources, err := loadSources(r.path, r.logger)
	if err != nil {
		r.logger.Error(ctx, "marketplace registry reload failed; keeping current sources",
			logging.Field{Key: "error", Value: err.Error()})
		return
	}

	r.mu.Lock()
	r.sources = sources
	r.mu.Unlock()

	r.logger.Info(ctx, "marketplace registry reloaded",
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: countSources(sources)})
}

// loadSources reads and parses marketplace-sources.json. An absent file
// returns an empty map, not an error.
func loadSources(path string, logger logging.Logger) (map[string][]MarketplaceSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]MarketplaceSource{}, nil
		}
		return nil, fmt.Errorf("read marketplace sources: %w", err)
	}

	var f marketplace.SourcesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse marketplace sources: %w", err)
	}

	out := make(map[string][]MarketplaceSource, len(f.Tenants))
	for tenantID, ts := range f.Tenants {
		seen := make(map[string]struct{}, len(ts.Sources))
		sources := make([]MarketplaceSource, 0, len(ts.Sources))
		for _, s := range ts.Sources {
			src := toMarketplaceSource(s)
			// The operator never renders an empty or clashing slug; this guards
			// a hand-edited file, keeping the first source as the operator would.
			if _, dup := seen[src.MarketplaceSlug]; dup || src.MarketplaceSlug == "" {
				logger.Warn(context.Background(), "skipping marketplace source with an empty or duplicate slug",
					logging.Field{Key: "tenant", Value: tenantID},
					logging.Field{Key: "name", Value: s.Name},
				)
				continue
			}
			seen[src.MarketplaceSlug] = struct{}{}
			sources = append(sources, src)
		}
		if len(sources) > 0 {
			out[tenantID] = sources
		}
	}
	return out, nil
}

// toMarketplaceSource maps one wire entry to the registry's view of it.
func toMarketplaceSource(s marketplace.Source) MarketplaceSource {
	return MarketplaceSource{
		Name:            s.Name,
		Org:             s.Org,
		RepoName:        s.Repo,
		Branch:          s.Branch,
		Link:            "https://github.com/" + s.Org + "/" + s.Repo,
		MarketplaceSlug: marketplace.Slug(s.Name),
	}
}

func countSources(sources map[string][]MarketplaceSource) int {
	total := 0
	for _, ss := range sources {
		total += len(ss)
	}
	return total
}

// isRelevantEvent reports whether an fsnotify event should trigger a reload:
// a change to the target file itself (local runs) or to the "..data" symlink
// Kubernetes swaps on a ConfigMap volume update.
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
