package skills_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/skills"
)

func writeSourcesFile(t *testing.T, dir string, sources []skills.MarketplaceSource) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"sources": sources})
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
	writeSourcesFile(t, dir, []skills.MarketplaceSource{
		{Name: "Clear Route Skills", Org: "clear-route", Repo: "skills", Branch: "main"},
		{Name: "Core Skills", Org: "clearroute", Repo: "core", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	sources := r.ListSources()
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources, got %d", len(sources))
	}
}

func TestRegistryFindByName(t *testing.T) {
	dir := t.TempDir()
	writeSourcesFile(t, dir, []skills.MarketplaceSource{
		{Name: "Clear Route Skills", Org: "clear-route", Repo: "skills", Branch: "main"},
	})

	r := newTestRegistry(t, filepath.Join(dir, "marketplace-sources.json"))

	src, ok := r.FindByName("Clear Route Skills")
	if !ok {
		t.Fatal("expected to find source")
	}
	if src.Org != "clear-route" {
		t.Errorf("Org: got %q want %q", src.Org, "clear-route")
	}

	_, ok = r.FindByName("Unknown")
	if ok {
		t.Fatal("expected not found for unknown name")
	}
}

func TestRegistryAbsentFileReturnsEmpty(t *testing.T) {
	r := newTestRegistry(t, "/nonexistent/marketplace-sources.json")
	if len(r.ListSources()) != 0 {
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
