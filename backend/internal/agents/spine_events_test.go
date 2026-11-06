package agents

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/internal/platform/events"
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
	if s, ok := ev.Entity.(sandboxEntity); !ok || s.Egress != "skipped" || s.RunID != run.ID || ev.Subject != "runs/"+run.ID+"/sandbox" {
		t.Errorf("ready entity = %+v, subject %s", ev.Entity, ev.Subject)
	}
	ev, _ = activityEvent(run, RunEvent{EventType: "guardrail_warning", Payload: []byte(`{"kind":"repeated_call","count":5,"tool":"Bash"}`)})
	if e, ok := ev.Entity.(runEntity); !ok || e.Guardrail == nil || *e.Guardrail != (guardrailFinding{Kind: "repeated_call", Count: 5, Tool: "Bash"}) {
		t.Errorf("warning entity = %+v", ev.Entity)
	}
}

func TestMessageEvent(t *testing.T) {
	run := Run{ID: uuid.NewString(), TenantID: "t1"}
	if e := messageEvent(run, nil).Entity.(runEntity); e.Sender != "person" {
		t.Errorf("sender = %q, want person", e.Sender)
	}
	if e := messageEvent(run, &StartedByTrigger{TriggerID: "trg"}).Entity.(runEntity); e.Sender != "trigger" || e.SenderTriggerID != "trg" {
		t.Errorf("sender = %q %q, want trigger trg", e.Sender, e.SenderTriggerID)
	}
}

func TestChangedFields(t *testing.T) {
	before := newAgentEntity(AgentType{Name: "Triage", Slug: "triage", GrantScopes: []string{"a"}})
	after := newAgentEntity(AgentType{Name: "Triage v2", Slug: "triage", GrantScopes: []string{"a", "b"}})
	if got := changedFields(before, after); !slices.Equal(got, []string{"grant_scopes", "name"}) {
		t.Errorf("changed = %v", got)
	}
	if got := changedFields(before, before); len(got) != 0 {
		t.Errorf("unchanged entity reports %v", got)
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
