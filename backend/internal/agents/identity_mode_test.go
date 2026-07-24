package agents

import (
	"context"
	"errors"
	"testing"
)

// ── IdentityMode model validation ─────────────────────────────────────────────

func TestIdentityMode_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mode    IdentityMode
		wantErr bool
	}{
		{"autonomous", IdentityModeAutonomous, false},
		{"delegated", IdentityModeDelegated, false},
		{"empty normalises to autonomous", "", false},
		{"unknown value rejected", "hybrid", true},
		{"case sensitive", "Delegated", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.mode.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("IdentityMode(%q).Validate() error = %v, wantErr %v", tc.mode, err, tc.wantErr)
			}
		})
	}
}

func TestIdentityMode_OrDefault(t *testing.T) {
	if got := IdentityMode("").OrDefault(); got != IdentityModeAutonomous {
		t.Errorf("empty OrDefault() = %q, want autonomous", got)
	}
	if got := IdentityModeDelegated.OrDefault(); got != IdentityModeDelegated {
		t.Errorf("delegated OrDefault() = %q, want delegated", got)
	}
}

// ── CreateAgentType: identity mode defaulting and validation ─────────────────

func TestCreateAgentType_DefaultsIdentityModeAutonomous(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()

	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if at.IdentityMode != IdentityModeAutonomous {
		t.Errorf("identity_mode = %q, want autonomous (default)", at.IdentityMode)
	}
	stored, err := repo.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("get stored agent type: %v", err)
	}
	if stored.IdentityMode != IdentityModeAutonomous {
		t.Errorf("stored identity_mode = %q, want autonomous", stored.IdentityMode)
	}
}

func TestCreateAgentType_DelegatedIdentityMode(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	ctx := context.Background()

	at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot", IdentityMode: IdentityModeDelegated,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if at.IdentityMode != IdentityModeDelegated {
		t.Errorf("identity_mode = %q, want delegated", at.IdentityMode)
	}
	stored, err := repo.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("get stored agent type: %v", err)
	}
	if stored.IdentityMode != IdentityModeDelegated {
		t.Errorf("stored identity_mode = %q, want delegated", stored.IdentityMode)
	}
}

func TestCreateAgentType_InvalidIdentityMode(t *testing.T) {
	svc, _, _, _ := newTestService(t)

	_, err := svc.CreateAgentType(context.Background(), CreateAgentTypeRequest{
		TenantID: "t-1", Name: "Bot", Slug: "bot", IdentityMode: "hybrid",
	})
	if !errors.Is(err, ErrInvalidIdentityMode) {
		t.Errorf("expected ErrInvalidIdentityMode, got %v", err)
	}
}

// ── UpdateAgentType: identity mode immutability ───────────────────────────────

func TestUpdateAgentType_IdentityModeChangeRejected(t *testing.T) {
	tests := []struct {
		name    string
		current IdentityMode
		update  IdentityMode
	}{
		{"autonomous to delegated", IdentityModeAutonomous, IdentityModeDelegated},
		{"delegated to autonomous", IdentityModeDelegated, IdentityModeAutonomous},
		{"legacy empty (autonomous) to delegated", "", IdentityModeDelegated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, _, _ := newTestService(t)
			seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", IdentityMode: tc.current})

			mode := tc.update
			_, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
				ID: "at-1", IdentityMode: &mode,
			})
			if !errors.Is(err, ErrIdentityModeImmutable) {
				t.Errorf("expected ErrIdentityModeImmutable, got %v", err)
			}

			stored, gErr := repo.GetAgentType(context.Background(), "at-1")
			if gErr != nil {
				t.Fatalf("get stored agent type: %v", gErr)
			}
			if stored.IdentityMode != tc.current {
				t.Errorf("stored identity_mode changed to %q, want %q", stored.IdentityMode, tc.current)
			}
		})
	}
}

func TestUpdateAgentType_IdentityModeRestatedIsNoOp(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", IdentityMode: IdentityModeDelegated})

	mode := IdentityModeDelegated
	newName := "Renamed"
	updated, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID: "at-1", IdentityMode: &mode, Name: &newName,
	})
	if err != nil {
		t.Fatalf("restating the current identity_mode must be a no-op, got: %v", err)
	}
	if updated.IdentityMode != IdentityModeDelegated {
		t.Errorf("identity_mode = %q, want delegated", updated.IdentityMode)
	}
	if updated.Name != "Renamed" {
		t.Errorf("name = %q, want Renamed", updated.Name)
	}
}

func TestUpdateAgentType_InvalidIdentityModeRejected(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", IdentityMode: IdentityModeAutonomous})

	mode := IdentityMode("hybrid")
	_, err := svc.UpdateAgentType(context.Background(), UpdateAgentTypeRequest{
		ID: "at-1", IdentityMode: &mode,
	})
	if !errors.Is(err, ErrInvalidIdentityMode) {
		t.Errorf("expected ErrInvalidIdentityMode, got %v", err)
	}
}
