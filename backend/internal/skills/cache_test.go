package skills_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		tenantID        string
		marketplaceSlug string
		want            string
	}{
		{tenantID: "tenant-a", marketplaceSlug: "clear-route__skills-marketplace", want: "marketplace:tenant-a:clear-route__skills-marketplace"},
		{tenantID: "tenant-b", marketplaceSlug: "clearroute__core-skills", want: "marketplace:tenant-b:clearroute__core-skills"},
	}

	for _, tt := range tests {
		got := skills.CacheKey(tt.tenantID, tt.marketplaceSlug)
		if got != tt.want {
			t.Errorf("CacheKey(%q, %q) = %q, want %q", tt.tenantID, tt.marketplaceSlug, got, tt.want)
		}
	}
}
