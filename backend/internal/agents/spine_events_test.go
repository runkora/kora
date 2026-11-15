package agents

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func TestStateEventType(t *testing.T) {
	for _, tc := range []struct {
		from, to RunState
		want     events.Type
		ok       bool
	}{
		{RunStateStarting, RunStateActive, events.RunStarted, true},
		{RunStateIdle, RunStateActive, "", false},
		{RunStateActive, RunStateIdle, events.RunIdle, true},
		{RunStateActive, RunStateCompleting, "", false},
		{RunStateCompleting, RunStateCompleted, events.RunCompleted, true},
		{RunStateActive, RunStateFailed, events.RunFailed, true},
		{RunStateActive, RunStateBudgetExhausted, events.RunBudgetExhausted, true},
		{RunStateIdle, RunStateTerminated, events.RunTerminated, true},
	} {
		got, ok := stateEventType(tc.from, tc.to)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s→%s = %q %v, want %q %v", tc.from, tc.to, got, ok, tc.want, tc.ok)
		}
	}
}

func TestActivityEvent(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1", AgentSlug: "triage", State: RunStateActive,
		CausedBy: uuid.NewString(), ChainDepth: 2, TriggerIDs: []string{"trg-1"}}
	for _, tc := range []struct {
		name, eventType, payload string
		state                    RunState
		want                     events.Type
	}{
		{"errored turn", "run_error", `{"code":"runner_error","message":"boom"}`, "", events.RunErrored},
		{"error after the run ended", "run_error", `{"code":"start_failed"}`, RunStateFailed, ""},
		{"budget error has its own state event", "run_error", `{"code":"budget_exhausted"}`, "", ""},
		{"loop error has its own pause event", "run_error", `{"code":"loop_detected"}`, "", ""},
		{"a crash fails the run", "run_error", `{"code":"subprocess_exit"}`, "", ""},
		{"person stop", "turn_stopped", `{"reason":"user"}`, "", events.RunInterrupted},
		{"guardrail stop", "turn_stopped", `{"reason":"guardrail"}`, "", events.RunGuardrailPaused},
		{"guardrail warning", "guardrail_warning", `{"kind":"repeated_call","count":5,"tool":"Bash"}`, "", events.RunGuardrailWarned},
		{"egress passed", "sandbox_status", `{"phase":"egress_ready","elapsed_ms":10}`, "", events.SandboxReady},
		{"egress skipped", "sandbox_status", `{"phase":"egress_skipped","message":"no host"}`, "", events.SandboxReady},
		{"provisioning phase", "sandbox_status", `{"phase":"provisioning"}`, "", ""},
		{"sandbox failed", "sandbox_status", `{"phase":"failed","message":"no node"}`, "", events.SandboxFailed},
		{"sandbox started", eventSandboxEnvironment, `{"action":"start","environment":"default","image":"img@sha256:1"}`, "", events.SandboxCreated},
		{"sandbox replaced", eventSandboxEnvironment, `{"action":"replace","environment":"go","image":"img@sha256:2"}`, "", events.SandboxReplaced},
		{"per-call activity", "tool_use", `{"id":"1","name":"Bash"}`, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := run
			if tc.state != "" {
				r.State = tc.state
			}
			ev, ok := activityEvent(r, RunEvent{RunID: r.ID, EventType: tc.eventType, Payload: []byte(tc.payload)})
			if ok != (tc.want != "") || ev.Type != tc.want {
				t.Fatalf("event = %q %v, want %q", ev.Type, ok, tc.want)
			}
			if !ok {
				return
			}
			if ev.Tenant != "t1" || ev.RunID != r.ID || ev.Agent != "triage" || ev.CausedBy != r.CausedBy || ev.ChainDepth != 2 || !slices.Equal(ev.Triggers, []string{"trg-1"}) {
				t.Errorf("attributes = %+v, want the run's", ev)
			}
		})
	}
}

func TestActivityEvent_Entities(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1", State: RunStateActive}
	ev, _ := activityEvent(run, RunEvent{EventType: "sandbox_status", Payload: []byte(`{"phase":"egress_skipped"}`)})
	if s, ok := ev.Entity.(events.SandboxPayload); !ok || s.Egress != "skipped" || s.RunID != run.ID || ev.Subject != "runs/"+run.ID+"/sandbox" {
		t.Errorf("ready entity = %+v, subject %s", ev.Entity, ev.Subject)
	}
	ev, _ = activityEvent(run, RunEvent{EventType: "guardrail_warning", Payload: []byte(`{"kind":"repeated_call","count":5,"tool":"Bash"}`)})
	if e, ok := ev.Entity.(events.RunPayload); !ok || e.Guardrail == nil || *e.Guardrail != (events.GuardrailFinding{Kind: "repeated_call", Count: 5, Tool: "Bash"}) {
		t.Errorf("warning entity = %+v", ev.Entity)
	}
}

func TestMessageEvent(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1"}
	if e := messageEvent(run, nil).Entity.(events.RunPayload); e.SenderTriggerID != "" {
		t.Errorf("sender_trigger_id = %q, want empty", e.SenderTriggerID)
	}
	if e := messageEvent(run, &StartedByTrigger{TriggerID: "trg"}).Entity.(events.RunPayload); e.SenderTriggerID != "trg" {
		t.Errorf("sender_trigger_id = %q, want trg", e.SenderTriggerID)
	}
}

func TestChangedFields(t *testing.T) {
	before := newAgentFields(AgentType{Name: "Triage", Slug: "triage", GrantScopes: []string{"a"}, SystemPrompt: "old"})
	after := newAgentFields(AgentType{Name: "Triage v2", Slug: "triage", GrantScopes: []string{"a", "b"}, SystemPrompt: "new"})
	if got := events.Changed(before, after); !slices.Equal(got, []string{"grant_scopes", "name", "system_prompt"}) {
		t.Errorf("changed = %v", got)
	}
	if got := events.Changed(before, before); len(got) != 0 {
		t.Errorf("unchanged entity reports %v", got)
	}
}

// Every events:read holder reads the entities. They name connections and carry no prompt or configuration.
func TestEntitiesCarryNoConfiguration(t *testing.T) {
	at := AgentType{ID: "a1", Slug: "triage", SystemPrompt: "secret instructions", Guardrails: Guardrails{RepeatedCallWarn: new(int)},
		Connectors: []Connector{{Connection: "github", Actions: []string{"contents:write"}}}}
	agent := jsonFields(agentEvent(events.AgentUpdated, at, nil).Entity)
	for _, k := range []string{"system_prompt", "guardrails", "connectors", "grant_scopes", "connectivity"} {
		if _, ok := agent[k]; ok {
			t.Errorf("agent entity carries %s", k)
		}
	}
	if string(agent["connections"]) != `["github"]` {
		t.Errorf("connections = %s", agent["connections"])
	}
	env := jsonFields(environmentEvent(events.EnvironmentUpdated, Environment{Slug: "go", EgressHosts: []string{"x.example.com"},
		Connectors: []Connector{{Connection: "github", Actions: []string{"contents:read"}}}}, nil).Entity)
	for _, k := range []string{"egress_hosts", "connectors", "description"} {
		if _, ok := env[k]; ok {
			t.Errorf("environment entity carries %s", k)
		}
	}
	failed, _ := activityEvent(Run{ID: uuid.NewString(), TenantID: "t1"}, RunEvent{EventType: "sandbox_status", Payload: []byte(`{"phase":"failed","message":"token=abc"}`)})
	if _, ok := jsonFields(failed.Entity)["error"]; ok {
		t.Error("sandbox failed entity carries the failure message")
	}
}

// A terminated run takes no agent callbacks, and an ended run gets no sandbox.
func TestEndedRunRefusesCallbacks(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateCompleted})
	if _, err := svc.run.ProvisionSandbox(context.Background(), "run-1", "", false); !errors.Is(err, ErrNotFound) {
		t.Errorf("provision for an ended run = %v, want ErrNotFound", err)
	}
	if k8s.provisionSpec.Image != "" {
		t.Error("a sandbox was provisioned for an ended run")
	}
	if _, err := repo.TerminateRun(context.Background(), "run-1"); err != nil {
		t.Fatal(err)
	}
	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeAgent, "agent")
	if err := svc.run.AuthorizeAgentCallback(ctx, "run-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("callback for a terminated run = %v, want ErrNotFound", err)
	}
	if err := svc.run.TransitionRunState(context.Background(), "run-1", RunStateCompleted, RunStateActive); err == nil {
		t.Error("an ended run moved back to active")
	}
}

func TestAuthorizeChain(t *testing.T) {
	trigger := &StartedByTrigger{TriggerID: "trg"}
	for _, tc := range []struct {
		name  string
		from  *StartedByTrigger
		cause string
		depth int
		want  error
	}{
		{"no chain", nil, "", 0, nil},
		{"trigger chain", trigger, uuid.NewString(), 2, nil},
		{"person reports a chain", nil, uuid.NewString(), 1, ErrTriggerAttributionRefused},
		{"cause is not an event id", trigger, "evt-1", 1, ErrInvalidChain},
		{"negative depth", trigger, "", -1, ErrInvalidChain},
		{"depth past the column", trigger, "", math.MaxInt32 + 1, ErrInvalidChain},
	} {
		if err := authorizeChain(tc.from, tc.cause, tc.depth); !errors.Is(err, tc.want) || (tc.want == nil && err != nil) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// closeReportsCompletion is a session-manager whose session reports the run completed as it closes.
type closeReportsCompletion struct {
	*mockSMClient
	report func()
}

func (c *closeReportsCompletion) CloseSession(ctx context.Context, url, runID string) error {
	c.report()
	return c.mockSMClient.CloseSession(ctx, url, runID)
}

// A live run ends terminated even though its closing session reports it completed.
func TestTerminateRun_ClosingSessionDoesNotCompleteTheRun(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	var reportErr error
	svc.run.sm = &closeReportsCompletion{mockSMClient: sm, report: func() {
		reportErr = svc.run.TransitionRunState(context.Background(), run.ID, RunStateActive, RunStateCompleted)
	}}
	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(reportErr, ErrConflict) {
		t.Errorf("completion report = %v, want ErrConflict", reportErr)
	}
	assertTerminated(t, repo, run.ID)
}

// Terminate closes the session of any live run placed on a pod, and skips a run still queued.
func TestTerminateRun_ClosesSessionOfPlacedRun(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "queued", TenantID: "t-1", AgentTypeID: "at-1", State: RunStatePending, PodOrdinal: -1})
	seedRun(t, repo, Run{ID: "placed", TenantID: "t-1", AgentTypeID: "at-1", State: RunStatePending, PodOrdinal: 0})
	for _, id := range []string{"queued", "placed"} {
		if err := svc.TerminateRun(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Equal(sm.closedSessions, []string{"placed"}) {
		t.Errorf("closed sessions = %v, want [placed]", sm.closedSessions)
	}
}

// jsonFields is the top-level JSON object of an event entity.
func jsonFields(v any) map[string]json.RawMessage {
	raw, _ := json.Marshal(v)
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// An unknown run error code or guardrail kind records the schema's unknown value.
func TestActivityEvent_UnknownCodes(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1", State: RunStateActive}
	ev, _ := activityEvent(run, RunEvent{EventType: "run_error", Payload: []byte(`{"code":"made_up"}`)})
	if e := ev.Entity.(events.RunPayload); e.Error != events.RunErrorUnknown {
		t.Errorf("error = %q, want %q", e.Error, events.RunErrorUnknown)
	}
	ev, _ = activityEvent(run, RunEvent{EventType: "guardrail_warning", Payload: []byte(`{"kind":"other","count":1,"tool":"Bash"}`)})
	if e := ev.Entity.(events.RunPayload); e.Guardrail.Kind != events.GuardrailUnknown {
		t.Errorf("guardrail kind = %q, want %q", e.Guardrail.Kind, events.GuardrailUnknown)
	}
}

// Every payload kora-agents publishes validates against its type schema, and
// every field an update can list is in the schema's changed enum.
func TestSpinePayloads_MatchSchema(t *testing.T) {
	now := time.Now()
	model := "anthropic/claude"
	run := Run{ID: uuid.NewString(), TenantID: "t1", AgentTypeID: "at-1", AgentSlug: "triage", State: RunStateFailed,
		CreatedBy: "user-1", StartedAt: &now, CompletedAt: &now, StartedBy: &StartedByTrigger{TriggerID: "trg", DeliveryID: "d"}}
	at := AgentType{ID: "at-1", Slug: "triage", Name: "Triage", ModelID: &model, State: AgentTypeStateActive,
		Environments: []AgentEnvironment{{EnvironmentID: "env-1"}}, Connectors: []Connector{{Connection: "github"}}}
	env := Environment{ID: "env-1", Slug: "go", Name: "Go", SandboxImage: "img", Size: EnvironmentSize{CPU: "1", Memory: "1Gi", Disk: "5Gi"}}
	failed, _ := stateEvent(RunStateActive, run, events.RunErrorSandboxLost)
	warned, _ := activityEvent(Run{ID: run.ID, TenantID: "t1", State: RunStateActive},
		RunEvent{EventType: "guardrail_warning", Payload: []byte(`{"kind":"repeated_call","count":5,"tool":"Bash"}`)})
	errored, _ := activityEvent(Run{ID: run.ID, TenantID: "t1", State: RunStateActive},
		RunEvent{EventType: "run_error", Payload: []byte(`{"code":"agent_error"}`)})
	ready, _ := activityEvent(run, RunEvent{EventType: "sandbox_status", Payload: []byte(`{"phase":"egress_ready"}`)})
	skipped, _ := activityEvent(run, RunEvent{EventType: "sandbox_status", Payload: []byte(`{"phase":"egress_skipped"}`)})
	for _, ev := range []events.Event{
		failed, warned, errored, ready, skipped, sandboxDeletedEvent(run), messageEvent(run, &StartedByTrigger{TriggerID: "trg"}),
		agentEvent(events.AgentUpdated, at, nil), environmentEvent(events.EnvironmentUpdated, env, nil),
	} {
		if err := events.ValidatePayload(ev.Type, ev.Entity); err != nil {
			t.Errorf("%s: %v", ev.Type, err)
		}
	}
	for typ, fields := range map[events.Type]any{events.AgentUpdated: newAgentFields(at), events.EnvironmentUpdated: newEnvironmentFields(env)} {
		for _, name := range events.Changed(map[string]any{}, fields) {
			if !slices.Contains(events.Changeable(typ), name) {
				t.Errorf("%s: changed field %q is not in the schema", typ, name)
			}
		}
	}
	for _, name := range []string{"state", "completed_at", "deleted_at"} {
		if !slices.Contains(events.Changeable(events.RunTerminated), name) {
			t.Errorf("run changed field %q is not in the schema", name)
		}
	}
}

// The schema's run and agent state enums hold every state kora-agents has.
func TestSpinePayloads_StateEnums(t *testing.T) {
	var states []string
	for _, s := range AllRunStates() {
		states = append(states, string(s))
	}
	if got := events.Enum("run_state"); !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(states))) {
		t.Errorf("run_state enum = %v, want %v", got, states)
	}
	agentStates := []string{string(AgentTypeStateProvisioning), string(AgentTypeStateActive), string(AgentTypeStateDeleting), string(AgentTypeStateDeleted)}
	if got := events.Enum("agent_state"); !slices.Equal(got, agentStates) {
		t.Errorf("agent_state enum = %v, want %v", got, agentStates)
	}
	modes := []string{string(IdentityModeAutonomous), string(IdentityModeDelegated)}
	if got := events.Enum("identity_mode"); !slices.Equal(got, modes) {
		t.Errorf("identity_mode enum = %v, want %v", got, modes)
	}
}

// A failed run's event carries its last run error code, and unknown for a code
// outside the schema or a run that recorded none. Other states carry none.
func TestStateEvent_FailedRunError(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1", State: RunStateFailed}
	for last, want := range map[string]string{
		events.RunErrorSubprocessExit: events.RunErrorSubprocessExit,
		"made_up":                     events.RunErrorUnknown,
		"":                            events.RunErrorUnknown,
	} {
		ev, ok := stateEvent(RunStateActive, run, last)
		if !ok || ev.Type != events.RunFailed {
			t.Fatalf("last %q: event %s %v, want a failed run event", last, ev.Type, ok)
		}
		if got := ev.Entity.(events.RunPayload).Error; got != want {
			t.Errorf("last %q: error %q, want %q", last, got, want)
		}
	}
	run.State = RunStateCompleted
	if ev, _ := stateEvent(RunStateActive, run, events.RunErrorAgentError); ev.Entity.(events.RunPayload).Error != "" {
		t.Errorf("completed run error = %q, want none", ev.Entity.(events.RunPayload).Error)
	}
}
