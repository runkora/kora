package skills_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestMarketplaceCacheKey(t *testing.T) {
	tests := []struct {
		tenantID string
		slug     string
		want     string
	}{
		{"tenant-a", "clear-route__skills-marketplace", "marketplace:tenant-a:clear-route__skills-marketplace"},
		{"tenant-b", "clearroute__core-skills", "marketplace:tenant-b:clearroute__core-skills"},
	}
	for _, tt := range tests {
		got := skills.MarketplaceCacheKey(tt.tenantID, tt.slug)
		if got != tt.want {
			t.Errorf("MarketplaceCacheKey(%q, %q) = %q, want %q", tt.tenantID, tt.slug, got, tt.want)
		}
	}
}

func TestSkillCacheKey(t *testing.T) {
	tests := []struct {
		slug string
		want string
	}{
		{"clear-route__skills__react-expert", "clear-route__skills__react-expert"},
		{"clearroute__core__go-expert", "clearroute__core__go-expert"},
	}
	for _, tt := range tests {
		got := skills.SkillCacheKey(tt.slug)
		if got != tt.want {
			t.Errorf("SkillCacheKey(%q) = %q, want %q", tt.slug, got, tt.want)
		}
	}
}
