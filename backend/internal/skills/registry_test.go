package skills_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/skills"
)

func writeSourcesFile(t *testing.T, dir string, tenantID string, sources []skills.MarketplaceSource) {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"tenants": map[string]any{
			tenantID: map[string]any{"sources": sources},
		},
	})
	if err != nil {
		t.Fatalf("marshal sources: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marketplace-sources.json"), data, 0600); err != nil {
		t.Fatalf("write sources file: %v", err)
	}
}

func newTestRegistry(t *testing.T, path string) *skills.Registry {
	t.Helper()
	r, err := skills.NewRegistry(path, logging.NewDevelopmentLogger())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestRegistryLoadsValidFile(t *testing.T) {
	dir := t.TempDir()
	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Clear Route Skills", Org: "clear-route", Repo: "skills", Branch: "main"},
		{Name: "Core Skills", Org: "clearroute", Repo: "core", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	sources := r.ListSources("tenant-a")
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources for tenant-a, got %d", len(sources))
	}
	if len(r.ListSources("tenant-b")) != 0 {
		t.Fatal("expected empty sources for unknown tenant")
	}
}

func TestRegistryFindByName(t *testing.T) {
	dir := t.TempDir()
	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Clear Route Skills", Org: "clear-route", Repo: "skills", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	src, ok := r.FindByName("tenant-a", "Clear Route Skills")
	if !ok {
		t.Fatal("expected to find source")
	}
	if src.Org != "clear-route" {
		t.Errorf("Org: got %q want %q", src.Org, "clear-route")
	}

	_, ok = r.FindByName("tenant-a", "Unknown")
	if ok {
		t.Fatal("expected not found for unknown name")
	}

	_, ok = r.FindByName("tenant-b", "Clear Route Skills")
	if ok {
		t.Fatal("expected not found for wrong tenant")
	}
}

func TestRegistryAbsentFileReturnsEmpty(t *testing.T) {
	r := newTestRegistry(t, "/nonexistent/marketplace-sources.json")
	if len(r.ListSources("tenant-a")) != 0 {
		t.Fatal("expected empty sources for absent file")
	}
}

func TestRegistryInvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace-sources.json")
	if err := os.WriteFile(path, []byte("not json"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := skills.NewRegistry(path, logging.NewDevelopmentLogger())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

// pollUntil retries f every 50 ms until it returns true or the deadline passes.
func pollUntil(t *testing.T, deadline time.Duration, f func() bool) bool {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if f() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestWatchReloadsOnWrite(t *testing.T) {
	dir := t.TempDir()
	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Initial", Org: "org", Repo: "repo", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.Watch(ctx)
	time.Sleep(50 * time.Millisecond) // let watcher subscribe before writing

	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Updated", Org: "org", Repo: "repo", Branch: "main"},
	})

	if !pollUntil(t, 2*time.Second, func() bool {
		srcs := r.ListSources("tenant-a")
		return len(srcs) > 0 && srcs[0].Name == "Updated"
	}) {
		t.Fatal("registry did not reload after write")
	}
}

func TestWatchReloadsOnRename(t *testing.T) {
	// Simulates the Kubernetes ConfigMap atomic-rename update pattern.
	dir := t.TempDir()
	path := filepath.Join(dir, "marketplace-sources.json")
	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Before", Org: "org", Repo: "repo", Branch: "main"},
	})

	r := newTestRegistry(t, path)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go r.Watch(ctx)
	time.Sleep(50 * time.Millisecond) // let watcher subscribe before writing

	data, err := json.Marshal(map[string]any{
		"tenants": map[string]any{
			"tenant-a": map[string]any{
				"sources": []skills.MarketplaceSource{
					{Name: "After", Org: "org", Repo: "repo", Branch: "main"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		t.Fatalf("write tmp: %v", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatalf("rename: %v", err)
	}

	if !pollUntil(t, 2*time.Second, func() bool {
		srcs := r.ListSources("tenant-a")
		return len(srcs) > 0 && srcs[0].Name == "After"
	}) {
		t.Fatal("registry did not reload after rename")
	}
}

func TestWatchExitsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	writeSourcesFile(t, dir, "tenant-a", []skills.MarketplaceSource{
		{Name: "Test", Org: "org", Repo: "repo", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		r.Watch(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Watch goroutine did not exit after context cancellation")
	}
}
