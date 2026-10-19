package config

import (
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
)

// Config is the root configuration for the inference-gateway service.
//
// Values are loaded from cmd/inference-gateway/config/base.yaml, its
// {APP_ENV}.yaml overlay and the file the chart renders.
type Config struct {
	App           config.AppConfig    `yaml:"app"`
	Server        config.ServerConfig `yaml:"server"`
	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
	Metrics   platformmetrics.Config `yaml:"metrics"`
	Pricing   PricingConfig          `yaml:"pricing"`
	Policy    PolicyConfig           `yaml:"policy"`
	Providers ProvidersConfig        `yaml:"providers"`
	Identity  struct {
		// InternalURL is the in-cluster base URL for identity. The gateway
		// exchanges its SVID there for the platform token it registers routes with.
		InternalURL string `yaml:"internalURL"`
	} `yaml:"identity"`
}

// ProvidersConfig is where the gateway watches InferenceProvider resources.
type ProvidersConfig struct {
	Namespace string `yaml:"namespace"`
}

// PolicyConfig configures the in-process OPA policy evaluator.
type PolicyConfig struct {
	// BundleEndpoint is the base URL for the policy-service bundle API.
	// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
	// PolicyServiceURL is the base URL of the policy service.
	// Used for route registration at startup.
	// e.g. http://orbit-policy-service.orbit-system.svc
	PolicyServiceURL string `yaml:"policyServiceURL"`
}

// BundlePollInterval returns the configured poll interval, defaulting to 30s.
func (c *PolicyConfig) BundlePollInterval() time.Duration {
	if c.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.BundlePollIntervalSecs) * time.Second
}

// PricingConfig configures the static pricing cache.
type PricingConfig struct {
	// RefreshIntervalSecs is how often the pricing cache refreshes.
	RefreshIntervalSecs int `yaml:"refreshIntervalSecs"`
	// SyncIntervalSecs is how often the pricing sync worker fetches upstream prices.
	SyncIntervalSecs int `yaml:"syncIntervalSecs"`
	// SourceURL is the upstream URL for the LiteLLM pricing JSON.
	SourceURL string `yaml:"sourceURL"`
}

// RefreshInterval returns the configured cache refresh interval, defaulting to 1 hour.
func (c *PricingConfig) RefreshInterval() time.Duration {
	if c.RefreshIntervalSecs <= 0 {
		return time.Hour
	}
	return time.Duration(c.RefreshIntervalSecs) * time.Second
}

// SyncInterval returns the configured sync interval, defaulting to 24 hours.
func (c *PricingConfig) SyncInterval() time.Duration {
	if c.SyncIntervalSecs <= 0 {
		return 24 * time.Hour
	}
	return time.Duration(c.SyncIntervalSecs) * time.Second
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current is the loaded configuration singleton, populated at init time.
var Current = config.MustLoadService[Config]("cmd/inference-gateway", config.Environment())
