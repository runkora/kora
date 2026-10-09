// Package skillsconfig holds the configuration for the skills-service binary.
package skillsconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// PolicyConfig configures the in-process OPA policy evaluator and the
// policy-service client used for route/scope registration.
type PolicyConfig struct {
	// BundleEndpoint is the base URL for the policy-service bundle API.
	// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
	// PolicyServiceURL is the base URL of the policy service.
	// Used for route registration at startup.
	// e.g. http://orbit-policy-service.orbit-system.svc
	PolicyServiceURL string `yaml:"policyServiceURL"`
	// LocalPolicyRegistrationBypass disables policy-service route registration
	// for local development only.
	LocalPolicyRegistrationBypass bool `yaml:"-"`
}

// BundlePollInterval returns the configured poll interval, defaulting to 30s.
func (c *PolicyConfig) BundlePollInterval() time.Duration {
	if c.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.BundlePollIntervalSecs) * time.Second
}

// Config holds the skills-service configuration.
//
// Secrets and deployment-specific paths are loaded from env vars only, never
// from YAML (per AGENTS.md "No secrets in YAML"):
//   - REDIS_HOST / REDIS_PASSWORD          — owned by internal/platform/storage
//   - ORBIT_MARKETPLACE_SOURCES_CONFIG     — ConfigMap mount path
//   - GITHUB_APP_ID / GITHUB_APP_INSTALLATION_ID / GITHUB_APP_PRIVATE_KEY_PATH
//     — GitHub App credentials for the TokenProvider
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port                int    `yaml:"port"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Policy PolicyConfig `yaml:"policy"`

	Marketplace struct {
		// SourcesConfigPath is the path to the mounted marketplace-sources.json.
		// Loaded from ORBIT_MARKETPLACE_SOURCES_CONFIG — never from YAML.
		SourcesConfigPath string `yaml:"-"`

		// CacheTTL is the Redis key TTL for cached skill sets.
		CacheTTL time.Duration `yaml:"cacheTTL"`
	} `yaml:"marketplace"`

	GitHub struct {
		// AppID is the GitHub App ID.
		// Loaded from GITHUB_APP_ID — never from YAML.
		AppID string `yaml:"-"`

		// InstallationID is the GitHub App installation ID for this tenant/org.
		// Loaded from GITHUB_APP_INSTALLATION_ID — never from YAML.
		InstallationID string `yaml:"-"`

		// PrivateKeyPath is the path to the mounted PEM-encoded App private key.
		// Loaded from GITHUB_APP_PRIVATE_KEY_PATH — never from YAML.
		PrivateKeyPath string `yaml:"-"`

		// APIBaseURL is the GitHub API root; overridable for GitHub Enterprise
		// or test doubles. Defaults to https://api.github.com when unset.
		APIBaseURL string `yaml:"apiBaseURL"`
	} `yaml:"github"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// CacheTTL returns the Redis key TTL.
func (c *Config) CacheTTL() time.Duration {
	return c.Marketplace.CacheTTL
}

// ApplyEnvOverrides reads env vars on top of YAML defaults. Required env vars
// must be present at boot; missing values return an error so the readiness
// probe fails fast rather than surfacing a 5xx on the first user request.
func (c *Config) ApplyEnvOverrides() error {
	c.Policy.LocalPolicyRegistrationBypass =
		os.Getenv("LOCAL_POLICY_REGISTRATION_BYPASS") == "true"

	dir := os.Getenv("ORBIT_MARKETPLACE_SOURCES_CONFIG")
	if dir == "" {
		return fmt.Errorf("ORBIT_MARKETPLACE_SOURCES_CONFIG environment variable is required")
	}
	c.Marketplace.SourcesConfigPath = filepath.Join(dir, "marketplace-sources.json")

	if v := os.Getenv("MARKETPLACE_CACHE_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return fmt.Errorf("invalid MARKETPLACE_CACHE_TTL %q: %w", v, err)
		}
		c.Marketplace.CacheTTL = d
	}

	c.GitHub.AppID = os.Getenv("GITHUB_APP_ID")
	if c.GitHub.AppID == "" {
		return fmt.Errorf("GITHUB_APP_ID environment variable is required")
	}

	c.GitHub.InstallationID = os.Getenv("GITHUB_APP_INSTALLATION_ID")
	if c.GitHub.InstallationID == "" {
		return fmt.Errorf("GITHUB_APP_INSTALLATION_ID environment variable is required")
	}

	c.GitHub.PrivateKeyPath = os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH")
	if c.GitHub.PrivateKeyPath == "" {
		return fmt.Errorf("GITHUB_APP_PRIVATE_KEY_PATH environment variable is required")
	}

	return nil
}

// Current holds all non-secret configuration for the skills-service.
var Current = config.MustLoadService[Config]("cmd/skills-service", config.Environment())
