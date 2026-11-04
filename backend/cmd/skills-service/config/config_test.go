package skillsconfig_test

import (
	"testing"
	"time"

	sc "github.com/clear-route/orbit/cmd/skills-service/config"
	"github.com/clear-route/orbit/internal/platform/config"
)

func TestConfigLoadsFromBaseYAML(t *testing.T) {
	c := config.MustLoadService[sc.Config]("cmd/skills-service", "")

	if c.Server.Port != 8098 || c.Server.BasePath != "/v1" {
		t.Errorf("server = %d %s, want 8098 /v1", c.Server.Port, c.Server.BasePath)
	}
	if c.CacheTTL() != 12*time.Hour || c.FailureTTL() != time.Minute {
		t.Errorf("TTLs = %s, %s, want 12h and 1m", c.CacheTTL(), c.FailureTTL())
	}
	if c.Marketplace.SourcesPath != "/etc/kora/skill-marketplaces/marketplaces.json" || c.Marketplace.KeysDir != "/etc/kora/skill-marketplace-secrets" {
		t.Errorf("mount paths = %q, %q", c.Marketplace.SourcesPath, c.Marketplace.KeysDir)
	}
	l := c.Marketplace.Limits
	if l.SkillsPerMarketplace <= 0 || l.FilesPerSkill <= 0 || l.BytesPerFile <= 0 {
		t.Errorf("limits = %+v, want all set", l)
	}
}
