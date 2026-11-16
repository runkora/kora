package events

import (
	"maps"
	"reflect"
	"slices"
	"strings"
)

// Type is a versioned event type of the form <domain>.<entity>.<event>.v<N>.
type Type string

// The v1 catalog (docs/prd/event-spine.md).
const (
	RunCreated         Type = "agents.run.created.v1"
	RunStarted         Type = "agents.run.started.v1"
	RunCompleted       Type = "agents.run.completed.v1"
	RunFailed          Type = "agents.run.failed.v1"
	RunBudgetExhausted Type = "agents.run.budget_exhausted.v1"
	RunTerminated      Type = "agents.run.terminated.v1"
	RunIdle            Type = "agents.run.idle.v1"
	RunInterrupted     Type = "agents.run.interrupted.v1"
	RunErrored         Type = "agents.run.errored.v1"
	RunGuardrailWarned Type = "agents.run.guardrail_warned.v1"
	RunGuardrailPaused Type = "agents.run.guardrail_paused.v1"
	RunMessageReceived Type = "agents.run.message_received.v1"

	SandboxCreated  Type = "agents.sandbox.created.v1"
	SandboxReplaced Type = "agents.sandbox.replaced.v1"
	SandboxReady    Type = "agents.sandbox.ready.v1"
	SandboxFailed   Type = "agents.sandbox.failed.v1"
	SandboxDeleted  Type = "agents.sandbox.deleted.v1"

	AgentCreated       Type = "agents.agent.created.v1"
	AgentUpdated       Type = "agents.agent.updated.v1"
	AgentDeleted       Type = "agents.agent.deleted.v1"
	EnvironmentCreated Type = "agents.environment.created.v1"
	EnvironmentUpdated Type = "agents.environment.updated.v1"
	EnvironmentDeleted Type = "agents.environment.deleted.v1"

	DeliveryFailed    Type = "triggers.delivery.failed.v1"
	DeliveryDenied    Type = "triggers.delivery.denied.v1"
	TriggerAutopaused Type = "triggers.trigger.autopaused.v1"
	TriggerCreated    Type = "triggers.trigger.created.v1"
	TriggerUpdated    Type = "triggers.trigger.updated.v1"
	TriggerDeleted    Type = "triggers.trigger.deleted.v1"
	SourceCreated     Type = "triggers.source.created.v1"
	SourceUpdated     Type = "triggers.source.updated.v1"
	SourceDeleted     Type = "triggers.source.deleted.v1"

	BudgetThresholdCrossed Type = "inference.budget.threshold_crossed.v1"
	BudgetExceeded         Type = "inference.budget.exceeded.v1"

	SessionCreated   Type = "identity.session.created.v1"
	PATCreated       Type = "identity.pat.created.v1"
	PATRevoked       Type = "identity.pat.revoked.v1"
	PATUpdated       Type = "identity.pat.updated.v1"
	DelegationIssued Type = "identity.delegation.issued.v1"
	TenantSwitched   Type = "identity.tenant.switched.v1"

	GrantCreated        Type = "policy.grant.created.v1"
	GrantUpdated        Type = "policy.grant.updated.v1"
	GrantDeleted        Type = "policy.grant.deleted.v1"
	RoleCreated         Type = "policy.role.created.v1"
	RoleUpdated         Type = "policy.role.updated.v1"
	RoleDeleted         Type = "policy.role.deleted.v1"
	TenantBudgetCreated Type = "policy.tenant_budget.created.v1"
	TenantBudgetUpdated Type = "policy.tenant_budget.updated.v1"
	TenantBudgetDeleted Type = "policy.tenant_budget.deleted.v1"

	DeclaredResourceFailed Type = "declarative.resource.failed.v1"
)

// entry is a catalog type's payload struct and its description.
type entry struct {
	payload     reflect.Type
	description string
}

func kind[T any]() reflect.Type { return reflect.TypeFor[T]() }

var catalog = map[Type]entry{
	RunCreated:         {kind[RunPayload](), "A run was created and queued."},
	RunStarted:         {kind[RunPayload](), "A run began executing."},
	RunCompleted:       {kind[RunPayload](), "A run finished, including a run that ended after its idle timeout."},
	RunFailed:          {kind[RunPayload](), "A run failed."},
	RunBudgetExhausted: {kind[RunPayload](), "A budget ran out and stopped a run."},
	RunTerminated:      {kind[RunPayload](), "A person or the platform terminated a run."},
	RunIdle:            {kind[RunPayload](), "A run finished its turn and is waiting for input."},
	RunInterrupted:     {kind[RunPayload](), "A person stopped the run's turn."},
	RunErrored:         {kind[RunPayload](), "A turn ended in an error and the run continued."},
	RunGuardrailWarned: {kind[RunPayload](), "The loop guard warned the agent."},
	RunGuardrailPaused: {kind[RunPayload](), "The loop guard paused the run."},
	RunMessageReceived: {kind[RunPayload](), "A person or a trigger sent the run a message."},

	SandboxCreated:  {kind[SandboxPayload](), "A sandbox was provisioned for a run."},
	SandboxReplaced: {kind[SandboxPayload](), "A run's sandbox was replaced with a different environment or image."},
	SandboxReady:    {kind[SandboxPayload](), "A sandbox finished provisioning."},
	SandboxFailed:   {kind[SandboxPayload](), "A sandbox failed to provision."},
	SandboxDeleted:  {kind[SandboxPayload](), "A run's sandbox was removed."},

	AgentCreated:       {kind[AgentPayload](), "An agent was created."},
	AgentUpdated:       {kind[AgentPayload](), "An agent was changed."},
	AgentDeleted:       {kind[AgentPayload](), "An agent was deleted."},
	EnvironmentCreated: {kind[EnvironmentPayload](), "An environment was created."},
	EnvironmentUpdated: {kind[EnvironmentPayload](), "An environment was changed."},
	EnvironmentDeleted: {kind[EnvironmentPayload](), "An environment was deleted."},

	DeliveryFailed:    {kind[DeliveryPayload](), "A fire could not start its run."},
	DeliveryDenied:    {kind[DeliveryPayload](), "A fire was refused for lack of permission."},
	TriggerAutopaused: {kind[TriggerPayload](), "A trigger paused itself after repeated failures."},
	TriggerCreated:    {kind[TriggerPayload](), "A trigger was created."},
	TriggerUpdated:    {kind[TriggerPayload](), "A trigger was changed."},
	TriggerDeleted:    {kind[TriggerPayload](), "A trigger was deleted."},
	SourceCreated:     {kind[SourcePayload](), "A webhook source was created."},
	SourceUpdated:     {kind[SourcePayload](), "A webhook source was changed."},
	SourceDeleted:     {kind[SourcePayload](), "A webhook source was deleted."},

	BudgetThresholdCrossed: {kind[BudgetPayload](), "Spend against a budget passed 50%, 80% or 100% of its limit."},
	BudgetExceeded:         {kind[BudgetPayload](), "An exhausted budget refused a request."},

	SessionCreated:   {kind[SessionPayload](), "A person signed in."},
	PATCreated:       {kind[PATPayload](), "A personal access token was issued."},
	PATRevoked:       {kind[PATPayload](), "A personal access token was revoked."},
	PATUpdated:       {kind[PATPayload](), "A personal access token's scopes changed."},
	DelegationIssued: {kind[DelegationPayload](), "A person delegated to an agent."},
	TenantSwitched:   {kind[TenantSwitchPayload](), "A person switched tenant."},

	GrantCreated:        {kind[GrantPayload](), "A grant was created."},
	GrantUpdated:        {kind[GrantPayload](), "A grant was changed."},
	GrantDeleted:        {kind[GrantPayload](), "A grant was deleted."},
	RoleCreated:         {kind[RolePayload](), "A role was created."},
	RoleUpdated:         {kind[RolePayload](), "A role was changed."},
	RoleDeleted:         {kind[RolePayload](), "A role was deleted."},
	TenantBudgetCreated: {kind[TenantBudgetPayload](), "The tenant budget was set."},
	TenantBudgetUpdated: {kind[TenantBudgetPayload](), "The tenant budget was changed."},
	TenantBudgetDeleted: {kind[TenantBudgetPayload](), "The tenant budget was removed."},

	DeclaredResourceFailed: {kind[DeclaredResourcePayload](), "A declared resource could not be applied."},
}

// hiddenChanges lists the fields an update can change that a payload does not
// carry. An event's changed list names them and never carries their values.
var hiddenChanges = map[reflect.Type][]string{
	kind[AgentPayload](): {"description", "system_prompt", "framework", "grant_scopes", "connectivity", "connectors",
		"mcp_tools", "guardrails", "max_concurrent_runs_per_pod", "repository"},
	kind[EnvironmentPayload](): {"description", "egress_hosts", "connectors", "repository"},
	kind[TriggerPayload](): {"match_condition", "match_contains", "prompt_template", "manual_config", "sender_restriction",
		"coalesce", "routing"},
	kind[SourcePayload](): {"config"},
}

// Types lists the catalog in name order.
func Types() []Type {
	return slices.Sorted(maps.Keys(catalog))
}

// Known reports whether t is in the catalog.
func (t Type) Known() bool { _, ok := catalog[t]; return ok }

// Description is what an event of type t records.
func (t Type) Description() string { return catalog[t].description }

// Category is the <domain>.<entity> prefix of t.
func (t Type) Category() string {
	parts := strings.SplitN(string(t), ".", 3)
	if len(parts) < 3 {
		return string(t)
	}
	return parts[0] + "." + parts[1]
}

// ZeroPayload is the zero value of type t's payload struct, nil for a type outside the catalog.
func ZeroPayload(t Type) any {
	e, ok := catalog[t]
	if !ok {
		return nil
	}
	return reflect.Zero(e.payload).Interface()
}
