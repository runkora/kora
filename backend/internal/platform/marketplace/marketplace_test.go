package marketplace

import (
	"testing"

	"sigs.k8s.io/yaml"
)

func TestRepoURL(t *testing.T) {
	tests := []struct {
		name        string
		owner, repo string
		want        string
	}{
		{"already lowercase", "clear-route", "skills-marketplace", "https://github.com/clear-route/skills-marketplace"},
		{"mixed case is lowercased", "Clear-Route", "Skills-Marketplace", "https://github.com/clear-route/skills-marketplace"},
		{"dots and underscores kept", "acme", "my_repo.v2", "https://github.com/acme/my_repo.v2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RepoURL(tt.owner, tt.repo); got != tt.want {
				t.Errorf("RepoURL(%q, %q) = %q, want %q", tt.owner, tt.repo, got, tt.want)
			}
		})
	}
}

// The wire shape is what skills-service already parses. Changing a key here
// breaks the hand-off, so the exact text and a round trip are both pinned.
func TestSourcesFileWireShape(t *testing.T) {
	file := SourcesFile{SkillsMapping: SkillsMapping{Tenants: []TenantMarketplaces{
		{ID: "acme", Marketplaces: []string{"https://github.com/acme-corp/internal-skills", "https://github.com/acme-corp/more"}},
	}}}

	raw, err := yaml.Marshal(file)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `skillsMapping:
  tenants:
  - id: acme
    marketplaces:
    - https://github.com/acme-corp/internal-skills
    - https://github.com/acme-corp/more
`
	if string(raw) != want {
		t.Errorf("wire text =\n%s\nwant\n%s", raw, want)
	}

	var back SourcesFile
	if err := yaml.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := back.SkillsMapping.Tenants[0].Marketplaces[1]; got != "https://github.com/acme-corp/more" {
		t.Errorf("round trip lost the second marketplace: %q", got)
	}
}

func TestEmptySourcesFileKeepsAnEmptyList(t *testing.T) {
	raw, err := yaml.Marshal(SourcesFile{SkillsMapping: SkillsMapping{Tenants: []TenantMarketplaces{}}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if want := "skillsMapping:\n  tenants: []\n"; string(raw) != want {
		t.Errorf("empty file = %q, want %q", raw, want)
	}
}
