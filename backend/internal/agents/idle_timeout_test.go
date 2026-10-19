package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

var testIdleTimeouts = IdleTimeouts{Manual: 1200, Trigger: 300}

func TestIdleTimeouts_ForRun(t *testing.T) {
	tests := []struct {
		name      string
		startedBy *StartedByTrigger
		want      int
	}{
		{name: "a run a person started takes the manual timeout", startedBy: nil, want: 1200},
		{name: "a run a trigger started takes the trigger timeout", startedBy: &StartedByTrigger{TriggerID: "trig-1"}, want: 300},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := testIdleTimeouts.ForRun(tt.startedBy); got != tt.want {
				t.Errorf("ForRun = %d, want %d", got, tt.want)
			}
		})
	}
	if got := (IdleTimeouts{}).ForRun(&StartedByTrigger{TriggerID: "trig-1"}); got != 0 {
		t.Errorf("unset timeouts: ForRun = %d, want 0 so the session-manager's default applies", got)
	}
}

func TestIdleTimeouts_ForPersonMessage(t *testing.T) {
	if got := testIdleTimeouts.ForPersonMessage(&StartedByTrigger{TriggerID: "trig-1"}); got != 1200 {
		t.Errorf("trigger run: ForPersonMessage = %d, want the manual timeout 1200", got)
	}
	if got := testIdleTimeouts.ForPersonMessage(nil); got != 0 {
		t.Errorf("manual run: ForPersonMessage = %d, want 0 (no change)", got)
	}
}

func TestStartRun_SendsIdleTimeoutByOrigin(t *testing.T) {
	t.Run("trigger run", func(t *testing.T) {
		f, repo, _, sm := newTestService(t)
		f.run.cfg.IdleTimeouts = testIdleTimeouts
		at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
		ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "trigger:trig-1")
		if _, err := startAndSchedule(ctx, f.run, StartRunRequest{AgentTypeID: at.ID, TriggerID: "trig-1"}); err != nil {
			t.Fatalf("startAndSchedule: %v", err)
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if len(sm.sessions) != 1 || sm.sessions[0].IdleTimeoutSecs != 300 || !sm.sessions[0].Triggered {
			t.Errorf("sessions = %+v, want one triggered session with idle_timeout_secs 300", sm.sessions)
		}
	})
	t.Run("manual run", func(t *testing.T) {
		f, repo, _, sm := newTestService(t)
		f.run.cfg.IdleTimeouts = testIdleTimeouts
		at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
		ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user:1")
		if _, err := startAndSchedule(ctx, f.run, StartRunRequest{AgentTypeID: at.ID}); err != nil {
			t.Fatalf("startAndSchedule: %v", err)
		}
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if len(sm.sessions) != 1 || sm.sessions[0].IdleTimeoutSecs != 1200 || sm.sessions[0].Triggered {
			t.Errorf("sessions = %+v, want one untriggered session with idle_timeout_secs 1200", sm.sessions)
		}
	})
}

func TestSendMessage_IdleTimeoutAndEventBySender(t *testing.T) {
	trig := &StartedByTrigger{TriggerID: "trig-1"}
	tests := []struct {
		name           string
		principal      token.PrincipalType
		subject        string
		startedBy      *StartedByTrigger
		req            SendMessageRequest
		wantTimeout    int
		wantFromPerson bool
		wantEvent      string
		wantPayload    string
	}{
		{
			name: "a person moves a trigger run to the manual timeout", principal: token.PrincipalTypeHuman, subject: "user:1",
			startedBy: trig, req: SendMessageRequest{Content: "keep going"},
			wantTimeout: 1200, wantFromPerson: true, wantEvent: "human_message", wantPayload: `{"content":"keep going"}`,
		},
		{
			name: "a person keeps a manual run's timeout", principal: token.PrincipalTypeHuman, subject: "user:1",
			req:         SendMessageRequest{Content: "keep going"},
			wantTimeout: 0, wantFromPerson: true, wantEvent: "human_message", wantPayload: `{"content":"keep going"}`,
		},
		{
			name: "a PAT is a person", principal: token.PrincipalTypePAT, subject: "pat:1",
			startedBy: trig, req: SendMessageRequest{Content: "keep going"},
			wantTimeout: 1200, wantFromPerson: true, wantEvent: "human_message", wantPayload: `{"content":"keep going"}`,
		},
		{
			name: "a trigger keeps the trigger run's timeout", principal: token.PrincipalTypeService, subject: "trigger-source:github",
			startedBy:   trig,
			req:         SendMessageRequest{Content: "new comment", TriggerID: "trig-2", TriggerName: "PR comments", DeliveryID: "del-9"},
			wantTimeout: 0, wantFromPerson: false, wantEvent: "trigger_message",
			wantPayload: `{"content":"new comment","trigger_id":"trig-2","trigger_name":"PR comments","delivery_id":"del-9"}`,
		},
		{
			name: "a trigger principal naming its trigger", principal: token.PrincipalTypeService, subject: "trigger:trig-1",
			startedBy: trig, req: SendMessageRequest{Content: "new comment", TriggerID: "trig-1", TriggerName: "Nightly"},
			wantTimeout: 0, wantFromPerson: false, wantEvent: "trigger_message",
			wantPayload: `{"content":"new comment","trigger_id":"trig-1","trigger_name":"Nightly"}`,
		},
		{
			name: "a trigger principal without a trigger id is attributed to itself", principal: token.PrincipalTypeService, subject: "trigger:trig-1",
			startedBy: trig, req: SendMessageRequest{Content: "new comment"},
			wantTimeout: 0, wantFromPerson: false, wantEvent: "trigger_message", wantPayload: `{"content":"new comment","trigger_id":"trig-1"}`,
		},
		{
			name: "a source principal without attribution is a trigger", principal: token.PrincipalTypeService, subject: "trigger-source:github",
			startedBy: trig, req: SendMessageRequest{Content: "new comment"},
			wantTimeout: 0, wantFromPerson: false, wantEvent: "trigger_message", wantPayload: `{"content":"new comment"}`,
		},
		{
			name: "another service principal keeps the pre-routing behaviour", principal: token.PrincipalTypeService, subject: "session-manager",
			startedBy: trig, req: SendMessageRequest{Content: "keep going"},
			wantTimeout: 1200, wantFromPerson: true, wantEvent: "human_message", wantPayload: `{"content":"keep going"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, sm := newTestService(t)
			svc.run.cfg.IdleTimeouts = testIdleTimeouts
			at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle, StartedBy: tt.startedBy})

			ctx := withPrincipalCtx(context.Background(), "t-1", tt.principal, tt.subject)
			if err := svc.run.SendMessage(ctx, "run-1", tt.req); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			sm.mu.Lock()
			defer sm.mu.Unlock()
			if len(sm.sentIdleTimeouts) != 1 || sm.sentIdleTimeouts[0] != tt.wantTimeout {
				t.Errorf("idle timeouts sent = %v, want [%d]", sm.sentIdleTimeouts, tt.wantTimeout)
			}
			if len(sm.sentFromPerson) != 1 || sm.sentFromPerson[0] != tt.wantFromPerson {
				t.Errorf("from_person sent = %v, want [%v]", sm.sentFromPerson, tt.wantFromPerson)
			}
			events, err := repo.ListRunEvents(ctx, "run-1", 0)
			if err != nil || len(events) == 0 {
				t.Fatalf("ListRunEvents = %v, %v; want events", events, err)
			}
			// The idle run's move to active is recorded first; the message event comes last.
			last := events[len(events)-1]
			if last.EventType != tt.wantEvent || string(last.Payload) != tt.wantPayload {
				t.Errorf("last event = %s %s, want %s %s", last.EventType, last.Payload, tt.wantEvent, tt.wantPayload)
			}
		})
	}
}

func TestSendMessage_RefusesTriggerAttribution(t *testing.T) {
	tests := []struct {
		name      string
		principal token.PrincipalType
		subject   string
		req       SendMessageRequest
		wantErr   error
	}{
		{
			name: "a person may not send as a trigger", principal: token.PrincipalTypeHuman, subject: "user:1",
			req: SendMessageRequest{Content: "hi", TriggerID: "trig-1"}, wantErr: ErrTriggerAttributionRefused,
		},
		{
			name: "another service principal may not send as a trigger", principal: token.PrincipalTypeService, subject: "session-manager",
			req: SendMessageRequest{Content: "hi", TriggerID: "trig-1"}, wantErr: ErrTriggerAttributionRefused,
		},
		{
			name: "a PAT may not send as a trigger", principal: token.PrincipalTypePAT, subject: "pat:1",
			req: SendMessageRequest{Content: "hi", TriggerName: "Nightly"}, wantErr: ErrTriggerAttributionRefused,
		},
		{
			name: "a trigger may not send as another trigger", principal: token.PrincipalTypeService, subject: "trigger:trig-1",
			req: SendMessageRequest{Content: "hi", TriggerID: "trig-2"}, wantErr: ErrTriggerAttributionRefused,
		},
		{
			name: "a delivery without a trigger is incomplete", principal: token.PrincipalTypeService, subject: "trigger-source:github",
			req: SendMessageRequest{Content: "hi", DeliveryID: "del-1"}, wantErr: ErrTriggerAttributionIncomplete,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, sm := newTestService(t)
			at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle})

			ctx := withPrincipalCtx(context.Background(), "t-1", tt.principal, tt.subject)
			if err := svc.run.SendMessage(ctx, "run-1", tt.req); !errors.Is(err, tt.wantErr) {
				t.Fatalf("SendMessage err = %v, want %v", err, tt.wantErr)
			}
			sm.mu.Lock()
			defer sm.mu.Unlock()
			if len(sm.sentMessages) != 0 {
				t.Errorf("sent %v, want nothing sent", sm.sentMessages)
			}
		})
	}
}
