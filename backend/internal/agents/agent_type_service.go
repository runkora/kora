package agents

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/google/uuid"
)

// AgentTypeServiceConfig holds the runtime parameters used by AgentTypeService.
type AgentTypeServiceConfig struct {
	MaxConcurrentRunsPerPod int
	TenantNamespaceFn       func(tenantID string) string // maps tenant ID → k8s namespace
}

// AgentTypeService manages agent type lifecycle: creation, provisioning, updates, and deletion.
type AgentTypeService struct {
	repo     AgentTypeRepository
	runRepo  RunRepository
	k8s      k8sProvisioner
	authz    *Authz
	cfg      AgentTypeServiceConfig
	logger   logging.Logger
	hook     func()            // called after agent type create/delete; nil if unset
	envs     environmentLookup // validates environment references; nil = skip validation
	resolver *ScopeResolver    // resolves effective access for the read endpoint; nil = degraded
}

// NewAgentTypeService creates an AgentTypeService with the given dependencies.
func NewAgentTypeService(
	repo AgentTypeRepository,
	runRepo RunRepository,
	k8s k8sProvisioner,
	authz *Authz,
	cfg AgentTypeServiceConfig,
	logger logging.Logger,
) *AgentTypeService {
	return &AgentTypeService{
		repo:    repo,
		runRepo: runRepo,
		k8s:     k8s,
		authz:   authz,
		cfg:     cfg,
		logger:  logger,
	}
}

// SetAgentTypeHook installs a callback that is invoked (synchronously, before
// returning) after any agent type is created or deleted. Intended for scope
// re-registration — the hook should launch its own goroutine if needed.
func (s *AgentTypeService) SetAgentTypeHook(fn func()) { s.hook = fn }

// SetEnvironmentLookup installs the environment store used to validate that an
// agent's environment reference exists within its tenant. When unset, the
// reference is not validated here (the database FK still guards bare existence).
func (s *AgentTypeService) SetEnvironmentLookup(envs environmentLookup) { s.envs = envs }

// SetScopeResolver installs the resolver used to compute the effective-access
// read model. When unset, GetEffectiveAccess degrades to the agent's own egress
// posture (no environment or connector resolution).
func (s *AgentTypeService) SetScopeResolver(r *ScopeResolver) { s.resolver = r }

// GetEffectiveAccess returns the resolved access view for an agent type — its
// egress posture, referenced environment, and connector auto-egress combined.
// Tenant-scoped and authz-read-checked like GetAgentType.
func (s *AgentTypeService) GetEffectiveAccess(ctx context.Context, id string) (*EffectiveAccess, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetEffectiveAccess",
		trace.WithAttributes(attribute.String("agent_type_id", id)))
	defer span.End()

	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "get agent type")
		}
		return nil, err
	}
	if at.TenantID != tenantFromContext(ctx) {
		return nil, ErrNotFound
	}
	if err := s.authz.checkAgentType(ctx, *at, "read"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = &ScopeResolver{}
	}
	return resolver.EffectiveAccess(ctx, *at)
}

// PreviewEffectiveAccess resolves the access view for a draft (unsaved) config —
// the live preview as the user edits. Tenant is derived from the verified claims;
// nothing is persisted, and it reveals only what the caller can already resolve
// for their own tenant.
func (s *AgentTypeService) PreviewEffectiveAccess(ctx context.Context, req EffectiveAccessPreviewRequest) (*EffectiveAccess, error) {
	draft := AgentType{
		TenantID:      tenantFromContext(ctx),
		Connectivity:  req.Connectivity,
		Connectors:    req.Connectors,
		EnvironmentID: req.EnvironmentID,
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = &ScopeResolver{}
	}
	return resolver.EffectiveAccess(ctx, draft)
}

// validateAgentTypeUpdate runs the partial-update field validations
// (connectivity posture, connectors, environment reference). Extracted from
// UpdateAgentType to keep that method's cyclomatic complexity in check.
func (s *AgentTypeService) validateAgentTypeUpdate(ctx context.Context, at *AgentType, req UpdateAgentTypeRequest) error {
	if req.Connectivity != nil {
		if vErr := req.Connectivity.Validate(); vErr != nil {
			return fmt.Errorf("%w: %w", ErrInvalidConnectivity, vErr)
		}
	}
	if req.Connectors != nil {
		if vErr := ValidateConnectors(req.Connectors); vErr != nil {
			return fmt.Errorf("%w: %w", ErrInvalidConnector, vErr)
		}
	}
	if req.EnvironmentID != nil {
		var envID *string
		if strings.TrimSpace(*req.EnvironmentID) != "" {
			envID = req.EnvironmentID
		}
		if vErr := s.validateEnvironmentRef(ctx, at.TenantID, envID); vErr != nil {
			return vErr
		}
	}
	return nil
}

// validateEnvironmentRef rejects an agent referencing a non-existent or
// cross-tenant environment. A nil/blank ref or an unwired env lookup is a no-op.
func (s *AgentTypeService) validateEnvironmentRef(ctx context.Context, tenantID string, envID *string) error {
	if envID == nil || strings.TrimSpace(*envID) == "" || s.envs == nil {
		return nil
	}
	if _, err := s.envs.GetEnvironment(ctx, tenantID, *envID); err != nil {
		if errors.Is(err, ErrNotFound) {
			return fmt.Errorf("%w: environment %q not found", ErrInvalidEnvironment, *envID)
		}
		return fmt.Errorf("validate environment reference: %w", err)
	}
	return nil
}

// CreateAgentType registers a new agent type and initiates k8s provisioning.
// Tenant is derived from the verified claims in ctx; the request-body field is
// ignored. Coarse-level authorization (agents:types:write) is enforced by
// ScopeEnforcementMiddleware before this method is called (docs/adr/2026-06-04-platform-scope-enforcement-middleware.md); no
// per-resource authz check is needed here since the resource does not yet exist.
func (s *AgentTypeService) CreateAgentType(ctx context.Context, req CreateAgentTypeRequest) (*AgentType, error) {
	req.TenantID = tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.CreateAgentType",
		trace.WithAttributes(
			attribute.String("tenant_id", req.TenantID),
			attribute.String("slug", req.Slug),
		),
	)
	defer span.End()

	maxRuns := req.MaxConcurrentRunsPerPod
	if maxRuns <= 0 {
		maxRuns = s.cfg.MaxConcurrentRunsPerPod
	}
	framework := req.Framework
	if framework == "" {
		framework = AgentFrameworkClaudeCode
	}

	toolScopes := req.ToolScopes
	if !slices.Contains(toolScopes, "mcp") {
		toolScopes = append(toolScopes, "mcp")
	}

	// grant_scopes is NOT NULL in the database; default a missing slice to an
	// empty one so a request that omits the field still creates cleanly.
	grantScopes := req.GrantScopes
	if grantScopes == nil {
		grantScopes = []string{}
	}
	// Inference grant scopes are derived server-side from the model (single source
	// of truth) unless the caller supplied explicit inference:* scopes. Connector
	// and connectivity scopes are compiled separately by EffectiveGrantScopes. See
	// the 2026-06-30 amendment to agent-identity-and-policy-provisioning.
	grantScopes = applyDerivedInferenceScopes(grantScopes, req.ModelID)

	// Connectivity posture: default to deny-by-default (none) when omitted.
	connectivity := Connectivity{Mode: ConnectivityNone}
	if req.Connectivity != nil {
		connectivity = *req.Connectivity
	}
	if connectivity.Mode == "" {
		connectivity.Mode = ConnectivityNone
	}
	if err := connectivity.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConnectivity, err)
	}

	// Connectors: credential connections this agent may use, and for which
	// actions. Compiles to connection:<name>:<action> grants alongside egress.
	if err := ValidateConnectors(req.Connectors); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidConnector, err)
	}

	// Environment reference: blank = none. Validate it exists in this tenant —
	// the FK guards bare existence but not tenant ownership.
	var environmentID *string
	if req.EnvironmentID != nil && strings.TrimSpace(*req.EnvironmentID) != "" {
		environmentID = req.EnvironmentID
	}
	if err := s.validateEnvironmentRef(ctx, req.TenantID, environmentID); err != nil {
		return nil, err
	}

	at := AgentType{
		ID:                      uuid.NewString(),
		TenantID:                req.TenantID,
		Name:                    req.Name,
		Slug:                    req.Slug,
		Description:             req.Description,
		SystemPrompt:            req.SystemPrompt,
		Framework:               framework,
		ToolScopes:              toolScopes,
		GrantScopes:             grantScopes,
		Connectivity:            connectivity,
		Connectors:              req.Connectors,
		MCPTools:                req.MCPTools,
		EnvironmentID:           environmentID,
		MaxConcurrentRunsPerPod: maxRuns,
		ModelID:                 req.ModelID,
		Budget:                  req.Budget,
		State:                   AgentTypeStateProvisioning,
	}

	if err := s.repo.CreateAgentType(ctx, at); err != nil {
		if errors.Is(err, ErrSlugTaken) {
			return nil, ErrSlugTaken
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "create agent type")
		return nil, fmt.Errorf("create agent type: %w", err)
	}
	span.SetAttributes(attribute.String("agent_type_id", at.ID))

	// Return immediately with state=provisioning; K8s resource creation runs in
	// the background so the caller can redirect to the detail page and poll.
	// Detach from the request context (it is canceled when the response is sent)
	// but carry the trace span so the async work is correlated in traces.
	detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go s.provisionAgentTypeAsync(detachedCtx, at)

	s.logger.Info(ctx, "Agent type created, provisioning in background",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "tenant_id", Value: at.TenantID},
		logging.Field{Key: "slug", Value: at.Slug},
	)
	return &at, nil
}

// provisionAgentTypeAsync runs in a goroutine after CreateAgentType returns.
// On success it transitions the agent type from provisioning → active.
// On failure it deletes the record so the slug is freed for a retry.
func (s *AgentTypeService) provisionAgentTypeAsync(detachedCtx context.Context, at AgentType) {
	ctx, span := agentsTracer.Start(detachedCtx, "agents.provisionAgentTypeAsync",
		trace.WithAttributes(
			attribute.String("agent_type_id", at.ID),
			attribute.String("tenant_id", at.TenantID),
			attribute.String("slug", at.Slug),
		),
	)
	defer span.End()

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if err := s.k8s.ProvisionAgentType(ctx, at, namespace); err != nil {
		// Log the underlying error so operators are not blind — `span.RecordError`
		// alone is invisible until someone opens the trace UI.
		s.logger.Error(ctx, "async provisioning failed",
			logging.Field{Key: "agent_type_id", Value: at.ID},
			logging.Field{Key: "slug", Value: at.Slug},
			logging.Field{Key: "tenant_id", Value: at.TenantID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		// Clean up any k8s resources that were partially created before the
		// failure. ProvisionAgentType is non-atomic — it creates ServiceAccount,
		// StatefulSet, Service, HPA, PDB, AgentGrant, and DestinationRule in
		// sequence, so a failure in step N leaves steps 1..N-1 in the cluster.
		// Without this, a DB row deletion below would strand those resources
		// (the original sales-agent bug). DeprovisionAgentType is idempotent
		// over missing resources.
		if depErr := s.k8s.DeprovisionAgentType(ctx, at, namespace); depErr != nil {
			s.logger.Warn(ctx, "rollback deprovision failed after async provisioning error",
				logging.Field{Key: "agent_type_id", Value: at.ID},
				logging.Field{Key: "error", Value: depErr.Error()},
			)
		}
		if delErr := s.repo.DeleteAgentType(ctx, at.ID); delErr != nil {
			s.logger.Warn(ctx, "rollback delete failed after async provisioning error",
				logging.Field{Key: "agent_type_id", Value: at.ID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "provision k8s resources")
		return
	}
	if err := s.k8s.WaitForStatefulSetReady(ctx, at.Slug, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "wait for statefulset ready")
		s.logger.Warn(ctx, "StatefulSet did not become ready within timeout; agent type left in provisioning state",
			logging.Field{Key: "agent_type_id", Value: at.ID},
			logging.Field{Key: "slug", Value: at.Slug},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	if err := s.repo.SetAgentTypeState(ctx, at.ID, AgentTypeStateProvisioning, AgentTypeStateActive); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "set agent type active")
		return
	}
	s.logger.Info(ctx, "Agent type provisioned and active",
		logging.Field{Key: "agent_type_id", Value: at.ID},
		logging.Field{Key: "slug", Value: at.Slug},
	)
	if s.hook != nil {
		s.hook()
	}
}

// GetAgentType retrieves an agent type by ID. Enforces tenant scoping against
// ctx claims and runs an authz read check; returns ErrNotFound on tenant
// mismatch or scope deny (existence is itself information).
func (s *AgentTypeService) GetAgentType(ctx context.Context, id string) (*AgentType, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.GetAgentType",
		trace.WithAttributes(attribute.String("agent_type_id", id)),
	)
	defer span.End()
	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNotFound) {
			span.RecordError(err)
			span.SetStatus(codes.Error, "get agent type")
		}
		return nil, err
	}
	if at.TenantID != tenantFromContext(ctx) {
		return nil, ErrNotFound
	}
	if err := s.authz.checkAgentType(ctx, *at, "read"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// Enrich with the budget carried by the agent's grant — the grant is the
	// source of truth for budget configuration. Best-effort: a policy-service
	// hiccup must not fail the read.
	if budget, budgetErr := s.k8s.GetAgentBudget(ctx, *at, s.cfg.TenantNamespaceFn(at.TenantID)); budgetErr == nil {
		at.Budget = budget
	} else {
		s.logger.Warn(ctx, "agent budget enrichment failed",
			logging.Field{Key: "agent_type_id", Value: at.ID},
			logging.Field{Key: "error", Value: budgetErr.Error()},
		)
	}
	return at, nil
}

// ListAgentTypes returns all agent types the principal in ctx may read.
// Tenant is derived from ctx claims; per-slug filtering is applied via
// authz.filterAgentTypes so principals with narrower grants see only the
// agent types their scopes cover.
func (s *AgentTypeService) ListAgentTypes(ctx context.Context) ([]AgentType, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.ListAgentTypes",
		trace.WithAttributes(attribute.String("tenant_id", tenantID)),
	)
	defer span.End()
	types, err := s.repo.ListAgentTypes(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list agent types")
		return nil, err
	}
	return s.authz.filterAgentTypes(ctx, types)
}

// UpdateAgentType applies partial updates to an existing agent type. Enforces
// tenant scoping against ctx claims and runs an authz write check; tenant
// mismatch returns ErrNotFound to avoid existence leak.
func (s *AgentTypeService) UpdateAgentType(ctx context.Context, req UpdateAgentTypeRequest) (*AgentType, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.UpdateAgentType",
		trace.WithAttributes(attribute.String("agent_type_id", req.ID)),
	)
	defer span.End()

	at, err := s.loadAgentTypeForWrite(ctx, req.ID, span)
	if err != nil {
		return nil, err
	}
	if vErr := s.validateAgentTypeUpdate(ctx, at, req); vErr != nil {
		return nil, vErr
	}
	applyAgentTypeUpdates(at, req)

	// Keep the inference grant aligned with the model: when the model changes and
	// the caller didn't supply explicit inference scopes in this update, re-derive
	// (replacing the prior model's inference scopes). See the 2026-06-30 amendment
	// to agent-identity-and-policy-provisioning.
	callerSetInference := req.GrantScopes != nil && hasInferenceScope(req.GrantScopes)
	if req.ModelID != nil && !callerSetInference {
		at.GrantScopes = reconcileInferenceScopes(at.GrantScopes, at.ModelID)
	}

	if err := s.repo.UpdateAgentType(ctx, *at); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update agent type")
		return nil, fmt.Errorf("update agent type: %w", err)
	}

	// Re-reconcile the policy-service grant when grant scopes, the model (which
	// drives inference scopes), budget, the connectivity posture, the connectors,
	// or the governed MCP tool grants change (all compiled into the grant via
	// EffectiveGrantScopes). Run in background so the update response is not
	// delayed by a policy-service round-trip.
	if req.GrantScopes != nil || req.ModelID != nil || req.Budget != nil || req.Connectivity != nil || req.Connectors != nil || req.MCPTools != nil || req.EnvironmentID != nil {
		detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
		namespace := s.cfg.TenantNamespaceFn(at.TenantID)
		go func() {
			if err := s.k8s.EnsureAgentGrant(detachedCtx, *at, namespace); err != nil {
				s.logger.Warn(detachedCtx, "failed to reconcile agent grant after update",
					logging.Field{Key: "agent_type_id", Value: at.ID},
					logging.Field{Key: "error", Value: err.Error()},
				)
			}
		}()
	}

	return at, nil
}

// loadAgentTypeForWrite loads an agent type by ID and enforces tenant scoping
// + authz write. Returns ErrNotFound on tenant mismatch or scope deny.
func (s *AgentTypeService) loadAgentTypeForWrite(ctx context.Context, id string, span trace.Span) (*AgentType, error) {
	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return nil, fmt.Errorf("get agent type: %w", err)
	}
	if at.TenantID != tenantFromContext(ctx) {
		return nil, ErrNotFound
	}
	if err := s.authz.checkAgentType(ctx, *at, "write"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return at, nil
}

// applyAgentTypeUpdates copies non-nil fields from req onto at.
func applyAgentTypeUpdates(at *AgentType, req UpdateAgentTypeRequest) {
	if req.Name != nil {
		at.Name = *req.Name
	}
	if req.Description != nil {
		at.Description = *req.Description
	}
	if req.SystemPrompt != nil {
		at.SystemPrompt = *req.SystemPrompt
	}
	if req.Framework != nil {
		at.Framework = *req.Framework
	}
	if req.MaxConcurrentRunsPerPod != nil {
		at.MaxConcurrentRunsPerPod = *req.MaxConcurrentRunsPerPod
	}
	if req.ModelID != nil {
		at.ModelID = req.ModelID
	}
	if req.ToolScopes != nil {
		at.ToolScopes = req.ToolScopes
	}
	if req.GrantScopes != nil {
		at.GrantScopes = req.GrantScopes
	}
	if req.MCPTools != nil {
		at.MCPTools = req.MCPTools
	}
	if req.Budget != nil {
		at.Budget = req.Budget
	}
	if req.Connectivity != nil {
		at.Connectivity = *req.Connectivity
	}
	if req.Connectors != nil {
		at.Connectors = req.Connectors
	}
	if req.EnvironmentID != nil {
		if strings.TrimSpace(*req.EnvironmentID) == "" {
			at.EnvironmentID = nil
		} else {
			at.EnvironmentID = req.EnvironmentID
		}
	}
}

// DeleteAgentType initiates teardown of a managed agent type. Enforces tenant
// scoping against ctx claims and runs an authz write check; tenant mismatch
// or scope deny returns ErrNotFound (existence is itself information).
func (s *AgentTypeService) DeleteAgentType(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.DeleteAgentType",
		trace.WithAttributes(attribute.String("agent_type_id", id)),
	)
	defer span.End()

	at, err := s.repo.GetAgentType(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "get agent type")
		return fmt.Errorf("get agent type: %w", err)
	}
	if at.TenantID != tenantFromContext(ctx) {
		return ErrNotFound
	}
	if err := s.authz.checkAgentType(ctx, *at, "write"); err != nil {
		if errors.Is(err, ErrForbidden) {
			return ErrNotFound
		}
		return err
	}
	span.SetAttributes(attribute.String("slug", at.Slug))

	// Allow retrying a stuck deletion; skip state transition if already deleting.
	// Also accept provisioning state — partially-created agent types must be deletable.
	if at.State != AgentTypeStateDeleting {
		fromState := AgentTypeStateActive
		if at.State == AgentTypeStateProvisioning {
			fromState = AgentTypeStateProvisioning
		}
		if err := s.repo.SetAgentTypeState(ctx, id, fromState, AgentTypeStateDeleting); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "set agent type deleting")
			return fmt.Errorf("set agent type deleting: %w", err)
		}
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if err := s.k8s.DeprovisionAgentType(ctx, *at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "deprovision k8s resources")
		return fmt.Errorf("deprovision agent type k8s resources: %w", err)
	}

	// Runs must be removed before the agent type row to satisfy the FK constraint.
	deletedRunIDs, delRunsErr := s.runRepo.DeleteRunsByAgentType(ctx, id)
	if delRunsErr != nil {
		span.RecordError(delRunsErr)
		span.SetStatus(codes.Error, "delete runs for agent type")
		return fmt.Errorf("delete runs for agent type: %w", delRunsErr)
	}

	// DeprovisionAgentType only removes StatefulSet-scoped resources; per-run sandbox
	// pods (sandbox-{runID}) outlive it. Delete them best-effort so they are not
	// orphaned — the run rows are gone, so the reaper can no longer reclaim them.
	for _, runID := range deletedRunIDs {
		if delErr := s.k8s.DeleteSandbox(ctx, runID, namespace); delErr != nil {
			s.logger.Warn(ctx, "failed to delete sandbox for deleted run",
				logging.Field{Key: "run_id", Value: runID},
				logging.Field{Key: "error", Value: delErr.Error()},
			)
		}
	}

	if err := s.repo.DeleteAgentType(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete agent type")
		return err
	}
	if s.hook != nil {
		s.hook()
	}
	return nil
}

// WaitForStatefulSetReady blocks until the StatefulSet for the agent type is ready.
func (s *AgentTypeService) WaitForStatefulSetReady(ctx context.Context, slug, namespace string) error {
	return s.k8s.WaitForStatefulSetReady(ctx, slug, namespace)
}
