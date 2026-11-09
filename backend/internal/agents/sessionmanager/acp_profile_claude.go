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

// claudeBypassMode skips the adapter's permission prompts, as
// --dangerously-skip-permissions does for the legacy runner.
const claudeBypassMode = "bypassPermissions"

// claudeSDKMessageMethod carries the raw Claude Agent SDK messages that
// emitRawSDKMessages asks for.
const claudeSDKMessageMethod = "_claude/sdkMessage"

// claudeProfile runs claude-agent-acp on the acpRunner with the legacy runner's
// options, environment and MCP servers.
type claudeProfile struct {
	cfg ClaudeRunnerConfig
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
// subagent's text out of the session, as the legacy runner drops it.
func (p *claudeProfile) ClientCapabilities() map[string]any { return nil }

// SessionParams carries the legacy runner's flags as Claude Code options, with
// prompt suggestions off: each costs a model call.
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
	return acpSessionParams{CWD: workDir, MCPServers: servers, Meta: meta, Mode: claudeBypassMode}
}

// claudeSDKMessage is the raw SDK message of a _claude/sdkMessage notification,
// the same message the legacy runner reads from stream-json.
func claudeSDKMessage(msg acpAdapterMessage) (claudeOutputWrapper, bool) {
	var n struct {
		Message claudeOutputWrapper `json:"message"`
	}
	if msg.Method != claudeSDKMessageMethod || json.Unmarshal(msg.Params, &n) != nil {
		return claudeOutputWrapper{}, false
	}
	return n.Message, true
}

// ClassifyError ends the turn as the legacy runner does from its last result.
// The JSON-RPC error alone cannot tell a budget denial from a failed login.
func (p *claudeProfile) ClassifyError(turn acpTurnEnd) acpTurnVerdict {
	result, budgetHit := claudeLastResult(turn.Messages)
	if result == nil || (turn.RPCErr == nil && turn.StopReason == acp.StopReasonCancelled) {
		return acpTurnVerdict{}
	}
	// The adapter also sends an API error it gave up on as reply text, which
	// the legacy runner never sees.
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
// message reported a budget denial, which the legacy runner sniffs the same way.
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
// a turn from the raw SDK messages, as the legacy runner reads them from stream-json.
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
