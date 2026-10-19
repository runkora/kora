package agents

import (
	"context"
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

func TestSendMessage_SwitchesTriggerRunToManualTimeout(t *testing.T) {
	tests := []struct {
		name      string
		startedBy *StartedByTrigger
		want      int
	}{
		{name: "a trigger run moves to the manual timeout", startedBy: &StartedByTrigger{TriggerID: "trig-1"}, want: 1200},
		{name: "a manual run keeps its timeout", startedBy: nil, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, sm := newTestService(t)
			svc.run.cfg.IdleTimeouts = testIdleTimeouts
			at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateIdle, StartedBy: tt.startedBy})

			if err := svc.SendMessage(context.Background(), "run-1", "keep going"); err != nil {
				t.Fatalf("SendMessage: %v", err)
			}
			sm.mu.Lock()
			defer sm.mu.Unlock()
			if len(sm.sentIdleTimeouts) != 1 || sm.sentIdleTimeouts[0] != tt.want {
				t.Errorf("idle timeouts sent = %v, want [%d]", sm.sentIdleTimeouts, tt.want)
			}
			if len(sm.sentFromPerson) != 1 || !sm.sentFromPerson[0] {
				t.Errorf("from_person sent = %v, want [true]", sm.sentFromPerson)
			}
		})
	}
}
