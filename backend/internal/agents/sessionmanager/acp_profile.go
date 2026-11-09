package sessionmanager

import (
	"encoding/json"

	acp "github.com/coder/acp-go-sdk"
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

// acpDefaultErrorCode is the run_error code when the profile has no better one.
const acpDefaultErrorCode = "runner_error"
