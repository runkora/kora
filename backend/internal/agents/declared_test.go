package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

// lockedTenants turns API authoring off for the listed tenants.
type lockedTenants map[string]bool

func (l lockedTenants) APIAuthoringEnabled(tenantID string) bool { return !l[tenantID] }

// newDenyingAgentTypeService grants the caller nothing and resolves no caller scopes, so every
// caller check refuses.
func newDenyingAgentTypeService(repo *mockRepository) *AgentTypeService {
	svc := NewAgentTypeService(repo, repo, &mockK8s{}, NewAuthz(&scopedEvaluator{}), AgentTypeServiceConfig{
		MaxConcurrentRunsPerPod: 5,
		TenantNamespaceFn:       func(id string) string { return "kora-tenant-" + id },
	}, logging.NewLoggerWithOutput(io.Discard))
	svc.SetEnvironmentLookup(repo)
	return svc
}

func declaredCtx() context.Context {
	return withDeclaredWrite(context.Background(), "acme", "kora-tenant-acme", "reviewer", "uid-1", 1)
}

func declaredCreateRequest() CreateAgentTypeRequest {
	return CreateAgentTypeRequest{
		Name: "Reviewer", Slug: "reviewer", ModelID: new("anthropic/sonnet"),
		IdentityMode: IdentityModeAutonomous,
		GrantScopes:  []string{"connection:github:contents:read"},
	}
}

// A declared write runs without a caller: no mode write scope, no ceiling, tenant from the registry.
func TestDeclaredAgentWriteSkipsCallerChecks(t *testing.T) {
	repo := newMockRepository()
	svc := newDenyingAgentTypeService(repo)
	svc.SetAuthoringSwitch(lockedTenants{"acme": true})

	if _, err := svc.CreateAgentType(withTenantCtx(context.Background(), "acme"), declaredCreateRequest()); err == nil {
		t.Fatal("an API create in a locked tenant without scopes succeeded")
	}

	at, err := svc.CreateAgentType(declaredCtx(), declaredCreateRequest())
	if err != nil {
		t.Fatalf("declared create: %v", err)
	}
	if at.TenantID != "acme" || at.Source != SourceCRD || at.ResourceUID != "uid-1" || at.CreatedBy != "resource:kora-tenant-acme/reviewer" {
		t.Errorf("declared agent = tenant %q source %q uid %q created_by %q", at.TenantID, at.Source, at.ResourceUID, at.CreatedBy)
	}

	name := "Reviewer 2"
	if _, err := svc.UpdateAgentType(declaredCtx(), UpdateAgentTypeRequest{ID: at.ID, Name: &name}); err != nil {
		t.Fatalf("declared update: %v", err)
	}
	if err := svc.DeleteAgentType(declaredCtx(), at.ID); err != nil {
		t.Fatalf("declared delete: %v", err)
	}
}

func TestAPIWritesToDeclaredAgentsAreRefused(t *testing.T) {
	facade, repo, _, _ := newTestService(t)
	svc := facade.at
	declared := &AgentType{ID: "a1", TenantID: "t-1", Slug: "declared", Source: SourceCRD, State: AgentTypeStateActive}
	repo.agentTypes[declared.ID] = declared
	repo.slugIndex["t-1:declared"] = declared
	ctx := withTenantCtx(context.Background(), "t-1")

	name := "edited"
	if _, err := svc.UpdateAgentType(ctx, UpdateAgentTypeRequest{ID: "a1", Name: &name}); !errors.Is(err, ErrManagedByResource) {
		t.Errorf("update = %v, want ErrManagedByResource", err)
	}
	if err := svc.DeleteAgentType(ctx, "a1"); !errors.Is(err, ErrManagedByResource) {
		t.Errorf("delete = %v, want ErrManagedByResource", err)
	}

	svc.SetAuthoringSwitch(lockedTenants{"t-1": true})
	if _, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{Name: "n", Slug: "fresh", ModelID: new("m/x")}); !errors.Is(err, ErrAPIAuthoringDisabled) {
		t.Errorf("create in a locked tenant = %v, want ErrAPIAuthoringDisabled", err)
	}
}

func TestCallerAccessOnDeclaredAgentsAndLockedTenants(t *testing.T) {
	facade, repo, _, _ := newTestService(t)
	svc := facade.at
	for _, at := range []*AgentType{
		{ID: "api", TenantID: "t-1", Slug: "api", Source: SourceAPI, State: AgentTypeStateActive},
		{ID: "crd", TenantID: "t-1", Slug: "crd", Source: SourceCRD, State: AgentTypeStateActive},
	} {
		repo.agentTypes[at.ID] = at
		repo.slugIndex["t-1:"+at.Slug] = at
	}
	ctx := withTenantCtx(context.Background(), "t-1")
	access := func() map[string][2]bool {
		ats, err := svc.ListAgentTypes(ctx)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string][2]bool{}
		for _, at := range ats {
			out[at.ID] = [2]bool{*at.CanEdit, *at.CanDelete}
			if !*at.CanRun {
				t.Errorf("%s: can_run = false, run starts stay open", at.ID)
			}
		}
		return out
	}
	if got := access(); got["api"] != [2]bool{true, true} || got["crd"] != [2]bool{false, false} {
		t.Errorf("access = %v, want the declared agent read-only", got)
	}
	svc.SetAuthoringSwitch(lockedTenants{"t-1": true})
	if got := access(); got["api"] != [2]bool{false, false} {
		t.Errorf("access in a locked tenant = %v, want every agent read-only", got)
	}
}

func TestDeclaredEnvironmentWrites(t *testing.T) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/base:1", "sha256:b")}}
	store := &fakeEnvStore{envs: map[string]Environment{}}
	repo := newMockRepository()
	// The attached agent's write check denies every caller; a declared edit does not ask it.
	atSvc := newDenyingAgentTypeService(repo)
	svc := NewEnvironmentService(store, repo, k8s, k8s, atSvc, EnvironmentServiceConfig{
		TenantNamespaceFn: func(id string) string { return "kora-tenant-" + id },
		EnvironmentSizes:  testSizes,
	}, logging.NewLoggerWithOutput(io.Discard))
	svc.SetAuthoringSwitch(lockedTenants{"acme": true})

	env, err := svc.CreateEnvironment(declaredCtx(), EnvironmentRequest{Slug: "go", Name: "Go"})
	if err != nil {
		t.Fatalf("declared create: %v", err)
	}
	if env.TenantID != "acme" || env.Source != SourceCRD || env.ResourceUID != "uid-1" {
		t.Errorf("declared environment = %+v", env)
	}
	attached := &AgentType{ID: "a1", TenantID: "acme", Slug: "a", IdentityMode: IdentityModeAutonomous, GrantScopes: []string{"x:y:z"},
		Environments: []AgentEnvironment{{EnvironmentID: env.ID, IsDefault: true}}}
	repo.agentTypes[attached.ID] = attached
	recreated := withDeclaredWrite(context.Background(), "acme", "kora-tenant-acme", "go", "uid-2", 2)
	updated, err := svc.UpdateEnvironment(recreated, EnvironmentRequest{ID: env.ID, Name: "Go 2"})
	if err != nil {
		t.Fatalf("declared update: %v", err)
	}
	if updated.Source != SourceCRD || updated.ResourceUID != "uid-2" {
		t.Errorf("updated environment source %q uid %q", updated.Source, updated.ResourceUID)
	}

	api := withTenantCtx(context.Background(), "acme")
	if _, err := svc.CreateEnvironment(api, EnvironmentRequest{Name: "Other"}); !errors.Is(err, ErrAPIAuthoringDisabled) {
		t.Errorf("API create in a locked tenant = %v", err)
	}
	svc.SetAuthoringSwitch(nil)
	if _, err := svc.UpdateEnvironment(api, EnvironmentRequest{ID: env.ID, Name: "Edited"}); !errors.Is(err, ErrManagedByResource) {
		t.Errorf("API update of a declared environment = %v", err)
	}
	if err := svc.DeleteEnvironment(api, env.ID); !errors.Is(err, ErrManagedByResource) {
		t.Errorf("API delete of a declared environment = %v", err)
	}
}

func TestHandlersRefuseDeclaredWrites(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	declared := &AgentType{ID: "a1", TenantID: defaultTestTenant, Slug: "declared", Source: SourceCRD, State: AgentTypeStateActive}
	repo.agentTypes[declared.ID] = declared
	repo.slugIndex[defaultTestTenant+":declared"] = declared

	req := withTenantRequest(httptest.NewRequest(http.MethodPatch, "/api/v1/agent-types/a1", strings.NewReader(`{"name":"x"}`)), defaultTestTenant)
	req.SetPathValue("id", "a1")
	w := httptest.NewRecorder()
	c.handleUpdateAgentType(w, req)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), `"managed_by_resource"`) {
		t.Errorf("update: %d %s, want 409 managed_by_resource", w.Code, w.Body.String())
	}

	c.agentTypes.SetAuthoringSwitch(lockedTenants{defaultTestTenant: true})
	req = withTenantRequest(httptest.NewRequest(http.MethodDelete, "/api/v1/agent-types/a1", http.NoBody), defaultTestTenant)
	req.SetPathValue("id", "a1")
	w = httptest.NewRecorder()
	c.handleDeleteAgentType(w, req)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `"api_authoring_disabled"`) {
		t.Errorf("delete in a locked tenant: %d %s, want 403 api_authoring_disabled", w.Code, w.Body.String())
	}

	req = withTenantRequest(httptest.NewRequest(http.MethodGet, "/api/v1/agent-types", http.NoBody), defaultTestTenant)
	w = httptest.NewRecorder()
	c.handleListAgentTypes(w, req)
	var list struct {
		AgentTypes []map[string]any `json:"agent_types"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || len(list.AgentTypes) != 1 {
		t.Fatalf("list: %v %s", err, w.Body.String())
	}
	if got := list.AgentTypes[0]; got["source"] != "crd" || got["can_edit"] != false || got["can_delete"] != false {
		t.Errorf("listed agent = source %v can_edit %v can_delete %v", got["source"], got["can_edit"], got["can_delete"])
	}
}
