// Static (pre-registered) OAuth client source for the authorization server.
//
// Hosted web applications are pre-registered as OAuthClient CRs in their tenant
// namespace. The orbit-operator's OAuthClientAggregator renders every tenant's
// OAuthClient CRs into the orbit-system ConfigMap orbit-tenant-oauth-clients
// (one key, oauth-clients.json); the identity service mounts it as a file and
// reads it here. This mirrors the egress gateway's ConfigMap-backed connection
// registry (internal/credentials/configmap.go) and the client-secrets watcher:
// admin-declared registrations arrive via GitOps, not a config file baked into
// the image, and add/update/delete reconcile without a pod restart.
//
// Static clients are read-only here — they are never written back. Dynamically
// registered (RFC 7591 DCR) clients live in the Redis ClientRepository; the two
// sources are layered by layeredClientRepository, static first.
//
// See docs/adr/2026-07-28-oauth2-web-redirect-clients.md.
package identity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// staticClientEntry is the wire shape of one element of oauth-clients.json —
// the runtime contract the operator's OAuthClientAggregator renders. TenantID
// is carried for provenance; the authorization server resolves by client_id.
type staticClientEntry struct {
	TenantID     string   `json:"tenant_id"`
	ClientID     string   `json:"client_id"`
	ClientName   string   `json:"client_name,omitempty"`
	RedirectURIs []string `json:"redirect_uris"`
}

// StaticClientSource is a file-backed, hot-reloadable, read-only source of
// pre-registered OAuth clients, keyed by client_id. It satisfies the read half
// of ClientRepository (Get) and is never written to.
type StaticClientSource struct {
	path   string
	logger logging.Logger

	mu      sync.RWMutex
	clients map[string]RegisteredClient
}

// LoadStaticClientSource reads oauth-clients.json from path and returns a source
// holding the parsed clients. An empty path or an absent file is permitted (the
// operator may not have rendered the ConfigMap yet) and yields an empty source
// that returns ErrClientNotFound for every client_id until a reload populates
// it — mirroring loadClientSecrets' tolerance of a missing mount.
func LoadStaticClientSource(ctx context.Context, path string, logger logging.Logger) (*StaticClientSource, error) {
	s := &StaticClientSource{path: path, logger: logger, clients: map[string]RegisteredClient{}}
	clients, err := loadStaticClients(ctx, path, logger)
	if err != nil {
		return nil, err
	}
	s.clients = clients
	logger.Info(ctx, "static oauth clients loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "clients", Value: len(clients)},
	)
	return s, nil
}

// Get returns the static client registered under clientID, or ErrClientNotFound.
func (s *StaticClientSource) Get(_ context.Context, clientID string) (RegisteredClient, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.clients[clientID]
	if !ok {
		return RegisteredClient{}, ErrClientNotFound
	}
	return c, nil
}

// Watch watches the directory containing oauth-clients.json and reloads the
// source on change, following the same fsnotify directory-watch + debounce
// pattern as watchClientSecrets (Kubernetes rotates the "..data" symlink
// atomically on a ConfigMap-volume update). The goroutine exits when ctx is
// canceled; an empty path disables the watch. Run it in a goroutine.
func (s *StaticClientSource) Watch(ctx context.Context) {
	if s.path == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logger.Error(ctx, "failed to create fsnotify watcher for static oauth clients — hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	defer func() {
		if cerr := watcher.Close(); cerr != nil {
			s.logger.Error(ctx, "failed to close static oauth clients fsnotify watcher",
				logging.Field{Key: "error", Value: cerr.Error()},
			)
		}
	}()

	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path)
	if addErr := watcher.Add(dir); addErr != nil {
		s.logger.Error(ctx, "failed to watch static oauth clients directory — hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()},
		)
		return
	}

	s.logger.Info(ctx, "watching static oauth clients for changes",
		logging.Field{Key: "path", Value: s.path},
	)

	var debounce *time.Timer
	var debounceC <-chan time.Time
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
			debounce, debounceC = resetDebounceTimer(debounce, debounceDelay)

		case <-debounceC:
			debounce = nil
			debounceC = nil
			s.reload(ctx)

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.logger.Error(ctx, "static oauth clients watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()},
			)
		}
	}
}

// reload re-reads oauth-clients.json and swaps in the new set under a write
// lock. A read error keeps the current set — a transient mount glitch must not
// blank the client registry mid-flight and lock hosted apps out.
func (s *StaticClientSource) reload(ctx context.Context) {
	clients, err := loadStaticClients(ctx, s.path, s.logger)
	if err != nil {
		s.logger.Error(ctx, "static oauth clients reload failed — keeping current clients",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	s.mu.Lock()
	s.clients = clients
	s.mu.Unlock()

	s.logger.Info(ctx, "static oauth clients reloaded",
		logging.Field{Key: "clients", Value: len(clients)},
	)
}

// loadStaticClients reads and parses oauth-clients.json into a map keyed by
// client_id. An empty path or absent file yields an empty map (not an error).
// Entries missing a client_id or redirect URIs are skipped; a duplicate
// client_id keeps the first (the aggregator already de-duplicates, so this is a
// defensive backstop).
func loadStaticClients(ctx context.Context, path string, logger logging.Logger) (map[string]RegisteredClient, error) {
	if path == "" {
		return map[string]RegisteredClient{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			logger.Warn(ctx, "static oauth clients file does not exist — no pre-registered clients until it is mounted",
				logging.Field{Key: "path", Value: path})
			return map[string]RegisteredClient{}, nil
		}
		return nil, fmt.Errorf("read static oauth clients %q: %w", path, err)
	}

	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return map[string]RegisteredClient{}, nil
	}

	var entries []staticClientEntry
	if err := json.Unmarshal([]byte(trimmed), &entries); err != nil {
		return nil, fmt.Errorf("parse static oauth clients %q: %w", path, err)
	}

	clients := make(map[string]RegisteredClient, len(entries))
	for _, e := range entries {
		if e.ClientID == "" || len(e.RedirectURIs) == 0 {
			logger.Warn(ctx, "static oauth client entry missing client_id or redirect_uris, skipping",
				logging.Field{Key: "client_id", Value: e.ClientID},
			)
			continue
		}
		if bad, ok := firstInvalidStaticRedirectURI(e.RedirectURIs); !ok {
			// Defense-in-depth: the operator's aggregator already rejects
			// malformed redirect URIs, but the authorize gate is a pure exact-match
			// against this set, so we must never register a URI that is not a
			// well-formed http(s) URL (or is a plaintext redirect to a remote host).
			logger.Warn(ctx, "static oauth client entry has an invalid redirect URI, skipping",
				logging.Field{Key: "client_id", Value: e.ClientID},
				logging.Field{Key: "redirect_uri", Value: bad},
			)
			continue
		}
		if _, dup := clients[e.ClientID]; dup {
			logger.Warn(ctx, "duplicate static oauth client_id, keeping first",
				logging.Field{Key: "client_id", Value: e.ClientID},
			)
			continue
		}
		clients[e.ClientID] = RegisteredClient{
			ClientID:     e.ClientID,
			ClientName:   e.ClientName,
			RedirectURIs: append([]string(nil), e.RedirectURIs...),
		}
	}
	return clients, nil
}

// firstInvalidStaticRedirectURI returns the first redirect URI that is not a
// well-formed http(s) URL (and false), or ("", true) when all are valid. A
// non-loopback host must use https; loopback may use http. Mirrors the
// operator aggregator's validation so producer and consumer agree.
func firstInvalidStaticRedirectURI(uris []string) (string, bool) {
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return raw, false
		}
		if u.Scheme == "http" && !isLoopbackRedirect(raw) {
			return raw, false
		}
	}
	return "", true
}

// layeredClientRepository resolves OAuth clients from a read-only static source
// (pre-registered OAuthClient CRs, mounted config) first, then the dynamic DCR
// store. Writes (RFC 7591 registration) go only to the dynamic store; static
// clients are immutable, GitOps-owned config and are never mutated at runtime.
type layeredClientRepository struct {
	static  *StaticClientSource
	dynamic ClientRepository
}

// newLayeredClientRepository composes a static source ahead of the dynamic DCR
// store. A nil static source degrades to the dynamic store alone.
func newLayeredClientRepository(static *StaticClientSource, dynamic ClientRepository) ClientRepository {
	return layeredClientRepository{static: static, dynamic: dynamic}
}

// Get tries the static source first; on ErrClientNotFound it falls through to
// the dynamic store. A static client_id therefore always shadows a dynamic one
// of the same id — pre-registration is authoritative.
func (l layeredClientRepository) Get(ctx context.Context, clientID string) (RegisteredClient, error) {
	if l.static != nil {
		c, err := l.static.Get(ctx, clientID)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, ErrClientNotFound) {
			return RegisteredClient{}, err
		}
	}
	return l.dynamic.Get(ctx, clientID)
}

// Store writes only to the dynamic store — static clients are config-owned.
func (l layeredClientRepository) Store(ctx context.Context, c RegisteredClient, ttl time.Duration) error {
	return l.dynamic.Store(ctx, c, ttl)
}
