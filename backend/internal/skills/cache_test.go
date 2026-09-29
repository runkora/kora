package skills_test

import (
	"testing"

	"github.com/clear-route/orbit/internal/skills"
)

func TestCacheKey(t *testing.T) {
	tests := []struct {
		tenantID string
		org      string
		repo     string
		want     string
	}{
		{"tenant-a", "clear-route", "skills-marketplace", "marketplace:tenant-a:clear-route:skills-marketplace"},
		{"tenant-b", "clearroute", "core-skills", "marketplace:tenant-b:clearroute:core-skills"},
	}

	for _, tt := range tests {
		got := skills.CacheKey(tt.tenantID, tt.org, tt.repo)
		if got != tt.want {
			t.Errorf("CacheKey(%q, %q, %q) = %q, want %q", tt.tenantID, tt.org, tt.repo, got, tt.want)
		}
	}
}
