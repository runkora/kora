package sessionmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/events"
)

// OpenCodeRunnerConfig holds the per-run settings of an OpenCode session.
type OpenCodeRunnerConfig struct {
	// ShellPath is written into the OpenCode config as the "shell" key, so tool
	// commands run through kora-shell in the run's sandbox rather than in this
	// container. Empty leaves OpenCode on the container's own shell.
	ShellPath string

	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// kora-shell callback variables).
	ExtraEnv []string

	// SystemPrompt, when non-empty, reaches OpenCode as an instructions file
	// named in the OpenCode config. ACP has no field for it.
	SystemPrompt string

	// SessionManagerURL is where kora-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. Empty
	// disables the governed entries.
	ToolGatewayURL string

	// Brand names the product as the provider OpenCode shows the agent.
	Brand brand.Brand

	// MCPServers are the governed MCP servers this agent holds tool grants on,
	// one remote entry each.
	MCPServers []string

	// Model is the agent type's model, served by the inference gateway. It is
	// written into the OpenCode config as the only model OpenCode can use.
	Model string

	// InferenceGatewayURL is written directly into the OpenCode config so OpenCode
	// routes all LLM calls through the kora inference gateway. Must be non-empty.
	InferenceGatewayURL string

	// RunID is the run identifier for this session.
	RunID string
}

// openCodeProfile runs `opencode acp` on the acpRunner. Its config carries the
// model, shell and instructions file, and session/new declares the MCP servers.
type openCodeProfile struct {
	cfg OpenCodeRunnerConfig
}

// openCodeBinary is the OpenCode CLI in the session-manager image. It speaks ACP itself.
const openCodeBinary = "opencode"

// Command is `opencode acp --cwd workDir`.
func (p *openCodeProfile) Command(workDir string) (string, []string) {
	return openCodeBinary, []string{"acp", "--cwd", workDir}
}

// Prepare validates the config, writes the run's OpenCode files and returns the
// environment. It leaves the "mcp" block out of OPENCODE_CONFIG_CONTENT, so
// session/new.mcpServers is the only MCP source. OPENCODE_DISABLE_PROJECT_CONFIG
// keeps a workspace opencode.json from adding servers.
func (p *openCodeProfile) Prepare(workDir string) ([]string, error) {
	if p.cfg.InferenceGatewayURL == "" {
		return nil, errors.New("InferenceGatewayURL must not be empty")
	}
	// Without a model OpenCode falls back to its own hosted free models.
	if p.cfg.Model == "" {
		return nil, errors.New("model must not be empty")
	}
	config, err := openCodeConfig(p.cfg, workDir)
	if err != nil {
		return nil, fmt.Errorf("build config: %w", err)
	}
	if _, err = runHomeDirs(workDir); err != nil {
		return nil, err
	}
	homeEnv, err := openCodeHomeEnv(workDir)
	if err != nil {
		return nil, err
	}
	// The platform's values last, so ExtraEnv cannot override them.
	env := append(append(append([]string{}, p.cfg.ExtraEnv...), openCodeOfflineEnv...), openCodeWorkspaceEnv...)
	env = append(env, "OPENCODE_CONFIG_CONTENT="+string(config))
	return append(env, homeEnv...), nil
}

// ClientCapabilities is empty: OpenCode has no subagent sessions.
func (p *openCodeProfile) ClientCapabilities() map[string]any { return nil }

// SessionParams declares the runtime server the agent provisions its sandbox
// through and one HTTP server per MCP server it holds tool grants on. The model
// is pinned in the config, so no mode or model is set on the session.
func (p *openCodeProfile) SessionParams(workDir string) acpSessionParams {
	servers := append([]acp.McpServer{acpRuntimeServer(p.cfg.SessionManagerURL, p.cfg.RunID)}, acpGatewayServers(p.cfg.ToolGatewayURL, p.cfg.MCPServers)...)
	return acpSessionParams{CWD: workDir, MCPServers: servers}
}

// ClassifyError reads the budget denial from the text of OpenCode's internal
// error, e.g. "Internal error: Forbidden: budget exceeded: ...".
func (p *openCodeProfile) ClassifyError(turn acpTurnEnd) acpTurnVerdict {
	if e := turn.RPCErr; e != nil && e.Code == acpInternalErrorCode && isBudgetExceededMessage(e.Message) {
		return acpTurnVerdict{Code: events.RunErrorBudgetExhausted}
	}
	return acpTurnVerdict{}
}

// IsQuestion is false: every OpenCode permission request is auto-allowed.
func (p *openCodeProfile) IsQuestion(acp.ToolCallUpdate) bool { return false }

// AdapterSignal reads nothing: OpenCode sends no retry or background-work signals.
func (p *openCodeProfile) AdapterSignal(acpAdapterMessage) acpSignal { return acpSignal{} }

// ToolView is the default: OpenCode's titles and rawOutput are what kora records.
func (p *openCodeProfile) ToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
	return acpDefaultToolView(c, u)
}

var _ acpProfile = (*openCodeProfile)(nil)

// openCodeOfflineEnv turns off OpenCode's own calls home: the models.dev
// catalog, update checks, sharing, provider auth plugins and LSP downloads.
var openCodeOfflineEnv = []string{
	"OPENCODE_DISABLE_MODELS_FETCH=1",
	"OPENCODE_DISABLE_AUTOUPDATE=1",
	"OPENCODE_DISABLE_SHARE=1",
	"OPENCODE_DISABLE_DEFAULT_PLUGINS=1",
	"OPENCODE_DISABLE_LSP_DOWNLOAD=1",
}

// openCodeWorkspaceEnv stops OpenCode loading config, plugins and tools from the
// workspace. Their commands and code would run in the agent pod.
var openCodeWorkspaceEnv = []string{"OPENCODE_DISABLE_PROJECT_CONFIG=1"}

// openCodeHomeEnv puts OpenCode's HOME and config home in the run's config dir.
// OpenCode loads config, plugins and custom tools from both and runs their code.
// The agent can only read that dir and its sandbox does not mount it. Data, state,
// cache and TMPDIR stay in the run's writable dirs.
func openCodeHomeEnv(workDir string) ([]string, error) {
	configDir := runfs.ConfigDir(workDir)
	// OpenCode creates its global config dir at start and exits when it cannot.
	if err := os.MkdirAll(filepath.Join(configDir, "opencode"), 0o700); err != nil {
		return nil, fmt.Errorf("create opencode config home: %w", err)
	}
	home := runfs.HomeDir(workDir)
	return []string{
		"HOME=" + configDir,
		"XDG_CONFIG_HOME=" + configDir,
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"TMPDIR=" + runfs.TmpDir(workDir),
	}, nil
}

// openCodeProvider is the only provider OpenCode may load: the inference gateway.
// It is the provider's table key and the model id prefix, not the product name.
const openCodeProvider = "kora"

// openCodeMCPTimeoutMs is how long OpenCode waits for an MCP tool call, in
// milliseconds. Without it OpenCode gives up after 60s, while provision_sandbox
// can run for the provisioning budget (the chart caps it at 600s) plus the egress
// canary (egressCanaryMaxWait). It must stay at or above provisionTimeout in
// backend/cmd/kora-runtime-mcp/main.go, so OpenCode never times out before
// kora-runtime-mcp reports the real outcome.
const openCodeMCPTimeoutMs = 720000

// openCodeConfig is the run's OpenCode config. The agent type's model is the
// only model on offer, served by the inference gateway, and every other provider
// (OpenCode's hosted free models included) is switched off. It reaches OpenCode
// as OPENCODE_CONFIG_CONTENT, which is merged after every config file, so an
// opencode.json or .opencode/ directory in the workspace cannot override it.
func openCodeConfig(c OpenCodeRunnerConfig, workDir string) ([]byte, error) {
	gatewayURL := strings.TrimRight(c.InferenceGatewayURL, "/")
	model := openCodeProvider + "/" + c.Model
	cfg := map[string]any{
		"enabled_providers": []string{openCodeProvider},
		"model":             model,
		"small_model":       model,
		"provider": map[string]any{
			openCodeProvider: map[string]any{
				"name": c.Brand.ProductName(),
				// Chat completions serves every gateway model; /messages serves only Anthropic's.
				"npm": "@ai-sdk/openai-compatible",
				"options": map[string]string{
					"baseURL": fmt.Sprintf("%s/run/%s/v1", gatewayURL, c.RunID),
					// Placeholder: the identity-proxy sidecar replaces the Authorization header.
					"apiKey": "kora-managed",
				},
				"models": map[string]any{c.Model: map[string]any{}},
			},
		},
		"share":      "disabled",
		"autoupdate": false,
		// Upper bound for every MCP server OpenCode holds, not only the runtime one.
		"experimental": map[string]any{"mcp_timeout": openCodeMCPTimeoutMs},
		// Both fetch from this container, outside the sandbox's egress policy.
		"permission": map[string]string{"webfetch": "deny", "websearch": "deny"},
	}
	// Project config is off, which also drops the workspace AGENTS.md. Read as
	// instructions it is text only. A missing file is skipped.
	instructions := []string{filepath.Join(workDir, "AGENTS.md")}
	if c.SystemPrompt != "" {
		// In the run's config dir, which the agent can read but not change.
		path := filepath.Join(runfs.ConfigDir(workDir), "instructions.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(c.SystemPrompt), 0o600); err != nil {
			return nil, err
		}
		instructions = append([]string{path}, instructions...)
	}
	cfg["instructions"] = instructions
	// "shell" is the binary OpenCode's shell tool runs commands with. Pointing it
	// at kora-shell routes them to the run's sandbox.
	if c.ShellPath != "" {
		cfg["shell"] = c.ShellPath
	}
	return json.Marshal(cfg)
}
