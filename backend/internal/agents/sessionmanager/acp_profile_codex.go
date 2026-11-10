package sessionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/brand"
	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// codexACPBinary is the Codex ACP adapter in the session-manager image.
const codexACPBinary = "codex-acp"

// codexPath is the pinned codex the adapter runs, the binary the Landlock
// wrapper lets codex-acp execute.
const codexPath = "/usr/local/bin/codex"

// codexFullAccessMode skips codex-acp's approval review: the platform's gateways
// enforce policy, and its default mode sends every command to a review model.
const codexFullAccessMode = "agent-full-access"

// CodexRunnerConfig holds the per-run settings of a Codex session.
type CodexRunnerConfig struct {
	// ExtraEnv is appended to the subprocess environment (WORKSPACE and the
	// kora-shell callback variables).
	ExtraEnv []string

	// SessionManagerURL is where kora-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. Empty
	// disables the governed entries.
	ToolGatewayURL string

	// Brand names the product as the model provider Codex shows the agent.
	Brand brand.Brand

	// MCPServers are the governed MCP servers this agent holds tool grants on,
	// one streamable-HTTP entry each.
	MCPServers []string

	// Model is written into config.toml. May be empty.
	Model string

	// InferenceGatewayURL is written into config.toml as the custom provider's
	// base_url, so Codex sends every Responses API call through the gateway.
	InferenceGatewayURL string

	// SystemPrompt, when non-empty, leads the developer instructions in config.toml.
	SystemPrompt string

	// Logger is used to emit structured log events.
	Logger logging.Logger

	// TraceCtx carries the create-session request's trace into the profile's log lines.
	TraceCtx context.Context

	// RunID is the run identifier for this session.
	RunID string
}

// codexProfile runs codex-acp on the acpRunner, with the config.toml that
// writeCodexHome rewrites in CODEX_HOME before every start.
type codexProfile struct {
	cfg CodexRunnerConfig
}

// Command is the adapter; CODEX_PATH in Prepare names the codex it wraps.
func (p *codexProfile) Command(string) (string, []string) { return codexACPBinary, nil }

// Prepare writes config.toml with the developer instructions and the runtime MCP server into CODEX_HOME.
func (p *codexProfile) Prepare(workDir string) ([]string, error) {
	home, err := writeCodexHome(p.cfg, workDir)
	if err != nil {
		return nil, fmt.Errorf("write codex config: %w", err)
	}
	homeEnv, err := runHomeDirs(workDir)
	if err != nil {
		return nil, err
	}
	env := append([]string{"CODEX_HOME=" + home, "CODEX_PATH=" + codexPath, "KORA_INFERENCE_KEY=kora-managed"}, p.cfg.ExtraEnv...)
	// HOME and TMPDIR last: nothing earlier may move the agent off the only
	// directory Landlock makes writable.
	return append(env, homeEnv...), nil
}

// ClientCapabilities declares child sessions, so codex-acp sends a subagent's
// tool calls on its own session.
func (p *codexProfile) ClientCapabilities() map[string]any {
	return map[string]any{"subagents": map[string]any{}}
}

// SessionParams declares the gateway MCP servers and switches off the approval review.
func (p *codexProfile) SessionParams(workDir string) acpSessionParams {
	return acpSessionParams{CWD: workDir, MCPServers: acpGatewayServers(p.cfg.ToolGatewayURL, p.cfg.MCPServers), Mode: codexFullAccessMode}
}

// codexError is codex-acp's session_info_update._meta.codex.error.
type codexError struct {
	Message           string          `json:"message"`
	CodexErrorInfo    json.RawMessage `json:"codexErrorInfo"`
	AdditionalDetails json.RawMessage `json:"additionalDetails"`
	WillRetry         bool            `json:"willRetry"`
}

func codexErrorOf(msg acpAdapterMessage) *codexError {
	var n struct {
		Update struct {
			Meta struct {
				Codex struct {
					Error *codexError `json:"error"`
				} `json:"codex"`
			} `json:"_meta"`
		} `json:"update"`
	}
	if msg.Method != acp.ClientMethodSessionUpdate || json.Unmarshal(msg.Params, &n) != nil {
		return nil
	}
	return n.Update.Meta.Codex.Error
}

// ClassifyError finds the gateway's budget denial in the turn's Codex errors.
// Codex retries it, then ends the turn normally with the error as reply text.
func (p *codexProfile) ClassifyError(turn acpTurnEnd) acpTurnVerdict {
	for _, msg := range turn.Messages {
		if e := codexErrorOf(msg); e != nil && isBudgetExceededMessage(p.details(e)) {
			return acpTurnVerdict{Code: events.RunErrorBudgetExhausted}
		}
	}
	return acpTurnVerdict{}
}

// details is additionalDetails as text. codex-acp 2.6.0 sends a string; any
// other shape is logged and read as raw JSON.
func (p *codexProfile) details(e *codexError) string {
	var s string
	if len(e.AdditionalDetails) == 0 || json.Unmarshal(e.AdditionalDetails, &s) == nil {
		return s
	}
	p.cfg.Logger.Warn(p.cfg.TraceCtx, "Codex error details are not a string",
		logging.Field{Key: "run_id", Value: p.cfg.RunID}, logging.Field{Key: "details", Value: string(e.AdditionalDetails)})
	return string(e.AdditionalDetails)
}

// IsQuestion is false: Codex asks no questions in full-access mode.
func (p *codexProfile) IsQuestion(acp.ToolCallUpdate) bool { return false }

// AdapterSignal reads a stream retry with the counts from the message
// ("Reconnecting... 2/5"). Without them the translator counts the turn's retries.
func (p *codexProfile) AdapterSignal(msg acpAdapterMessage) acpSignal {
	e := codexErrorOf(msg)
	if e == nil || !e.WillRetry {
		return acpSignal{}
	}
	class, status := codexErrorClass(e.CodexErrorInfo)
	retry := APIRetryPayload{ErrorStatus: status, Error: class}
	if m := codexRetryCount.FindStringSubmatch(e.Message); m != nil {
		retry.Attempt, _ = strconv.Atoi(m[1])
		retry.MaxRetries, _ = strconv.Atoi(m[2])
	}
	return acpSignal{Retry: &retry}
}

// ToolView rebuilds the App Server item behind a codex-acp tool call and records
// it through the App Server item mapping.
func (p *codexProfile) ToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
	if delta, _ := u.Meta["terminal_output_delta"].(map[string]any); delta != nil {
		data, _ := delta["data"].(string)
		c.output.WriteString(data)
	}
	if changes := codexDiffChanges(u.Content); changes != nil && c.input == nil {
		c.input = map[string]any{"changes": changes}
	}
	it, ok := codexACPItem(c, u)
	if !ok {
		return acpDefaultToolView(c, u)
	}
	name, input := codexToolCall(it)
	out, failed := codexItemResult(it, c.output.String())
	return acpToolView{Name: name, Input: input, Output: out, IsError: failed}
}

// codexACPItem is the App Server item behind a codex-acp call: its own fields
// from rawInput, an MCP result from rawOutput, and the exit code from
// terminal_exit or, for a read, search or list command, rawOutput.
func codexACPItem(c *acpToolCall, u acp.ToolCallUpdate) (codexItem, bool) {
	var it codexItem
	decodeAs(c.input, &it)
	decodeAs(u.RawOutput, &it)
	switch {
	case it.Server != "" && it.Tool != "":
		it.Type = "mcpToolCall"
	case it.ReceiverThreadIDs != nil:
		it.Type, it.Tool = "collabAgentToolCall", c.title
	case c.kind == acp.ToolKindExecute || c.kind == acp.ToolKindRead || c.kind == acp.ToolKindSearch:
		it.Type = "commandExecution"
		exit, _ := u.Meta["terminal_exit"].(map[string]any)
		raw, _ := u.RawOutput.(map[string]any)
		for _, m := range []map[string]any{exit, raw} {
			if code, ok := m["exit_code"].(float64); ok {
				n := int(code)
				it.ExitCode = &n
				break
			}
		}
	case c.kind == acp.ToolKindEdit:
		it.Type = "fileChange"
	default:
		return it, false
	}
	return it, true
}

// codexDiffChanges is a file change's path and kind per file, from the diff
// blocks codex-acp sends in place of the App Server's changes. The diff text is
// not rebuilt.
func codexDiffChanges(content []acp.ToolCallContent) []map[string]any {
	var changes []map[string]any
	seen := map[string]bool{}
	for _, c := range content {
		if d := c.Diff; d != nil && !seen[d.Path] {
			seen[d.Path] = true
			kind, _ := d.Meta["kind"].(string)
			changes = append(changes, map[string]any{"path": d.Path, "kind": map[string]string{"type": kind}})
		}
	}
	return changes
}

// codexSubagentView records a child session as the spawnAgent call it replaces.
// The child's id is "<root>:subagent:<thread>".
func codexSubagentView(child, description string) (acpToolView, string) {
	_, thread, ok := strings.Cut(child, ":subagent:")
	if !ok {
		thread = child
	}
	it := codexItem{Type: "collabAgentToolCall", Tool: "spawnAgent", Prompt: &description, ReceiverThreadIDs: []string{thread}}
	name, input := codexToolCall(it)
	return acpToolView{Name: name, Input: input}, collabResult(it)
}

// decodeAs copies the JSON fields of v into out; v of another shape adds nothing.
func decodeAs(v any, out *codexItem) {
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, out)
	}
}

var _ acpProfile = (*codexProfile)(nil)

// sandboxToolInstructions tells a Codex agent to provision a sandbox, and where
// to find the tool. Codex keeps MCP tools out of the model's tool list until
// they are searched for. Claude Code and OpenCode list them directly.
const sandboxToolInstructions = `Commands run only in a sandbox. Provision one with the provision_sandbox tool before your first command. If the tool is not already in your tool list, search for it by name.`

// codexDeveloperInstructions is the agent's own prompt with the sandbox
// instructions appended, or those instructions alone when the agent has no prompt.
func codexDeveloperInstructions(systemPrompt string) string {
	if systemPrompt == "" {
		return sandboxToolInstructions
	}
	return systemPrompt + "\n\n" + sandboxToolInstructions
}

// codexRuntimeMCPTOML renders the runtime server's table. It stays in the config
// file because Codex passes an MCP server only the variables env_vars names.
func codexRuntimeMCPTOML(cfg CodexRunnerConfig) string {
	return fmt.Sprintf(`
[mcp_servers.runtime]
command = "kora-runtime-mcp"
args    = []
default_tools_approval_mode = "approve"
# Codex starts MCP servers with a filtered env; the run token proves the run on /internal calls.
env_vars = ["KORA_RUN_TOKEN"]

[mcp_servers.runtime.env]
KORA_SESSION_MANAGER_URL = %q
KORA_RUN_ID              = %q
`, cfg.SessionManagerURL, cfg.RunID)
}

// codexItem holds the App Server ThreadItem fields the profile rebuilds from a
// codex-acp call. The protocol's ThreadItem is a union keyed by type; unused
// fields stay zero.
type codexItem struct {
	Type             string          `json:"type"`
	Command          string          `json:"command"`
	Cwd              string          `json:"cwd"`
	AggregatedOutput *string         `json:"aggregatedOutput"`
	ExitCode         *int            `json:"exitCode"`
	Changes          json.RawMessage `json:"changes"`
	Server           string          `json:"server"`
	Tool             string          `json:"tool"`
	Arguments        json.RawMessage `json:"arguments"`
	Result           *struct {
		Content json.RawMessage `json:"content"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	Prompt            *string                              `json:"prompt"`
	ReceiverThreadIDs []string                             `json:"receiverThreadIds"`
	AgentsStates      map[string]struct{ Message *string } `json:"agentsStates"`
}

// codexShellWrap matches Codex's command line for a shell call: the configured
// shell run with -lc and the agent's command as one quoted argument.
var codexShellWrap = regexp.MustCompile(`(?s)^\S+ -lc '(.*)'$`)

// unwrapShell returns the command the agent asked for, without the shell Codex
// wraps it in; anything else is returned as it is.
func unwrapShell(command string) string {
	m := codexShellWrap.FindStringSubmatch(command)
	if m == nil {
		return command
	}
	return strings.ReplaceAll(m[1], `'\''`, `'`)
}

// codexToolCall names a tool item and gives its own arguments as the recorded
// input, in the same shape every profile records: what the tool was asked to do.
// Items that are not tool calls (messages, reasoning, plans) have no name.
func codexToolCall(it codexItem) (name string, input any) {
	switch it.Type {
	case "commandExecution":
		return "commandExecution", map[string]string{"command": unwrapShell(it.Command), "cwd": it.Cwd}
	case "fileChange":
		return "fileChange", map[string]json.RawMessage{"changes": it.Changes}
	case "mcpToolCall":
		return "mcp__" + it.Server + "__" + it.Tool, it.Arguments
	case "collabAgentToolCall":
		return it.Tool, map[string]any{"prompt": it.Prompt, "receiver_thread_ids": it.ReceiverThreadIDs}
	}
	return "", nil
}

// codexItemResult is a finished tool item's result as text, and whether it failed.
func codexItemResult(it codexItem, streamed string) (string, bool) {
	switch it.Type {
	case "commandExecution":
		out := streamed
		if it.AggregatedOutput != nil {
			out = *it.AggregatedOutput
		}
		if it.ExitCode != nil && *it.ExitCode != 0 {
			return fmt.Sprintf("Exit code %d\n%s", *it.ExitCode, out), true
		}
		return out, false
	case "mcpToolCall":
		if it.Error != nil {
			return it.Error.Message, true
		}
		if it.Result != nil {
			return toolResultText(it.Result.Content), false
		}
	case "fileChange":
		return string(it.Changes), false
	case "collabAgentToolCall":
		return collabResult(it), false
	}
	return "", false
}

// collabResult is what a subagent call reported: its agents' messages, or, for
// a spawn, which answers before the subagent does, the thread it started.
func collabResult(it codexItem) string {
	// Sorted by thread, so the recorded result does not depend on map order.
	var parts []string
	for _, thread := range slices.Sorted(maps.Keys(it.AgentsStates)) {
		if m := it.AgentsStates[thread].Message; m != nil {
			parts = append(parts, *m)
		}
	}
	if len(parts) == 0 && len(it.ReceiverThreadIDs) > 0 {
		return "Started subagent thread " + strings.Join(it.ReceiverThreadIDs, ", ")
	}
	return strings.Join(parts, "\n")
}

// codexRetryCount reads "attempt/max" from a retry message such as
// "Reconnecting... 2/5"; Codex puts the counts only in the text.
var codexRetryCount = regexp.MustCompile(`(\d+)/(\d+)`)

// codexErrorClass reads Codex's error info: a bare string ("unauthorized") or an
// object keyed by the class carrying the upstream HTTP status.
func codexErrorClass(raw json.RawMessage) (string, *int) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, nil
	}
	var obj map[string]struct {
		HTTPStatusCode *int `json:"httpStatusCode"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		for k, v := range obj {
			return k, v.HTTPStatusCode
		}
	}
	return "", nil
}

// codexProvider is Codex's config-table key for the inference gateway, not the product name.
const codexProvider = "kora"

// writeCodexHome writes the run's config.toml into CODEX_HOME (workDir/.codex)
// and returns that directory.
func writeCodexHome(cfg CodexRunnerConfig, workDir string) (string, error) {
	if cfg.InferenceGatewayURL == "" {
		return "", fmt.Errorf("InferenceGatewayURL must not be empty")
	}
	// A JSON string is also a valid TOML basic string.
	instructions, err := json.Marshal(codexDeveloperInstructions(cfg.SystemPrompt))
	if err != nil {
		return "", fmt.Errorf("encode developer instructions: %w", err)
	}
	gatewayURL := strings.TrimRight(cfg.InferenceGatewayURL, "/")
	// Codex treats base_url as already containing the API version segment (its
	// own default is "https://api.openai.com/v1") and appends "/responses" to
	// it. Without the trailing /v1, requests are missing the version segment
	// and are rejected by the gateway.
	perRunURL := fmt.Sprintf("%s/run/%s/v1", gatewayURL, cfg.RunID)

	// Written through an os.Root on the workspace the agent can write, so a
	// symlink it plants that leads out of the workspace is refused.
	root, err := os.OpenRoot(workDir)
	if err != nil {
		return "", fmt.Errorf("open workspace: %w", err)
	}
	defer root.Close()
	if err := root.MkdirAll(".codex", 0o700); err != nil {
		return "", fmt.Errorf("create codex home dir: %w", err)
	}

	// Build minimal config.toml: one custom provider pointing at the gateway.
	// wire_api = "responses" is required: Codex only speaks the Responses API.
	// env_key names an environment variable that holds the bearer token; the
	// identity-proxy sidecar injects a platform token via KORA_INFERENCE_KEY,
	// so the actual secret value never appears on the command line or in the file.
	configContent := fmt.Sprintf(`model_provider = %q
model = %q
# Codex refuses to run a command unless it can sandbox it with bubblewrap, which
# this image does not carry: without this it panics with "bubblewrap is
# unavailable" and no command runs at all. The platform's boundary is the sandbox
# pod and the Landlock wrapper around this process, so a second sandbox nested
# inside them would add nothing (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
# Stays above the first table header: a key after one belongs to that table.
sandbox_mode = "danger-full-access"
# Headless: no human can answer an approval, and the platform's gateways enforce policy.
approval_policy = "never"
# The agent pod reaches only platform services
# (docs/adr/2026-09-30-agent-pod-egress-lockdown.md): no update check, and no
# hosted web search tool, as for Claude Code.
check_for_update_on_startup = false
web_search = "disabled"
%s
[analytics]
enabled = false

[feedback]
enabled = false

[model_providers.%s]
name     = %q
base_url = %q
env_key  = "KORA_INFERENCE_KEY"
wire_api = "responses"

[projects.%q]
trust_level = "trusted"

# Codex's unified exec runs a persistent shell session and sends it a startup
# script that names its resolved shell by path, which is meaningless once
# kora-shell forwards the command into the sandbox container. The legacy shell
# tool sends one self-contained command per call, which is the same shape Claude
# Code uses and the shape kora-shell implements
# (docs/adr/2026-09-18-sandbox-exec-trust-boundary.md).
[features]
shell_tool = true
unified_exec = false
# The shell snapshot is taken by a login shell on this pod, whose /etc/profile
# resets PATH; replayed in the sandbox it would hide the toolchains and user
# installs. Without it, commands get the sandbox's own environment.
shell_snapshot = false
# The curated plugin sync clones github.com/openai/plugins at start.
plugins = false
%s`, codexProvider, cfg.Model, "developer_instructions = "+string(instructions)+"\n", codexProvider, cfg.Brand.ProductName(), perRunURL, workDir, codexRuntimeMCPTOML(cfg))

	if err := root.WriteFile(filepath.Join(".codex", "config.toml"), []byte(configContent), 0o600); err != nil {
		return "", fmt.Errorf("write codex config.toml: %w", err)
	}
	return filepath.Join(workDir, ".codex"), nil
}
