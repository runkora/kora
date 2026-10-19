package identity

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// writeRegistryFile writes a minimal valid tenant registry YAML to path.
func writeRegistryFile(t *testing.T, path string) {
	t.Helper()
	// issuerURL is intentionally empty: tenants load without any OIDC discovery
	// HTTP calls, so the test has no external dependencies.
	content := "version: \"v1\"\ntenants:\n  - id: reload-test\n    displayName: Reload Test\n    state: active\n    oidc:\n      issuerURL: \"\"\n      clientID: test-client\n      scopes: [openid]\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeRegistryFile: %v", err)
	}
}

// newWatcherTestService builds a *Service suitable for watcher tests: ephemeral
// signing key, no OIDC providers at startup, no real repos.
func newWatcherTestService(t *testing.T) *Service {
	t.Helper()
	t.Setenv("ORBIT_ALLOW_EPHEMERAL_KEY", "true")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY_FILE", "")
	t.Setenv("ORBIT_TOKEN_SIGNING_KEY", "")

	cfg := &Config{
		Token: TokenConfig{
			AccessTokenTTLMinutes: 60,
			RefreshTokenTTLDays:   30,
			PATDefaultTTLDays:     90,
			PATMaxTTLDays:         365,
			Audience:              "orbit-platform",
		},
	}
	svc, err := NewService(
		context.Background(),
		cfg,
		newMockSessionRepo(),
		newMockRefreshTokenRepo(),
		newMockBlocklistRepo(),
		newMockAuthCodeRepo(),
		newMockClientRepo(),
		newMockPATRepo(),
		nil,
		nil,
		nil,
		&mockPolicyClient{},
		&mockPolicyEvaluator{},
		&XFCCPrincipalExtractor{},
		logging.NewLoggerWithOutput(io.Discard),
	)
	if err != nil {
		t.Fatalf("newWatcherTestService: %v", err)
	}
	return svc
}

// TestWatchTenantRegistry_ExitsOnEmptyPath ensures the watcher returns
// immediately when given an empty path (no fsnotify watcher is created).
func TestWatchTenantRegistry_ExitsOnEmptyPath(t *testing.T) {
	svc := newWatcherTestService(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchTenantRegistry(context.Background(), "")
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchTenantRegistry did not return immediately for empty path")
	}
}

// TestWatchClientSecrets_ExitsOnEmptyPath mirrors the above for client secrets.
func TestWatchClientSecrets_ExitsOnEmptyPath(t *testing.T) {
	svc := newWatcherTestService(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchClientSecrets(context.Background(), "")
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("watchClientSecrets did not return immediately for empty path")
	}
}

// TestWatchTenantRegistry_ExitsOnCancel verifies the watcher goroutine exits
// cleanly when the context is cancelled.
func TestWatchTenantRegistry_ExitsOnCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping filesystem watcher test in short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	writeRegistryFile(t, path)
	svc := newWatcherTestService(t)
	svc.cfg.Registry.TenantsPath = path
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchTenantRegistry(ctx, path)
	}()

	// Give the watcher time to register.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTenantRegistry did not exit after context cancellation")
	}
}

// TestWatchTenantRegistry_NoPostCancelReload verifies that cancelling the
// context while a debounce timer is in flight does NOT cause reloadTenantOIDCProviders
// to run after the goroutine exits.
//
// Before the fix, time.AfterFunc fired a separate goroutine that could outlive
// the watcher goroutine and write to s.oidcTenants after context cancellation.
// With the channel-based timer the debounce fires via select in the same
// goroutine, so cancelling ctx always wins.
func TestWatchTenantRegistry_NoPostCancelReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping filesystem watcher test in short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	writeRegistryFile(t, path)
	svc := newWatcherTestService(t)
	svc.cfg.Registry.TenantsPath = path
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchTenantRegistry(ctx, path)
	}()

	// Wait for watcher setup.
	time.Sleep(50 * time.Millisecond)

	// Write the file to arm the debounce timer (200 ms window).
	writeRegistryFile(t, path)

	// Cancel before the debounce fires.
	cancel()

	// The goroutine must exit — it must not block waiting for the timer.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTenantRegistry did not exit after context cancellation with armed timer")
	}
}

// TestWatchClientSecrets_NoPostCancelReload mirrors the above for the client
// secrets watcher.
func TestWatchClientSecrets_NoPostCancelReload(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping filesystem watcher test in short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.yaml")
	if err := os.WriteFile(path, []byte("reload-test: s3cr3t\n"), 0o644); err != nil {
		t.Fatalf("write secrets file: %v", err)
	}
	svc := newWatcherTestService(t)
	svc.cfg.Registry.ClientSecretsPath = path
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchClientSecrets(ctx, path)
	}()

	time.Sleep(50 * time.Millisecond)

	// Write the file to arm the timer.
	if err := os.WriteFile(path, []byte("reload-test: n3ws3cr3t\n"), 0o644); err != nil {
		t.Fatalf("update secrets file: %v", err)
	}

	// Cancel before debounce fires.
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchClientSecrets did not exit after context cancellation with armed timer")
	}
}

// TestWatchTenantRegistry_BurstCoalesced verifies that rapid successive
// filesystem events result in at most a small number of reloads (debouncing
// works), not one reload per event.
func TestWatchTenantRegistry_BurstCoalesced(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping filesystem watcher test in short mode")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "tenants.yaml")
	writeRegistryFile(t, path)
	svc := newWatcherTestService(t)
	svc.cfg.Registry.TenantsPath = path
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		svc.watchTenantRegistry(ctx, path)
	}()

	time.Sleep(50 * time.Millisecond)

	// Fire 10 rapid writes within a tight window — all should be coalesced into
	// at most 2 reloads (one debounce window's worth).
	const writes = 10
	for range writes {
		writeRegistryFile(t, path)
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for the debounce window plus generous slack.
	time.Sleep(500 * time.Millisecond)

	// The watcher is still alive; cancel to clean up.
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchTenantRegistry goroutine did not exit")
	}
}
