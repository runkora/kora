// Package toolGatewayConfig holds the configuration for the tool-gateway binary.
package toolGatewayConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds tool-gateway configuration values.
//
// Values are loaded from:
//
//  1. cmd/tool-gateway/config/base.yaml      — service defaults
//  2. cmd/tool-gateway/config/{env}.yaml     — env overrides (optional)
//
// The tool-gateway runs in two modes from one binary: the publisher (this
// phase — mounts the registry, introspects, publishes scopes) and the
// request-path proxy/PEP (Phase 4). Both share this config.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	// Mode selects the run mode: "publisher" (replicas=1 — introspection +
	// scope publishing + catalog write) or "proxy" (horizontally scaled —
	// the request-path PEP + MCP proxy). One binary, two deployments.
	Mode string `yaml:"mode"`

	Server struct {
		// HTTPPort serves health (and, in Phase 4, the MCP request path).
		HTTPPort            int    `yaml:"httpPort"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
	} `yaml:"server"`

	Policy struct {
		// PolicyServiceInternalURL is the base URL for the policy-service
		// route+scope registration endpoint (the ScopePublisher target).
		PolicyServiceInternalURL string `yaml:"policyServiceInternalURL"`
		// BundleEndpoint is the policy-service bundle-polling endpoint the
		// in-process evaluator (proxy mode) polls for per-tool authz.
		BundleEndpoint string `yaml:"bundleEndpoint"`
		// BundlePollIntervalSecs is how often the evaluator polls for bundles.
		BundlePollIntervalSecs int `yaml:"bundlePollIntervalSecs"`
	} `yaml:"policy"`

	Registry struct {
		// ServersPath is the mounted servers.json file rendered by the
		// operator's MCPServerAggregator into the orbit-mcp-servers ConfigMap.
		// The gateway parses it into the server registry and hot-reloads on
		// change. Absent/empty means no servers until the operator renders it.
		ServersPath string `yaml:"serversPath"`
	} `yaml:"registry"`

	Introspection struct {
		// ReintrospectIntervalSecs is the tick rate of the publisher's
		// re-introspection timer (besides on registry reload). It is the
		// polling granularity, not a per-server cadence: on each tick, a
		// server is only actually re-introspected once its own
		// reintrospectIntervalSeconds has elapsed since its last introspection
		// (0 = reload-only, never due on the timer).
		ReintrospectIntervalSecs int `yaml:"reintrospectIntervalSecs"`
	} `yaml:"introspection"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// ShutdownTimeout returns the configured graceful-shutdown timeout.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// ReintrospectInterval returns the configured re-introspection interval,
// defaulting to 5 minutes.
func (c *Config) ReintrospectInterval() time.Duration {
	if c.Introspection.ReintrospectIntervalSecs <= 0 {
		return 5 * time.Minute
	}
	return time.Duration(c.Introspection.ReintrospectIntervalSecs) * time.Second
}

// BundlePollInterval returns the evaluator bundle-poll interval, default 30s.
func (c *Config) BundlePollInterval() time.Duration {
	if c.Policy.BundlePollIntervalSecs <= 0 {
		return 30 * time.Second
	}
	return time.Duration(c.Policy.BundlePollIntervalSecs) * time.Second
}

// IsProxyMode reports whether the binary runs the request-path proxy/PEP.
func (c *Config) IsProxyMode() bool { return c.Mode == "proxy" }

// Current holds non-secret configuration for the tool-gateway.
var Current = config.MustLoadService[Config]("cmd/tool-gateway", config.Environment())
