package agents

import (
	"bytes"
	"encoding/json"
	"slices"
	"time"

	sandboxfolder "github.com/runkora/kora/backend/internal/agents/sandbox/folder"
	"github.com/runkora/kora/backend/internal/platform/events"
)

// The event spine entities of kora-agents (docs/adr/2026-11-06-event-spine.md).
// Each is built field by field from its row, so a new column reaches an event only when it is added here.

// runEntity is a run in an agents.run event.
type runEntity struct {
	ID            string     `json:"id"`
	AgentID       string     `json:"agent_id"`
	Agent         string     `json:"agent"`
	State         RunState   `json:"state"`
	CreatedBy     string     `json:"created_by"`
	TriggerID     string     `json:"trigger_id,omitempty"`
	DeliveryID    string     `json:"delivery_id,omitempty"`
	EnvironmentID string     `json:"environment_id,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	DeletedAt     *time.Time `json:"deleted_at,omitempty"`
	// Error is the run_error code of an errored turn.
	Error string `json:"error,omitempty"`
	// Guardrail is the loop guard finding of a warning.
	Guardrail *guardrailFinding `json:"guardrail,omitempty"`
	// SenderTriggerID is the trigger that sent a message, when it is known.
	SenderTriggerID string `json:"sender_trigger_id,omitempty"`
}

// guardrailFinding is the guardrail_warning payload.
type guardrailFinding struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
	Tool  string `json:"tool"`
}

// sandboxEntity is a run's sandbox in an agents.sandbox event.
type sandboxEntity struct {
	RunID       string `json:"run_id"`
	Name        string `json:"name"`
	Environment string `json:"environment,omitempty"`
	Image       string `json:"image,omitempty"`
	// Egress is "passed" or "skipped" on a ready sandbox.
	Egress string `json:"egress,omitempty"`
}

// agentEntity is an agent in an agents.agent event. Every events:read holder sees it, so it
// names the agent's connections and environments and carries no prompt or configuration.
type agentEntity struct {
	ID           string         `json:"id"`
	Slug         string         `json:"slug"`
	Name         string         `json:"name"`
	ModelID      *string        `json:"model_id,omitempty"`
	State        AgentTypeState `json:"state"`
	IdentityMode IdentityMode   `json:"identity_mode"`
	Environments []string       `json:"environment_ids"`
	Connections  []string       `json:"connections"`
}

// agentFields are the agent's settings an update compares. An update lists each changed field,
// the ones its entity leaves out included.
type agentFields struct {
	agentEntity
	Description             string         `json:"description"`
	SystemPrompt            string         `json:"system_prompt"`
	Framework               AgentFramework `json:"framework"`
	GrantScopes             []string       `json:"grant_scopes"`
	Connectivity            Connectivity   `json:"connectivity"`
	Connectors              []Connector    `json:"connectors"`
	MCPTools                []string       `json:"mcp_tools"`
	Guardrails              Guardrails     `json:"guardrails"`
	MaxConcurrentRunsPerPod int            `json:"max_concurrent_runs_per_pod"`
	Repository              string         `json:"repository"`
}

// environmentEntity is an environment in an agents.environment event. Like agentEntity it carries no configuration.
type environmentEntity struct {
	ID           string          `json:"id"`
	Slug         string          `json:"slug"`
	Name         string          `json:"name"`
	SandboxImage string          `json:"sandbox_image"`
	Size         EnvironmentSize `json:"size"`
	Connections  []string        `json:"connections"`
}

// environmentFields are the environment's settings an update compares.
type environmentFields struct {
	environmentEntity
	Description string      `json:"description"`
	EgressHosts []string    `json:"egress_hosts"`
	Connectors  []Connector `json:"connectors"`
	Repository  string      `json:"repository"`
}

func newRunEntity(r Run) runEntity {
	e := runEntity{
		ID: r.ID, AgentID: r.AgentTypeID, Agent: r.AgentSlug, State: r.State, CreatedBy: r.CreatedBy,
		StartedAt: r.StartedAt, CompletedAt: r.CompletedAt, DeletedAt: r.DeletedAt,
	}
	if r.StartedBy != nil {
		e.TriggerID, e.DeliveryID = r.StartedBy.TriggerID, r.StartedBy.DeliveryID
	}
	if r.EnvironmentID != nil {
		e.EnvironmentID = *r.EnvironmentID
	}
	return e
}

func newAgentEntity(at AgentType) agentEntity {
	envs := make([]string, len(at.Environments))
	for i, e := range at.Environments {
		envs[i] = e.EnvironmentID
	}
	return agentEntity{
		ID: at.ID, Slug: at.Slug, Name: at.Name, ModelID: at.ModelID, State: at.State,
		IdentityMode: at.IdentityMode.OrDefault(), Environments: envs, Connections: connectionNames(at.Connectors),
	}
}

func newAgentFields(at AgentType) agentFields {
	return agentFields{
		agentEntity: newAgentEntity(at), Description: at.Description, SystemPrompt: at.SystemPrompt, Framework: at.Framework,
		GrantScopes: at.GrantScopes, Connectivity: at.Connectivity, Connectors: at.Connectors, MCPTools: at.MCPTools,
		Guardrails: at.Guardrails, MaxConcurrentRunsPerPod: at.MaxConcurrentRunsPerPod, Repository: at.Repository,
	}
}

func newEnvironmentEntity(env Environment) environmentEntity {
	return environmentEntity{
		ID: env.ID, Slug: env.Slug, Name: env.Name, SandboxImage: env.SandboxImage, Size: env.Size,
		Connections: connectionNames(env.Connectors),
	}
}

func newEnvironmentFields(env Environment) environmentFields {
	return environmentFields{
		environmentEntity: newEnvironmentEntity(env), Description: env.Description, EgressHosts: env.EgressHosts,
		Connectors: env.Connectors, Repository: env.Repository,
	}
}

// connectionNames names the connections of an attachment list.
func connectionNames(connectors []Connector) []string {
	out := make([]string, len(connectors))
	for i, c := range connectors {
		out[i] = c.Connection
	}
	return out
}

// runEvent is an event about run r. The tenant, agent and loop guard attributes come from the row.
func runEvent(t events.Type, r Run, entity any, changed ...string) events.Event {
	return events.Event{
		Type: t, Tenant: r.TenantID, Subject: "runs/" + r.ID, RunID: r.ID, Agent: r.AgentSlug,
		CausedBy: r.CausedBy, ChainDepth: r.ChainDepth, Triggers: r.TriggerIDs, Entity: entity, Changed: changed,
	}
}

// sandboxEvent is an event about the sandbox of run r.
func sandboxEvent(t events.Type, r Run, entity sandboxEntity) events.Event {
	e := runEvent(t, r, entity)
	e.Subject += "/sandbox"
	return e
}

func agentEvent(t events.Type, at AgentType, changed []string) events.Event {
	return events.Event{Type: t, Tenant: at.TenantID, Subject: "agents/" + at.Slug, Agent: at.Slug, Entity: newAgentEntity(at), Changed: changed}
}

func environmentEvent(t events.Type, env Environment, changed []string) events.Event {
	return events.Event{Type: t, Tenant: env.TenantID, Subject: "environments/" + env.Slug, Entity: newEnvironmentEntity(env), Changed: changed}
}

// stateEventType is the event a state change records. idle to active is a new turn and records none.
func stateEventType(from, to RunState) (events.Type, bool) {
	switch to {
	case RunStateActive:
		return events.RunStarted, from == RunStateStarting
	case RunStateIdle:
		return events.RunIdle, true
	case RunStateCompleted:
		return events.RunCompleted, true
	case RunStateFailed:
		return events.RunFailed, true
	case RunStateBudgetExhausted:
		return events.RunBudgetExhausted, true
	case RunStateTerminated:
		return events.RunTerminated, true
	}
	return "", false
}

// stateEvent is the event of run r entering its current state from from.
func stateEvent(from RunState, r Run) (events.Event, bool) {
	t, ok := stateEventType(from, r.State)
	if !ok {
		return events.Event{}, false
	}
	changed := []string{"state"}
	if r.State.Terminal() && r.CompletedAt != nil {
		changed = append(changed, "completed_at")
	}
	return runEvent(t, r, newRunEntity(r), changed...), true
}

// recordsActivity lists the run_events types that may record a spine event. Per-call activity records none.
var recordsActivity = map[string]bool{
	"run_error": true, "turn_stopped": true, "guardrail_warning": true, "sandbox_status": true, eventSandboxEnvironment: true,
}

// runErrorsWithOwnEvent are run_error codes another event records. The session ends the run on
// subprocess_exit, empty_turn and budget_exhausted, and loop_detected precedes the guardrail pause.
var runErrorsWithOwnEvent = []string{"budget_exhausted", "loop_detected", "subprocess_exit", "empty_turn"}

// activityEvent is the spine event that run_events row e of run r records, if any.
func activityEvent(r Run, e RunEvent) (events.Event, bool) {
	var p struct {
		Code, Reason, Phase, Action, Environment, Image string
	}
	if json.Unmarshal(e.Payload, &p) != nil {
		return events.Event{}, false
	}
	entity := newRunEntity(r)
	sandbox := sandboxEntity{RunID: r.ID, Name: sandboxfolder.SandboxPodName(r.ID), Environment: p.Environment, Image: p.Image}
	switch e.EventType {
	case "run_error":
		// A run_error after the run ended describes the end, which its state event records.
		if r.State.Terminal() || slices.Contains(runErrorsWithOwnEvent, p.Code) {
			return events.Event{}, false
		}
		entity.Error = p.Code
		return runEvent(events.RunErrored, r, entity), true
	case "turn_stopped":
		if p.Reason == "guardrail" {
			return runEvent(events.RunGuardrailPaused, r, entity), true
		}
		return runEvent(events.RunInterrupted, r, entity), true
	case "guardrail_warning":
		var g guardrailFinding
		if json.Unmarshal(e.Payload, &g) != nil {
			return events.Event{}, false
		}
		entity.Guardrail = &g
		return runEvent(events.RunGuardrailWarned, r, entity), true
	case "sandbox_status":
		switch p.Phase {
		case "egress_ready":
			sandbox.Egress = "passed"
			return sandboxEvent(events.SandboxReady, r, sandbox), true
		case "egress_skipped":
			sandbox.Egress = "skipped"
			return sandboxEvent(events.SandboxReady, r, sandbox), true
		case "failed":
			return sandboxEvent(events.SandboxFailed, r, sandbox), true
		}
	case eventSandboxEnvironment:
		if p.Action == "replace" {
			return sandboxEvent(events.SandboxReplaced, r, sandbox), true
		}
		return sandboxEvent(events.SandboxCreated, r, sandbox), true
	}
	return events.Event{}, false
}

// messageEvent records a message sent to run r. from is the sending trigger, or
// nil. The envelope's koraprincipaltype says who sent it.
func messageEvent(r Run, from *StartedByTrigger) events.Event {
	entity := newRunEntity(r)
	if from != nil {
		entity.SenderTriggerID = from.TriggerID
	}
	return runEvent(events.RunMessageReceived, r, entity)
}

// sandboxDeletedEvent records the removal of run r's sandbox.
func sandboxDeletedEvent(r Run) events.Event {
	return sandboxEvent(events.SandboxDeleted, r, sandboxEntity{RunID: r.ID, Name: sandboxfolder.SandboxPodName(r.ID)})
}

// changedFields lists the top-level JSON fields that differ between two entities of one type.
func changedFields(before, after any) []string {
	b, a := jsonFields(before), jsonFields(after)
	var out []string
	for k, v := range a {
		if !bytes.Equal(b[k], v) {
			out = append(out, k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, k)
		}
	}
	slices.Sort(out)
	return out
}

func jsonFields(v any) map[string]json.RawMessage {
	raw, _ := json.Marshal(v)
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &out)
	return out
}
