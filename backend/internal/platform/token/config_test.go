package token

import "testing"

func TestLoadConfig_BaseDefaults(t *testing.T) {
	cfg := LoadConfig()
	if cfg.Issuer != DefaultIssuer || cfg.Audience != DefaultAudience || cfg.JWKSURL == "" {
		t.Errorf("LoadConfig() = %+v", cfg)
	}
}
