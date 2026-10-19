package agents

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// heldScopes is a callerScopeSource holding a fixed scope set.
type heldScopes []string

func (h heldScopes) CallerEffectiveScopes(context.Context, string) ([]string, error) {
	return h, nil
}

// broadScopes covers every scope the other agent tests configure.
var broadScopes = heldScopes{
	"agents:read", "agents:write", "connection:read", "connection:write", "connection:use",
	"inference:invoke", "inference:use", "mcp:invoke", "mcp:use", "platform:read", "platform:write",
}

// fakeWriteChecker answers every environment-edit check with err.
type fakeWriteChecker struct{ err error }

func (f fakeWriteChecker) checkEnvironmentEdit(context.Context, AgentType, Environment) error {
	return f.err
}

func TestUncoveredAgentScopes(t *testing.T) {
	tests := []struct {
		name  string
		held  []string
		agent []string
		want  []string
	}{
		{"held exactly", []string{"inference:invoke"}, []string{"inference:invoke"}, nil},
		{"held via parent", []string{"connection:github:read"}, []string{"connection:github:contents:read"}, nil},
		{"not held", []string{"inference:invoke"}, []string{"platform:grants:write"}, []string{"platform:grants:write"}},
		{"connector egress skipped", nil, []string{"egress:api.github.com:connector"}, nil},
		{"own egress needs egress:connect", nil, []string{"egress:connect", "egress:github.com:connect"}, []string{"egress:connect", "egress:github.com:connect"}},
		{"egress:connect covers a host", []string{"egress:connect"}, []string{"egress:connect", "egress:github.com:connect"}, nil},
		{"only uncovered listed", []string{"inference:invoke"}, []string{"inference:invoke", "connection:github:contents:write"}, []string{"connection:github:contents:write"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := uncoveredAgentScopes(tt.held, tt.agent)
			if fmt.Sprint(got) != fmt.Sprint(tt.want) {
				t.Errorf("uncoveredAgentScopes = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCreateAgentType_ScopeCeiling(t *testing.T) {
	writer := []string{"agents:types:write"}
	tests := []struct {
		name      string
		callers   callerScopeSource
		principal token.PrincipalType
		req       CreateAgentTypeRequest
		wantErr   error
	}{
		{
			name:    "autonomous agent with a scope the caller lacks is refused",
			callers: heldScopes{"inference:invoke"},
			req:     CreateAgentTypeRequest{GrantScopes: []string{"platform:grants:write"}},
			wantErr: ErrScopeCeiling,
		},
		{
			name:    "autonomous agent within the caller's scopes is created",
			callers: heldScopes{"inference:invoke", "platform:grants:write"},
			req:     CreateAgentTypeRequest{GrantScopes: []string{"platform:grants:write"}},
		},
		{
			name:    "connector action the caller lacks is refused",
			callers: heldScopes{"inference:invoke", "connection:github:contents:read"},
			req:     CreateAgentTypeRequest{Connectors: []Connector{{Connection: "github", Actions: []string{"contents:write"}}}},
			wantErr: ErrScopeCeiling,
		},
		{
			name:    "open egress the caller lacks is refused",
			callers: heldScopes{"inference:invoke"},
			req:     CreateAgentTypeRequest{Connectivity: &Connectivity{Mode: ConnectivityOpen}},
			wantErr: ErrScopeCeiling,
		},
		{
			name:    "host egress is covered by egress:connect",
			callers: heldScopes{"inference:invoke", "egress:connect"},
			req:     CreateAgentTypeRequest{Connectivity: &Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"github.com"}}},
		},
		{
			name:    "connector egress typed into grant scopes is checked",
			callers: heldScopes{"inference:invoke"},
			req:     CreateAgentTypeRequest{GrantScopes: []string{"egress:evil.example:connector"}},
			wantErr: ErrScopeCeiling,
		},
		{
			name:    "delegated agent is not ceilinged",
			callers: heldScopes{},
			req:     CreateAgentTypeRequest{IdentityMode: IdentityModeDelegated, GrantScopes: []string{"platform:grants:write"}},
		},
		{
			name:      "system caller is not ceilinged",
			principal: token.PrincipalTypeSystem,
			req:       CreateAgentTypeRequest{GrantScopes: []string{"platform:grants:write"}},
		},
		{
			name:    "no caller scope source refuses",
			req:     CreateAgentTypeRequest{},
			wantErr: errNoCallerScopes,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, writer...)
			svc.SetCallerScopes(tc.callers)
			tc.req.Name, tc.req.Slug = "Bot", "bot"
			_, err := svc.CreateAgentType(withPrincipalCtx(context.Background(), "t-1", tc.principal, "user-1"), tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && len(repo.agentTypes) != 0 {
				t.Errorf("refused create stored %d agent types", len(repo.agentTypes))
			}
		})
	}
}

// The refusal names the scopes the caller does not hold.
func TestCreateAgentType_ScopeCeilingNamesScopes(t *testing.T) {
	svc, _ := newModeTestService(t, "agents:types:write")
	svc.SetCallerScopes(heldScopes{"inference:invoke"})
	_, err := svc.CreateAgentType(withTenantCtx(context.Background(), "t-1"), CreateAgentTypeRequest{
		Name: "Bot", Slug: "bot", GrantScopes: []string{"platform:grants:write", "inference:invoke"},
	})
	if !errors.Is(err, ErrScopeCeiling) || !strings.Contains(err.Error(), "platform:grants:write") || strings.Contains(err.Error(), "inference:invoke") {
		t.Fatalf("err = %v, want ErrScopeCeiling naming only platform:grants:write", err)
	}
}

// Any update to an autonomous agent needs the caller to hold the whole grant.
func TestUpdateAgentType_ScopeCeiling(t *testing.T) {
	tests := []struct {
		name    string
		mode    IdentityMode
		held    heldScopes
		wantErr error
	}{
		{"autonomous, caller lacks a held scope", IdentityModeAutonomous, heldScopes{}, ErrScopeCeiling},
		{"autonomous, caller holds the grant", IdentityModeAutonomous, heldScopes{"platform:grants:write"}, nil},
		{"delegated is not ceilinged", IdentityModeDelegated, heldScopes{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, "agents:types:write")
			svc.SetCallerScopes(tc.held)
			at := seedModeAgentType(t, repo, tc.mode)
			repo.agentTypes[at.ID].GrantScopes = []string{"platform:grants:write"}
			prompt := "new prompt"
			_, err := svc.UpdateAgentType(withTenantCtx(context.Background(), "t-1"), UpdateAgentTypeRequest{ID: at.ID, SystemPrompt: &prompt})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil && repo.agentTypes[at.ID].SystemPrompt == prompt {
				t.Error("refused update was stored")
			}
		})
	}
}

// An environment edit is checked against each attached agent with the edit applied.
func TestCheckEnvironmentEdit(t *testing.T) {
	stored := Environment{ID: "e1", TenantID: "t-1", Slug: "e1"}
	edited := stored
	edited.Connectors = []Connector{{Connection: "github", Actions: []string{"contents:write"}}}
	tests := []struct {
		name    string
		granted []string
		mode    IdentityMode
		held    heldScopes
		wantErr error
	}{
		{"caller lacks the added connector action", []string{"agents:types:write"}, IdentityModeAutonomous, heldScopes{"connection:github:contents:read"}, ErrScopeCeiling},
		{"caller holds the added connector action", []string{"agents:types:write"}, IdentityModeAutonomous, heldScopes{"connection:github:contents:write"}, nil},
		{"caller lacks the agent's mode write scope", []string{"agents:types:delegated:write"}, IdentityModeAutonomous, heldScopes{"connection:github:contents:write"}, ErrIdentityModeWriteForbidden},
		{"delegated agent needs only its mode write scope", []string{"agents:types:delegated:write"}, IdentityModeDelegated, heldScopes{}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, tc.granted...)
			svc.SetCallerScopes(tc.held)
			svc.SetScopeResolver(NewScopeResolver(repo, nil))
			repo.envs["e1"] = &stored
			at := AgentType{TenantID: "t-1", IdentityMode: tc.mode, Environments: []AgentEnvironment{{EnvironmentID: "e1", IsDefault: true}}}
			err := svc.checkEnvironmentEdit(withTenantCtx(context.Background(), "t-1"), at, edited)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// A refused environment edit is a 403, stores nothing and recompiles no grant.
func TestHandleUpdateEnvironment_RefusedForAttachedAgent(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/kora-sandbox-base:1", "sha256:base"),
	}}
	store := &fakeEnvStore{envs: map[string]Environment{
		"e1": {ID: "e1", TenantID: "t-1", Slug: "e1", Name: "Before", SandboxImage: si.BaseName},
	}}
	agents := newMockRepository()
	seedAgentType(t, agents, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", Environments: []AgentEnvironment{{EnvironmentID: "e1", IsDefault: true}}})
	refusal := fmt.Errorf("%w: connection:github:contents:write", ErrScopeCeiling)
	svc := NewEnvironmentService(store, agents, k8s, k8s, fakeWriteChecker{err: refusal}, EnvironmentServiceConfig{
		TenantNamespaceFn: func(id string) string { return "orbit-tenant-" + id },
		EnvironmentSizes:  testSizes,
	}, logging.NewLoggerWithOutput(io.Discard))
	c := NewController(nil, nil, svc, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))

	req := withTenantRequest(httptest.NewRequest(http.MethodPut, "/api/v1/environments/e1", strings.NewReader(`{"name":"After"}`)), "t-1")
	req.SetPathValue("id", "e1")
	w := httptest.NewRecorder()
	c.handleUpdateEnvironment(w, req)

	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "connection:github:contents:write") || !strings.Contains(w.Body.String(), "bot") {
		t.Errorf("status %d body %s, want 403 naming the agent and the scope", w.Code, w.Body.String())
	}
	if store.envs["e1"].Name != "Before" {
		t.Errorf("stored name = %q, want the edit refused", store.envs["e1"].Name)
	}
}

// The create handler maps a ceiling refusal to a 403.
func TestHandleCreateAgentType_ScopeCeilingIs403(t *testing.T) {
	c, _, _, _ := newTestController(t)
	c.agentTypes.SetCallerScopes(heldScopes{"inference:invoke"})
	w := postJSON(t, c.handleCreateAgentType, map[string]any{
		"name": "Bot", "slug": "bot", "model_id": "anthropic/claude", "grant_scopes": []string{"platform:grants:write"},
	})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "platform:grants:write") {
		t.Errorf("status %d body %s, want 403 naming the scope", w.Code, w.Body.String())
	}
}

// An agent acting under delegation cannot create an autonomous agent.
func TestCreateAgentType_DelegatedCallerRefused(t *testing.T) {
	svc, _ := newModeTestService(t, "agents:types:write")
	svc.SetCallerScopes(broadScopes)
	ctx := policy.WithDelegation(withTenantCtx(context.Background(), "t-1"), &policy.DelegationClaims{Sub: "user-1"})
	_, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{Name: "Bot", Slug: "bot"})
	if !errors.Is(err, ErrScopeCeiling) || !errors.Is(err, errDelegatedCaller) {
		t.Fatalf("err = %v, want the delegated-caller refusal", err)
	}
}

// The update handler maps a ceiling refusal to a 403.
func TestHandleUpdateAgentType_ScopeCeilingIs403(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	c.agentTypes.SetCallerScopes(heldScopes{})
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", GrantScopes: []string{"platform:grants:write"}})
	req := withTenantRequest(httptest.NewRequest(http.MethodPatch, "/api/v1/agent-types/at-1", strings.NewReader(`{"system_prompt":"x"}`)), "t-1")
	req.SetPathValue("id", "at-1")
	w := httptest.NewRecorder()
	c.handleUpdateAgentType(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "platform:grants:write") {
		t.Errorf("status %d body %s, want 403 naming the scope", w.Code, w.Body.String())
	}
}
