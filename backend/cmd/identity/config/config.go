// Package identityConfig holds the configuration for the identity service binary.
package identityConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds the identity service's own configuration values.
// Platform-wide settings (OTLP endpoint, sampling rate, log format) are
// owned by the observability package and are not repeated here.
//
// Values are loaded from:
//
//  1. cmd/identity/config/base.yaml        — service defaults
//  2. cmd/identity/config/{env}.yaml       — service env overrides (optional)
//  3. /etc/orbit/config/cmd/identity.yaml  — rendered by the chart
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

	// PublicHosts is the trusted-host allowlist for OAuth discovery metadata,
	// canonical host first (ADR 2026-07-04). Empty disables validation.
	PublicHosts []string `yaml:"publicHosts"`

	// PolicyServiceURL is the policy service's in-cluster base URL, for PAT
	// grant management and the policy bundle.
	PolicyServiceURL string `yaml:"policyServiceURL"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds all non-secret configuration for the identity service.
// Secrets must be supplied via environment variables — never via config files.
var Current = config.MustLoadService[Config]("cmd/identity", config.Environment())
