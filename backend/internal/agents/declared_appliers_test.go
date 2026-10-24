package agents

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

const applierTenant = "acme"

func tenantNamespace(id string) string { return "kora-tenant-" + id }

// declaredRows is an in-memory store and writer for both appliers. It records the declared
// write context of every write.
type declaredRows struct {
	agents   map[string]*AgentType   // by slug
	envs     map[string]*Environment // by slug
	writes   []declaredWrite
	created  []CreateAgentTypeRequest
	updated  []UpdateAgentTypeRequest
	envReqs  []EnvironmentRequest
	deleted  []string
	inUse    bool
	writeErr error
}

// emptyRows holds no rows.
func emptyRows() *declaredRows {
	return &declaredRows{agents: map[string]*AgentType{}, envs: map[string]*Environment{}}
}

// newDeclaredRows holds the declared environments env-go and env-go-2.
func newDeclaredRows() *declaredRows {
	return &declaredRows{agents: map[string]*AgentType{}, envs: map[string]*Environment{
		"go-build":   {ID: "env-go", Slug: "go-build", Source: SourceCRD, ResourceUID: "uid-env"},
		"go-build-2": {ID: "env-go-2", Slug: "go-build-2", Source: SourceCRD, ResourceUID: "uid-env-2"},
	}}
}

func (d *declaredRows) record(ctx context.Context) declaredWrite {
	w, _ := declaredFromContext(ctx)
	d.writes = append(d.writes, w)
	return w
}

func (d *declaredRows) GetAgentTypeBySlug(_ context.Context, _, slug string) (*AgentType, error) {
	if at, ok := d.agents[slug]; ok {
		cp := *at
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (d *declaredRows) GetEnvironmentBySlug(_ context.Context, _, slug string) (*Environment, error) {
	if e, ok := d.envs[slug]; ok {
		cp := *e
		return &cp, nil
	}
	return nil, ErrNotFound
}

func (d *declaredRows) GetEnvironment(_ context.Context, _, id string) (*Environment, error) {
	for _, e := range d.envs {
		if e.ID == id {
			cp := *e
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (d *declaredRows) ListDeclaredAgentTypes(context.Context, string) ([]AgentType, error) {
	var out []AgentType
	for _, at := range d.agents {
		if at.Source == SourceCRD {
			out = append(out, *at)
		}
	}
	return out, nil
}

func (d *declaredRows) ListDeclaredEnvironments(context.Context, string) ([]Environment, error) {
	var out []Environment
	for _, e := range d.envs {
		if e.Source == SourceCRD {
			out = append(out, *e)
		}
	}
	return out, nil
}

func (d *declaredRows) CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error) {
	w := d.record(ctx)
	d.created = append(d.created, req)
	if d.writeErr != nil {
		return nil, d.writeErr
	}
	at := &AgentType{ID: "agent-" + w.resourceUID, Slug: req.Slug, Source: SourceCRD, ResourceUID: w.resourceUID,
		ResourceGeneration: w.generation, State: AgentTypeStateProvisioning, Environments: req.Environments}
	d.agents[req.Slug] = at
	return at, nil
}

func (d *declaredRows) UpdateAgentType(ctx context.Context, req UpdateAgentTypeRequest) (*AgentType, error) {
	w := d.record(ctx)
	d.updated = append(d.updated, req)
	if d.writeErr != nil {
		return nil, d.writeErr
	}
	for _, at := range d.agents {
		if at.ID == req.ID {
			at.Environments, at.ResourceGeneration = *req.Environments, w.generation
			cp := *at
			return &cp, nil
		}
	}
	return nil, ErrNotFound
}

func (d *declaredRows) DeleteAgentType(ctx context.Context, id string) error {
	d.record(ctx)
	d.deleted = append(d.deleted, id)
	for slug, at := range d.agents {
		if at.ID == id {
			delete(d.agents, slug)
		}
	}
	return nil
}

func (d *declaredRows) CreateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error) {
	w := d.record(ctx)
	d.envReqs = append(d.envReqs, req)
	if d.writeErr != nil {
		return nil, d.writeErr
	}
	e := &Environment{ID: "env-" + req.Slug, Slug: req.Slug, Source: SourceCRD, ResourceUID: w.resourceUID, ResourceGeneration: w.generation}
	d.envs[req.Slug] = e
	return e, nil
}

func (d *declaredRows) UpdateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error) {
	w := d.record(ctx)
	d.envReqs = append(d.envReqs, req)
	if d.writeErr != nil {
		return nil, d.writeErr
	}
	e := d.envs[req.Slug]
	e.ResourceUID, e.ResourceGeneration = w.resourceUID, w.generation
	return e, nil
}

func (d *declaredRows) DeleteEnvironment(ctx context.Context, id string) error {
	d.record(ctx)
	if d.inUse {
		return ErrEnvironmentInUse
	}
	d.deleted = append(d.deleted, id)
	for slug, e := range d.envs {
		if e.ID == id {
			delete(d.envs, slug)
		}
	}
	return nil
}

func item(t *testing.T, name, uid string, generation int64, spec any, refs map[string]string) declarative.Item {
	t.Helper()
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	return declarative.Item{TenantID: applierTenant, Name: name, UID: uid, Generation: generation, Spec: raw, Refs: refs}
}

func envSpec() v1alpha1.EnvironmentSpec {
	return v1alpha1.EnvironmentSpec{DisplayName: "Go build", SandboxImage: "kora-go",
		Connectors: []v1alpha1.Connector{{Connection: "github", Actions: []string{"read"}}}}
}

func TestEnvironmentApplier(t *testing.T) {
	ctx := context.Background()
	rows := emptyRows()
	a := NewEnvironmentApplier(rows, rows, tenantNamespace)

	res, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, envSpec(), nil))
	if err != nil || res != (declarative.Result{Outcome: declarative.OutcomeProjected, ID: "env-go-build", Ready: true}) {
		t.Fatalf("create = %+v, %v", res, err)
	}
	req := rows.envReqs[0]
	if req.Slug != "go-build" || req.Name != "Go build" || req.SandboxImage != "kora-go" || req.ID != "" || len(req.Connectors) != 1 {
		t.Errorf("create request = %+v", req)
	}
	if w := rows.writes[0]; w != (declaredWrite{tenantID: applierTenant, createdBy: "resource:kora-tenant-acme/go-build", resourceUID: "uid-1", generation: 1}) {
		t.Errorf("declared write = %+v", w)
	}

	if _, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, envSpec(), nil)); err != nil || len(rows.envReqs) != 1 {
		t.Errorf("an unchanged generation was written again: %d writes, %v", len(rows.envReqs), err)
	}

	spec := envSpec()
	spec.DisplayName = "Go builds"
	if res, err := a.Apply(ctx, item(t, "go-build", "uid-1", 2, spec, nil)); err != nil || res.ID != "env-go-build" {
		t.Fatalf("update = %+v, %v", res, err)
	}
	if req := rows.envReqs[1]; req.ID != "env-go-build" || req.Name != "Go builds" {
		t.Errorf("update request = %+v", req)
	}

	// A recreated resource takes over the row and keeps its id.
	if res, err := a.Apply(ctx, item(t, "go-build", "uid-2", 1, spec, nil)); err != nil || res.ID != "env-go-build" {
		t.Fatalf("takeover = %+v, %v", res, err)
	}
	if rows.envs["go-build"].ResourceUID != "uid-2" || len(rows.deleted) != 0 {
		t.Errorf("takeover left uid %q, deleted %v", rows.envs["go-build"].ResourceUID, rows.deleted)
	}

	if got, _ := a.List(ctx, applierTenant); !slices.Equal(got, []declarative.Declared{{Name: "go-build", UID: "uid-2"}}) {
		t.Errorf("list = %+v", got)
	}
}

func TestEnvironmentApplierRefusals(t *testing.T) {
	ctx := context.Background()
	rows := emptyRows()
	rows.envs["dash"] = &Environment{ID: "e1", Slug: "dash", Source: SourceAPI}
	a := NewEnvironmentApplier(rows, rows, tenantNamespace)

	if res, _ := a.Apply(ctx, item(t, "dash", "uid-1", 1, envSpec(), nil)); res.Outcome != declarative.OutcomeNameConflict || len(rows.writes) != 0 {
		t.Errorf("conflict = %+v, writes %d", res, len(rows.writes))
	}
	rows.writeErr = errors.Join(ErrInvalidEnvironment, errors.New("size.cpu is above the maximum"))
	res, err := a.Apply(ctx, item(t, "go-build", "uid-1", 1, envSpec(), nil))
	if err != nil || res.Outcome != declarative.OutcomeInvalid || res.Message == "" {
		t.Errorf("invalid = %+v, %v", res, err)
	}
	if res, _ := a.Apply(ctx, declarative.Item{TenantID: applierTenant, Name: "x", UID: "u", Spec: json.RawMessage(`[]`)}); res.Outcome != declarative.OutcomeInvalid {
		t.Errorf("undecodable spec = %+v", res)
	}
}

func TestEnvironmentApplierDelete(t *testing.T) {
	ctx := context.Background()
	rows := emptyRows()
	rows.envs["go-build"] = &Environment{ID: "env-1", Slug: "go-build", Source: SourceCRD}
	rows.envs["dash"] = &Environment{ID: "env-2", Slug: "dash", Source: SourceAPI}
	a := NewEnvironmentApplier(rows, rows, tenantNamespace)

	rows.inUse = true
	if err := a.Delete(ctx, applierTenant, "go-build"); !errors.Is(err, ErrEnvironmentInUse) {
		t.Errorf("in-use delete = %v, want ErrEnvironmentInUse for the operator to retry", err)
	}
	rows.inUse = false
	for _, name := range []string{"go-build", "dash", "absent"} {
		if err := a.Delete(ctx, applierTenant, name); err != nil {
			t.Errorf("delete %s: %v", name, err)
		}
	}
	if !slices.Equal(rows.deleted, []string{"env-1"}) || rows.envs["dash"] == nil {
		t.Errorf("deleted = %v, want only the declared row", rows.deleted)
	}
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

func TestAgentApplier(t *testing.T) {
	ctx := context.Background()
	rows := newDeclaredRows()
	a := NewAgentApplier(rows, rows, tenantNamespace)

	res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
	if err != nil || res.Outcome != declarative.OutcomePending || res.ID != "agent-uid-1" || res.Ready {
		t.Fatalf("create = %+v, %v; want Pending while provisioning", res, err)
	}
	req := rows.created[0]
	if req.Slug != "code-reviewer" || req.Name != "Code reviewer" || *req.ModelID != "anthropic/sonnet" || req.IdentityMode != IdentityModeAutonomous {
		t.Errorf("create request = %+v", req)
	}
	if !slices.Equal(req.MCPTools, []string{"mcp:github:create_review:use", "mcp:github:list_prs:use"}) {
		t.Errorf("mcp tools = %v", req.MCPTools)
	}
	if !slices.Equal(req.Environments, []AgentEnvironment{{EnvironmentID: "env-go", IsDefault: true}}) {
		t.Errorf("environments = %+v", req.Environments)
	}
	if w := rows.writes[0]; w != (declaredWrite{tenantID: applierTenant, createdBy: "resource:kora-tenant-acme/code-reviewer", resourceUID: "uid-1", generation: 1}) {
		t.Errorf("declared write = %+v", w)
	}

	rows.agents["code-reviewer"].State = AgentTypeStateActive
	res, err = a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
	if err != nil || res != (declarative.Result{Outcome: declarative.OutcomeProjected, ID: "agent-uid-1", Ready: true}) {
		t.Fatalf("re-apply once active = %+v, %v", res, err)
	}
	if len(rows.writes) != 1 {
		t.Errorf("an unchanged agent was written again: %d writes", len(rows.writes))
	}

	// A recreated Environment gives a new id, and the agent follows it at the same generation.
	if _, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), map[string]string{"environment/go-build": "env-go-2"})); err != nil {
		t.Fatal(err)
	}
	spec := agentSpec()
	spec.DisplayName = "Reviewer"
	if _, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 2, spec, map[string]string{"environment/go-build": "env-go-2"})); err != nil {
		t.Fatal(err)
	}
	if len(rows.updated) != 2 {
		t.Fatalf("updates = %d, want one for the new environment id and one for the new generation", len(rows.updated))
	}
	u := rows.updated[1]
	if u.ID != "agent-uid-1" || *u.Name != "Reviewer" || *u.Budget != (AgentBudget{}) || u.Guardrails == nil || *u.IdentityMode != IdentityModeAutonomous {
		t.Errorf("update request = %+v", u)
	}

	if got, _ := a.List(ctx, applierTenant); !slices.Equal(got, []declarative.Declared{{Name: "code-reviewer", UID: "uid-1"}}) {
		t.Errorf("list = %+v", got)
	}
}

func TestAgentApplierRecreatedResourceIsANewAgent(t *testing.T) {
	rows := newDeclaredRows()
	rows.agents["code-reviewer"] = &AgentType{ID: "old", Slug: "code-reviewer", Source: SourceCRD, ResourceUID: "uid-old", ResourceGeneration: 1, State: AgentTypeStateActive}
	a := NewAgentApplier(rows, rows, tenantNamespace)
	res, err := a.Apply(context.Background(), item(t, "code-reviewer", "uid-new", 1, agentSpec(), agentRefs))
	if err != nil || res.ID != "agent-uid-new" {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if !slices.Equal(rows.deleted, []string{"old"}) || len(rows.created) != 1 {
		t.Errorf("deleted = %v, created = %d; want the old agent soft-deleted and a new one created", rows.deleted, len(rows.created))
	}
}

func TestAgentApplierRefusals(t *testing.T) {
	ctx := context.Background()
	t.Run("name conflict", func(t *testing.T) {
		rows := newDeclaredRows()
		rows.agents["code-reviewer"] = &AgentType{ID: "a1", Slug: "code-reviewer", Source: SourceAPI}
		res, err := NewAgentApplier(rows, rows, tenantNamespace).Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs))
		if err != nil || res.Outcome != declarative.OutcomeNameConflict || len(rows.writes) != 0 {
			t.Errorf("conflict = %+v, %v, writes %d", res, err, len(rows.writes))
		}
	})
	t.Run("invalid", func(t *testing.T) {
		rows := newDeclaredRows()
		rows.agents["code-reviewer"] = &AgentType{ID: "a1", Slug: "code-reviewer", Source: SourceCRD, ResourceUID: "uid-1", ResourceGeneration: 1}
		rows.writeErr = ErrIdentityModeImmutable
		res, err := NewAgentApplier(rows, rows, tenantNamespace).Apply(ctx, item(t, "code-reviewer", "uid-1", 2, agentSpec(), agentRefs))
		if err != nil || res.Outcome != declarative.OutcomeInvalid || res.Message == "" {
			t.Errorf("invalid = %+v, %v", res, err)
		}
	})
	t.Run("dashboard-built environment", func(t *testing.T) {
		rows := newDeclaredRows()
		rows.envs["dash"] = &Environment{ID: "env-dash", Slug: "dash", Source: SourceAPI}
		res, err := NewAgentApplier(rows, rows, tenantNamespace).Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), map[string]string{"environment/go-build": "env-dash"}))
		if err != nil || res.Outcome != declarative.OutcomePending || len(rows.writes) != 0 {
			t.Errorf("dashboard environment = %+v, %v, writes %d", res, err, len(rows.writes))
		}
	})
	t.Run("unresolved environment", func(t *testing.T) {
		rows := newDeclaredRows()
		res, err := NewAgentApplier(rows, rows, tenantNamespace).Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), nil))
		if err != nil || res.Outcome != declarative.OutcomePending || len(rows.writes) != 0 {
			t.Errorf("unresolved = %+v, %v, writes %d", res, err, len(rows.writes))
		}
	})
}

func TestAgentApplierDelete(t *testing.T) {
	rows := newDeclaredRows()
	rows.agents["code-reviewer"] = &AgentType{ID: "a1", Slug: "code-reviewer", Source: SourceCRD}
	rows.agents["dash"] = &AgentType{ID: "a2", Slug: "dash", Source: SourceAPI}
	a := NewAgentApplier(rows, rows, tenantNamespace)
	for _, name := range []string{"code-reviewer", "dash", "absent"} {
		if err := a.Delete(context.Background(), applierTenant, name); err != nil {
			t.Errorf("delete %s: %v", name, err)
		}
	}
	if !slices.Equal(rows.deleted, []string{"a1"}) || rows.writes[0].tenantID != applierTenant {
		t.Errorf("deleted = %v, writes %+v; want only the declared agent", rows.deleted, rows.writes)
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

func (slugStore) ListDeclaredAgentTypes(context.Context, string) ([]AgentType, error) {
	return nil, nil
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

// Through the real service, under an evaluator that refuses every caller, a re-apply of the
// same generation writes nothing.
func TestAgentApplierWithService(t *testing.T) {
	repo := newMockRepository()
	repo.envs["env-go"] = &Environment{ID: "env-go", TenantID: applierTenant, Source: SourceCRD}
	writer := &countingWriter{AgentTypeService: newDenyingAgentTypeService(repo)}
	a := NewAgentApplier(slugStore{repo}, writer, tenantNamespace)
	ctx := context.Background()
	if res, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs)); err != nil || res.ID == "" {
		t.Fatalf("apply = %+v, %v", res, err)
	}
	if _, err := a.Apply(ctx, item(t, "code-reviewer", "uid-1", 1, agentSpec(), agentRefs)); err != nil {
		t.Fatal(err)
	}
	if writer.writes != 1 {
		t.Errorf("writes = %d, want the create only", writer.writes)
	}
	at, _ := slugStore{repo}.GetAgentTypeBySlug(ctx, applierTenant, "code-reviewer")
	if at.Source != SourceCRD || at.CreatedBy != "resource:kora-tenant-acme/code-reviewer" || at.ResourceGeneration != 1 {
		t.Errorf("row = source %q created_by %q generation %d", at.Source, at.CreatedBy, at.ResourceGeneration)
	}
}
