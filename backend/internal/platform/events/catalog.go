package events

import (
	"maps"
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

var catalog = map[Type]bool{
	RunCreated: true, RunStarted: true, RunCompleted: true, RunFailed: true, RunBudgetExhausted: true,
	RunTerminated: true, RunIdle: true, RunInterrupted: true, RunErrored: true, RunGuardrailWarned: true,
	RunGuardrailPaused: true, RunMessageReceived: true,
	SandboxCreated: true, SandboxReplaced: true, SandboxReady: true, SandboxFailed: true, SandboxDeleted: true,
	AgentCreated: true, AgentUpdated: true, AgentDeleted: true,
	EnvironmentCreated: true, EnvironmentUpdated: true, EnvironmentDeleted: true,
	DeliveryFailed: true, DeliveryDenied: true, TriggerAutopaused: true,
	TriggerCreated: true, TriggerUpdated: true, TriggerDeleted: true,
	SourceCreated: true, SourceUpdated: true, SourceDeleted: true,
	BudgetThresholdCrossed: true, BudgetExceeded: true,
	SessionCreated: true, PATCreated: true, PATRevoked: true, PATUpdated: true,
	DelegationIssued: true, TenantSwitched: true,
	GrantCreated: true, GrantUpdated: true, GrantDeleted: true,
	RoleCreated: true, RoleUpdated: true, RoleDeleted: true,
	TenantBudgetCreated: true, TenantBudgetUpdated: true, TenantBudgetDeleted: true,
	DeclaredResourceFailed: true,
}

// Types lists the catalog in name order.
func Types() []Type {
	return slices.Sorted(maps.Keys(catalog))
}

// Known reports whether t is in the catalog.
func (t Type) Known() bool { return catalog[t] }

// Category is the <domain>.<entity> prefix of t.
func (t Type) Category() string {
	parts := strings.SplitN(string(t), ".", 3)
	if len(parts) < 3 {
		return string(t)
	}
	return parts[0] + "." + parts[1]
}
