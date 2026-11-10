package sessionmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	acp "github.com/coder/acp-go-sdk"

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

// codexProfile runs codex-acp on the acpRunner, with the config.toml that
// writeCodexHome rewrites in CODEX_HOME before every start.
type codexProfile struct {
	cfg CodexRunnerConfig
}

// Command is the adapter; CODEX_PATH in Prepare names the codex it wraps.
func (p *codexProfile) Command(string) (string, []string) { return codexACPBinary, nil }

// Prepare writes config.toml with the developer instructions and the runtime MCP server into CODEX_HOME.
func (p *codexProfile) Prepare(workDir string) ([]string, error) {
	instructions, err := json.Marshal(codexDeveloperInstructions(p.cfg.SystemPrompt))
	if err != nil {
		return nil, fmt.Errorf("encode developer instructions: %w", err)
	}
	// A JSON string is also a valid TOML basic string.
	home, err := writeCodexHome(p.cfg, workDir, "developer_instructions = "+string(instructions)+"\n", codexRuntimeMCPTOML(p.cfg))
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
	p.cfg.Logger.Warn(context.Background(), "Codex error details are not a string",
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
// it through the legacy runner's mapping.
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
	name, input, _ := codexToolCall(it)
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
	it.Status = ""
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
	name, input, _ := codexToolCall(it)
	return acpToolView{Name: name, Input: input}, collabResult(it)
}

// decodeAs copies the JSON fields of v into out; v of another shape adds nothing.
func decodeAs(v any, out *codexItem) {
	if b, err := json.Marshal(v); err == nil {
		_ = json.Unmarshal(b, out)
	}
}

var _ acpProfile = (*codexProfile)(nil)
