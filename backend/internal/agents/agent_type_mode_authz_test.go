package agents

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

// scopedEvaluator is a policy.Evaluator stub whose Check answers from a fixed
// set of granted scopes using the platform's covers hierarchy — the same
// semantics as the rego scope_covers rule. It lets mode-derivation tests grant
// exactly agents:types:delegated:write (etc.) and observe real coverage
// behaviour without a compiled bundle.
type scopedEvaluator struct {
	allowAllEvaluator
	granted []string
}

func (e *scopedEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	required := policy.DeriveRequiredScope(in.ResourceType, in.PathSegments, in.Action)
	for _, g := range e.granted {
		if policy.ScopeCovers(g, required) {
			return policy.Result{Allow: true, Reason: "granted"}, nil
		}
	}
	return policy.Result{Allow: false, Reason: "no_matching_grant"}, nil
}

// newModeTestService builds an AgentTypeService whose authz layer evaluates
// against exactly the given granted scopes.
func newModeTestService(t *testing.T, granted ...string) (*AgentTypeService, *mockRepository) {
	t.Helper()
	repo := newMockRepository()
	k8s := &mockK8s{}
	authz := NewAuthz(&scopedEvaluator{granted: granted})
	svc := NewAgentTypeService(
		repo, repo, k8s, authz, nil, lockedTenants{},
		AgentTypeServiceConfig{
			MaxConcurrentRunsPerPod: 5,
			TenantNamespaceFn:       func(tenantID string) string { return "kora-tenant-" + tenantID },
		},
		logging.NewLoggerWithOutput(io.Discard),
	)
	svc.SetCallerScopes(broadScopes)
	return svc, repo
}

func seedModeAgentType(t *testing.T, repo *mockRepository, mode IdentityMode) AgentType {
	t.Helper()
	at := AgentType{
		ID:           "at-" + string(mode),
		TenantID:     "t-1",
		Name:         "Bot",
		Slug:         "bot-" + string(mode),
		IdentityMode: mode,
		GrantScopes:  []string{},
		MCPTools:     []string{},
		State:        AgentTypeStateActive,
	}
	if err := repo.CreateAgentType(context.Background(), at); err != nil {
		t.Fatalf("seed agent type: %v", err)
	}
	return at
}

// ── Create: required scope derives from the requested mode ───────────────────

func TestCreateAgentType_ModeScopes(t *testing.T) {
	tests := []struct {
		name        string
		granted     []string
		mode        IdentityMode // "" = omitted, defaults to autonomous
		wantAllowed bool
	}{
		{
			name:        "delegated create allowed with only delegated:write",
			granted:     []string{"agents:types:delegated:write"},
			mode:        IdentityModeDelegated,
			wantAllowed: true,
		},
		{
			name:        "autonomous create refused with only delegated:write",
			granted:     []string{"agents:types:delegated:write"},
			mode:        IdentityModeAutonomous,
			wantAllowed: false,
		},
		{
			name:        "omitted mode defaults to autonomous and is refused with only delegated:write",
			granted:     []string{"agents:types:delegated:write"},
			mode:        "",
			wantAllowed: false,
		},
		{
			name:        "delegated create refused with only autonomous:write",
			granted:     []string{"agents:types:autonomous:write"},
			mode:        IdentityModeDelegated,
			wantAllowed: false,
		},
		{
			name:        "parent types:write allows delegated create",
			granted:     []string{"agents:types:write"},
			mode:        IdentityModeDelegated,
			wantAllowed: true,
		},
		{
			name:        "parent types:write allows autonomous create",
			granted:     []string{"agents:types:write"},
			mode:        IdentityModeAutonomous,
			wantAllowed: true,
		},
		{
			name:        "no types scopes refuses delegated create",
			granted:     []string{"agents:types:read"},
			mode:        IdentityModeDelegated,
			wantAllowed: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newModeTestService(t, tc.granted...)
			ctx := withTenantCtx(context.Background(), "t-1")

			_, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
				Name: "Bot", Slug: "bot", IdentityMode: tc.mode,
			})
			if tc.wantAllowed {
				if err != nil {
					t.Fatalf("expected create to be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIdentityModeWriteForbidden) {
				t.Fatalf("expected ErrIdentityModeWriteForbidden, got %v", err)
			}
		})
	}
}

// The refusal names the missing mode-qualified scope and the identity model,
// so a 403 tells the caller exactly which grant to request.
func TestCreateAgentType_ModeScopeRefusalNamesScope(t *testing.T) {
	svc, _ := newModeTestService(t, "agents:types:delegated:write")
	ctx := withTenantCtx(context.Background(), "t-1")

	_, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{
		Name: "Bot", Slug: "bot", IdentityMode: IdentityModeAutonomous,
	})
	if !errors.Is(err, ErrIdentityModeWriteForbidden) {
		t.Fatalf("expected ErrIdentityModeWriteForbidden, got %v", err)
	}
	if !strings.Contains(err.Error(), "agents:types:autonomous:write") {
		t.Errorf("refusal should name the missing scope, got: %v", err)
	}
	if !strings.Contains(err.Error(), "autonomous") || !strings.Contains(err.Error(), "standing authority") {
		t.Errorf("refusal should name the identity model and its stakes, got: %v", err)
	}
}

// ── Update / delete: required scope derives from the STORED mode ─────────────

func TestUpdateAgentType_GatedByStoredMode(t *testing.T) {
	tests := []struct {
		name        string
		granted     []string
		storedMode  IdentityMode
		wantAllowed bool
	}{
		{"delegated:write may update stored delegated", []string{"agents:types:delegated:write"}, IdentityModeDelegated, true},
		{"delegated:write may not update stored autonomous", []string{"agents:types:delegated:write"}, IdentityModeAutonomous, false},
		{"autonomous:write may update stored autonomous", []string{"agents:types:autonomous:write"}, IdentityModeAutonomous, true},
		{"autonomous:write may not update stored delegated", []string{"agents:types:autonomous:write"}, IdentityModeDelegated, false},
		{"parent types:write may update either", []string{"agents:types:write"}, IdentityModeAutonomous, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, tc.granted...)
			at := seedModeAgentType(t, repo, tc.storedMode)
			ctx := withTenantCtx(context.Background(), "t-1")

			name := "Renamed"
			_, err := svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: at.ID, Name: &name})
			if tc.wantAllowed {
				if err != nil {
					t.Fatalf("expected update to be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIdentityModeWriteForbidden) {
				t.Fatalf("expected ErrIdentityModeWriteForbidden, got %v", err)
			}
		})
	}
}

func TestDeleteAgentType_GatedByStoredMode(t *testing.T) {
	tests := []struct {
		name        string
		granted     []string
		storedMode  IdentityMode
		wantAllowed bool
	}{
		{"delegated:write may delete stored delegated", []string{"agents:types:delegated:write"}, IdentityModeDelegated, true},
		{"delegated:write may not delete stored autonomous", []string{"agents:types:delegated:write"}, IdentityModeAutonomous, false},
		{"parent types:write may delete either", []string{"agents:types:write"}, IdentityModeDelegated, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, tc.granted...)
			at := seedModeAgentType(t, repo, tc.storedMode)
			ctx := withTenantCtx(context.Background(), "t-1")

			err := svc.DeleteAgentType(ctx, at.ID)
			if tc.wantAllowed {
				if err != nil {
					t.Fatalf("expected delete to be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIdentityModeWriteForbidden) {
				t.Fatalf("expected ErrIdentityModeWriteForbidden, got %v", err)
			}
		})
	}
}

// ── Drafts: any mode-qualified write scope confers draft authority ───────────

func TestDraftWrites_RequireAnyModeWriteScope(t *testing.T) {
	tests := []struct {
		name        string
		granted     []string
		wantAllowed bool
	}{
		{"delegated:write alone may draft", []string{"agents:types:delegated:write"}, true},
		{"autonomous:write alone may draft", []string{"agents:types:autonomous:write"}, true},
		{"parent types:write may draft", []string{"agents:types:write"}, true},
		{"read-only may not draft", []string{"agents:types:read"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := newMockDraftRepository()
			authz := NewAuthz(&scopedEvaluator{granted: tc.granted})
			svc := NewDraftService(repo, authz, DraftServiceConfig{}, logging.NewLoggerWithOutput(io.Discard))
			ctx := withUserCtx(context.Background(), "t-1", "user-1")

			_, err := svc.CreateDraft(ctx, CreateDraftRequest{})
			if tc.wantAllowed {
				if err != nil {
					t.Fatalf("expected draft create to be allowed, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrIdentityModeWriteForbidden) {
				t.Fatalf("expected ErrIdentityModeWriteForbidden, got %v", err)
			}
		})
	}
}
