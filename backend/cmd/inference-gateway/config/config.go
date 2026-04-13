package config

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config is the root configuration for the inference-gateway service.
//
// Values are loaded from cmd/inference-gateway/config/{APP_ENV}.yaml at
// startup. The YAML provides build-time defaults that are baked into the
type Config struct {
	App           config.AppConfig    `yaml:"app"`
	Server        config.ServerConfig `yaml:"server"`
	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
	Pricing PricingConfig `yaml:"pricing"`
	Policy  PolicyConfig  `yaml:"policy"`
}

// PolicyConfig configures the in-process OPA policy evaluator.
type PolicyConfig struct {
	// BundleEndpoint is the base URL for the policy-service bundle API.
	// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
	BundleEndpoint         string `yaml:"bundleEndpoint"`
	BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
	// PolicyServiceInternalURL is the base URL for the policy-service internal port.
	// Used for route registration at startup.
	// e.g. http://orbit-policy-service.orbit-system.svc:9091
	PolicyServiceInternalURL string `yaml:"policyServiceInternalURL"`
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
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current is the loaded configuration singleton, populated at init time.
var Current *Config

func init() {
	Current = config.MustLoadService[Config]("cmd/inference-gateway", config.Environment())
}
