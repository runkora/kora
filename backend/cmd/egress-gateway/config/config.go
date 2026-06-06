// Package egressGatewayConfig holds the configuration for the egress-gateway binary.
package egressGatewayConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds egress-gateway configuration values.
//
// Values are loaded from:
//
//  1. cmd/egress-gateway/config/base.yaml        — service defaults
//  2. cmd/egress-gateway/config/{env}.yaml       — env overrides (optional)
//
// Secrets (DB password, K8s service-account token if any) are NOT in this
// struct — they come from the environment via internal/platform/storage.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		// HTTPPort is the single port the gateway binds. CONNECT requests are
		// routed to the credential-broker pipeline; non-CONNECT requests are
		// routed to the admin connections API.
		HTTPPort            int    `yaml:"httpPort"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Policy struct {
		// BundleEndpoint is the base URL for the policy-service bundle polling endpoint.
		BundleEndpoint string `yaml:"bundleEndpoint"`
		// BundlePollIntervalSecs is how often the in-process evaluator polls
		// for new bundles.
		BundlePollIntervalSecs int `yaml:"bundlePollIntervalSecs"`
		// PolicyServiceInternalURL is the base URL for the policy-service
		// route+scope registration endpoint.
		PolicyServiceInternalURL string `yaml:"policyServiceInternalURL"`
	} `yaml:"policy"`

	// Secrets describes where the gateway reads tenant CA material and
	// connection secrets from. Both are mounted as Kubernetes secrets at the
	// configured root paths; per-tenant subdirectories carry the actual
	// material so a single mount point serves every tenant the gateway
	// owns.
	Secrets struct {
		// TenantCARoot is the directory containing per-tenant CA material.
		// For tenant {tenant}, the gateway expects:
		//   {TenantCARoot}/{tenant}/ca.crt
		//   {TenantCARoot}/{tenant}/ca.key
		TenantCARoot string `yaml:"tenantCARoot"`

		// ConnectionSecretRoot is the directory containing per-connection
		// secrets. A connection's secret_ref is interpreted as a relative path
		// under this root, e.g. secret_ref = "acme/github/app.pem" reads
		// {ConnectionSecretRoot}/acme/github/app.pem.
		ConnectionSecretRoot string `yaml:"connectionSecretRoot"`
	} `yaml:"secrets"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// ShutdownTimeout returns the configured graceful-shutdown timeout.
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

// Current holds non-secret configuration for the egress-gateway.
var Current = config.MustLoadService[Config]("cmd/egress-gateway", config.Environment())
