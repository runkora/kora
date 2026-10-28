package skillsconfig_test

import (
	"os"
	"strings"
	"testing"
	"time"

	sc "github.com/clear-route/orbit/cmd/skills-service/config"
	"github.com/clear-route/orbit/internal/platform/config"
)

// setRequiredEnv sets every env var ApplyEnvOverrides requires so tests can
// isolate the one behaviour they care about.
func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ORBIT_MARKETPLACE_SOURCES_CONFIG", "/tmp")
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "67890")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", "/tmp/key.pem")
}

func TestConfigLoadsFromBaseYAML(t *testing.T) {
	setRequiredEnv(t)

	c := config.MustLoadService[sc.Config]("cmd/skills-service", "")

	if c.Server.Port != 8085 {
		t.Errorf("expected port 8085, got %d", c.Server.Port)
	}
	if c.Server.BasePath != "/v1" {
		t.Errorf("expected basePath /v1, got %s", c.Server.BasePath)
	}
	if c.Observability.ServiceName != "orbit-skills-service" {
		t.Errorf("expected serviceName orbit-skills-service, got %s", c.Observability.ServiceName)
	}
	if c.CacheTTL() != 12*time.Hour {
		t.Errorf("expected cacheTTL 12h, got %s", c.CacheTTL())
	}
	if c.GitHub.APIBaseURL != "https://api.github.com" {
		t.Errorf("expected github.apiBaseURL default, got %q", c.GitHub.APIBaseURL)
	}
	if c.Policy.BundleEndpoint != "http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles" {
		t.Errorf("expected policy.bundleEndpoint default, got %q", c.Policy.BundleEndpoint)
	}
	if c.Policy.BundlePollInterval() != 30*time.Second {
		t.Errorf("expected bundle poll interval 30s, got %s", c.Policy.BundlePollInterval())
	}
	if c.Policy.PolicyServiceURL != "http://orbit-policy-service.orbit-system.svc" {
		t.Errorf("expected policy.policyServiceURL default, got %q", c.Policy.PolicyServiceURL)
	}
}

func TestApplyEnvOverridesPopulatesGitHubCredentials(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("GITHUB_APP_ID", "12345")
	t.Setenv("GITHUB_APP_INSTALLATION_ID", "67890")
	t.Setenv("GITHUB_APP_PRIVATE_KEY_PATH", "/tmp/key.pem")

	var c sc.Config
	if err := c.ApplyEnvOverrides(); err != nil {
		t.Fatalf("ApplyEnvOverrides: %v", err)
	}

	if c.GitHub.AppID != "12345" {
		t.Errorf("AppID: got %q, want %q", c.GitHub.AppID, "12345")
	}
	if c.GitHub.InstallationID != "67890" {
		t.Errorf("InstallationID: got %q, want %q", c.GitHub.InstallationID, "67890")
	}
	if c.GitHub.PrivateKeyPath != "/tmp/key.pem" {
		t.Errorf("PrivateKeyPath: got %q, want %q", c.GitHub.PrivateKeyPath, "/tmp/key.pem")
	}
	if c.Marketplace.SourcesConfigPath != "/tmp/skills-mapping.yaml" {
		t.Errorf("SourcesConfigPath: got %q, want %q", c.Marketplace.SourcesConfigPath, "/tmp/skills-mapping.yaml")
	}
}

func TestEnvVarOverridesCacheTTL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MARKETPLACE_CACHE_TTL", "6h")

	c := config.MustLoadService[sc.Config]("cmd/skills-service", "")
	if err := c.ApplyEnvOverrides(); err != nil {
		t.Fatalf("ApplyEnvOverrides: %v", err)
	}

	if c.CacheTTL() != 6*time.Hour {
		t.Errorf("expected overridden cacheTTL 6h, got %s", c.CacheTTL())
	}
}

func TestApplyEnvOverridesRejectsInvalidTTL(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("MARKETPLACE_CACHE_TTL", "not-a-duration")

	var c sc.Config
	if err := c.ApplyEnvOverrides(); err == nil {
		t.Fatal("expected error for invalid MARKETPLACE_CACHE_TTL")
	}
}

func TestApplyEnvOverridesRequiredVars(t *testing.T) {
	cases := []struct {
		name    string
		unset   string
		wantErr string
	}{
		{"missing sources config path", "ORBIT_MARKETPLACE_SOURCES_CONFIG", "ORBIT_MARKETPLACE_SOURCES_CONFIG"},
		{"missing github app id", "GITHUB_APP_ID", "GITHUB_APP_ID"},
		{"missing github installation id", "GITHUB_APP_INSTALLATION_ID", "GITHUB_APP_INSTALLATION_ID"},
		{"missing github private key path", "GITHUB_APP_PRIVATE_KEY_PATH", "GITHUB_APP_PRIVATE_KEY_PATH"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			os.Unsetenv(tc.unset)

			var c sc.Config
			err := c.ApplyEnvOverrides()
			if err == nil {
				t.Fatalf("expected error when %s is unset", tc.unset)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should mention %q", err.Error(), tc.wantErr)
			}
		})
	}
}
