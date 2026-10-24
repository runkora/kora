package agents

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

// Rows default to source api. A declared row keeps its source and resource UID, and the declared
// lists leave out API and soft-deleted rows.
func TestPostgres_DeclaredSource(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()

	apiEnv := testEnvironment(t, r, tenant, "api")
	declaredEnv := Environment{ID: "", TenantID: tenant, Slug: "declared", Name: "Declared", SandboxImage: "kora-base",
		Size: EnvironmentSize{CPU: "1", Memory: "1Gi", Disk: "1Gi"}, EgressHosts: []string{}, Source: declarative.SourceCRD, ResourceUID: "uid-env", ResourceGeneration: 3}
	if err := r.CreateEnvironment(ctx, declaredEnv); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	got, err := r.GetEnvironmentBySlug(ctx, tenant, "declared")
	if err != nil {
		t.Fatalf("GetEnvironmentBySlug: %v", err)
	}
	if got.Source != declarative.SourceCRD || got.ResourceUID != "uid-env" || got.ResourceGeneration != 3 {
		t.Errorf("declared environment source = %q uid = %q", got.Source, got.ResourceUID)
	}
	if e, _ := r.GetEnvironment(ctx, tenant, apiEnv.ID); e.Source != declarative.SourceAPI || e.ResourceUID != "" {
		t.Errorf("API environment source = %q uid = %q", e.Source, e.ResourceUID)
	}
	if _, err := r.GetEnvironmentBySlug(ctx, tenant, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetEnvironmentBySlug(missing) = %v, want ErrNotFound", err)
	}
	got.ResourceUID, got.ResourceGeneration = "uid-env-2", 4
	if err := r.UpdateEnvironment(ctx, *got); err != nil {
		t.Fatalf("UpdateEnvironment: %v", err)
	}
	if e, _ := r.GetEnvironment(ctx, tenant, got.ID); e.ResourceUID != "uid-env-2" || e.ResourceGeneration != 4 {
		t.Errorf("updated environment uid = %q generation = %d", e.ResourceUID, e.ResourceGeneration)
	}
	envs, err := r.ListDeclaredEnvironments(ctx, tenant)
	if err != nil {
		t.Fatalf("ListDeclaredEnvironments: %v", err)
	}
	if !slices.Equal(envs, []declarative.Declared{{Name: "declared", UID: "uid-env-2"}}) {
		t.Errorf("declared environments = %+v, want the declared one with its new UID", envs)
	}

	apiAgent := testAgentType(tenant)
	declared := testAgentType(tenant, AgentEnvironment{EnvironmentID: got.ID, IsDefault: true})
	declared.Source, declared.ResourceUID, declared.ResourceGeneration = declarative.SourceCRD, "uid-agent", 2
	gone := testAgentType(tenant)
	gone.Source, gone.ResourceUID = declarative.SourceCRD, "uid-gone"
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
	if at.Source != declarative.SourceCRD || at.ResourceUID != "uid-agent" || at.ResourceGeneration != 2 {
		t.Errorf("declared agent source = %q uid = %q", at.Source, at.ResourceUID)
	}
	if a, _ := r.GetAgentType(ctx, apiAgent.ID); a.Source != declarative.SourceAPI || a.ResourceUID != "" {
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
	if !slices.Equal(ats, []declarative.Declared{{Name: declared.Slug, UID: "uid-agent"}}) {
		t.Errorf("declared agents = %+v, want only the live declared one", ats)
	}
	if a, _ := r.GetAgentType(ctx, declared.ID); a.ResourceGeneration != 5 {
		t.Errorf("updated agent generation = %d, want 5", a.ResourceGeneration)
	}
}
