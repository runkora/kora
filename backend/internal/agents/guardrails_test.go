package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/token"
)

func TestGuardrails_Validate(t *testing.T) {
	tests := []struct {
		name string
		g    Guardrails
		ok   bool
	}{
		{"defaults", Guardrails{}, true},
		{"custom", Guardrails{RepeatedCallWarn: new(2), RepeatedCallPause: new(3), WaitRearmWarn: new(10), WaitRearmPause: new(30)}, true},
		{"warn below 1", Guardrails{RepeatedCallWarn: new(0)}, false},
		{"pause equal to warn", Guardrails{WaitRearmWarn: new(5), WaitRearmPause: new(5)}, false},
		{"no off switch", Guardrails{RepeatedCallPause: new(0)}, false},
		{"warn above the default pause", Guardrails{RepeatedCallWarn: new(9)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.g.Validate()
			if tt.ok != (err == nil) {
				t.Fatalf("Validate() = %v, want ok=%v", err, tt.ok)
			}
			if err != nil && !errors.Is(err, ErrInvalidGuardrails) {
				t.Errorf("error %v is not ErrInvalidGuardrails", err)
			}
		})
	}
}

func TestGuardrails_ResolveFillsDefaults(t *testing.T) {
	got := Guardrails{WaitRearmPause: new(20)}.Resolve()
	want := ResolvedGuardrails{DefaultRepeatedCallWarn, DefaultRepeatedCallPause, DefaultWaitRearmWarn, 20}
	if got != want {
		t.Errorf("Resolve() = %+v, want %+v", got, want)
	}
}

// Create validates and stores the thresholds; update replaces them, and an
// empty object restores the defaults.
func TestAgentType_GuardrailsCreateAndUpdate(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()

	if _, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{TenantID: "t-1", Name: "Bot", Slug: "bad", Guardrails: &Guardrails{RepeatedCallWarn: new(5), RepeatedCallPause: new(5)}}); !errors.Is(err, ErrInvalidGuardrails) {
		t.Fatalf("create with pause equal to warn: err = %v", err)
	}
	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{TenantID: "t-1", Name: "Bot", Slug: "bot", Guardrails: &Guardrails{RepeatedCallWarn: new(2), RepeatedCallPause: new(3)}})
	if err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	stored, _ := repo.GetAgentType(ctx, at.ID)
	if r := stored.Guardrails.Resolve(); r.RepeatedCallWarn != 2 || r.RepeatedCallPause != 3 || r.WaitRearmWarn != DefaultWaitRearmWarn {
		t.Errorf("stored thresholds = %+v", r)
	}

	if _, err := svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: at.ID, Guardrails: &Guardrails{WaitRearmWarn: new(0)}}); !errors.Is(err, ErrInvalidGuardrails) {
		t.Fatalf("update with warn 0: err = %v", err)
	}
	if _, err := svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: at.ID, Name: new("Renamed")}); err != nil {
		t.Fatalf("update without guardrails: %v", err)
	}
	stored, _ = repo.GetAgentType(ctx, at.ID)
	if stored.Guardrails.Resolve().RepeatedCallWarn != 2 {
		t.Error("an update without guardrails changed them")
	}
	if _, err := svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: at.ID, Guardrails: &Guardrails{}}); err != nil {
		t.Fatalf("update restoring defaults: %v", err)
	}
	stored, _ = repo.GetAgentType(ctx, at.ID)
	if !stored.Guardrails.isEmpty() {
		t.Errorf("guardrails after restoring defaults = %+v", stored.Guardrails)
	}
}

// A run carries the install's sandbox settings and the agent type's resolved
// loop thresholds to the session-manager.
func TestStartRun_SendsGuardrails(t *testing.T) {
	f, repo, _, sm := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t1", Slug: "bot", MaxConcurrentRunsPerPod: 5,
		Guardrails: Guardrails{WaitRearmWarn: new(3), WaitRearmPause: new(7)}})

	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user:1")
	if _, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if len(sm.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sm.sessions))
	}
	want := sessionmanager.RunGuardrails{
		SandboxProvisionAttempts: 2, SandboxProvisionBackoffSecs: 1, SandboxProvisionBudgetSecs: 60,
		RepeatedCallWarn: DefaultRepeatedCallWarn, RepeatedCallPause: DefaultRepeatedCallPause,
		WaitRearmWarn: 3, WaitRearmPause: 7,
	}
	if got := sm.sessions[0].Guardrails; got != want {
		t.Errorf("run guardrails = %+v, want %+v", got, want)
	}
}
