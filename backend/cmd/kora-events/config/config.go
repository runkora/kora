// Package eventsconfig holds the configuration for the kora-events binary.
package eventsconfig

import (
	"fmt"
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
)

// Config holds kora-events configuration. Secrets come from
// internal/platform/storage.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port int `yaml:"port"`
		// BasePath is / because the public API sits under /v1 and the
		// internal stream under /internal/v1.
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Identity struct {
		// InternalURL is the in-cluster base URL for identity, which exchanges
		// this pod's SVID for the token route registration uses.
		InternalURL string `yaml:"internalURL"`
	} `yaml:"identity"`

	Policy struct {
		BundleEndpoint         string `yaml:"bundleEndpoint"`
		BundlePollIntervalSecs int    `yaml:"bundlePollIntervalSecs"`
		PolicyServiceURL       string `yaml:"policyServiceURL"`
	} `yaml:"policy"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Metrics platformmetrics.Config `yaml:"metrics"`

	Kubernetes struct {
		// TenantsPath is where the chart mounts the kora-tenants registry.
		TenantsPath string `yaml:"tenantsPath"`
	} `yaml:"kubernetes"`

	Retention struct {
		// DefaultDays applies to tenants whose KoraTenant sets no retention.
		DefaultDays int `yaml:"defaultDays"`
	} `yaml:"retention"`
}

// ShutdownTimeout returns the server shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// BundlePollInterval returns the evaluator bundle poll interval, defaulting to 30s.
func (c *Config) BundlePollInterval() time.Duration {
	if c.Policy.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Policy.BundlePollIntervalSecs) * time.Second
}

// Validate rejects settings the service cannot run with.
func (c *Config) Validate() error {
	if c.Retention.DefaultDays < 1 {
		return fmt.Errorf("retention.defaultDays must be at least 1, got %d", c.Retention.DefaultDays)
	}
	return nil
}

// Current holds the kora-events configuration.
var Current = config.MustLoadService[Config]("cmd/kora-events", config.Environment())
