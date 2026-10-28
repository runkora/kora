package skills

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// skillsMappingYAML is the wire shape of skills-mapping.yaml.
type skillsMappingYAML struct {
	SkillsMapping struct {
		Tenants []struct {
			ID           string   `json:"id"`
			Marketplaces []string `json:"marketplaces"`
		} `json:"tenants"`
	} `json:"skillsMapping"`
}

// Registry is the in-memory view of skills-mapping.yaml built once at
// startup. It is read-only after construction and safe for concurrent use.
type Registry struct {
	mu      sync.RWMutex
	sources map[string][]MarketplaceSource // key: tenantID
	logger  logging.Logger
}

// NewRegistry reads skills-mapping.yaml at path, parses every GitHub URL into
// a MarketplaceSource, and returns a ready-to-use Registry. An absent file is
// not an error — the ConfigMap may not be mounted yet; ListSources returns
// empty until a new process starts with the file present.
func NewRegistry(path string, logger logging.Logger) (*Registry, error) {
	sources, err := loadYAMLSources(path, logger)
	if err != nil {
		return nil, err
	}

	total := 0
	for _, ss := range sources {
		total += len(ss)
	}
	logger.Info(context.Background(), "marketplace registry loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: total},
	)

	return &Registry{sources: sources, logger: logger}, nil
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

// FindSource returns the marketplace registered for tenantID with the given
// slug. It returns false if the marketplace is not registered for that tenant.
func (r *Registry) FindSource(tenantID, marketplaceSlug string) (MarketplaceSource, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, source := range r.sources[tenantID] {
		if source.MarketplaceSlug == marketplaceSlug {
			return source, true
		}
	}

	return MarketplaceSource{}, false
}

// Reload reloads marketplace sources and keeps the previous
// configuration if the new file cannot be parsed.
func (r *Registry) Reload(path string) error {
	sources, err := loadYAMLSources(path, r.logger)
	if err != nil {
		return fmt.Errorf("reload skills mapping: %w", err)
	}

	r.mu.Lock()
	r.sources = sources
	r.mu.Unlock()

	total := 0
	for _, ss := range sources {
		total += len(ss)
	}

	r.logger.Info(context.Background(), "marketplace registry reloaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "tenants", Value: len(sources)},
		logging.Field{Key: "sources", Value: total},
	)

	return nil
}

// Watch watches the containing directory because Kubernetes ConfigMap
// updates replace symlinks instead of modifying the mounted file directly.
func (r *Registry) Watch(ctx context.Context, path string) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		_ = watcher.Close()
		return fmt.Errorf("watch marketplace directory %s: %w", dir, err)
	}

	go func() {
		defer watcher.Close()

		for {
			select {
			case <-ctx.Done():
				return

			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&(fsnotify.Write|fsnotify.Create|
					fsnotify.Rename|fsnotify.Remove) == 0 {
					continue
				}

				// Allow Kubernetes to finish switching ConfigMap symlinks.
				time.Sleep(200 * time.Millisecond)

				if err := r.Reload(path); err != nil {
					r.logger.Error(ctx,
						"marketplace registry reload failed",
						logging.Field{Key: "error", Value: err.Error()},
					)
				}

			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				r.logger.Error(ctx,
					"marketplace file watcher error",
					logging.Field{Key: "error", Value: err.Error()},
				)
			}
		}
	}()
	return nil
}

// loadYAMLSources reads and parses skills-mapping.yaml. An absent file
// returns an empty map (not an error).
func loadYAMLSources(path string, logger logging.Logger) (map[string][]MarketplaceSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string][]MarketplaceSource{}, nil
		}
		return nil, fmt.Errorf("read skills mapping: %w", err)
	}

	var f skillsMappingYAML // yaml to go
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse skills mapping yaml: %w", err)
	}

	out := make(map[string][]MarketplaceSource, len(f.SkillsMapping.Tenants))
	for _, tenant := range f.SkillsMapping.Tenants {
		seen := make(map[string]struct{})
		sources := make([]MarketplaceSource, 0, len(tenant.Marketplaces))
		for _, rawURL := range tenant.Marketplaces {
			src, err := parseGitHubURL(rawURL)
			if err != nil {
				logger.Warn(context.Background(), "skipping invalid marketplace URL",
					logging.Field{Key: "tenant", Value: tenant.ID},
					logging.Field{Key: "url", Value: rawURL},
					logging.Field{Key: "error", Value: err.Error()},
				)
				continue
			}
			// Deduplicate: same URL listed twice under one tenant is kept once.
			if _, dup := seen[src.MarketplaceSlug]; dup {
				continue
			}
			seen[src.MarketplaceSlug] = struct{}{}
			sources = append(sources, src)
		}
		if len(sources) > 0 {
			out[tenant.ID] = sources
		}
	}
	return out, nil
}

// parseGitHubURL parses a GitHub repository URL of the form
// https://github.com/{org}/{repo}[/][.git] and returns a MarketplaceSource.
// Any other form is an error.
func parseGitHubURL(raw string) (MarketplaceSource, error) {
	// Strip trailing .git or /
	raw = strings.TrimSuffix(
		strings.TrimSuffix(strings.TrimRight(raw, "/"), ".git"),
		"/",
	)

	u, err := url.Parse(raw)
	if err != nil || u.Host != "github.com" {
		return MarketplaceSource{}, fmt.Errorf("must be https://github.com/{org}/{repo}, got %q", raw)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return MarketplaceSource{}, fmt.Errorf("must be https://github.com/{org}/{repo}, got %q", raw)
	}

	org := strings.ToLower(parts[0])
	repo := strings.ToLower(parts[1])
	slug := org + "__" + repo
	link := "https://github.com/" + org + "/" + repo

	return MarketplaceSource{
		Name:            repo,
		Org:             org,
		RepoName:        repo,
		Link:            link,
		MarketplaceSlug: slug,
	}, nil
}
