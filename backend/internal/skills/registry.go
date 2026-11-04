package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/marketplace"
)

// registryReloadDebounce coalesces a burst of fsnotify events into one reload.
const registryReloadDebounce = 200 * time.Millisecond

// maxKeySize bounds one mounted private key.
const maxKeySize = 16 << 10

// Marketplace is one registered Marketplace and its credential.
type Marketplace struct {
	TenantID string
	marketplace.Marketplace

	// key is the mounted private key, compared on reload so a token provider
	// and its cached token survive an unrelated change.
	key    []byte
	tokens *tokenProvider
}

// Summary is the Marketplace as the API shows it.
func (m *Marketplace) Summary() MarketplaceSummary {
	return MarketplaceSummary{Slug: m.Slug, Name: m.Repo, Repository: m.Owner + "/" + m.Repo}
}

// source identifies what a crawl reads and with which App installation, so a
// cache entry from another repository, ref or credential is ignored.
func (m *Marketplace) source() string {
	return fmt.Sprintf("%s/%s@%s app %s/%d", m.Owner, m.Repo, m.Ref, m.AppID, m.InstallationID)
}

// Token returns an installation token that reads only this repository.
func (m *Marketplace) Token(ctx context.Context) (string, error) {
	if m.tokens == nil {
		return "", ErrCredentialUnavailable
	}
	return m.tokens.Token(ctx)
}

// Registry holds the mounted Marketplaces file and keys and reloads both on
// change. It is safe for concurrent use.
type Registry struct {
	path    string
	keysDir string
	minter  installationTokenMinter
	logger  logging.Logger

	mu       sync.RWMutex
	byTenant map[string][]*Marketplace
}

// NewRegistry reads the Marketplaces file at path and the keys in keysDir. An
// absent file gives an empty registry: the operator may not have rendered it.
func NewRegistry(ctx context.Context, path, keysDir string, minter installationTokenMinter, logger logging.Logger) (*Registry, error) {
	r := &Registry{path: path, keysDir: keysDir, minter: minter, logger: logger, byTenant: map[string][]*Marketplace{}}
	if err := r.reload(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Marketplaces returns the tenant's Marketplaces, oldest first.
func (r *Registry) Marketplaces(tenantID string) []*Marketplace {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byTenant[tenantID]
}

// Find returns the tenant's Marketplace with this slug.
func (r *Registry) Find(tenantID, slug string) (*Marketplace, bool) {
	for _, m := range r.Marketplaces(tenantID) {
		if m.Slug == slug {
			return m, true
		}
	}
	return nil, false
}

// reload swaps in the current file and keys. A file that cannot be read or
// parsed keeps the previous Marketplaces.
func (r *Registry) reload(ctx context.Context) error {
	var file marketplace.File
	data, err := os.ReadFile(r.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return fmt.Errorf("read %s: %w", r.path, err)
	default:
		if err := json.Unmarshal(data, &file); err != nil {
			return fmt.Errorf("parse %s: %w", r.path, err)
		}
	}

	r.mu.RLock()
	previous := r.byTenant
	r.mu.RUnlock()

	next := make(map[string][]*Marketplace, len(file.Tenants))
	total := 0
	for _, tenant := range file.Tenants {
		for _, spec := range tenant.Marketplaces {
			next[tenant.ID] = append(next[tenant.ID], r.build(ctx, tenant.ID, spec, previous))
			total++
		}
	}

	r.mu.Lock()
	r.byTenant = next
	r.mu.Unlock()
	r.logger.Info(ctx, "skill marketplace registry loaded",
		logging.Field{Key: "tenants", Value: len(next)},
		logging.Field{Key: "marketplaces", Value: total},
	)
	return nil
}

// build returns the Marketplace for spec, reusing the previous one when
// neither the spec nor the key changed. A missing or invalid key leaves the
// Marketplace without a credential, so browsing it fails closed.
func (r *Registry) build(ctx context.Context, tenantID string, spec marketplace.Marketplace, previous map[string][]*Marketplace) *Marketplace {
	key := r.readKey(spec.PrivateKeySecretKey)
	for _, old := range previous[tenantID] {
		if old.Marketplace == spec && bytes.Equal(old.key, key) {
			return old
		}
	}
	m := &Marketplace{TenantID: tenantID, Marketplace: spec, key: key}
	if key == nil {
		r.logger.Warn(ctx, "skill marketplace has no private key",
			logging.Field{Key: "tenant", Value: tenantID},
			logging.Field{Key: "marketplace", Value: spec.Slug},
		)
		return m
	}
	tokens, err := newTokenProvider(r.minter, spec.AppID, spec.InstallationID, spec.Repo, key)
	if err != nil {
		r.logger.Warn(ctx, "skill marketplace private key is invalid",
			logging.Field{Key: "tenant", Value: tenantID},
			logging.Field{Key: "marketplace", Value: spec.Slug},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return m
	}
	m.tokens = tokens
	return m
}

// readKey returns the mounted key named name, or nil. Names are flat file
// names written by the operator.
func (r *Registry) readKey(name string) []byte {
	if name == "" || strings.HasPrefix(name, ".") || strings.ContainsAny(name, `/\`) {
		return nil
	}
	f, err := os.Open(filepath.Join(r.keysDir, name))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	key, err := io.ReadAll(io.LimitReader(f, maxKeySize+1))
	if err != nil || len(key) == 0 || len(key) > maxKeySize {
		return nil
	}
	return key
}

// Watch reloads on a change to the Marketplaces file or the keys directory
// until ctx ends. Kubernetes updates both mounts by swapping a "..data"
// symlink, so the directories are watched rather than the files.
func (r *Registry) Watch(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		r.logger.Error(ctx, "skill marketplace registry watch failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	defer func() { _ = watcher.Close() }()
	for _, dir := range []string{filepath.Dir(r.path), r.keysDir} {
		if err := watcher.Add(dir); err != nil {
			r.logger.Error(ctx, "skill marketplace registry watch failed",
				logging.Field{Key: "path", Value: dir},
				logging.Field{Key: "error", Value: err.Error()})
		}
	}

	base := filepath.Base(r.path)
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
			if !isRelevantEvent(event, base, r.keysDir) {
				continue
			}
			if debounce != nil {
				debounce.Stop()
			}
			debounce = time.AfterFunc(registryReloadDebounce, func() {
				if err := r.reload(ctx); err != nil {
					r.logger.Error(ctx, "skill marketplace registry reload failed, keeping the current Marketplaces",
						logging.Field{Key: "error", Value: err.Error()})
				}
			})
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			r.logger.Error(ctx, "skill marketplace registry watcher error", logging.Field{Key: "error", Value: err.Error()})
		}
	}
}

// isRelevantEvent reports whether an event changes the file or a key: a write
// to the file, any change in the keys directory, or a "..data" swap.
func isRelevantEvent(event fsnotify.Event, base, keysDir string) bool {
	name := filepath.Base(event.Name)
	if name != base && name != "..data" && filepath.Dir(event.Name) != filepath.Clean(keysDir) {
		return false
	}
	return event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove)
}
