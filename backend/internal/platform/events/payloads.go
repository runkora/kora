package events

import (
	"slices"
	"time"
)

// Payload structs are the public contract of data.entity, one per entity kind
// (docs/adr/2026-11-06-event-spine.md, section 14). Producers map their models
// into them. The json, doc and enum tags drive the published schema, and an
// enum tag names a value set in enums.

// Run error codes a run_error records, the enum of RunPayload.Error.
const (
	RunErrorAgentError     = "agent_error"
	RunErrorEmptyResponse  = "empty_response"
	RunErrorRunnerError    = "runner_error"
	RunErrorSandboxLost    = "sandbox_lost"
	RunErrorLoopDetected   = "loop_detected"
	RunErrorEmptyTurn      = "empty_turn"
	RunErrorSubprocessExit = "subprocess_exit"
	RunErrorBudgetExhaust  = "budget_exhausted"
	RunErrorStartFailed    = "start_failed"
	// RunErrorUnknown stands for a code outside this list.
	RunErrorUnknown = "unknown"
)

// Loop guard kinds, the enum of GuardrailFinding.Kind.
const (
	GuardrailRepeatedCall = "repeated_call"
	GuardrailWaitRearm    = "wait_rearm"
	// GuardrailUnknown stands for a kind outside this list.
	GuardrailUnknown = "unknown"
)

// enums are the value sets an enum tag names.
var enums = map[string][]string{
	"run_state": {"pending", "starting", "active", "idle", "completing", "completed", "failed", "budget_exhausted", "terminated"},
	"run_error": {RunErrorAgentError, RunErrorEmptyResponse, RunErrorRunnerError, RunErrorSandboxLost, RunErrorLoopDetected,
		RunErrorEmptyTurn, RunErrorSubprocessExit, RunErrorBudgetExhaust, RunErrorStartFailed, RunErrorUnknown},
	"guardrail_kind":        {GuardrailRepeatedCall, GuardrailWaitRearm, GuardrailUnknown},
	"sandbox_egress":        {"passed", "skipped"},
	"agent_state":           {"provisioning", "active", "deleting", "deleted"},
	"identity_mode":         {"autonomous", "delegated"},
	"trigger_kind":          {"webhook", "scheduled", "manual", "event"},
	"target_kind":           {"agent", "routed"},
	"overflow":              {"wait", "replace", "refuse"},
	"resource_source":       {"api", "crd"},
	"delivery_status":       {"failed", "denied"},
	"delivery_reason":       {"forbidden", "agent_not_found", "run_not_found", "run_not_accepting_messages", "agents_unavailable", "timeout", "dispatch_failed"},
	"budget_scope":          {"tenant", "principal"},
	"budget_level":          {"50", "80", "100"},
	"token_principal_type":  {"HUMAN", "AGENT", "SERVICE", "SYSTEM", "PAT"},
	"identity_source":       {"FEDERATED", "ATTESTED", "PLATFORM"},
	"switch_direction":      {"in", "out"},
	"grant_principal_type":  {"AGENT", "HUMAN", "SERVICE", "PAT"},
	"declared_outcome":      {"NameConflict", "Invalid", "Pending"},
	"event_principal_type":  {PrincipalUser, PrincipalAgent, PrincipalTrigger, PrincipalSystem},
	"cloudevents_version":   {"1.0"},
	"datacontenttype_value": {"application/json"},
}

// Enum returns the values of the named set.
func Enum(name string) []string { return slices.Clone(enums[name]) }

// RunPayload is a run after the change.
type RunPayload struct {
	ID              string            `json:"id" doc:"The run id."`
	AgentID         string            `json:"agent_id" doc:"The id of the run's agent."`
	Agent           string            `json:"agent" doc:"The slug of the run's agent."`
	State           string            `json:"state" doc:"The run state after the change." enum:"run_state"`
	CreatedBy       string            `json:"created_by" doc:"The principal that created the run."`
	TriggerID       string            `json:"trigger_id,omitempty" doc:"The trigger that started the run."`
	DeliveryID      string            `json:"delivery_id,omitempty" doc:"The delivery that started the run."`
	EnvironmentID   string            `json:"environment_id,omitempty" doc:"The environment the run's sandbox uses."`
	StartedAt       *time.Time        `json:"started_at,omitempty" doc:"When the run started."`
	CompletedAt     *time.Time        `json:"completed_at,omitempty" doc:"When the run ended."`
	DeletedAt       *time.Time        `json:"deleted_at,omitempty" doc:"When the run was terminated."`
	Error           string            `json:"error,omitempty" doc:"The error code of an errored turn, or of a failed run's last run error." enum:"run_error"`
	Guardrail       *GuardrailFinding `json:"guardrail,omitempty" doc:"The loop guard finding of a warning."`
	SenderTriggerID string            `json:"sender_trigger_id,omitempty" doc:"The trigger that sent the run a message."`
}

// GuardrailFinding is a loop guard warning.
type GuardrailFinding struct {
	Kind  string `json:"kind" doc:"The loop the guard detected." enum:"guardrail_kind"`
	Count int    `json:"count" doc:"How many times the agent repeated the call."`
	Tool  string `json:"tool" doc:"The tool the agent repeated."`
}

// SandboxPayload is a run's sandbox.
type SandboxPayload struct {
	RunID       string `json:"run_id" doc:"The run the sandbox belongs to."`
	Environment string `json:"environment,omitempty" doc:"The environment the sandbox runs in."`
	Image       string `json:"image,omitempty" doc:"The sandbox image."`
	Egress      string `json:"egress,omitempty" doc:"Whether the egress check passed or was skipped." enum:"sandbox_egress"`
}

// AgentPayload is an agent after the change, or before it for a delete.
type AgentPayload struct {
	ID             string   `json:"id" doc:"The agent id."`
	Slug           string   `json:"slug" doc:"The agent slug."`
	Name           string   `json:"name" doc:"The agent name."`
	ModelID        *string  `json:"model_id,omitempty" doc:"The agent's model."`
	State          string   `json:"state" doc:"The agent's provisioning state." enum:"agent_state"`
	IdentityMode   string   `json:"identity_mode" doc:"Whether the agent acts for itself or for a person." enum:"identity_mode"`
	EnvironmentIDs []string `json:"environment_ids" doc:"The environments the agent may run in."`
	Connections    []string `json:"connections" doc:"The connections the agent uses."`
}

// EnvironmentPayload is an environment after the change, or before it for a delete.
type EnvironmentPayload struct {
	ID           string          `json:"id" doc:"The environment id."`
	Slug         string          `json:"slug" doc:"The environment slug."`
	Name         string          `json:"name" doc:"The environment name."`
	SandboxImage string          `json:"sandbox_image" doc:"The sandbox image the environment runs."`
	Size         EnvironmentSize `json:"size" doc:"The sandbox resources."`
	Connections  []string        `json:"connections" doc:"The connections the environment grants."`
}

// EnvironmentSize is a sandbox's resources.
type EnvironmentSize struct {
	CPU    string `json:"cpu" doc:"The CPU request."`
	Memory string `json:"memory" doc:"The memory request."`
	Disk   string `json:"disk" doc:"The disk size."`
}

// TriggerPayload is a trigger after the change, or before it for a delete.
type TriggerPayload struct {
	ID               string           `json:"id" doc:"The trigger id."`
	Name             string           `json:"name" doc:"The trigger name."`
	Kind             string           `json:"kind" doc:"What starts the trigger." enum:"trigger_kind"`
	TargetKind       string           `json:"target_kind" doc:"Whether the trigger runs one agent or chooses one." enum:"target_kind"`
	SourceID         string           `json:"source_id,omitempty" doc:"The webhook source of a webhook trigger."`
	AgentTypeID      string           `json:"agent_type_id,omitempty" doc:"The agent the trigger runs."`
	AgentSlug        string           `json:"agent_slug,omitempty" doc:"The slug of the agent the trigger runs."`
	EventType        string           `json:"event_type,omitempty" doc:"The event type the trigger listens for."`
	IgnoreOwnRuns    *bool            `json:"ignore_own_runs,omitempty" doc:"Whether an event trigger skips events about its own runs."`
	Schedule         *TriggerSchedule `json:"schedule_config,omitempty" doc:"The schedule of a scheduled trigger."`
	GrantID          *string          `json:"grant_id,omitempty" doc:"The trigger's grant."`
	Enabled          bool             `json:"enabled" doc:"Whether the trigger fires."`
	AutoPausedAt     *time.Time       `json:"auto_paused_at,omitempty" doc:"When the trigger paused itself."`
	MaxParallel      int16            `json:"max_parallel" doc:"How many runs the trigger keeps at once."`
	Overflow         string           `json:"overflow" doc:"What a fire does when the trigger is at its limit." enum:"overflow"`
	MaxQueueDepth    int16            `json:"max_queue_depth" doc:"How many fires may wait."`
	TimeoutSeconds   int16            `json:"timeout_seconds" doc:"How long a start may take."`
	DedupeEnabled    bool             `json:"dedupe_enabled" doc:"Whether a repeated delivery is dropped."`
	AutopauseEnabled bool             `json:"autopause_enabled" doc:"Whether the trigger pauses after repeated failures."`
	Source           string           `json:"source" doc:"Whether the trigger was made through the API or declared." enum:"resource_source"`
	Repository       string           `json:"repository,omitempty" doc:"The repository that declares the trigger."`
}

// TriggerSchedule is a scheduled trigger's cron schedule.
type TriggerSchedule struct {
	CronExpression string `json:"cron_expression" doc:"The cron expression."`
	Timezone       string `json:"timezone" doc:"The schedule's time zone."`
	CatchUp        bool   `json:"catch_up" doc:"Whether a missed fire runs late."`
}

// SourcePayload is a webhook source after the change, or before it for a delete.
type SourcePayload struct {
	ID              string     `json:"id" doc:"The source id."`
	Name            string     `json:"name" doc:"The source name."`
	Provider        string     `json:"provider" doc:"The webhook provider."`
	GrantID         *string    `json:"grant_id,omitempty" doc:"The source's grant."`
	Enabled         bool       `json:"enabled" doc:"Whether the source accepts deliveries."`
	SecretRotatedAt *time.Time `json:"secret_rotated_at,omitempty" doc:"When the signing secret last changed."`
	Source          string     `json:"source" doc:"Whether the source was made through the API or declared." enum:"resource_source"`
	Repository      string     `json:"repository,omitempty" doc:"The repository that declares the source."`
}

// DeliveryPayload is a fire that could not start its run.
type DeliveryPayload struct {
	ID          string `json:"id" doc:"The outcome id."`
	DeliveryID  string `json:"delivery_id" doc:"The delivery."`
	TriggerID   string `json:"trigger_id" doc:"The trigger that fired."`
	TriggerName string `json:"trigger_name,omitempty" doc:"The trigger name."`
	Status      string `json:"status" doc:"The outcome." enum:"delivery_status"`
	Reason      string `json:"reason" doc:"Why the run did not start." enum:"delivery_reason"`
}

// BudgetPayload is a budget's spend at a threshold or a refusal.
type BudgetPayload struct {
	Scope       string    `json:"scope" doc:"Whether the budget is the tenant's or a principal's." enum:"budget_scope"`
	ID          string    `json:"id" doc:"The principal of a principal budget."`
	Window      string    `json:"window" doc:"The budget period."`
	PeriodStart time.Time `json:"period_start" doc:"When the budget period started."`
	LimitUSD    float64   `json:"limit_usd" doc:"The budget limit in US dollars."`
	SpendUSD    float64   `json:"spend_usd" doc:"The spend in the period in US dollars."`
	Level       int       `json:"level,omitempty" doc:"The percentage of the limit that spend crossed." enum:"budget_level"`
}

// SessionPayload is a session that started.
type SessionPayload struct {
	ID             string    `json:"id" doc:"The session id."`
	PrincipalID    string    `json:"principal_id" doc:"The principal that signed in."`
	PrincipalType  string    `json:"principal_type" doc:"The principal's type." enum:"token_principal_type"`
	IdentitySource string    `json:"identity_source" doc:"Where the principal's identity comes from." enum:"identity_source"`
	CreatedAt      time.Time `json:"created_at" doc:"When the session started."`
	ExpiresAt      time.Time `json:"expires_at" doc:"When the session expires."`
}

// TenantSwitchPayload is one side of a tenant switch.
type TenantSwitchPayload struct {
	SessionID   string `json:"session_id" doc:"The session that switched."`
	PrincipalID string `json:"principal_id" doc:"The person who switched."`
	Direction   string `json:"direction" doc:"Whether the person entered or left this tenant." enum:"switch_direction"`
}

// PATPayload is a personal access token after the change.
type PATPayload struct {
	ID        string     `json:"id" doc:"The token id."`
	UserID    string     `json:"user_id" doc:"The token's owner."`
	Name      string     `json:"name" doc:"The token name."`
	Scopes    []string   `json:"scopes" doc:"The token's scopes."`
	GrantID   *string    `json:"grant_id,omitempty" doc:"The token's grant."`
	ExpiresAt time.Time  `json:"expires_at" doc:"When the token expires."`
	CreatedAt time.Time  `json:"created_at" doc:"When the token was issued."`
	RevokedAt *time.Time `json:"revoked_at,omitempty" doc:"When the token was revoked."`
}

// DelegationPayload is a delegation a person issued to an agent.
type DelegationPayload struct {
	RunID                  string    `json:"run_id" doc:"The run the delegation is bound to."`
	AgentPrincipalID       string    `json:"agent_principal_id" doc:"The agent that acts for the person."`
	DelegatorPrincipalID   string    `json:"delegator_principal_id" doc:"The person who delegated."`
	DelegatorPrincipalType string    `json:"delegator_principal_type" doc:"The delegator's type." enum:"token_principal_type"`
	CreatedAt              time.Time `json:"created_at" doc:"When the delegation was issued."`
}

// GrantPayload is a grant after the change, or before it for a delete.
type GrantPayload struct {
	ID                 string       `json:"id" doc:"The grant id."`
	PrincipalType      string       `json:"principal_type" doc:"The type of the principal the grant is for." enum:"grant_principal_type"`
	PrincipalID        string       `json:"principal_id" doc:"The principal the grant is for."`
	Scopes             []string     `json:"scopes" doc:"The scopes the grant holds."`
	Budget             *GrantBudget `json:"budget,omitempty" doc:"The grant's budget."`
	DelegationRequired bool         `json:"delegation_required" doc:"Whether the grant applies only under a delegation."`
	CreatedAt          time.Time    `json:"created_at" doc:"When the grant was created."`
}

// GrantBudget is a grant's budget.
type GrantBudget struct {
	LimitUSD       float64 `json:"limit_usd,omitempty" doc:"The limit in US dollars."`
	ResetCadence   string  `json:"reset_cadence" doc:"How often the budget resets."`
	PerRunLimitUSD float64 `json:"per_run_limit_usd,omitempty" doc:"The limit of one run in US dollars."`
}

// RolePayload is a role after the change, or before it for a delete.
type RolePayload struct {
	ID         string      `json:"id" doc:"The role id."`
	Role       string      `json:"role" doc:"The role name."`
	Scopes     []string    `json:"scopes" doc:"The scopes the role holds."`
	Budget     *RoleBudget `json:"budget,omitempty" doc:"The role's budget."`
	CreatedAt  time.Time   `json:"created_at" doc:"When the role was created."`
	Source     string      `json:"source" doc:"Whether the role was made through the API or declared." enum:"resource_source"`
	Repository string      `json:"repository,omitempty" doc:"The repository that declares the role."`
}

// RoleBudget is a role's budget.
type RoleBudget struct {
	LimitUSD     float64 `json:"limit_usd" doc:"The limit in US dollars."`
	ResetCadence string  `json:"reset_cadence" doc:"How often the budget resets."`
	Pooled       bool    `json:"pooled,omitempty" doc:"Whether the role's members share one budget."`
}

// TenantBudgetPayload is the tenant budget after the change, or before it for a delete.
type TenantBudgetPayload struct {
	LimitUSD     float64 `json:"limit_usd" doc:"The limit in US dollars."`
	ResetCadence string  `json:"reset_cadence" doc:"How often the budget resets."`
	Source       string  `json:"source" doc:"Whether the budget was set through the API or declared." enum:"resource_source"`
}

// DeclaredResourcePayload is a declared resource the platform could not apply.
type DeclaredResourcePayload struct {
	Kind       string `json:"kind" doc:"The declared resource kind."`
	Namespace  string `json:"namespace" doc:"The namespace of the declared resource."`
	Name       string `json:"name" doc:"The declared resource name."`
	Generation int64  `json:"generation" doc:"The generation that failed."`
	Outcome    string `json:"outcome" doc:"Why the resource was not applied." enum:"declared_outcome"`
}
