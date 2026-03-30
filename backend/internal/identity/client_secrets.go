package identity

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// clientSecretsEnvVar is the environment variable that holds the path to the
// aggregated client secrets YAML file. The file is mounted from the
// orbit-tenant-oidc-secrets Secret (managed by the orbit-operator) and
// contains a flat YAML map of tenantID → clientSecret.
//
// When this env var is unset (e.g. local dev with all-public OIDC clients),
// loadClientSecrets returns an empty map and no client_secret parameter is
// sent in token requests.
//
// See docs/adr/0029-oidc-client-secret-projection-for-confidential-tenants.md
const clientSecretsEnvVar = "ORBIT_TENANT_SECRETS_CONFIG"

// clientSecretsPath returns the path to the aggregated client secrets file
// from the ORBIT_TENANT_SECRETS_CONFIG environment variable.
func clientSecretsPath() string {
	return os.Getenv(clientSecretsEnvVar)
}

// loadClientSecrets reads the aggregated client secrets YAML file written by
// the orbit-operator to orbit-tenant-oidc-secrets. The file format is a flat
// YAML map:
//
//	acme-corp: s3cr3t
//	example-tenant: another-secret
//
// An empty or unset ORBIT_TENANT_SECRETS_CONFIG is not an error — it returns
// an empty map, meaning all tenants are treated as public OIDC clients.
func loadClientSecrets(path string) (map[string]string, error) {
	if path == "" {
		return map[string]string{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File absent means the aggregated Secret has not been created yet
			// (no confidential tenants provisioned). Treat as empty map.
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("read client secrets file %s: %w", path, err)
	}

	var secrets map[string]string
	if err := yaml.Unmarshal(data, &secrets); err != nil {
		return nil, fmt.Errorf("parse client secrets file %s: %w", path, err)
	}
	if secrets == nil {
		secrets = map[string]string{}
	}
	return secrets, nil
}

// initClientSecrets loads the aggregated client secrets at startup and stores
// them in s.clientSecrets under a write lock. This must be called before
// initTenantOIDCProviders so that confidential-tenant client secrets are
// available during the initial OIDC discovery pass.
func (s *Service) initClientSecrets(ctx context.Context) error {
	path := clientSecretsPath()
	secrets, err := loadClientSecrets(path)
	if err != nil {
		return fmt.Errorf("load client secrets: %w", err)
	}

	s.mu.Lock()
	s.clientSecrets = secrets
	s.mu.Unlock()

	s.logger.Info(ctx, "client secrets loaded",
		logging.Field{Key: "path", Value: path},
		logging.Field{Key: "tenant_count", Value: len(secrets)},
	)
	return nil
}

// reloadClientSecrets re-reads the aggregated client secrets file and swaps in
// the new map under a write lock. After updating clientSecrets it triggers a
// full tenant OIDC provider reload so that rotated secrets are picked up by
// in-flight device-auth and token-poll requests without a pod restart.
func (s *Service) reloadClientSecrets(ctx context.Context) {
	path := clientSecretsPath()
	secrets, err := loadClientSecrets(path)
	if err != nil {
		s.logger.Error(ctx, "client secrets reload failed — keeping current secrets",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}

	s.mu.Lock()
	s.clientSecrets = secrets
	s.mu.Unlock()

	s.logger.Info(ctx, "client secrets reloaded",
		logging.Field{Key: "tenant_count", Value: len(secrets)},
	)

	// Re-run OIDC provider reload so that tenantOIDCState.cfg.ClientSecret
	// reflects the freshly loaded values without requiring a pod restart.
	s.reloadTenantOIDCProviders(ctx)
}

// watchClientSecrets watches the directory containing the aggregated client
// secrets file for changes and triggers a live reload whenever the file is
// updated. It follows the same fsnotify directory-watch pattern as
// watchTenantRegistry: watching the parent directory handles the atomic
// symlink swap that Kubernetes uses for Secret-backed volume updates.
//
// A 200 ms debounce coalesces rapid event bursts into a single reload call.
// The goroutine exits cleanly when ctx is canceled (SIGTERM).
func (s *Service) watchClientSecrets(ctx context.Context, secretsPath string) {
	if secretsPath == "" {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.logger.Error(ctx, "failed to create fsnotify watcher for client secrets — hot-reload disabled",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	defer func() {
		if err := watcher.Close(); err != nil {
			s.logger.Error(ctx, "failed to close client secrets fsnotify watcher",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()

	dir := filepath.Dir(secretsPath)
	base := filepath.Base(secretsPath)

	if addErr := watcher.Add(dir); addErr != nil {
		s.logger.Error(ctx, "failed to watch client secrets directory — hot-reload disabled",
			logging.Field{Key: "path", Value: dir},
			logging.Field{Key: "error", Value: addErr.Error()},
		)
		return
	}

	s.logger.Info(ctx, "watching client secrets for changes",
		logging.Field{Key: "path", Value: secretsPath},
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
			debounce = time.AfterFunc(debounceDelay, func() {
				s.reloadClientSecrets(ctx)
			})

		case watchErr, ok := <-watcher.Errors:
			if !ok {
				return
			}
			s.logger.Error(ctx, "client secrets watcher error",
				logging.Field{Key: "error", Value: watchErr.Error()},
			)
		}
	}
}
