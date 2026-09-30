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

	// Runner holds the settings that apply to every agent framework.
	Runner struct {
		// WrapperPath is the path to orbit-session-runner, used by every framework.
		// When set, an agent is invoked as: WrapperPath <workDir> <binary> [args...]
		// so Landlock is applied first. Empty disables it, which is local dev on macOS.
		WrapperPath string `yaml:"wrapperPath"`

		// ShellPath is the orbit-shell proxy each framework's shell resolves to, so
		// tool commands run in the run's sandbox. Claude Code reads it from
		// CLAUDE_CODE_SHELL, Codex from the runtime user's passwd entry, and
		// OpenCode from the "shell" key in its config. Empty leaves
		// OpenCode on the container's shell.
		ShellPath string `yaml:"shellPath"`
	} `yaml:"runner"`

	Claude struct {
		// BinaryPath overrides the location of the claude binary.
		// Defaults to "claude" (resolved via $PATH).
		BinaryPath string `yaml:"binaryPath"`

		// Bare enables --bare mode (no CLAUDE.md discovery, no OAuth/keychain).
		// Required in containers where ANTHROPIC_API_KEY is set via env var.
		Bare bool `yaml:"bare"`
	} `yaml:"claude"`

	Codex struct {
		// BinaryPath overrides the location of the codex binary.
		// Defaults to "codex" (resolved via $PATH).
		BinaryPath string `yaml:"binaryPath"`
	} `yaml:"codex"`

	OpenCode struct {
		// BinaryPath overrides the location of the opencode binary.
		// Defaults to "opencode" (resolved via $PATH).
		BinaryPath string `yaml:"binaryPath"`
	} `yaml:"opencode"`

	InferenceGateway struct {
		// URL is the base URL of the Orbit inference gateway.
		// Injected as OPENAI_BASE_URL for Codex; written into the OpenCode config for OpenCode.
		URL string `yaml:"url"`
	} `yaml:"inferenceGateway"`

	ToolGateway struct {
		// URL is the in-mesh base URL of the MCP tool-gateway; written into the
		// per-run MCP config so agents reach governed tools through it.
		URL string `yaml:"url"`
	} `yaml:"toolGateway"`

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
// Secrets must be supplied via environment variables. The session-manager
// reaches no database: run events travel to orbit-agents over HTTP, which is
// what keeps database credentials out of the pod the agent runs in.
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/session-manager", config.Environment())
	if v := os.Getenv("ORBIT_AGENTS_URL"); v != "" {
		cfg.OrbitAgents.URL = v
	}
	return cfg
}()
