// Package policyGatewayConfig holds the configuration for the policy-gateway binary.
package policyGatewayConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds policy-gateway configuration values.
//
// Values are loaded from:
//
//  1. cmd/policy-gateway/config/base.yaml        — service defaults
//  2. cmd/policy-gateway/config/{env}.yaml       — env overrides (optional)
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		HTTPPort            int    `yaml:"httpPort"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Policy struct {
		// BundleEndpoint is the base URL for the policy-service bundle polling endpoint.
		// e.g. http://policy-service.orbit-system.svc/api/v1/policy/bundles
		BundleEndpoint         string `yaml:"bundleEndpoint"`
		BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
	} `yaml:"policy"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// BundlePollInterval returns the configured poll interval, defaulting to 30s.
func (c *Config) BundlePollInterval() time.Duration {
	if c.Policy.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Policy.BundlePollIntervalSecs) * time.Second
}

// Current holds non-secret configuration for the policy-gateway.
var Current = config.MustLoadService[Config]("cmd/policy-gateway", config.Environment())
