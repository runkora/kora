// Package smConfig holds the configuration for the session-manager binary.
package smConfig

import (
	"time"

	"github.com/clear-route/orbit/internal/platform/config"
)

// Config holds session-manager configuration values.
//
// Values are loaded from:
//
//  1. cmd/session-manager/config/base.yaml  — service defaults
//  2. cmd/session-manager/config/{env}.yaml — env overrides (optional)
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

	Workspaces struct {
		// BasePath is the root directory for per-run workspace subdirectories.
		// Typically /workspaces inside the agent pod.
		BasePath string `yaml:"basePath"`
	} `yaml:"workspaces"`

	Claude struct {
		// BinaryPath overrides the location of the claude binary.
		// Defaults to "claude" (resolved via $PATH).
		BinaryPath string `yaml:"binaryPath"`

		// DefaultModel is the model passed to Claude Code when the session
		// creation request does not specify one. For the Orbit inference
		// gateway, use the virtual model name (e.g. "anthropic/claude-sonnet-4-6").
		DefaultModel string `yaml:"defaultModel"`

		// Bare enables --bare mode (no CLAUDE.md discovery, no OAuth/keychain).
		// Required in containers where ANTHROPIC_API_KEY is set via env var.
		Bare bool `yaml:"bare"`
	} `yaml:"claude"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds all non-secret configuration for the session-manager.
// Secrets (Postgres DSN, API key) must be supplied via environment variables.
var Current = config.MustLoadService[Config]("cmd/session-manager", config.Environment())
