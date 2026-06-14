package agents

import (
	"encoding/json"
	"fmt"
	"strings"
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

// ConnectivityMode is an agent type's outbound egress posture — the platform's
// reachability gate for the agent's sandboxes. It compiles to egress:<host>:connect
// scopes enforced at the sandbox OPA. See the egress-gateway ADR (Amendment
// 2026-06-07): connections are a credential overlay; this posture decides what a
// sandbox may reach at all.
type ConnectivityMode string

const (
	// ConnectivityNone — no external egress (default; deny-by-default).
	ConnectivityNone ConnectivityMode = "none"
	// ConnectivityWhitelist — egress only to the listed host patterns.
	ConnectivityWhitelist ConnectivityMode = "whitelist"
	// ConnectivityOpen — egress to any host (the egress gateway + connection
	// registry still gate credentialed destinations).
	ConnectivityOpen ConnectivityMode = "open"
)

// Connectivity is an agent type's egress posture. Hosts is consulted only in
// whitelist mode and holds host patterns (exact, "*", or "*.suffix"), IPv4/DNS
// only.
type Connectivity struct {
	Mode  ConnectivityMode `json:"mode"`
	Hosts []string         `json:"hosts,omitempty"`
}

// Validate checks the posture is well-formed. Whitelist hosts must be IPv4/DNS
// host patterns (exact, "*", or "*.suffix") with no whitespace, ":" (an IPv6
// literal or port), or "/" — mirroring the egress scope rules the policy
// service enforces, so a bad host is rejected at config time, not silently at
// grant-reconcile time.
func (c Connectivity) Validate() error {
	switch c.Mode {
	case ConnectivityNone, ConnectivityOpen, "":
		return nil
	case ConnectivityWhitelist:
		for _, h := range c.Hosts {
			if strings.TrimSpace(h) == "" {
				return fmt.Errorf("connectivity whitelist host must not be empty")
			}
			if strings.ContainsAny(h, " \t:/") {
				return fmt.Errorf("connectivity host %q is invalid: IPv4/DNS hostnames only (no spaces, ':' or '/')", h)
			}
		}
		return nil
	default:
		return fmt.Errorf("connectivity mode %q is invalid: want none, whitelist, or open", c.Mode)
	}
}

// EgressScopes compiles the posture into the egress scope set granted to the
// agent principal:
//
//	none       → (nothing)
//	open       → ["egress:connect"]   (the 2-part form the rego reads as any host)
//	whitelist  → ["egress:<host>:connect", …] for each non-empty host
func (c Connectivity) EgressScopes() []string {
	switch c.Mode {
	case ConnectivityOpen:
		return []string{"egress:connect"}
	case ConnectivityWhitelist:
		scopes := make([]string, 0, len(c.Hosts))
		for _, h := range c.Hosts {
			if h = strings.TrimSpace(h); h != "" {
				scopes = append(scopes, "egress:"+h+":connect")
			}
		}
		return scopes
	default: // none or unset
		return nil
	}
}

// Connector attaches a registered credential connection to an agent type and the
// actions the agent may invoke through it. It is the credential parallel to
// Connectivity: where Connectivity compiles hosts to egress:<host>:connect, a
// Connector compiles to connection:<connection>:<action> grants. The connection
// itself — its adapter, secret, and scope_ceiling — is registered once at the
// tenant level on the egress gateway; a Connector records which agents may use it
// and for which actions.
type Connector struct {
	Connection string   `json:"connection"` // registered connection name, e.g. "github"
	Actions    []string `json:"actions"`    // adapter actions, e.g. ["repo:read","repo:write"]
}

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

// AgentType defines a category of managed agent within a tenant.
// Each active AgentType has a corresponding StatefulSet in the tenant namespace.
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
	Connectivity            Connectivity   `json:"connectivity"`
	Connectors              []Connector    `json:"connectors,omitempty"`
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

// ConnectorScopes compiles the attached connectors into the connection scope set
// granted to the agent principal: connection:<connection>:<action> for each
// action. Empty connection names or actions are skipped. Action strings keep
// their own ':' (e.g. "repo:read") so the full scope is a 4-segment
// connection:<name>:<resource>:<verb>.
func (at AgentType) ConnectorScopes() []string {
	scopes := make([]string, 0, len(at.Connectors))
	for _, c := range at.Connectors {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			continue
		}
		for _, a := range c.Actions {
			if a = strings.TrimSpace(a); a != "" {
				scopes = append(scopes, "connection:"+name+":"+a)
			}
		}
	}
	return scopes
}

// ValidateConnectors checks each connector names a connection and lists at least
// one well-formed action. Connection names are single scope segments (no
// whitespace, ':' or '/'); actions may contain ':' (e.g. "repo:read") but not
// whitespace or '/'. Mirrors Connectivity.Validate so a bad connector is
// rejected at config time, not silently at grant-reconcile time.
func ValidateConnectors(connectors []Connector) error {
	for _, c := range connectors {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			return fmt.Errorf("connector connection name must not be empty")
		}
		if strings.ContainsAny(name, " \t:/") {
			return fmt.Errorf("connector connection %q is invalid: no spaces, ':' or '/'", name)
		}
		if len(c.Actions) == 0 {
			return fmt.Errorf("connector %q must list at least one action", name)
		}
		for _, a := range c.Actions {
			if strings.TrimSpace(a) == "" {
				return fmt.Errorf("connector %q has an empty action", name)
			}
			if strings.ContainsAny(a, " \t/") {
				return fmt.Errorf("connector %q action %q is invalid: no spaces or '/'", name, a)
			}
		}
	}
	return nil
}

// EffectiveGrantScopes is the full scope set reconciled onto the agent's policy
// grant: its capability GrantScopes plus the egress scopes compiled from its
// connectivity posture, de-duplicated. This is what ensureAgentGrant enforces,
// so egress posture is durable — it survives every re-provision rather than
// being wiped like a hand-added grant.
func (at AgentType) EffectiveGrantScopes() []string {
	out := make([]string, 0, len(at.GrantScopes)+len(at.Connectivity.Hosts)+len(at.Connectors)+1)
	seen := make(map[string]struct{})
	for _, s := range at.GrantScopes {
		if _, ok := seen[s]; !ok {
			seen[s], out = struct{}{}, append(out, s)
		}
	}
	for _, s := range at.Connectivity.EgressScopes() {
		if _, ok := seen[s]; !ok {
			seen[s], out = struct{}{}, append(out, s)
		}
	}
	for _, s := range at.ConnectorScopes() {
		if _, ok := seen[s]; !ok {
			seen[s], out = struct{}{}, append(out, s)
		}
	}
	return out
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
