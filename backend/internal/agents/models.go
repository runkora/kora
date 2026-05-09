package agents

import (
	"encoding/json"
	"time"
)

// ── Agent Types ───────────────────────────────────────────────────────────────

type AgentTypeState string

const (
	AgentTypeStateProvisioning AgentTypeState = "provisioning"
	AgentTypeStateActive       AgentTypeState = "active"
	AgentTypeStateDeleting     AgentTypeState = "deleting"
)

// AgentFramework identifies the CLI framework running inside agent pods.
type AgentFramework string

const (
	AgentFrameworkClaudeCode AgentFramework = "claude-code"
	AgentFrameworkCodex      AgentFramework = "codex"
	AgentFrameworkOpenCode   AgentFramework = "opencode"
)

// AgentType defines a category of managed agent within a tenant.
// Each active AgentType has a corresponding StatefulSet in the tenant namespace.
type AgentType struct {
	ID                      string
	TenantID                string
	Name                    string
	Slug                    string         // URL-safe identifier, unique per tenant
	Description             string         // human-readable summary shown in the portal
	SystemPrompt            string         // injected as the agent's initial system context
	Framework               AgentFramework // CLI framework running in the pod
	ToolScopes              []string       // Claude Code built-in tools allowed (e.g. ["shell", "filesystem"])
	GrantScopes             []string       // inference gateway scopes granted via OPA (e.g. ["inference:invoke"])
	MaxConcurrentRunsPerPod int            // cap per StatefulSet pod
	State                   AgentTypeState
	CreatedAt               time.Time
	UpdatedAt               time.Time
}

// ── Runs ──────────────────────────────────────────────────────────────────────

type RunState string

const (
	RunStatePending    RunState = "pending"    // created, pod not yet assigned
	RunStateStarting   RunState = "starting"   // pod assigned, subprocess starting
	RunStateActive     RunState = "active"     // CLI subprocess running and responsive
	RunStateCompleting RunState = "completing" // subprocess exited cleanly; snapshot in progress
	RunStateCompleted  RunState = "completed"  // snapshot done; resources released
	RunStateFailed     RunState = "failed"     // subprocess exited with error
)

// Run is the durable unit of work. It outlives browser connections and persists
// until completed, failed, or explicitly deleted.
type Run struct {
	ID          string
	TenantID    string
	AgentTypeID string
	State       RunState
	PodOrdinal  int    // which StatefulSet replica owns this run
	CreatedBy   string // principal_id of the initiating user
	StartedAt   *time.Time
	CompletedAt *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ── Sessions ──────────────────────────────────────────────────────────────────

// Session is a transient browser connection to a Run.
// A run can have zero or one active session. Disconnect does not affect the run.
type Session struct {
	ID             string
	RunID          string
	PrincipalID    string
	ConnectedAt    time.Time
	DisconnectedAt *time.Time
}

// ── Events ────────────────────────────────────────────────────────────────────

// RunEvent is one entry in the event log for a run.
// Written by orbit-agents when state changes, or by draining session-manager events.
type RunEvent struct {
	SequenceNo int64
	RunID      string
	EventType  string
	Payload    json.RawMessage
	CreatedAt  time.Time
}

// ── Messages ──────────────────────────────────────────────────────────────────

// RunMessage is a user message sent to a run. Stored separately from events —
// the event log is agent output only; messages are the input side.
type RunMessage struct {
	ID         string
	RunID      string
	SequenceNo int64
	Content    string
	CreatedAt  time.Time
}
