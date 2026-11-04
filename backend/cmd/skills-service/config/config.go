// Package skillsconfig holds the configuration for the skills-service binary.
package skillsconfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// PolicyConfig configures the in-process OPA evaluator and the policy-service
// client used for route and scope registration.
type PolicyConfig struct {
	// BundleEndpoint is the base URL of the policy-service bundle API.
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
	// PolicyServiceURL is the base URL of the policy service.
	PolicyServiceURL string `yaml:"policyServiceURL"`
	RegisterRoutes   bool   `yaml:"registerRoutes"`
}

// BundlePollInterval returns the configured poll interval, defaulting to 30s.
func (c *PolicyConfig) BundlePollInterval() time.Duration {
	if c.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.BundlePollIntervalSecs) * time.Second
}

// Config holds the skills-service configuration. It holds no secrets: the
// Redis password comes from internal/platform/storage and the App keys from
// the mounted KeysDir.
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

	Identity struct {
		// InternalURL is the in-cluster base URL for identity.
		InternalURL string `yaml:"internalURL"`
	} `yaml:"identity"`

	Policy PolicyConfig `yaml:"policy"`

	Marketplace struct {
		// SourcesPath is the mounted kora-skill-marketplaces file.
		SourcesPath string `yaml:"sourcesPath"`
		// KeysDir is the mounted kora-skill-marketplace-secrets Secret.
		KeysDir string `yaml:"keysDir"`
		// CacheTTLSecs is how long skill lists and files stay cached.
		CacheTTLSecs int `yaml:"cacheTTLSecs"`
		// FailureTTLSecs is how long a failed crawl stays cached.
		FailureTTLSecs int `yaml:"failureTTLSecs"`
		// Limits cap the crawl of one Marketplace. Zero is no cap.
		Limits struct {
			SkillsPerMarketplace       int   `yaml:"skillsPerMarketplace"`
			FilesPerSkill              int   `yaml:"filesPerSkill"`
			BytesPerFile               int64 `yaml:"bytesPerFile"`
			SkillMdBytesPerMarketplace int64 `yaml:"skillMdBytesPerMarketplace"`
		} `yaml:"limits"`
	} `yaml:"marketplace"`

	GitHub struct {
		// APIBaseURL is the GitHub API root.
		APIBaseURL string `yaml:"apiBaseURL"`
	} `yaml:"github"`
}

// ShutdownTimeout returns the server shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// CacheTTL returns how long skill lists and files stay cached.
func (c *Config) CacheTTL() time.Duration {
	return time.Duration(c.Marketplace.CacheTTLSecs) * time.Second
}

// FailureTTL returns how long a failed crawl stays cached.
func (c *Config) FailureTTL() time.Duration {
	return time.Duration(c.Marketplace.FailureTTLSecs) * time.Second
}

// Current holds the skills-service configuration.
var Current = config.MustLoadService[Config]("cmd/skills-service", config.Environment())
