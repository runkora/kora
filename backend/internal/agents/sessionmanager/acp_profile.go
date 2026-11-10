package sessionmanager

import (
	"encoding/json"
	"strings"

	acp "github.com/coder/acp-go-sdk"

	"github.com/runkora/kora/backend/internal/platform/events"
)

// acpProfile is everything that differs between ACP harnesses, so a new harness
// is a profile and not a new parser.
type acpProfile interface {
	// Command is the adapter binary and its arguments, before the sandbox wrapper.
	Command(workDir string) (binary string, args []string)
	// Prepare writes the run's config files and returns environment entries
	// applied over the process environment.
	Prepare(workDir string) (env []string, err error)
	// ClientCapabilities are clientCapabilities keys the harness declares at
	// initialize on top of the runner's own, such as subagents for Codex.
	ClientCapabilities() map[string]any
	// SessionParams are sent on session/new, session/resume and session/load.
	SessionParams(workDir string) acpSessionParams
	// ClassifyError returns the run_error code for a turn, or "" when the turn
	// carries no error the profile recognizes.
	ClassifyError(turn acpTurnEnd) string
	// IsQuestion marks a permission request as a question for the human,
	// answered through SendToolResult instead of auto-allowed.
	IsQuestion(tool acp.ToolCallUpdate) bool
	// AdapterSignal reads provider retries and background work from an adapter
	// message.
	AdapterSignal(msg acpAdapterMessage) acpSignal
	// ToolView is kora's name, input and result for a tool call, given what its
	// updates carried so far and the latest update u. It runs once per update.
	ToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView
}

// acpToolView is a tool call as kora records it. Output and IsError matter once
// the call has ended.
type acpToolView struct {
	Name    string
	Input   any
	Output  string
	IsError bool
}

// acpDefaultToolView names the call by its first title (or Claude's tool name),
// records its latest input and reads the result from rawOutput or the content
// blocks. Profiles whose adapter needs nothing else use it.
func acpDefaultToolView(c *acpToolCall, u acp.ToolCallUpdate) acpToolView {
	out, failed := acpToolOutput(u.Content, u.RawOutput)
	return acpToolView{Name: c.toolName(), Input: c.input, Output: out, IsError: failed}
}

// acpSessionParams is what a profile contributes to the ACP session.
type acpSessionParams struct {
	CWD        string
	MCPServers []acp.McpServer
	Meta       map[string]any
	// Mode is set with session/set_config_option after the session opens; Start
	// fails if the adapter refuses it.
	Mode string
}

// acpAdapterMessage is an extension notification or a session_info_update: the
// adapter-specific signals outside the mapped session/update variants.
type acpAdapterMessage struct {
	Method string
	Params json.RawMessage
}

// acpTurnEnd is what ClassifyError sees of a finished turn. RPCErr is nil and
// StopReason empty when they do not apply.
type acpTurnEnd struct {
	StopReason acp.StopReason
	RPCErr     *acp.RequestError
	Messages   []acpAdapterMessage
}

// acpSignal is what a profile reads from one adapter message.
type acpSignal struct {
	Retry *APIRetryPayload
	// InFlightWork is the number of background tasks running, nil when the
	// message does not report it.
	InFlightWork *int
}

// acpGatewayServers declares one HTTP server per MCP server the agent holds
// tool grants on. The identity-proxy sidecar injects the platform token on the
// way out (docs/adr/2026-06-29-mcp-tool-gateway.md).
func acpGatewayServers(gatewayURL string, names []string) []acp.McpServer {
	servers := []acp.McpServer{}
	if url := strings.TrimRight(gatewayURL, "/"); url != "" {
		for _, name := range names {
			servers = append(servers, acp.McpServer{Http: &acp.McpServerHttpInline{
				Type: "http", Name: name, Url: url + "/v1/mcp/" + name, Headers: []acp.HttpHeader{},
			}})
		}
	}
	return servers
}

// acpDefaultErrorCode is the run_error code when the profile has no better one.
const acpDefaultErrorCode = events.RunErrorRunnerError
