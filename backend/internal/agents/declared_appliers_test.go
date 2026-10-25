package agents

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"github.com/runkora/kora/backend/internal/platform/logging"
	si "github.com/runkora/kora/backend/internal/platform/sandboximages/v1alpha1"
)

const applierTenant = "acme"

func item(t *testing.T, name, uid string, generation int64, spec any, refs map[string]string) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: applierTenant, Namespace: "kora-tenant-acme", Name: name, UID: uid, Generation: generation, Spec: raw, Refs: refs}
}

// newEnvironmentApplier runs the applier on the real environment service in a locked tenant,
// so every write it makes passes as a declared write.
func newEnvironmentApplier() (*EnvironmentApplier, *fakeEnvStore) {
	k8s := &mockK8s{sandboxImages: map[string]*si.SandboxImage{
		"kora-go":   validSandboxImage("kora-go", "ghcr.io/acme/go:1", "sha256:go"),
		si.BaseName: validSandboxImage(si.BaseName, "ghcr.io/runkora/base:1", "sha256:b"),
	}}
	store := &fakeEnvStore{envs: map[string]Environment{}}
	svc := NewEnvironmentService(store, newMockRepository(), k8s, k8s, fakeWriteChecker{}, lockedTenants{applierTenant: true}, EnvironmentServiceConfig{
		TenantNamespaceFn: func(id string) string { return "kora-tenant-" + id },
		EnvironmentSizes:  testSizes,
	}, logging.NewLoggerWithOutput(io.Discard))
	return NewEnvironmentApplier(store, svc), store
}

func envSpec() v1alpha1.EnvironmentSpec {
	return v1alpha1.EnvironmentSpec{DisplayName: "Go build", SandboxImage: "kora-go",
		Connectors: []v1alpha1.Connector{{Connection: "github", Actions: []string{"read"}}}}
}

func TestEnvironmentApplier(t *testing.T) {
	ctx := context.Background()
	a, store := newEnvironmentApplier()

	res, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, envSpec(), nil))
	if err != nil || res.Outcome != declarative.OutcomeProjected || !res.Ready {
		t.Fatalf("create = %+v, %v", res, err)
	}
	env := store.envs[res.ID]
	if env.Slug != "go-build" || env.Name != "Go build" || env.SandboxImage != "kora-go" || len(env.Connectors) != 1 ||
		env.Source != declarative.SourceCRD || env.ResourceUID != "uid-1" || env.ResourceGeneration != 1 {
		t.Errorf("created row = %+v", env)
	}

	if _, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, envSpec(), nil)); err != nil || store.writes != 1 {
		t.Errorf("an unchanged generation was written again: %d writes, %v", store.writes, err)
	}

	spec := envSpec()
	spec.DisplayName = "Go builds"
	if got, err := a.Apply(ctx, item(t, "go-build", "uid-1", 2, spec, nil)); err != nil || got.ID != res.ID || store.envs[res.ID].Name != "Go builds" {
		t.Fatalf("update = %+v, %v, row %+v", got, err, store.envs[res.ID])
	}

	// A recreated resource takes over the row and keeps its id.
	if got, err := a.Apply(ctx, item(t, "go-build", "uid-2", 1, spec, nil)); err != nil || got.ID != res.ID || store.envs[res.ID].ResourceUID != "uid-2" {
		t.Fatalf("takeover = %+v, %v, row %+v", got, err, store.envs[res.ID])
	}

	if got, _ := a.List(ctx, applierTenant); !slices.Equal(got, []declarative.Declared{{Name: "go-build", UID: "uid-2"}}) {
		t.Errorf("list = %+v", got)
	}
}

func TestEnvironmentApplierRefusals(t *testing.T) {
	ctx := context.Background()
	a, store := newEnvironmentApplier()
	store.envs["e1"] = Environment{ID: "e1", TenantID: applierTenant, Slug: "dash", Source: declarative.SourceAPI}

	if res, _ := a.Apply(ctx, item(t, "dash", "uid-1", 1, envSpec(), nil)); res.Outcome != declarative.OutcomeNameConflict || store.writes != 0 {
		t.Errorf("conflict = %+v, writes %d", res, store.writes)
	}
	spec := envSpec()
	spec.Size.Memory = "64Gi"
	res, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, spec, nil))
	if err != nil || res.Outcome != declarative.OutcomeInvalid || res.Message == "" {
		t.Errorf("invalid = %+v, %v", res, err)
	}
	if res, _ := a.Apply(ctx, declarative.Item{TenantID: applierTenant, Name: "x", UID: "u", Spec: json.RawMessage(`[]`)}); res.Outcome != declarative.OutcomeInvalid {
		t.Errorf("undecodable spec = %+v", res)
	}
}

func TestEnvironmentApplierDelete(t *testing.T) {
	ctx := context.Background()
	a, store := newEnvironmentApplier()
	store.envs["env-1"] = Environment{ID: "env-1", TenantID: applierTenant, Slug: "go-build", Source: declarative.SourceCRD}
	store.envs["env-2"] = Environment{ID: "env-2", TenantID: applierTenant, Slug: "dash", Source: declarative.SourceAPI}

	store.deleteErr = ErrEnvironmentInUse
	if err := a.Delete(ctx, applierTenant, "go-build"); !errors.Is(err, ErrEnvironmentInUse) {
		t.Errorf("in-use delete = %v, want ErrEnvironmentInUse for the operator to retry", err)
	}
	store.deleteErr = nil
	for _, name := range []string{"go-build", "dash", "absent"} {
		if err := a.Delete(ctx, applierTenant, name); err != nil {
			t.Errorf("delete %s: %v", name, err)
		}
	}
	if _, ok := store.envs["env-1"]; ok {
		t.Error("the declared environment was kept")
	}
	if _, ok := store.envs["env-2"]; !ok {
		t.Error("the dashboard-built environment was deleted")
	}
}

// slugStore reads agents by slug through the id, since the mock repository's slug index keeps
// the row as created.
type slugStore struct{ *mockRepository }

func (s slugStore) GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error) {
	at, err := s.mockRepository.GetAgentTypeBySlug(ctx, tenantID, slug)
	if err != nil {
		return nil, err
	}
	return s.GetAgentType(ctx, at.ID)
}

func (s slugStore) ListDeclaredAgentTypes(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	ats, err := s.ListAgentTypes(ctx, tenantID)
	var out []declarative.Declared
	for _, at := range ats {
		if at.Source == declarative.SourceCRD {
			out = append(out, declarative.Declared{Name: at.Slug, UID: at.ResourceUID})
		}
	}
	return out, err
}

// countingWriter counts the writes that reach the real service.
type countingWriter struct {
	*AgentTypeService
	writes int
}

func (c *countingWriter) CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error) {
	c.writes++
	return c.AgentTypeService.CreateAgentType(ctx, req)
}

func (c *countingWriter) UpdateAgentType(ctx context.Context, req UpdateAgentTypeRequest) (*AgentType, error) {
	c.writes++
	return c.AgentTypeService.UpdateAgentType(ctx, req)
}

// newAgentApplier runs the applier on the real agent type service under an evaluator that
// refuses every caller, in a locked tenant. The repository holds the declared environments
// env-go and env-go-2.
func newAgentApplier() (*AgentApplier, *mockRepository, *countingWriter) {
	repo := newMockRepository()
	for _, id := range []string{"env-go", "env-go-2"} {
		repo.envs[id] = &Environment{ID: id, TenantID: applierTenant, Source: declarative.SourceCRD}
	}
	writer := &countingWriter{AgentTypeService: newDenyingAgentTypeService(repo, lockedTenants{applierTenant: true})}
	return NewAgentApplier(slugStore{repo}, writer), repo, writer
}

func agentSpec() v1alpha1.AgentSpec {
	return v1alpha1.AgentSpec{
		DisplayName: "Code reviewer", Framework: "claude-code", IdentityMode: "autonomous",
		Model:        v1alpha1.ModelRef{Family: "anthropic", Name: "sonnet"},
		Environments: []v1alpha1.AgentEnvironment{{Name: "go-build"}},
		MCPTools:     []v1alpha1.MCPTools{{Server: "github", Tools: []string{"create_review", "list_prs"}}},
	}
}

var agentRefs = map[string]string{"environment/go-build": "env-go"}

// waitActive waits for the service's background provisioning to finish, so a later write does
// not race its state change.
func waitActive(t *testing.T, repo *mockRepository, id string) {
	t.Helper()
	waitFor(t, time.Second, func() bool {
		at, err := repo.GetAgentType(context.Background(), id)
		return err == nil && at.State == AgentTypeStateActive
	})
}

func TestAgentApplier(t *testing.T) {
	ctx := context.Background()
	a, repo, writer := newAgentApplier()

	res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
	if err != nil || res.Outcome != declarative.OutcomeProjected || res.ID == "" || res.Ready {
		t.Fatalf("create = %+v, %v; want Projected with the id and not ready while provisioning", res, err)
	}
	at, _ := repo.GetAgentType(ctx, res.ID)
	if at.Slug != "code-reviewer" || at.Name != "Code reviewer" || at.ModelID == nil || *at.ModelID != "anthropic/sonnet" || at.IdentityMode != IdentityModeAutonomous {
		t.Errorf("created row = %+v", at)
	}
	if at.Source != declarative.SourceCRD || at.CreatedBy != "resource:kora-tenant-acme/code-reviewer" || at.ResourceUID != "uid-1" || at.ResourceGeneration != 1 {
		t.Errorf("row = source %q created_by %q uid %q generation %d", at.Source, at.CreatedBy, at.ResourceUID, at.ResourceGeneration)
	}
	if !slices.Equal(at.MCPTools, []string{"mcp:github:create_review:use", "mcp:github:list_prs:use"}) {
		t.Errorf("mcp tools = %v", at.MCPTools)
	}
	if !slices.Equal(at.Environments, []AgentEnvironment{{EnvironmentID: "env-go", IsDefault: true}}) {
		t.Errorf("environments = %+v", at.Environments)
	}

	waitActive(t, repo, res.ID)
	res, err = a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
	if err != nil || !res.Ready || writer.writes != 1 {
		t.Fatalf("re-apply once active = %+v, %v, %d writes; want ready with no new write", res, err, writer.writes)
	}

	// The operator sends a different environment id, and the agent follows it at the same generation.
	refs := map[string]string{"environment/go-build": "env-go-2"}
	if _, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), refs)); err != nil {
		t.Fatal(err)
	}
	spec := agentSpec()
	spec.DisplayName = "Reviewer"
	if _, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 2, spec, refs)); err != nil {
		t.Fatal(err)
	}
	at, _ = repo.GetAgentType(ctx, res.ID)
	if writer.writes != 3 || at.Name != "Reviewer" || at.ResourceGeneration != 2 || at.Environments[0].EnvironmentID != "env-go-2" {
		t.Errorf("after updates: %d writes, row %+v; want one write for the new environment id and one for the new generation", writer.writes, at)
	}

	if got, _ := a.List(ctx, applierTenant); !slices.Equal(got, []declarative.Declared{{Name: "code-reviewer", UID: "uid-1"}}) {
		t.Errorf("list = %+v", got)
	}
}

func TestAgentApplierRecreatedResourceIsANewAgent(t *testing.T) {
	ctx := context.Background()
	a, repo, _ := newAgentApplier()
	old, err := a.Apply(ctx, item(t, "code-reviewer", "uid-old", 1, agentSpec(), agentRefs))
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, repo, old.ID)
	res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-new", 1, agentSpec(), agentRefs))
	if err != nil || res.ID == "" || res.ID == old.ID {
		t.Fatalf("apply = %+v, %v; want a new agent", res, err)
	}
	if _, err := repo.GetAgentType(ctx, old.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("old agent = %v, want it deleted", err)
	}
}

func TestAgentApplierRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("name conflict", func(t *testing.T) {
		a, repo, writer := newAgentApplier()
		if err := repo.CreateAgentType(ctx, AgentType{ID: "a1", TenantID: applierTenant, Slug: "code-reviewer", Source: declarative.SourceAPI}); err != nil {
			t.Fatal(err)
		}
		res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
		if err != nil || res.Outcome != declarative.OutcomeNameConflict || writer.writes != 0 {
			t.Errorf("conflict = %+v, %v, writes %d", res, err, writer.writes)
		}
	})
	t.Run("identity mode change", func(t *testing.T) {
		a, repo, _ := newAgentApplier()
		created, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
		if err != nil {
			t.Fatal(err)
		}
		waitActive(t, repo, created.ID)
		spec := agentSpec()
		spec.IdentityMode = "delegated"
		res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 2, spec, agentRefs))
		if err != nil || res.Outcome != declarative.OutcomeInvalid || res.Message == "" {
			t.Errorf("invalid = %+v, %v", res, err)
		}
	})
	t.Run("dashboard-built environment", func(t *testing.T) {
		a, repo, writer := newAgentApplier()
		repo.envs["env-dash"] = &Environment{ID: "env-dash", TenantID: applierTenant, Source: declarative.SourceAPI}
		res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), map[string]string{"environment/go-build": "env-dash"}))
		if err != nil || res.Outcome != declarative.OutcomePending || writer.writes != 0 {
			t.Errorf("dashboard environment = %+v, %v, writes %d", res, err, writer.writes)
		}
	})
	t.Run("unresolved environment", func(t *testing.T) {
		a, _, writer := newAgentApplier()
		res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), nil))
		if err != nil || res.Outcome != declarative.OutcomePending || writer.writes != 0 {
			t.Errorf("unresolved = %+v, %v, writes %d", res, err, writer.writes)
		}
	})
}

func TestAgentApplierDelete(t *testing.T) {
	ctx := context.Background()
	a, repo, _ := newAgentApplier()
	declared, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
	if err != nil {
		t.Fatal(err)
	}
	waitActive(t, repo, declared.ID)
	if err := repo.CreateAgentType(ctx, AgentType{ID: "a2", TenantID: applierTenant, Slug: "dash", Source: declarative.SourceAPI}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"code-reviewer", "dash", "absent"} {
		if err := a.Delete(ctx, applierTenant, name); err != nil {
			t.Errorf("delete %s: %v", name, err)
		}
	}
	if _, err := repo.GetAgentType(ctx, declared.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("declared agent = %v, want it deleted", err)
	}
	if _, err := repo.GetAgentType(ctx, "a2"); err != nil {
		t.Errorf("dashboard-built agent = %v, want it kept", err)
	}
}
