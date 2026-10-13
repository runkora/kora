package skills_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/skills"
)

func writeYAMLMapping(t *testing.T, dir string, content string) string {
	t.Helper()
	path := filepath.Join(dir, "skills-mapping.yaml")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write yaml: %v", err)
	}
	return path
}

func newTestRegistry(t *testing.T, path string) *skills.Registry {
	t.Helper()
	r, err := skills.NewRegistry(path, logging.NewDevelopmentLogger())
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

func TestRegistryLoadsValidYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, `
skillsMapping:
  tenants:
    - id: "tenant-a"
      marketplaces:
        - "https://github.com/clear-route/skills-marketplace"
        - "https://github.com/clearroute/core"
`)
	r := newTestRegistry(t, path)

	sources := r.ListSources("tenant-a")
	if len(sources) != 2 {
		t.Fatalf("expected 2 sources for tenant-a, got %d", len(sources))
	}
	if sources[0].MarketplaceSlug != "clear-route__skills-marketplace" {
		t.Errorf("slug: got %q, want %q", sources[0].MarketplaceSlug, "clear-route__skills-marketplace")
	}
	if sources[0].RepoName != "skills-marketplace" {
		t.Errorf("RepoName: got %q, want %q", sources[0].RepoName, "skills-marketplace")
	}
	if sources[0].Link != "https://github.com/clear-route/skills-marketplace" {
		t.Errorf("Link: got %q, want %q", sources[0].Link, "https://github.com/clear-route/skills-marketplace")
	}
	if len(r.ListSources("tenant-b")) != 0 {
		t.Fatal("expected empty sources for unknown tenant")
	}
}

func TestRegistryMultipleTenants(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, `
skillsMapping:
  tenants:
    - id: "tenant-a"
      marketplaces:
        - "https://github.com/org/repo-a"
    - id: "tenant-b"
      marketplaces:
        - "https://github.com/org/repo-b"
`)
	r := newTestRegistry(t, path)

	if len(r.ListSources("tenant-a")) != 1 || r.ListSources("tenant-a")[0].RepoName != "repo-a" {
		t.Fatal("tenant-a sources wrong")
	}
	if len(r.ListSources("tenant-b")) != 1 || r.ListSources("tenant-b")[0].RepoName != "repo-b" {
		t.Fatal("tenant-b sources wrong")
	}
}

func TestRegistryDeduplicatesSameURL(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, `
skillsMapping:
  tenants:
    - id: "tenant-a"
      marketplaces:
        - "https://github.com/org/repo"
        - "https://github.com/org/repo"
        - "https://github.com/org/repo.git"
`)
	r := newTestRegistry(t, path)

	if len(r.ListSources("tenant-a")) != 1 {
		t.Fatalf("expected 1 deduplicated source, got %d", len(r.ListSources("tenant-a")))
	}
}

func TestRegistrySkipsInvalidURLAndWarns(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, `
skillsMapping:
  tenants:
    - id: "tenant-a"
      marketplaces:
        - "https://github.com/org/valid-repo"
        - "not-a-url"
        - "https://gitlab.com/org/repo"
`)
	r := newTestRegistry(t, path)

	// Only the valid GitHub URL should be loaded.
	if len(r.ListSources("tenant-a")) != 1 {
		t.Fatalf("expected 1 source (invalid URLs skipped), got %d", len(r.ListSources("tenant-a")))
	}
	if r.ListSources("tenant-a")[0].RepoName != "valid-repo" {
		t.Errorf("wrong source loaded: %+v", r.ListSources("tenant-a")[0])
	}
}

func TestRegistryAbsentFileReturnsEmpty(t *testing.T) {
	r := newTestRegistry(t, "/nonexistent/skills-mapping.yaml")
	if len(r.ListSources("tenant-a")) != 0 {
		t.Fatal("expected empty sources for absent file")
	}
}

func TestRegistryInvalidYAMLReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, "not: valid: yaml: [[[")

	_, err := skills.NewRegistry(path, logging.NewDevelopmentLogger())
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestRegistryNormalizesCase(t *testing.T) {
	dir := t.TempDir()
	path := writeYAMLMapping(t, dir, `
skillsMapping:
  tenants:
    - id: "tenant-a"
      marketplaces:
        - "https://github.com/Clear-Route/Skills-Marketplace"
`)
	r := newTestRegistry(t, path)

	sources := r.ListSources("tenant-a")
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].MarketplaceSlug != "clear-route__skills-marketplace" {
		t.Errorf("slug not lowercased: got %q", sources[0].MarketplaceSlug)
	}
	if sources[0].Org != "clear-route" {
		t.Errorf("org not lowercased: got %q", sources[0].Org)
	}
}
