package agents

import (
	"context"
	"errors"
	"testing"
)

// Rows default to source api. A declared row keeps its source and resource UID, and the declared
// lists leave out API and soft-deleted rows.
func TestPostgres_DeclaredSource(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()

	apiEnv := testEnvironment(t, r, tenant, "api")
	declaredEnv := Environment{ID: "", TenantID: tenant, Slug: "declared", Name: "Declared", SandboxImage: "kora-base",
		Size: EnvironmentSize{CPU: "1", Memory: "1Gi", Disk: "1Gi"}, EgressHosts: []string{}, Source: SourceCRD, ResourceUID: "uid-env", ResourceGeneration: 3}
	if err := r.CreateEnvironment(ctx, declaredEnv); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	got, err := r.GetEnvironmentBySlug(ctx, tenant, "declared")
	if err != nil {
		t.Fatalf("GetEnvironmentBySlug: %v", err)
	}
	if got.Source != SourceCRD || got.ResourceUID != "uid-env" || got.ResourceGeneration != 3 {
		t.Errorf("declared environment source = %q uid = %q", got.Source, got.ResourceUID)
	}
	if e, _ := r.GetEnvironment(ctx, tenant, apiEnv.ID); e.Source != SourceAPI || e.ResourceUID != "" {
		t.Errorf("API environment source = %q uid = %q", e.Source, e.ResourceUID)
	}
	if _, err := r.GetEnvironmentBySlug(ctx, tenant, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEnvironmentBySlug(missing) = %v, want ErrNotFound", err)
	}
	got.ResourceUID, got.ResourceGeneration = "uid-env-2", 4
	if err := r.UpdateEnvironment(ctx, *got); err != nil {
		t.Fatalf("UpdateEnvironment: %v", err)
	}
	envs, err := r.ListDeclaredEnvironments(ctx, tenant)
	if err != nil {
		t.Fatalf("ListDeclaredEnvironments: %v", err)
	}
	if len(envs) != 1 || envs[0].ID != got.ID || envs[0].ResourceUID != "uid-env-2" || envs[0].ResourceGeneration != 4 {
		t.Errorf("declared environments = %+v, want the declared one with its new UID and generation", envs)
	}

	apiAgent := testAgentType(tenant)
	declared := testAgentType(tenant, AgentEnvironment{EnvironmentID: got.ID, IsDefault: true})
	declared.Source, declared.ResourceUID, declared.ResourceGeneration = SourceCRD, "uid-agent", 2
	gone := testAgentType(tenant)
	gone.Source, gone.ResourceUID = SourceCRD, "uid-gone"
	for _, at := range []AgentType{apiAgent, declared, gone} {
		if err := r.CreateAgentType(ctx, at); err != nil {
			t.Fatalf("CreateAgentType: %v", err)
		}
	}
	if err := r.SoftDeleteAgentType(ctx, gone.ID); err != nil {
		t.Fatalf("SoftDeleteAgentType: %v", err)
	}
	at, err := r.GetAgentTypeBySlug(ctx, tenant, declared.Slug)
	if err != nil {
		t.Fatalf("GetAgentTypeBySlug: %v", err)
	}
	if at.Source != SourceCRD || at.ResourceUID != "uid-agent" || at.ResourceGeneration != 2 {
		t.Errorf("declared agent source = %q uid = %q", at.Source, at.ResourceUID)
	}
	if a, _ := r.GetAgentType(ctx, apiAgent.ID); a.Source != SourceAPI || a.ResourceUID != "" {
		t.Errorf("API agent source = %q uid = %q", a.Source, a.ResourceUID)
	}
	at.ResourceGeneration = 5
	if err := r.UpdateAgentType(ctx, *at); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	ats, err := r.ListDeclaredAgentTypes(ctx, tenant)
	if err != nil {
		t.Fatalf("ListDeclaredAgentTypes: %v", err)
	}
	if len(ats) != 1 || ats[0].ID != declared.ID || ats[0].ResourceGeneration != 5 {
		t.Errorf("declared agents = %+v, want only the live declared one at generation 5", ats)
	}
}
