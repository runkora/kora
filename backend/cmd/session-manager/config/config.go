// Package smConfig holds the configuration for the session-manager binary.
package smConfig

import (
	"os"
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

	OrbitAgents struct {
		// URL is the base URL of orbit-agents (e.g. "http://orbit-agents.orbit-system.svc.cluster.local").
		// When empty, event push is disabled (local dev without orbit-agents).
		URL string `yaml:"url"`
	} `yaml:"orbitAgents"`

	Claude struct {
		// BinaryPath overrides the location of the claude binary.
		// Defaults to "claude" (resolved via $PATH).
		BinaryPath string `yaml:"binaryPath"`

		// WrapperPath is the path to orbit-session-runner. When set, it is
		// invoked as: WrapperPath <workDir> <BinaryPath> [args...]
		// to apply Landlock filesystem restrictions before exec'ing claude.
		// Empty disables the wrapper (used in local dev on macOS).
		WrapperPath string `yaml:"wrapperPath"`

		// DefaultModel is the model passed to Claude Code when the session
		// creation request does not specify one. For the Orbit inference
		// gateway, use the virtual model name (e.g. "anthropic/claude-sonnet-4-6").
		DefaultModel string `yaml:"defaultModel"`

		// Bare enables --bare mode (no CLAUDE.md discovery, no OAuth/keychain).
		// Required in containers where ANTHROPIC_API_KEY is set via env var.
		Bare bool `yaml:"bare"`
	} `yaml:"claude"`

	Session struct {
		// IdleTimeoutSecs is how long a session may be idle (no message_stop
		// followed by a new message) before it is automatically closed.
		IdleTimeoutSecs int `yaml:"idleTimeoutSecs"`
	} `yaml:"session"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// Current holds all non-secret configuration for the session-manager.
// Secrets (Postgres DSN, API key) must be supplied via environment variables.
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/session-manager", config.Environment())
	if v := os.Getenv("ORBIT_AGENTS_URL"); v != "" {
		cfg.OrbitAgents.URL = v
	}
	return cfg
}()
