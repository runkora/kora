package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
)

// The operator projects Environment and Agent resources through these Appliers, which write
// through the services' declared write path (docs/adr/2026-10-24-declarative-resources.md).

// declaredEnvironmentStore reads the rows the Environment applier projects into.
type declaredEnvironmentStore interface {
	GetEnvironmentBySlug(ctx context.Context, tenantID, slug string) (*Environment, error)
	ListDeclaredEnvironments(ctx context.Context, tenantID string) ([]declarative.Declared, error)
}

// environmentWriter is the environment service's write path.
type environmentWriter interface {
	CreateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error)
	UpdateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error)
	DeleteEnvironment(ctx context.Context, id string) error
}

// declaredAgentStore reads the rows the Agent applier projects into and the environments it attaches.
type declaredAgentStore interface {
	GetAgentTypeBySlug(ctx context.Context, tenantID, slug string) (*AgentType, error)
	ListDeclaredAgentTypes(ctx context.Context, tenantID string) ([]declarative.Declared, error)
	GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error)
}

// agentWriter is the agent type service's write path.
type agentWriter interface {
	CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error)
	UpdateAgentType(ctx context.Context, req UpdateAgentTypeRequest) (*AgentType, error)
	DeleteAgentType(ctx context.Context, id string) error
}

// ── Environment ─────────────────────────────────────────────────────────────

// EnvironmentApplier applies declared environments.
type EnvironmentApplier struct {
	store  declaredEnvironmentStore
	writer environmentWriter
}

// NewEnvironmentApplier returns the declarative.Applier for KindEnvironments.
func NewEnvironmentApplier(store declaredEnvironmentStore, writer environmentWriter) *EnvironmentApplier {
	return &EnvironmentApplier{store: store, writer: writer}
}

// Apply creates or updates the environment named item.Name. A recreated resource takes over the
// row and keeps its id, so attached agents stay attached.
func (a *EnvironmentApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.EnvironmentSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalidResult(fmt.Errorf("decode spec: %w", err)), nil
	}
	row, err := a.store.GetEnvironmentBySlug(ctx, item.TenantID, item.Name)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return declarative.Result{}, fmt.Errorf("get environment %q: %w", item.Name, err)
	}
	if row != nil && row.Source != declarative.SourceCRD {
		return nameConflictResult("environment", item.Name), nil
	}
	if row != nil && row.ResourceUID == item.UID && row.ResourceGeneration == item.Generation {
		return declarative.Result{Outcome: declarative.OutcomeProjected, ID: row.ID, Ready: true}, nil
	}

	ctx = withDeclaredWrite(ctx, item)
	req := environmentRequest(item.Name, spec)
	var env *Environment
	if row == nil {
		env, err = a.writer.CreateEnvironment(ctx, req)
	} else {
		req.ID = row.ID
		env, err = a.writer.UpdateEnvironment(ctx, req)
	}
	switch {
	case errors.Is(err, ErrEnvironmentSlugTaken):
		return nameConflictResult("environment", item.Name), nil
	case errors.Is(err, ErrInvalidEnvironment):
		return invalidResult(err), nil
	case err != nil:
		return declarative.Result{}, fmt.Errorf("apply environment %q: %w", item.Name, err)
	}
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: env.ID, Ready: true}, nil
}

// Delete deletes the declared environment. One still attached to an agent returns an error
// wrapping ErrEnvironmentInUse, and the operator retries.
func (a *EnvironmentApplier) Delete(ctx context.Context, tenantID, name string) error {
	row, err := a.store.GetEnvironmentBySlug(ctx, tenantID, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get environment %q: %w", name, err)
	}
	if row.Source != declarative.SourceCRD {
		return nil
	}
	err = a.writer.DeleteEnvironment(withDeclaredWrite(ctx, declarative.Item{TenantID: tenantID, Name: name}), row.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete environment %q: %w", name, err)
	}
	return nil
}

// List returns the tenant's declared environments.
func (a *EnvironmentApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return a.store.ListDeclaredEnvironments(ctx, tenantID)
}

func environmentRequest(name string, spec v1alpha1.EnvironmentSpec) EnvironmentRequest {
	return EnvironmentRequest{
		Slug:         name,
		Name:         spec.DisplayName,
		Description:  spec.Description,
		SandboxImage: spec.SandboxImage,
		Size:         EnvironmentSize{CPU: spec.Size.CPU, Memory: spec.Size.Memory, Disk: spec.Size.Disk},
		EgressHosts:  spec.EgressHosts,
		Connectors:   connectorsOf(spec.Connectors),
	}
}

// ── Agent ───────────────────────────────────────────────────────────────────

// AgentApplier applies declared agents and provisions them.
type AgentApplier struct {
	store  declaredAgentStore
	writer agentWriter
}

// NewAgentApplier returns the declarative.Applier for KindAgents.
func NewAgentApplier(store declaredAgentStore, writer agentWriter) *AgentApplier {
	return &AgentApplier{store: store, writer: writer}
}

// Apply creates or updates the agent named item.Name. A recreated resource is a new agent with a
// new id. Ready is false until the agent's row is active.
func (a *AgentApplier) Apply(ctx context.Context, item declarative.Item) (declarative.Result, error) {
	var spec v1alpha1.AgentSpec
	if err := json.Unmarshal(item.Spec, &spec); err != nil {
		return invalidResult(fmt.Errorf("decode spec: %w", err)), nil
	}
	envs, err := agentEnvironments(spec, item.Refs)
	if err == nil {
		err = a.checkDeclaredEnvironments(ctx, item.TenantID, envs)
	}
	if errors.Is(err, errEnvironmentUnresolved) {
		return declarative.Result{Outcome: declarative.OutcomePending, Message: err.Error()}, nil
	}
	if err != nil {
		return declarative.Result{}, err
	}
	ctx = withDeclaredWrite(ctx, item)
	row, conflict, err := a.currentRow(ctx, item)
	if err != nil {
		return declarative.Result{}, err
	}
	if conflict {
		return nameConflictResult("agent", item.Name), nil
	}
	if row != nil && row.ResourceGeneration == item.Generation && slices.Equal(row.Environments, envs) {
		return agentResult(row), nil
	}

	create := agentCreateRequest(item.Name, spec, envs)
	var at *AgentType
	if row == nil {
		at, err = a.writer.CreateAgentType(ctx, create)
	} else {
		at, err = a.writer.UpdateAgentType(ctx, agentUpdateRequest(row.ID, create))
	}
	switch {
	case errors.Is(err, ErrSlugTaken):
		return nameConflictResult("agent", item.Name), nil
	case invalidAgentTypeField(err), errors.Is(err, ErrIdentityModeImmutable):
		return invalidResult(err), nil
	case err != nil:
		return declarative.Result{}, fmt.Errorf("apply agent %q: %w", item.Name, err)
	}
	return agentResult(at), nil
}

// currentRow returns the item's declared row, or nil when it has none. conflict reports a
// dashboard-built agent with the slug. The row of a deleted and recreated resource is deleted
// first: a recreated agent is a new agent with a new id.
func (a *AgentApplier) currentRow(ctx context.Context, item declarative.Item) (row *AgentType, conflict bool, err error) {
	row, err = a.store.GetAgentTypeBySlug(ctx, item.TenantID, item.Name)
	switch {
	case errors.Is(err, ErrNotFound):
		return nil, false, nil
	case err != nil:
		return nil, false, fmt.Errorf("get agent type %q: %w", item.Name, err)
	case row.Source != declarative.SourceCRD:
		return nil, true, nil
	case row.ResourceUID != item.UID:
		if err = a.writer.DeleteAgentType(ctx, row.ID); err != nil {
			return nil, false, fmt.Errorf("delete agent type of a replaced resource: %w", err)
		}
		return nil, false, nil
	}
	return row, false, nil
}

// Delete soft-deletes the declared agent.
func (a *AgentApplier) Delete(ctx context.Context, tenantID, name string) error {
	row, err := a.store.GetAgentTypeBySlug(ctx, tenantID, name)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get agent type %q: %w", name, err)
	}
	if row.Source != declarative.SourceCRD {
		return nil
	}
	err = a.writer.DeleteAgentType(withDeclaredWrite(ctx, declarative.Item{TenantID: tenantID, Name: name}), row.ID)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return fmt.Errorf("delete agent type %q: %w", name, err)
	}
	return nil
}

// List returns the tenant's live declared agents.
func (a *AgentApplier) List(ctx context.Context, tenantID string) ([]declarative.Declared, error) {
	return a.store.ListDeclaredAgentTypes(ctx, tenantID)
}

// agentResult is Projected once the spec is applied. Ready waits for provisioning to finish.
func agentResult(at *AgentType) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeProjected, ID: at.ID, Ready: at.State == AgentTypeStateActive}
}

// errEnvironmentUnresolved marks an Agent environment the applier cannot attach yet.
var errEnvironmentUnresolved = errors.New("environment not resolved")

// checkDeclaredEnvironments refuses any environment that is not a declared environment of the
// tenant. API environment edits then never reach a declared agent's grant.
func (a *AgentApplier) checkDeclaredEnvironments(ctx context.Context, tenantID string, envs []AgentEnvironment) error {
	for _, e := range envs {
		env, err := a.store.GetEnvironment(ctx, tenantID, e.EnvironmentID)
		if errors.Is(err, ErrNotFound) || (err == nil && env.Source != declarative.SourceCRD) {
			return fmt.Errorf("%w: %q is not a declared environment of this tenant", errEnvironmentUnresolved, e.EnvironmentID)
		}
		if err != nil {
			return fmt.Errorf("get environment %q: %w", e.EnvironmentID, err)
		}
	}
	return nil
}

// agentEnvironments maps the spec's environments to the ids the operator resolved, in the order
// the store returns them: the default first, then by id.
func agentEnvironments(spec v1alpha1.AgentSpec, refs map[string]string) ([]AgentEnvironment, error) {
	envs := []AgentEnvironment{}
	for _, e := range spec.Environments {
		id := refs[declarative.RefEnvironment(e.Name)]
		if id == "" {
			return nil, fmt.Errorf("%w: %q", errEnvironmentUnresolved, e.Name)
		}
		envs = append(envs, AgentEnvironment{EnvironmentID: id, IsDefault: e.Default})
	}
	// The service marks a lone environment as the default; mirror it so a re-read compares equal.
	if len(envs) == 1 {
		envs[0].IsDefault = true
	}
	slices.SortStableFunc(envs, func(x, y AgentEnvironment) int {
		if x.IsDefault != y.IsDefault {
			if x.IsDefault {
				return -1
			}
			return 1
		}
		return strings.Compare(x.EnvironmentID, y.EnvironmentID)
	})
	return envs, nil
}

// agentCreateRequest maps an Agent's spec onto the API's create request.
func agentCreateRequest(name string, spec v1alpha1.AgentSpec, envs []AgentEnvironment) CreateAgentTypeRequest {
	modelID := spec.Model.Family + "/" + spec.Model.Name
	scopes := spec.Scopes
	if scopes == nil {
		scopes = []string{}
	}
	mcpTools := []string{}
	for _, t := range spec.MCPTools {
		for _, tool := range t.Tools {
			mcpTools = append(mcpTools, "mcp:"+t.Server+":"+tool+":use")
		}
	}
	req := CreateAgentTypeRequest{
		Name:                    spec.DisplayName,
		Slug:                    name,
		Description:             spec.Description,
		SystemPrompt:            spec.SystemPrompt,
		Framework:               AgentFramework(spec.Framework),
		IdentityMode:            IdentityMode(spec.IdentityMode),
		GrantScopes:             scopes,
		Connectivity:            &Connectivity{Mode: ConnectivityMode(spec.Connectivity.Mode), Hosts: spec.Connectivity.Hosts},
		Connectors:              connectorsOf(spec.Connectors),
		MCPTools:                mcpTools,
		Environments:            envs,
		MaxConcurrentRunsPerPod: int(spec.MaxConcurrentRunsPerPod),
		ModelID:                 &modelID,
	}
	if g := spec.Guardrails; g != nil {
		req.Guardrails = &Guardrails{
			RepeatedCallWarn:  intPtr(g.RepeatedCallWarn),
			RepeatedCallPause: intPtr(g.RepeatedCallPause),
			WaitRearmWarn:     intPtr(g.WaitRearmWarn),
			WaitRearmPause:    intPtr(g.WaitRearmPause),
		}
	}
	if b := spec.Budget; b != nil {
		req.Budget = &AgentBudget{LimitUSD: b.LimitUSD, ResetCadence: b.ResetCadence, PerRunLimitUSD: b.PerRunLimitUSD}
	}
	return req
}

// agentUpdateRequest replaces every field the spec owns. An unset guardrail or budget clears it,
// and an unset framework or run ceiling keeps the stored value.
func agentUpdateRequest(id string, c CreateAgentTypeRequest) UpdateAgentTypeRequest {
	req := UpdateAgentTypeRequest{
		ID:           id,
		Name:         &c.Name,
		Description:  &c.Description,
		SystemPrompt: &c.SystemPrompt,
		ModelID:      c.ModelID,
		Guardrails:   c.Guardrails,
		GrantScopes:  c.GrantScopes,
		Budget:       c.Budget,
		Connectivity: c.Connectivity,
		Connectors:   c.Connectors,
		MCPTools:     c.MCPTools,
		Environments: &c.Environments,
	}
	if req.Guardrails == nil {
		req.Guardrails = &Guardrails{}
	}
	if req.Budget == nil {
		req.Budget = &AgentBudget{}
	}
	if c.Framework != "" {
		req.Framework = &c.Framework
	}
	if c.IdentityMode != "" {
		req.IdentityMode = &c.IdentityMode
	}
	if c.MaxConcurrentRunsPerPod > 0 {
		req.MaxConcurrentRunsPerPod = &c.MaxConcurrentRunsPerPod
	}
	return req
}

func intPtr(v *int32) *int {
	if v == nil {
		return nil
	}
	i := int(*v)
	return &i
}

func connectorsOf(in []v1alpha1.Connector) []Connector {
	out := make([]Connector, 0, len(in))
	for _, c := range in {
		out = append(out, Connector{Connection: c.Connection, Actions: c.Actions, Resources: c.Resources})
	}
	return out
}

func invalidResult(err error) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeInvalid, Message: err.Error()}
}

func nameConflictResult(kind, name string) declarative.Result {
	return declarative.Result{Outcome: declarative.OutcomeNameConflict, Message: fmt.Sprintf("an %s named %q was created through the API", kind, name)}
}
