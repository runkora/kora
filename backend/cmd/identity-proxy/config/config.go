// Package identityProxyConfig holds the configuration for the identity-proxy sidecar binary.
package identityProxyConfig

import (
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
)

// Config holds the identity-proxy sidecar's own configuration values.
// Platform-wide settings (OTLP endpoint, sampling rate, log format) are
// owned by the observability package and are not repeated here.
//
// Values are loaded from:
//
//  1. cmd/identity-proxy/config/base.yaml  — service defaults
//  2. cmd/identity-proxy/config/{env}.yaml — service env overrides (optional)
//
// Secrets (ORBIT_IDENTITY_SERVICE_URL, SPIFFE_ENDPOINT_SOCKET) are supplied
// via environment variables and must not appear in YAML files.
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		ShutdownTimeoutSecs   int    `yaml:"shutdownTimeoutSecs"`
		SocketPath            string `yaml:"socketPath"`
		ReadyTimeoutSecs      int    `yaml:"readyTimeoutSecs"`
		HealthAddr            string `yaml:"healthAddr"`
		EnvoyReadyTimeoutSecs int    `yaml:"envoyReadyTimeoutSecs"`
	} `yaml:"server"`

	Proxy struct {
		SpiffeSocketDefault  string `yaml:"spiffeSocketDefault"`
		PollIntervalSecs     int    `yaml:"pollIntervalSecs"`
		RefreshThresholdSecs int    `yaml:"refreshThresholdSecs"`
		BackoffInitialMs     int    `yaml:"backoffInitialMs"`
		BackoffMaxSecs       int    `yaml:"backoffMaxSecs"`
		// DelegationNegativeTTLSecs is how long a "run has no delegation"
		// (autonomous) answer is cached before re-consulting the identity
		// service (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4).
		DelegationNegativeTTLSecs int `yaml:"delegationNegativeTTLSecs"`
	} `yaml:"proxy"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// ReadyTimeout returns the maximum wait time for the initial token exchange.
func (c *Config) ReadyTimeout() time.Duration {
	return time.Duration(c.Server.ReadyTimeoutSecs) * time.Second
}

// PollInterval returns the background refresh poll interval.
func (c *Config) PollInterval() time.Duration {
	return time.Duration(c.Proxy.PollIntervalSecs) * time.Second
}

// RefreshThreshold returns how far before expiry the background goroutine
// should pre-emptively re-exchange the token.
func (c *Config) RefreshThreshold() time.Duration {
	return time.Duration(c.Proxy.RefreshThresholdSecs) * time.Second
}

// BackoffInitial returns the starting retry interval.
func (c *Config) BackoffInitial() time.Duration {
	return time.Duration(c.Proxy.BackoffInitialMs) * time.Millisecond
}

// BackoffMax returns the ceiling on retry intervals.
func (c *Config) BackoffMax() time.Duration {
	return time.Duration(c.Proxy.BackoffMaxSecs) * time.Second
}

// EnvoyReadyTimeout returns the maximum time to wait for Envoy to become ready.
func (c *Config) EnvoyReadyTimeout() time.Duration {
	return time.Duration(c.Server.EnvoyReadyTimeoutSecs) * time.Second
}

// DelegationNegativeTTL returns how long a "run has no delegation" answer is
// cached before re-consulting the identity service.
func (c *Config) DelegationNegativeTTL() time.Duration {
	return time.Duration(c.Proxy.DelegationNegativeTTLSecs) * time.Second
}

// Current holds all non-secret configuration for the identity-proxy sidecar.
// Secrets must be supplied via environment variables — never via config files.
var Current = config.MustLoadService[Config]("cmd/identity-proxy", config.Environment())
