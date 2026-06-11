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
// AgentBudget is the spending configuration applied to the agent's policy
// grant. Not persisted by orbit-agents — the grant is the source of truth
// (budgets live in policy, one place); this struct carries the desired value
// through create/update and the current value on reads. Both limits zero =
// remove the budget.
type AgentBudget struct {
	// LimitUSD is the per-window ceiling (0 = none).
	LimitUSD float64 `json:"limit_usd,omitempty"`
	// ResetCadence is "weekly" or "monthly" (default monthly).
	ResetCadence string `json:"reset_cadence,omitempty"`
	// PerRunLimitUSD is the windowless per-run ceiling (0 = none).
	PerRunLimitUSD float64 `json:"per_run_limit_usd,omitempty"`
}

type AgentType struct {
	ID                      string         `json:"id"`
	TenantID                string         `json:"tenant_id"`
	Name                    string         `json:"name"`
	Slug                    string         `json:"slug"`
	Description             string         `json:"description"`
	SystemPrompt            string         `json:"system_prompt"`
	Framework               AgentFramework `json:"framework"`
	ToolScopes              []string       `json:"tool_scopes"`
	GrantScopes             []string       `json:"grant_scopes"`
	MaxConcurrentRunsPerPod int            `json:"max_concurrent_runs_per_pod"`
	State                   AgentTypeState `json:"state"`
	CreatedAt               time.Time      `json:"created_at"`
	UpdatedAt               time.Time      `json:"updated_at"`
	ModelID                 *string        `json:"model_id,omitempty"`
	// Budget mirrors the budget on the agent's policy grant. Transient: never
	// written to the agent_types table; populated on detail reads and carried
	// as the desired state during grant reconciliation.
	Budget *AgentBudget `json:"budget,omitempty"`
}

// ── Runs ──────────────────────────────────────────────────────────────────────

type RunState string

const (
	RunStatePending    RunState = "pending"    // created, pod not yet assigned
	RunStateStarting   RunState = "starting"   // pod assigned, subprocess starting
	RunStateActive     RunState = "active"     // agent is actively processing a turn
	RunStateIdle       RunState = "idle"       // agent completed its turn; waiting for next user message
	RunStateCompleting RunState = "completing" // subprocess exited cleanly; snapshot in progress
	RunStateCompleted  RunState = "completed"  // snapshot done; resources released
	RunStateFailed     RunState = "failed"     // unrecoverable error; subprocess crashed or failed to start
	// RunStateBudgetExhausted is terminal: the run hit an applicable budget
	// ceiling and the gateway started rejecting its LLM calls with
	// BUDGET_EXCEEDED. Distinct from failed — the run did nothing wrong; its
	// budget ended. See docs/adr/2026-06-10-hierarchical-budget-enforcement.md.
	RunStateBudgetExhausted RunState = "budget_exhausted"
)

// Run is the durable unit of work. It outlives browser connections and persists
// until completed, failed, or explicitly deleted.
type Run struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	AgentTypeID string     `json:"agent_type_id"`
	State       RunState   `json:"state"`
	PodOrdinal  int        `json:"pod_ordinal"`
	CreatedBy   string     `json:"created_by"`
	StartedAt   *time.Time `json:"started_at"`
	CompletedAt *time.Time `json:"completed_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
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
