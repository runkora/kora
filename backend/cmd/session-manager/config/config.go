// Package smConfig holds the configuration for the session-manager binary.
package smConfig

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/runkora/kora/backend/internal/platform/config"
	platformmetrics "github.com/runkora/kora/backend/internal/platform/metrics"
)

// Config holds session-manager configuration values.
//
// Values are loaded from:
//
//  1. cmd/session-manager/config/base.yaml  — service defaults
//  2. cmd/session-manager/config/{env}.yaml — env overrides (optional)
//  3. environment variables kora-agents sets on the agent pod (applyEnvOverrides)
type Config struct {
	App struct {
		Version string `yaml:"version"`
	} `yaml:"app"`

	Server struct {
		Port                int    `yaml:"port"`
		BasePath            string `yaml:"basePath"`
		ShutdownTimeoutSecs int    `yaml:"shutdownTimeoutSecs"`
		// TerminationGracePeriodSecs is the pod's grace period, set by kora-agents. Zero drains until the pod is stopped.
		TerminationGracePeriodSecs int `yaml:"terminationGracePeriodSecs"`
	} `yaml:"server"`

	Observability struct {
		ServiceName string `yaml:"serviceName"`
	} `yaml:"observability"`

	Workspaces struct {
		// BasePath is the root directory for per-run workspace subdirectories.
		// Typically /workspaces inside the agent pod.
		BasePath string `yaml:"basePath"`
	} `yaml:"workspaces"`

	KoraAgents struct {
		// URL is the base URL of kora-agents (e.g. "http://kora-agents.kora-system.svc.cluster.local").
		// When empty, event push is disabled (local dev without kora-agents).
		URL string `yaml:"url"`
	} `yaml:"koraAgents"`

	// Runner holds the settings that apply to every agent framework.
	Runner struct {
		// WrapperPath is the path to kora-session-runner, used by every framework.
		// When set, an agent is invoked as: WrapperPath <workDir> <binary> [args...]
		// so Landlock is applied first. Empty disables it, which is local dev on macOS.
		WrapperPath string `yaml:"wrapperPath"`

		// ShellPath is the kora-shell proxy each framework's shell resolves to, so
		// tool commands run in the run's sandbox. Claude Code reads it from
		// CLAUDE_CODE_SHELL, Codex from the runtime user's passwd entry, and
		// OpenCode from the "shell" key in its config. Empty leaves
		// OpenCode on the container's shell.
		ShellPath string `yaml:"shellPath"`

		// NodeMaxOldSpaceMB is the --max-old-space-size set in each run's
		// NODE_OPTIONS. kora-agents sets it per install through
		// KORA_NODE_MAX_OLD_SPACE_MB; zero adds no NODE_OPTIONS.
		NodeMaxOldSpaceMB int `yaml:"nodeMaxOldSpaceMB"`
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
		// URL is the base URL of the kora inference gateway.
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

	Metrics platformmetrics.Config `yaml:"metrics"`
}

// ShutdownTimeout returns the server shutdown timeout as a time.Duration.
func (c *Config) ShutdownTimeout() time.Duration {
	return time.Duration(c.Server.ShutdownTimeoutSecs) * time.Second
}

// DrainTimeout is the grace period less the shutdown timeout and a margin to close the rest.
// limited is false when no grace period is known, and the drain then waits for the runs.
func (c *Config) DrainTimeout() (d time.Duration, limited bool) {
	if c.Server.TerminationGracePeriodSecs <= 0 {
		return 0, false
	}
	return max(time.Duration(c.Server.TerminationGracePeriodSecs)*time.Second-c.ShutdownTimeout()-drainMargin, 0), true
}

// drainMargin is the part of the grace period kept for closing the sessions left after a drain.
const drainMargin = 30 * time.Second

// Current holds all non-secret configuration for the session-manager.
// Secrets must be supplied via environment variables. The session-manager
// reaches no database: run events travel to kora-agents over HTTP, which is
// what keeps database credentials out of the pod the agent runs in.
var Current = func() *Config {
	cfg := config.MustLoadService[Config]("cmd/session-manager", config.Environment())
	if err := applyEnvOverrides(cfg, os.Getenv); err != nil {
		panic(err)
	}
	return cfg
}()

// applyEnvOverrides layers the values kora-agents sets on the agent pod over
// the YAML defaults. getenv is injected so tests can drive it.
func applyEnvOverrides(cfg *Config, getenv func(string) string) error {
	if v := getenv("KORA_AGENTS_URL"); v != "" {
		cfg.KoraAgents.URL = v
	}
	if v := getenv("KORA_INFERENCE_GATEWAY_URL"); v != "" {
		cfg.InferenceGateway.URL = v
	}
	if v := getenv("KORA_TOOL_GATEWAY_URL"); v != "" {
		cfg.ToolGateway.URL = v
	}
	if v := getenv("KORA_TERMINATION_GRACE_PERIOD_SECS"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 0 {
			return fmt.Errorf("KORA_TERMINATION_GRACE_PERIOD_SECS must be a non-negative integer, got %q", v)
		}
		cfg.Server.TerminationGracePeriodSecs = secs
	}
	if v := getenv("KORA_NODE_MAX_OLD_SPACE_MB"); v != "" {
		mb, err := strconv.Atoi(v)
		if err != nil || mb < 0 {
			return fmt.Errorf("KORA_NODE_MAX_OLD_SPACE_MB must be a non-negative integer, got %q", v)
		}
		cfg.Runner.NodeMaxOldSpaceMB = mb
	}
	return nil
}
