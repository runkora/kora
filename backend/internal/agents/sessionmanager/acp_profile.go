package sessionmanager

import "encoding/json"

// acpSessionParams is what a profile contributes to session/new, session/load
// and session/resume.
type acpSessionParams struct {
	CWD        string
	MCPServers []any  // nil sends an empty list
	Model      string // set through the "model" session config option when non-empty
}

// acpPermissionTool describes the tool call a session/request_permission is about.
type acpPermissionTool struct {
	ToolCallID string
	Title      string
	Kind       string
	RawInput   json.RawMessage
}

// acpQuirks are the per-harness differences the generic runner honors.
type acpQuirks struct {
	// SteerMethod is the adapter's mid-turn steering notification (codex-acp
	// documents _session/steering). It is used only when the adapter advertises
	// it as agentCapabilities._meta.steering; otherwise a mid-turn Send queues.
	SteerMethod string

	// IsQuestion marks a permission request that is a question for the human
	// (answered through SendToolResult) rather than a tool permission the runner
	// answers itself. Nil means none are.
	IsQuestion func(acpPermissionTool) bool
}

// acpProfile is everything that differs between harnesses. A new harness is a
// new profile, not a new parser.
type acpProfile interface {
	// Name labels the harness in logs.
	Name() string
	// Command is the ACP agent binary and its arguments, before the sandbox wrapper.
	Command(workDir string) (binary string, args []string)
	// Prepare writes the run's config files and returns environment entries that
	// are applied over the process environment.
	Prepare(workDir string) (env []string, err error)
	// SessionParams are the cwd, MCP servers and model for the ACP session.
	SessionParams(workDir string) acpSessionParams
	// ClassifyError maps a non-end_turn stop reason (max_tokens, refusal, ...) or
	// a JSON-RPC error to a kora run_error code. Both are empty-valued when they
	// do not apply, e.g. for an unexpected exit.
	ClassifyError(stopReason string, rpcErr *acpRPCError) string
	Quirks() acpQuirks
}

// acpDefaultErrorCode is the run_error code when a profile has no better one.
const acpDefaultErrorCode = "runner_error"
