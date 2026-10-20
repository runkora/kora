package agents

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RunOutcomes counts the runs that finished in a window.
type RunOutcomes struct {
	Completed int64 `json:"completed"`
	Failed    int64 `json:"failed"`
}

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

// IdentityMode is an agent type's identity model — who the agent acts as.
// Autonomous agents act on their own provisioned grant; delegated agents act on
// behalf of the person who runs them, bounded by the intersection of the agent's
// grant and the delegator's live access. Set at creation and immutable
// thereafter (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 1).
type IdentityMode string

const (
	// IdentityModeAutonomous — the agent acts as itself, on its own grant (default).
	IdentityModeAutonomous IdentityMode = "autonomous"
	// IdentityModeDelegated — the agent acts on behalf of the person who runs it;
	// its grant is a ceiling, not an entitlement, and every action requires valid
	// delegation context (fail-closed).
	IdentityModeDelegated IdentityMode = "delegated"
)

// Validate checks the identity mode is a known value. Empty is accepted and
// normalised to autonomous at create time.
func (m IdentityMode) Validate() error {
	switch m {
	case IdentityModeAutonomous, IdentityModeDelegated, "":
		return nil
	default:
		return fmt.Errorf("identity mode %q is invalid: want autonomous or delegated", m)
	}
}

// OrDefault returns the mode with empty normalised to autonomous.
func (m IdentityMode) OrDefault() IdentityMode {
	if m == "" {
		return IdentityModeAutonomous
	}
	return m
}

// IsDelegated reports whether runs of this type execute with a person's
// authority rather than the agent's own.
//
// A named predicate rather than the comparison spelled out at each site: it
// decides who may start a run, whether a delegation is minted, and what the
// sandbox is allowed to do, and an empty mode has to normalise to autonomous
// every time. One spelling that cannot drift.
func (t AgentType) IsDelegated() bool {
	return t.IdentityMode.OrDefault() == IdentityModeDelegated
}

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
			// Lowercase to match: the egress gateway / OPA normalize the CONNECT
			// host to lowercase before comparing, so a scope built from a
			// mixed-case whitelist entry (e.g. "GitHub.com") would never match and
			// egress would be silently denied.
			if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
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
	Actions    []string `json:"actions"`    // adapter actions, e.g. ["contents:read","issues:write"]
	// Resources optionally narrows the attachment to specific upstream
	// resources (repositories, namespaces — the adapter's qualifier
	// dimension). Empty means all resources: the compiled scopes stay
	// unqualified, which the covers hierarchy makes the parent of every
	// qualified form. See docs/adr/2026-07-22-connection-scope-qualifiers.md.
	Resources []string `json:"resources,omitempty"`
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

// AgentEnvironment attaches an environment to an agent.
type AgentEnvironment struct {
	EnvironmentID string `json:"environment_id"`
	IsDefault     bool   `json:"is_default"`
}

// AgentType defines a category of managed agent within a tenant.
// Each active AgentType has a corresponding StatefulSet in the tenant namespace.
type AgentType struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Name         string         `json:"name"`
	Slug         string         `json:"slug"`
	Description  string         `json:"description"`
	SystemPrompt string         `json:"system_prompt"`
	Framework    AgentFramework `json:"framework"`
	// IdentityMode is the agent's identity model (autonomous or delegated).
	// Immutable after creation — see docs/adr/2026-07-23-delegated-agent-identity.md.
	IdentityMode IdentityMode `json:"identity_mode"`
	GrantScopes  []string     `json:"grant_scopes"`
	Connectivity Connectivity `json:"connectivity"`
	Connectors   []Connector  `json:"connectors,omitempty"`
	// MCPTools are the governed MCP tools this agent may call through the
	// tool-gateway, as mcp:<server>:<tool>:use scopes. A first-class field the
	// backend composes into EffectiveGrantScopes (like Connectors); the coarse
	// mcp:invoke gate is derived there, not stored.
	MCPTools []string `json:"mcp_tools,omitempty"`
	// Environments are the sandboxes this agent may run in, one of them the default; none means kora-base.
	// Their egress and connectors join the agent's grant.
	Environments            []AgentEnvironment `json:"environments"`
	MaxConcurrentRunsPerPod int                `json:"max_concurrent_runs_per_pod"`
	// Guardrails are the loop-guard thresholds; unset fields take the platform defaults.
	Guardrails Guardrails `json:"guardrails"`
	// CreatedBy is the token subject that created the type; the owner check in Authz reads it.
	CreatedBy string         `json:"created_by,omitempty"`
	State     AgentTypeState `json:"state"`
	// ActivatedAt is when the type last became active; zero if it never has. The run start budget runs from it.
	ActivatedAt time.Time `json:"activated_at,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ModelID     *string   `json:"model_id,omitempty"`
	// Budget mirrors the budget on the agent's policy grant. Transient: never
	// written to the agent_types table; populated on detail reads and carried
	// as the desired state during grant reconciliation.
	Budget *AgentBudget `json:"budget,omitempty"`
	// CanRun and CanEdit are the caller's own permissions on this agent, set per
	// request on reads: CanRun by the run-start check, CanEdit by the mode write
	// check. Triggers ask CanRun for a caller before binding. Absent means not
	// computed: write responses leave them unset. Never stored.
	CanRun  *bool `json:"can_run,omitempty"`
	CanEdit *bool `json:"can_edit,omitempty"`
	// CanDelete is set like CanEdit but by the delete check: mode write, and no
	// delegated caller on an autonomous agent. The scope ceiling does not apply.
	CanDelete *bool `json:"can_delete,omitempty"`
}

// ConnectorScopes compiles the attached connectors into the connection scope set
// granted to the agent principal. An unrestricted connector compiles each action
// to connection:<connection>:<action> (the action fragment keeps its own ':',
// e.g. "contents:read", so the scope is connection:<name>:<category>:<verb>).
// A connector with Resources compiles the qualifier into each category action —
// connection:<name>:<category>:<resource>:<verb> — one scope per selected
// resource. Single-word actions ("use") take no qualifier and stay unqualified.
// Empty connection names or actions are skipped.
func (at AgentType) ConnectorScopes() []string {
	scopes := make([]string, 0, len(at.Connectors))
	for _, c := range at.Connectors {
		name := strings.TrimSpace(c.Connection)
		if name == "" {
			continue
		}
		resources := make([]string, 0, len(c.Resources))
		for _, r := range c.Resources {
			if r = strings.ToLower(strings.TrimSpace(r)); r != "" {
				resources = append(resources, r)
			}
		}
		for _, a := range c.Actions {
			if a = strings.TrimSpace(a); a == "" {
				continue
			}
			i := strings.LastIndex(a, ":")
			if len(resources) == 0 || i <= 0 {
				scopes = append(scopes, "connection:"+name+":"+a)
				continue
			}
			category, verb := a[:i], a[i+1:]
			for _, r := range resources {
				scopes = append(scopes, "connection:"+name+":"+category+":"+r+":"+verb)
			}
		}
	}
	return scopes
}

// deriveInferenceScopes returns the inference grant scopes for a model id:
// inference:invoke, plus the fine-grained inference:{family}:{name}:use when the id
// is a two-segment family/name (e.g. anthropic/claude-sonnet-4-6). This is the
// single source of truth for inference grant derivation — clients (orbit-mcp, the
// frontend BFF) declare model_id and do NOT derive. See the 2026-06-30 amendment to
// docs/adr/2026-05-11-agent-identity-and-policy-provisioning.md.
func deriveInferenceScopes(modelID string) []string {
	scopes := []string{"inference:invoke"}
	if parts := strings.Split(modelID, "/"); len(parts) == 2 && parts[0] != "" && parts[1] != "" {
		scopes = append(scopes, "inference:"+parts[0]+":"+parts[1]+":use")
	}
	return scopes
}

// hasInferenceScope reports whether any scope is an inference:* scope. An explicit
// inference scope on a create/update suppresses auto-derivation (caller override).
func hasInferenceScope(scopes []string) bool {
	for _, s := range scopes {
		if strings.HasPrefix(s, "inference:") {
			return true
		}
	}
	return false
}

// applyDerivedInferenceScopes appends the inference scopes derived from modelID to
// grantScopes, unless the caller already supplied an explicit inference:* scope
// (which is taken as an override and suppresses derivation).
func applyDerivedInferenceScopes(grantScopes []string, modelID *string) []string {
	if hasInferenceScope(grantScopes) {
		return grantScopes
	}
	model := ""
	if modelID != nil {
		model = *modelID
	}
	return append(grantScopes, deriveInferenceScopes(model)...)
}

// reconcileInferenceScopes strips any inference:* scopes from grantScopes and
// re-derives them from modelID, so the inference grant always matches the current
// model after a model change. Non-inference scopes are preserved in order.
func reconcileInferenceScopes(grantScopes []string, modelID *string) []string {
	kept := make([]string, 0, len(grantScopes))
	for _, s := range grantScopes {
		if !strings.HasPrefix(s, "inference:") {
			kept = append(kept, s)
		}
	}
	model := ""
	if modelID != nil {
		model = *modelID
	}
	return append(kept, deriveInferenceScopes(model)...)
}

// mcpInvokeScope is the coarse gate a caller needs to reach the MCP tool-gateway
// at all; the per-tool decision is enforced there. Mirrors toolgateway.mcpInvokeScope.
const mcpInvokeScope = "mcp:invoke"

// MCPToolScopes compiles the agent's governed MCP tool grants into policy scopes:
// the granted mcp:<server>:<tool>:use tools plus the coarse mcp:invoke gate, so a
// granted agent both clears the gateway's coarse gate and is admitted per tool.
// Empty when nothing is granted. The parallel to ConnectorScopes / EgressScopes —
// composed into EffectiveGrantScopes, so the gate can never drift from the tools.
func (at AgentType) MCPToolScopes() []string {
	if len(at.MCPTools) == 0 {
		return nil
	}
	out := make([]string, 0, len(at.MCPTools)+1)
	out = append(out, at.MCPTools...)
	out = append(out, mcpInvokeScope)
	return out
}

// MCPServers returns the distinct MCP servers this agent has tool grants on,
// derived from MCPTools (mcp:<server>:<...>:use), preserving first-seen order.
// The gateway mounts one endpoint per server (POST /v1/mcp/<server>), so the run
// gets one governed MCP config entry per server. Empty when nothing is granted.
func (at AgentType) MCPServers() []string {
	seen := make(map[string]bool)
	out := make([]string, 0)
	for _, t := range at.MCPTools {
		parts := strings.Split(t, ":")
		if len(parts) < 2 || parts[0] != "mcp" || parts[1] == "" {
			continue
		}
		if server := parts[1]; !seen[server] {
			seen[server] = true
			out = append(out, server)
		}
	}
	return out
}

// ValidateConnectors checks each connector names a connection, lists at least
// one well-formed action, and names resources that can be a qualifier segment.
// Connection names are single scope segments (no whitespace, ':' or '/');
// actions may contain ':' (e.g. "repo:read") but not whitespace or '/';
// resources are a single segment. Mirrors Connectivity.Validate so a bad
// connector is rejected at config time, not silently at grant-reconcile time.
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
		for _, r := range c.Resources {
			res := strings.TrimSpace(r)
			if res == "" {
				return fmt.Errorf("connector %q has an empty resource", name)
			}
			// A resource becomes the qualifier segment of
			// connection:<name>:<category>:<resource>:<verb>, one segment
			// (docs/adr/2026-07-22-connection-scope-qualifiers.md). Case is not
			// checked: ConnectorScopes lowercases the qualifier before it
			// reaches the grant.
			if strings.ContainsAny(res, " \t:/*") {
				return fmt.Errorf("connector %q resource %q is invalid: must be a single segment, no spaces, ':', '/' or '*'", name, r)
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
	for _, s := range at.MCPToolScopes() {
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

// AllRunStates lists every declared RunState. Pinned by a unit test so a new
// state cannot be added without OccupiesPod answering for it.
func AllRunStates() []RunState {
	return []RunState{
		RunStatePending,
		RunStateStarting,
		RunStateActive,
		RunStateIdle,
		RunStateCompleting,
		RunStateCompleted,
		RunStateFailed,
		RunStateBudgetExhausted,
	}
}

// OccupiesPod reports whether a run in this state still holds a slot on its
// assigned pod, and so counts towards MaxConcurrentRunsPerPod when a new run
// is placed.
//
// A state occupies a pod whenever a session process may still be resident:
// pending and starting are on their way in, active is mid-turn, idle has
// finished a turn but keeps the session alive waiting for the next message,
// and completing is snapshotting. The terminal states have released their
// resources by then (RunService.transition deprovisions the sandbox on
// completed, failed and budget_exhausted), so none of them occupies a slot;
// budget_exhausted in particular is terminal rather than a paused run that
// might resume.
//
// known is false for a value that is not a declared RunState, so a caller can
// tell "does not occupy" apart from "not a state at all".
func (s RunState) OccupiesPod() (occupies, known bool) {
	switch s {
	case RunStatePending, RunStateStarting, RunStateActive, RunStateIdle, RunStateCompleting:
		return true, true
	case RunStateCompleted, RunStateFailed, RunStateBudgetExhausted:
		return false, true
	default:
		return false, false
	}
}

// PodOccupyingRunStates returns the states for which OccupiesPod reports true,
// in declaration order. It is the source of truth for both the SQL predicate
// that counts runs per pod and the matching partial index on runs.
func PodOccupyingRunStates() []RunState {
	all := AllRunStates()
	out := make([]RunState, 0, len(all))
	for _, s := range all {
		if occupies, _ := s.OccupiesPod(); occupies {
			out = append(out, s)
		}
	}
	return out
}

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
	// Summary is a short LLM-generated sentence describing the run's task
	// intent, derived from the initial message at run start. Null when the run
	// had no initial message or when generation failed (best-effort).
	Summary *string `json:"summary"`
	// DelegationJTI identifies the delegation issuance minted for this run at
	// start when the agent type is delegated (docs/adr/2026-07-23-delegated-agent-identity.md,
	// Decision 3). Nil for autonomous runs. orbit-agents revokes the issuance
	// by this JTI when the run reaches a terminal state.
	DelegationJTI *string `json:"delegation_jti,omitempty"`
	// IdentityMode is the agent type's identity model, denormalized onto run
	// reads (repository queries join agent_types) so every run surface can show
	// the governance posture — and attribute created_by accordingly — without a
	// second lookup. Not a runs column.
	IdentityMode IdentityMode `json:"identity_mode,omitempty"`
	// StartedBy names the trigger that started this run, when one did. Nil for a
	// run a person started, which is what its absence means on the wire.
	//
	// Without it a trigger-started run is attributable only to the trigger's
	// SERVICE principal, which reads as an opaque id: the run detail could not
	// answer "why did this happen?" at all. See the ADR on extended trigger
	// types, US-004.
	StartedBy *StartedByTrigger `json:"started_by,omitempty"`
	// EnvironmentID is the environment of the run's current sandbox. It is nil
	// for the built-in default and before the first sandbox.
	EnvironmentID *string `json:"environment_id,omitempty"`
	// InitialMessage is the run's first message. Only a claimed run carries it, and
	// the scheduling worker sends it when it creates the session.
	InitialMessage string `json:"-"`
	// Usage is the run's inference total so far. It is read from
	// inference_usage when the run is served and is not a runs column. A run
	// with no usage rows reads as zeros.
	Usage RunUsage `json:"usage"`
}

// RunUsage is the sum of a run's inference_usage rows. A live run reports its
// total so far.
type RunUsage struct {
	CostUSD      float64 `json:"cost_usd"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	RequestCount int64   `json:"request_count"`
}

// StartedByTrigger is the run's backlink to the trigger and delivery that
// started it.
//
// The name is a snapshot taken at start time, not a live lookup. A run is audit
// history and outlives the trigger that started it, so a deleted trigger must
// still read as something a person recognizes, and a later rename must not
// rewrite what this run says happened.
type StartedByTrigger struct {
	TriggerID   string `json:"trigger_id"`
	TriggerName string `json:"trigger_name,omitempty"`
	DeliveryID  string `json:"delivery_id,omitempty"`
}

// DeletedRun is one force-deleted run returned by DeleteRunsByAgentType: the
// ID (for sandbox pod cleanup) and the delegation JTI, when the run was
// delegated (for issuance revocation — the delegation ends with the run).
type DeletedRun struct {
	ID            string
	DelegationJTI *string
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
