package sessionmanager

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/agents/sandbox/runfs"
	"github.com/runkora/kora/backend/internal/platform/events"
)

// claudeACPBinary is the Claude Code ACP adapter in the session-manager image.
const claudeACPBinary = "claude-agent-acp"

// claudePath is the pinned claude the adapter runs, the binary the Landlock
// wrapper lets claude-agent-acp execute.
const claudePath = "/usr/local/bin/claude"

// claudeBypassMode skips the adapter's permission prompts.
const claudeBypassMode = "bypassPermissions"

// claudeSDKMessageMethod carries the raw Claude Agent SDK messages that
// emitRawSDKMessages asks for.
const claudeSDKMessageMethod = "_claude/sdkMessage"

// ClaudeRunnerConfig holds the per-run settings of a Claude Code session.
type ClaudeRunnerConfig struct {
	// Model is the agent type's model (e.g. "anthropic/claude-sonnet-5").
	Model string

	// SystemPrompt is appended to Claude's default system prompt. Empty adds nothing.
	SystemPrompt string

	// ExtraEnv is appended to the subprocess environment (e.g. ANTHROPIC_BASE_URL).
	ExtraEnv []string

	// Bare enables bare mode (no CLAUDE.md discovery, no OAuth/keychain, no
	// auto-memory). Containers run bare; local development turns it off in
	// config/local.yaml to use OAuth.
	Bare bool

	// SessionManagerURL is where kora-runtime-mcp calls back to provision a
	// sandbox. Always http://localhost:{port}, since it runs in this pod.
	SessionManagerURL string

	// ToolGatewayURL is the in-mesh base URL of the MCP tool-gateway. The agent
	// calls it unauthenticated and the identity-proxy sidecar injects the
	// platform token. Empty disables the governed entries.
	ToolGatewayURL string

	// MCPServers are the distinct MCP servers the agent has tool grants on, one
	// governed endpoint (/v1/mcp/<server>) each.
	MCPServers []string

	// RunID is the run identifier, passed to kora-runtime-mcp.
	RunID string
}

// claudeProfile runs claude-agent-acp on the acpRunner.
type claudeProfile struct {
	cfg ClaudeRunnerConfig
}

// claudeAttributionSettings disables Claude Code's own commit trailer and
// pull-request footer. kora is the control plane of record for who ran an
// agent and under what authority; a second, unverifiable identity string
// baked into the subprocess's default settings would contradict that.
const claudeAttributionSettings = `{"attribution":{"commit":"","pr":""}}`

// claudeSettingSources are the settings tiers Claude Code reads. User settings
// live in the run's HOME, which the agent writes. Unlike project settings, their
// env can load a plugin directory, and a plugin's LSP servers start in the agent pod.
var claudeSettingSources = []string{"project", "local"}

// claudeExcludedTools are the Claude Code built-ins that cannot work headless or would reach the network outside the sandbox.
// Every agent gets all other built-in tools; this list is platform policy, not agent configuration.
var claudeExcludedTools = []string{
	"WebSearch",        // Anthropic server-side tool, not served through the inference gateway.
	"WebFetch",         // fetches from the session-manager container, outside the sandbox's egress policy.
	"AskUserQuestion",  // nobody can answer it in a headless run.
	"PushNotification", // calls Anthropic's hosted service from the session-manager container, outside the egress policy.
	"DesignSync",       // calls Claude Design from the session-manager container, outside the egress policy.
}

// claudeOfflineEnv turns off Claude Code's background calls to Anthropic, which
// the agent pod's egress policy blocks. CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC
// is not used: it would also stop OTLP export to the platform collector.
var claudeOfflineEnv = []string{
	"DISABLE_AUTOUPDATER=1",
	"DISABLE_TELEMETRY=1",
	"DISABLE_ERROR_REPORTING=1",
	"DISABLE_FEEDBACK_COMMAND=1",
	"CLAUDE_CODE_DISABLE_FEEDBACK_SURVEY=1",
	"CLAUDE_CODE_DISABLE_OFFICIAL_MARKETPLACE_AUTOINSTALL=1",
	// Tool search returns tool_reference blocks that the gateway on Bedrock rejects with a 400 on the next call.
	"ENABLE_TOOL_SEARCH=false",
}

// Command is the adapter; CLAUDE_CODE_EXECUTABLE in Prepare names the claude it runs.
func (p *claudeProfile) Command(string) (string, []string) { return claudeACPBinary, nil }

// Prepare empties the user settings in the run home before every start. The
// adapter reads that tier whatever settingSources says, so a file the agent
// wrote there could refuse bypass mode at the next start. ANTHROPIC_MODEL ranks
// above a model that workspace settings name when the adapter opens a session.
func (p *claudeProfile) Prepare(workDir string) ([]string, error) {
	homeEnv, err := runHomeDirs(workDir)
	if err != nil {
		return nil, err
	}
	if err := writeClaudeUserSettings(workDir); err != nil {
		return nil, fmt.Errorf("write claude user settings: %w", err)
	}
	env := append(append([]string{}, claudeOfflineEnv...), p.cfg.ExtraEnv...)
	env = append(env, "CLAUDE_CODE_EXECUTABLE="+claudePath)
	if p.cfg.Model != "" {
		env = append(env, "ANTHROPIC_MODEL="+p.cfg.Model)
	}
	// HOME and TMPDIR last: nothing earlier may move the agent off the only
	// directory Landlock makes writable.
	return append(env, homeEnv...), nil
}

// writeClaudeUserSettings writes an empty .claude/settings.json in the run home
// through an os.Root on the workspace, which refuses a symlink out of it. The
// old file is removed first, so a symlink inside the workspace is replaced, not followed.
func writeClaudeUserSettings(workDir string) error {
	root, err := os.OpenRoot(workDir)
	if err != nil {
		return fmt.Errorf("open workspace: %w", err)
	}
	defer func() { _ = root.Close() }()
	dir, err := filepath.Rel(workDir, filepath.Join(runfs.HomeDir(workDir), ".claude"))
	if err != nil {
		return fmt.Errorf("config dir: %w", err)
	}
	if err := root.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	path := filepath.Join(dir, "settings.json")
	if err := root.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove settings.json: %w", err)
	}
	if err := root.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		return fmt.Errorf("write settings.json: %w", err)
	}
	return nil
}

// ClientCapabilities is empty: without subagents the adapter keeps a
// subagent's text out of the session.
func (p *claudeProfile) ClientCapabilities() map[string]any { return nil }

// SessionParams carries the run's Claude Code options, with prompt suggestions
// off: each costs a model call.
func (p *claudeProfile) SessionParams(workDir string) acpSessionParams {
	options := map[string]any{
		"strictMcpConfig":   true,
		"settingSources":    claudeSettingSources,
		"settings":          json.RawMessage(claudeAttributionSettings),
		"disallowedTools":   claudeExcludedTools,
		"promptSuggestions": false,
	}
	if p.cfg.Bare {
		options["extraArgs"] = map[string]any{"bare": nil}
	}
	if p.cfg.Model != "" {
		options["model"] = p.cfg.Model
	}
	meta := map[string]any{"claudeCode": map[string]any{
		"options":            options,
		"emitRawSDKMessages": []map[string]string{{"type": "system"}, {"type": "result"}},
	}}
	if p.cfg.SystemPrompt != "" {
		meta["systemPrompt"] = map[string]string{"append": p.cfg.SystemPrompt}
	}
	// Claude Code passes its process environment, run token included, to
	// kora-runtime-mcp, so the token stays off the ACP channel.
	servers := append([]acp.McpServer{acpRuntimeServer(p.cfg.SessionManagerURL, p.cfg.RunID)}, acpGatewayServers(p.cfg.ToolGatewayURL, p.cfg.MCPServers)...)
	return acpSessionParams{CWD: workDir, MCPServers: servers, Meta: meta, Mode: claudeBypassMode, OwnTurns: true}
}

// claudeSDKMessage is the raw SDK message of a _claude/sdkMessage notification.
func claudeSDKMessage(msg acpAdapterMessage) (claudeOutputWrapper, bool) {
	var n struct {
		Message claudeOutputWrapper `json:"message"`
	}
	if msg.Method != claudeSDKMessageMethod || json.Unmarshal(msg.Params, &n) != nil {
		return claudeOutputWrapper{}, false
	}
	return n.Message, true
}

// ClassifyError ends the turn from its last raw result. The JSON-RPC error
// alone cannot tell a budget denial from a failed login.
func (p *claudeProfile) ClassifyError(turn acpTurnEnd) acpTurnVerdict {
	result, budgetHit := claudeLastResult(turn.Messages)
	if result == nil || (turn.RPCErr == nil && turn.StopReason == acp.StopReasonCancelled) {
		return acpTurnVerdict{}
	}
	// The adapter also sends an API error it gave up on as reply text, which
	// is not the agent's.
	text := turn.Text
	if result.IsError {
		text = strings.TrimSuffix(text, result.Result)
	}
	v := acpTurnVerdict{Text: &text}
	switch {
	case isBudgetExceededMessage(result.Result) || (budgetHit && result.StopReason != "end_turn"):
		v.Code, v.Message = events.RunErrorBudgetExhausted, result.Result
	case result.Subtype != "success":
		v.Code, v.Message = events.RunErrorAgentError, result.Result
		if v.Message == "" {
			v.Message = fmt.Sprintf("agent exited with subtype=%q stop_reason=%q", result.Subtype, result.StopReason)
		}
	case !result.IsError:
	case text == "" && !turn.HadText && result.StopReason != "end_turn" && result.Result != "":
		v.Code, v.Message = events.RunErrorEmptyResponse, result.Result
	default:
		v.Complete = true
	}
	return v
}

// claudeLastResult is the last raw result of a turn, and whether a system
// message reported a budget denial.
func claudeLastResult(msgs []acpAdapterMessage) (result *claudeOutputWrapper, budgetHit bool) {
	for _, msg := range msgs {
		w, ok := claudeSDKMessage(msg)
		switch {
		case !ok:
		case w.Type == "result":
			result = &w
		case w.Type == "system" && isBudgetExceededMessage(string(msg.Params)):
			budgetHit = true
		}
	}
	return result, budgetHit
}

// IsQuestion is false: bypass mode sends no permission requests. An MCP
// server's question arrives as elicitation/create.
func (p *claudeProfile) IsQuestion(acp.ToolCallUpdate) bool { return false }

// AdapterSignal reads provider retries, the background task set and the end of
// a turn from the raw SDK messages.
func (p *claudeProfile) AdapterSignal(msg acpAdapterMessage) acpSignal {
	w, ok := claudeSDKMessage(msg)
	switch {
	case !ok:
	case w.Type == "result":
		return acpSignal{TurnEnded: true}
	case w.Subtype == "api_retry":
		retry := claudeAPIRetry(w)
		return acpSignal{Retry: &retry}
	case w.Subtype == "background_tasks_changed":
		n := liveBackgroundTasks(w.Tasks)
		return acpSignal{InFlightWork: &n}
	}
	return acpSignal{}
}

// ToolView names a call by Claude's tool name and reads its result from
// rawOutput. An update without content carries input that is still streaming.
func (p *claudeProfile) ToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
	out := ""
	if u.RawOutput != nil {
		if b, err := json.Marshal(u.RawOutput); err == nil {
			out = toolResultText(b)
		}
	}
	return acpToolView{Name: c.toolName(), Input: c.input, Output: out, Streaming: u.Content == nil}
}

var _ acpProfile = (*claudeProfile)(nil)

// claudeOutputWrapper is the part of a raw SDK message the profile reads.
type claudeOutputWrapper struct {
	Type       string `json:"type"`
	Subtype    string `json:"subtype"`
	StopReason string `json:"stop_reason"`
	Result     string `json:"result"`   // error message when Subtype == "error"
	IsError    bool   `json:"is_error"` // a result that reports an API error

	// system/api_retry
	Attempt      int             `json:"attempt"`
	MaxRetries   int             `json:"max_retries"`
	RetryDelayMs int             `json:"retry_delay_ms"`
	ErrorStatus  *int            `json:"error_status"`
	Error        json.RawMessage `json:"error"` // a class name here; raw so another line's shape cannot fail the decode

	// system/background_tasks_changed
	Tasks []claudeBackgroundTask `json:"tasks"`
}

// claudeBackgroundTask is one entry of a background_tasks_changed event.
type claudeBackgroundTask struct {
	// Ambient marks a task that is not activity, such as a live-update watcher.
	Ambient bool `json:"ambient"`
}

// claudeAPIRetry is the api_retry payload of a system/api_retry message.
func claudeAPIRetry(w claudeOutputWrapper) APIRetryPayload {
	return APIRetryPayload{
		Attempt:      w.Attempt,
		MaxRetries:   w.MaxRetries,
		RetryDelayMs: w.RetryDelayMs,
		ErrorStatus:  w.ErrorStatus,
		Error:        rawString(w.Error),
	}
}

// rawString returns a JSON string's value, or empty for any other JSON.
func rawString(raw json.RawMessage) string {
	var s string
	_ = json.Unmarshal(raw, &s)
	return s
}

// liveBackgroundTasks counts the tasks that are activity, leaving out ambient ones.
func liveBackgroundTasks(tasks []claudeBackgroundTask) int {
	n := 0
	for _, t := range tasks {
		if !t.Ambient {
			n++
		}
	}
	return n
}
