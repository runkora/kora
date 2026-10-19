package skills_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestMarketplaceCacheKey(t *testing.T) {
	tests := []struct {
		tenantID, sourceKey, want string
	}{
		{"tenant-a", "clear-route/skills-marketplace@main", "marketplace:tenant-a:clear-route/skills-marketplace@main"},
		{"tenant-b", "clearroute/core-skills@HEAD", "marketplace:tenant-b:clearroute/core-skills@HEAD"},
	}
	for _, tt := range tests {
		if got := skills.MarketplaceCacheKey(tt.tenantID, tt.sourceKey); got != tt.want {
			t.Errorf("MarketplaceCacheKey(%q, %q) = %q, want %q", tt.tenantID, tt.sourceKey, got, tt.want)
		}
	}
}

func TestSkillCacheKey(t *testing.T) {
	got := skills.SkillCacheKey("tenant-a", "clear-route/skills@main", "skills__react-expert")
	const want = "skill:tenant-a:clear-route/skills@main:skills__react-expert"
	if got != want {
		t.Errorf("SkillCacheKey = %q, want %q", got, want)
	}
}

// The skill slug comes from the display name, so two tenants can each have a
// "Skills" marketplace backed by different repositories. Their skills must
// never share a cache entry.
func TestSkillCacheKeyIsolatesTenantsAndRepositories(t *testing.T) {
	a := skills.SkillCacheKey("tenant-a", "acme/skills@main", "skills__deploy")
	b := skills.SkillCacheKey("tenant-b", "globex/skills@main", "skills__deploy")
	if a == b {
		t.Fatalf("SkillCacheKey collides across tenants: %q", a)
	}
}
