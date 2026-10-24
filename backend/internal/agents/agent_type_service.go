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

	"github.com/google/uuid"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"k8s.io/apimachinery/pkg/util/validation"
)

// ErrInvalidSlug is returned when an agent type slug cannot name the
// Kubernetes resources provisioned for it.
var ErrInvalidSlug = errors.New("invalid slug")

// maxAgentSlugLength is the longest agent type slug. The slug names the agent's
// StatefulSet, whose pods carry a controller-revision-hash label of the form
// "<slug>-<hash>" with a hash of up to 10 characters, and label values stop at
// 63 characters.
const maxAgentSlugLength = 52

// validateAgentSlug checks slug can name the agent's ServiceAccount,
// StatefulSet and Service. A Service name is a DNS-1035 label, so the slug
// must start with a letter.
func validateAgentSlug(slug string) error {
	if len(slug) > maxAgentSlugLength || len(validation.IsDNS1035Label(slug)) > 0 {
		return fmt.Errorf("%w %q: slug must be lowercase letters, digits and hyphens, start with a letter, end with a letter or digit, and be at most %d characters",
			ErrInvalidSlug, slug, maxAgentSlugLength)
	}
	if slices.Contains(reservedAgentSlugs, slug) {
		return fmt.Errorf("%w %q: the identity mode names %q are reserved", ErrInvalidSlug, slug, reservedAgentSlugs)
	}
	return nil
}

// creatorSubject is the person behind the create (D37). Under delegation it is
// the delegator, so owner access goes to the person and never to the acting agent.
func creatorSubject(ctx context.Context) string {
	if d := policy.DelegationFromContext(ctx); d != nil && d.Sub != "" {
		return d.Sub
	}
	return callerSubject(ctx)
}

// reservedAgentSlugs are the identity mode names. Mode-qualified scopes such as
// agents:runs:delegated:write sit where a per-agent scope's slug would.
var reservedAgentSlugs = []string{string(IdentityModeDelegated), string(IdentityModeAutonomous)}

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
	envs     environmentLookup // validates attached environments; nil = skip validation
	resolver *ScopeResolver    // resolves effective access for the read endpoint; nil = degraded
	callers  callerScopeSource // caller scopes for the ceiling; nil refuses autonomous writes
	// delegation revokes the delegations of runs that agent deletion cancels
	// (the delegation ends with the run); nil = skip (issuer not wired).
	delegation *DelegationBroker
	// pubsub tells live viewers of a cancelled run that it ended; nil = skip.
	pubsub *PubSub
}

// SetDelegationBroker installs the broker used to revoke delegation issuances
// of runs cancelled by agent-type deletion
// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 3).
func (s *AgentTypeService) SetDelegationBroker(b *DelegationBroker) { s.delegation = b }

// SetPubSub installs the run event fan-out shared with RunService.
func (s *AgentTypeService) SetPubSub(p *PubSub) { s.pubsub = p }

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

// SetEnvironmentLookup installs the environment store that checks an agent's
// environments exist within its tenant. When unset, only the database FK checks them.
func (s *AgentTypeService) SetEnvironmentLookup(envs environmentLookup) { s.envs = envs }

// SetScopeResolver installs the resolver used to compute the effective-access
// read model. When unset, GetEffectiveAccess degrades to the agent's own egress
// posture (no environment or connector resolution).
func (s *AgentTypeService) SetScopeResolver(r *ScopeResolver) { s.resolver = r }

// SetCallerScopes installs the source of the caller's effective scopes, which
// bound the scopes of an autonomous agent the caller creates or changes.
func (s *AgentTypeService) SetCallerScopes(c callerScopeSource) { s.callers = c }

// scopeResolver returns the configured resolver, or one that sees only the agent's own scopes.
func (s *AgentTypeService) scopeResolver() *ScopeResolver {
	if s.resolver == nil {
		return &ScopeResolver{}
	}
	return s.resolver
}

// checkEnvironmentEdit authorizes an edit to env as a write to at, an agent it is
// attached to: the mode-qualified write scope and the ceiling over at's grant with env applied.
func (s *AgentTypeService) checkEnvironmentEdit(ctx context.Context, at AgentType, env Environment) error {
	if err := s.authz.checkAgentTypeModeWrite(ctx, at.IdentityMode.OrDefault()); err != nil {
		return err
	}
	return s.checkCeiling(ctx, at, s.scopeResolver().withEnvironment(env))
}

// GetEffectiveAccess returns an agent type's egress posture, attached environments and connector auto-egress combined.
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
	return s.scopeResolver().EffectiveAccess(ctx, *at)
}

// PreviewEffectiveAccess resolves the access view for a draft (unsaved) config —
// the live preview as the user edits. Tenant is derived from the verified claims;
// nothing is persisted, and it reveals only what the caller can already resolve
// for their own tenant.
func (s *AgentTypeService) PreviewEffectiveAccess(ctx context.Context, req EffectiveAccessPreviewRequest) (*EffectiveAccess, error) {
	draft := AgentType{
		TenantID:     tenantFromContext(ctx),
		Connectivity: req.Connectivity,
		Connectors:   req.Connectors,
		Environments: req.Environments,
	}
	return s.scopeResolver().EffectiveAccess(ctx, draft)
}

// validateAgentTypeUpdate runs the partial-update field validations
// (connectivity posture, connectors, environment reference). Extracted from
// UpdateAgentType to keep that method's cyclomatic complexity in check.
func (s *AgentTypeService) validateAgentTypeUpdate(ctx context.Context, at *AgentType, req UpdateAgentTypeRequest) error {
	// identity_mode is immutable after creation (delegated-agent-identity ADR,
	// Decision 1): the fail-closed enforcement stack is provisioned around the
	// mode the agent was created with. Restating the current mode is a no-op;
	// any change is rejected.
	if req.IdentityMode != nil && *req.IdentityMode != "" {
		if vErr := req.IdentityMode.Validate(); vErr != nil {
			return fmt.Errorf("%w: %w", ErrInvalidIdentityMode, vErr)
		}
		if *req.IdentityMode != at.IdentityMode.OrDefault() {
			return fmt.Errorf("%w (current mode: %s)", ErrIdentityModeImmutable, at.IdentityMode.OrDefault())
		}
	}
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
	if req.Guardrails != nil {
		if vErr := req.Guardrails.Validate(); vErr != nil {
			return vErr
		}
	}
	if req.Environments != nil {
		envs, vErr := s.validateEnvironments(ctx, at.TenantID, *req.Environments)
		if vErr != nil {
			return vErr
		}
		*req.Environments = envs
	}
	return nil
}

// validateEnvironments checks each environment exists in the tenant and that
// exactly one is the default. A lone environment is the default.
func (s *AgentTypeService) validateEnvironments(ctx context.Context, tenantID string, envs []AgentEnvironment) ([]AgentEnvironment, error) {
	out := make([]AgentEnvironment, 0, len(envs))
	defaults := 0
	for _, e := range envs {
		e.EnvironmentID = strings.TrimSpace(e.EnvironmentID)
		if e.EnvironmentID == "" || slices.ContainsFunc(out, func(o AgentEnvironment) bool { return o.EnvironmentID == e.EnvironmentID }) {
			return nil, fmt.Errorf("%w: environment ids must be set and unique", ErrInvalidEnvironment)
		}
		if e.IsDefault {
			defaults++
		}
		out = append(out, e)
	}
	if len(out) == 1 {
		out[0].IsDefault = true
		defaults = 1
	}
	if len(out) > 0 && defaults != 1 {
		return nil, fmt.Errorf("%w: mark exactly one environment as the default", ErrInvalidEnvironment)
	}
	if s.envs == nil {
		return out, nil
	}
	found, err := s.envs.ListEnvironmentsByIDs(ctx, tenantID, environmentIDs(out))
	if err != nil {
		return nil, fmt.Errorf("validate environments: %w", err)
	}
	for _, e := range out {
		if !slices.ContainsFunc(found, func(f Environment) bool { return f.ID == e.EnvironmentID }) {
			return nil, fmt.Errorf("%w: environment %q not found", ErrInvalidEnvironment, e.EnvironmentID)
		}
	}
	return out, nil
}

// createValidated carries the validated + normalised create-request fields
// out of validateAgentTypeCreate.
type createValidated struct {
	identityMode IdentityMode
	connectivity Connectivity
	environments []AgentEnvironment
}

// validateAgentTypeCreate runs the create-request field validations (identity
// mode, connectivity posture, connectors, environment reference) and returns
// the normalised values. Extracted from CreateAgentType to keep that method's
// cyclomatic complexity in check, mirroring validateAgentTypeUpdate.
func (s *AgentTypeService) validateAgentTypeCreate(ctx context.Context, req CreateAgentTypeRequest) (createValidated, error) {
	var out createValidated

	if err := validateAgentSlug(req.Slug); err != nil {
		return out, err
	}

	// Identity model: autonomous unless explicitly created delegated. Immutable
	// after creation (delegated-agent-identity ADR, Decision 1).
	if err := req.IdentityMode.Validate(); err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalidIdentityMode, err)
	}
	out.identityMode = req.IdentityMode.OrDefault()

	// Connectivity posture: default to deny-by-default (none) when omitted.
	connectivity := Connectivity{Mode: ConnectivityNone}
	if req.Connectivity != nil {
		connectivity = *req.Connectivity
	}
	if connectivity.Mode == "" {
		connectivity.Mode = ConnectivityNone
	}
	if err := connectivity.Validate(); err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalidConnectivity, err)
	}
	out.connectivity = connectivity

	// Connectors: credential connections this agent may use, and for which
	// actions. Compiles to connection:<name>:<action> grants alongside egress.
	if err := ValidateConnectors(req.Connectors); err != nil {
		return out, fmt.Errorf("%w: %w", ErrInvalidConnector, err)
	}
	if req.Guardrails != nil {
		if err := req.Guardrails.Validate(); err != nil {
			return out, err
		}
	}

	// No environments means the built-in default. The FK checks only existence.
	// Tenant ownership is checked here.
	envs, err := s.validateEnvironments(ctx, req.TenantID, req.Environments)
	if err != nil {
		return out, err
	}
	out.environments = envs
	return out, nil
}

// CreateAgentType registers a new agent type and initiates k8s provisioning.
// Tenant is derived from the verified claims in ctx; the request-body field is
// ignored. The create route is authenticated-only at the route layer; the
// authoritative authorization is the mode-qualified check below —
// agents:types:{mode}:write derived from the requested identity mode after
// defaulting, because creating an agent confers its authority and the two
// modes are separately grantable tiers (delegated-agent-identity ADR,
// Decision 1).
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

	validated, err := s.validateAgentTypeCreate(ctx, req)
	if err != nil {
		return nil, err
	}

	// Mode-qualified authorization AFTER defaulting: an omitted mode defaults
	// to autonomous, which must not slip past the higher-privilege check.
	if err := s.authz.checkAgentTypeModeWrite(ctx, validated.identityMode); err != nil {
		return nil, err
	}

	// grant_scopes is NOT NULL in the database; default a missing slice to an
	// empty one so a request that omits the field still creates cleanly.
	grantScopes := req.GrantScopes
	if grantScopes == nil {
		grantScopes = []string{}
	}

	// mcp_tools is likewise NOT NULL: a nil slice encodes as SQL NULL, which
	// overrides the column DEFAULT '{}' and trips the not-null constraint. Default
	// a missing slice to empty so a request that omits MCP tools still creates.
	mcpTools := req.MCPTools
	if mcpTools == nil {
		mcpTools = []string{}
	}
	// Inference grant scopes are derived server-side from the model (single source
	// of truth) unless the caller supplied explicit inference:* scopes. Connector
	// and connectivity scopes are compiled separately by EffectiveGrantScopes. See
	// the 2026-06-30 amendment to agent-identity-and-policy-provisioning.
	grantScopes = applyDerivedInferenceScopes(grantScopes, req.ModelID)

	at := AgentType{
		ID:                      uuid.NewString(),
		TenantID:                req.TenantID,
		Name:                    req.Name,
		Slug:                    req.Slug,
		Description:             req.Description,
		SystemPrompt:            req.SystemPrompt,
		Framework:               framework,
		IdentityMode:            validated.identityMode,
		GrantScopes:             grantScopes,
		Connectivity:            validated.connectivity,
		Connectors:              req.Connectors,
		MCPTools:                mcpTools,
		Environments:            validated.environments,
		Guardrails:              guardrailsOrDefault(req.Guardrails),
		MaxConcurrentRunsPerPod: maxRuns,
		ModelID:                 req.ModelID,
		Budget:                  req.Budget,
		State:                   AgentTypeStateProvisioning,
		CreatedBy:               creatorSubject(ctx),
	}
	if err := s.checkCeiling(ctx, at, s.scopeResolver()); err != nil {
		return nil, err
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

	// Publish the new slug's scopes now. A trigger bound seconds after create
	// needs agents:runs:<slug>:* in the vocabulary before provisioning ends.
	if s.hook != nil {
		s.hook()
	}

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

// activeAgentTypeLister lists the active agent types of every tenant.
type activeAgentTypeLister interface {
	ListActiveAgentTypes(ctx context.Context) ([]AgentType, error)
}

// ApplyAll applies the rendered resources of every active agent type, which
// rolls an agent whose template changed. kora-agents runs it at startup, so an
// upgrade reaches existing agents (docs/adr/2026-10-12-run-scheduling.md).
func (s *AgentTypeService) ApplyAll(ctx context.Context, lister activeAgentTypeLister) {
	ats, err := lister.ListActiveAgentTypes(ctx)
	if err != nil {
		s.logger.Error(ctx, "list agent types to apply failed", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, at := range ats {
		if err := s.k8s.ProvisionAgentType(ctx, at, s.cfg.TenantNamespaceFn(at.TenantID)); err != nil {
			// Another kora-agents replica applying the same resources at startup causes update conflicts.
			s.logger.Warn(ctx, "apply agent type resources failed",
				logging.Field{Key: "agent_type_id", Value: at.ID},
				logging.Field{Key: "slug", Value: at.Slug},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
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
	withAccess := []AgentType{*at}
	s.setCallerAccess(ctx, withAccess)
	at = &withAccess[0]

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
	types, err = s.authz.filterAgentTypes(ctx, types)
	if err != nil {
		return nil, err
	}
	s.setCallerAccess(ctx, types)
	return types, nil
}

// ListAgentTypesByIDs returns the listed agent types the principal in ctx may read, in listing order.
// An ID the principal cannot read or the tenant lacks is left out. With withAccess each returned type
// carries its effective connectors, resolved for the returned types only.
func (s *AgentTypeService) ListAgentTypesByIDs(ctx context.Context, ids []string, withAccess bool) ([]AgentType, error) {
	tenantID := tenantFromContext(ctx)
	ctx, span := agentsTracer.Start(ctx, "agents.ListAgentTypesByIDs",
		trace.WithAttributes(attribute.String("tenant_id", tenantID), attribute.Int("ids", len(ids))),
	)
	defer span.End()
	all, err := s.repo.ListAgentTypes(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "list agent types")
		return nil, err
	}
	var types []AgentType
	for _, at := range all {
		if slices.Contains(ids, at.ID) {
			types = append(types, at)
		}
	}
	if types, err = s.authz.filterAgentTypes(ctx, types); err != nil {
		return nil, err
	}
	s.setCallerAccess(ctx, types)
	if !withAccess {
		return types, nil
	}
	connectors, err := s.scopeResolver().EffectiveConnectors(ctx, types)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "resolve effective connectors")
		return nil, err
	}
	for i := range types {
		types[i].EffectiveConnectors = connectors[types[i].ID]
	}
	return types, nil
}

// setCallerAccess fills CanRun and CanEdit on each agent type for the caller in
// ctx. Run uses checkRun, the run-start check. Edit uses the mode-qualified check
// update and delete use, once per mode, plus the scope ceiling for autonomous
// agents. Delete is the mode check without the ceiling. The flags only drive the UI, which disables what they deny, so an
// evaluator error leaves them false and the read succeeds.
func (s *AgentTypeService) setCallerAccess(ctx context.Context, ats []AgentType) {
	canEdit := map[IdentityMode]bool{}
	var callers callerScopeSource
	if s.callers != nil {
		callers = &onceCallerScopes{src: s.callers}
	}
	for i := range ats {
		mode := ats[i].IdentityMode.OrDefault()
		edit, seen := canEdit[mode]
		if !seen {
			err := s.authz.checkAgentTypeModeWrite(ctx, mode)
			if err != nil && !errors.Is(err, ErrIdentityModeWriteForbidden) {
				s.logger.Warn(ctx, "agent type edit access check failed", logging.Field{Key: "error", Value: err.Error()})
			}
			edit = err == nil
			canEdit[mode] = edit
		}
		runErr := s.authz.checkRun(ctx, ats[i], "write")
		if runErr != nil && !errors.Is(runErr, ErrForbidden) {
			s.logger.Warn(ctx, "agent type run access check failed",
				logging.Field{Key: "agent_type_id", Value: ats[i].ID},
				logging.Field{Key: "error", Value: runErr.Error()})
		}
		run := runErr == nil
		editThis := edit && s.withinCeiling(ctx, ats[i], callers)
		// Delete runs the same refusal of delegated callers on autonomous agents (D36).
		deleteThis := edit && refuseDelegatedAutonomousWrite(ctx, ats[i]) == nil
		ats[i].CanEdit, ats[i].CanRun, ats[i].CanDelete = &editThis, &run, &deleteThis
	}
}

// withinCeiling reports whether the caller holds everything an autonomous agent
// holds, the ceiling every update checks. A delegated agent's grant only caps
// its runners, so it passes. An evaluator error reads as no.
func (s *AgentTypeService) withinCeiling(ctx context.Context, at AgentType, callers callerScopeSource) bool {
	if at.IsDelegated() {
		return true
	}
	err := s.checkCeilingWith(ctx, at, s.scopeResolver(), callers)
	if err != nil && !errors.Is(err, ErrScopeCeiling) {
		s.logger.Warn(ctx, "agent type ceiling check failed",
			logging.Field{Key: "agent_type_id", Value: at.ID},
			logging.Field{Key: "error", Value: err.Error()})
	}
	return err == nil
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

	// Capture pod-baked fields before the update to detect changes that require
	// recycling the StatefulSet pod.
	oldFramework := at.Framework
	oldModelID := at.ModelID

	applyAgentTypeUpdates(at, req)

	// Keep the inference grant aligned with the model: when the model changes and
	// the caller didn't supply explicit inference scopes in this update, re-derive
	// (replacing the prior model's inference scopes). See the 2026-06-30 amendment
	// to agent-identity-and-policy-provisioning.
	callerSetInference := req.GrantScopes != nil && hasInferenceScope(req.GrantScopes)
	if req.ModelID != nil && !callerSetInference {
		at.GrantScopes = reconcileInferenceScopes(at.GrantScopes, at.ModelID)
	}
	// Any edit, a prompt included, steers an autonomous agent's standing authority.
	// The caller must hold the whole grant.
	if err := s.checkCeiling(ctx, *at, s.scopeResolver()); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateAgentType(ctx, *at); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "update agent type")
		return nil, fmt.Errorf("update agent type: %w", err)
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)

	s.reconcileGrantIfNeeded(ctx, at, req, namespace)
	s.rolloutIfPodBakedFieldsChanged(ctx, at, req, namespace, oldFramework, oldModelID)

	return at, nil
}

// reconcileGrantIfNeeded re-reconciles the policy-service grant when grant
// scopes, the model (which drives inference scopes), budget, the connectivity
// posture, the connectors, or the governed MCP tool grants change (all
// compiled into the grant via EffectiveGrantScopes). Runs in background so
// the update response is not delayed by a policy-service round-trip.
func (s *AgentTypeService) reconcileGrantIfNeeded(ctx context.Context, at *AgentType, req UpdateAgentTypeRequest, namespace string) {
	if req.GrantScopes == nil && req.ModelID == nil && req.Budget == nil && req.Connectivity == nil && req.Connectors == nil && req.MCPTools == nil && req.Environments == nil {
		return
	}
	detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		if err := s.k8s.EnsureAgentGrant(detachedCtx, *at, namespace); err != nil {
			s.logger.Warn(detachedCtx, "failed to reconcile agent grant after update",
				logging.Field{Key: "agent_type_id", Value: at.ID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()
}

// rolloutIfPodBakedFieldsChanged rolls the agent StatefulSet when framework or
// model_id changed. Both are consumed per-run at session-create time, so a
// running pod may still be dispatching sessions with the old values; rolling
// the StatefulSet makes pods restart and drain before accepting new runs.
func (s *AgentTypeService) rolloutIfPodBakedFieldsChanged(ctx context.Context, at *AgentType, req UpdateAgentTypeRequest, namespace string, oldFramework AgentFramework, oldModelID *string) {
	frameworkChanged := req.Framework != nil && at.Framework != oldFramework
	modelChanged := req.ModelID != nil && !modelIDsEqual(at.ModelID, oldModelID)
	if !frameworkChanged && !modelChanged {
		return
	}
	detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	go func() {
		if err := s.k8s.RolloutStatefulSet(detachedCtx, at.Slug, namespace); err != nil {
			s.logger.Warn(detachedCtx, "failed to roll StatefulSet after framework/model update",
				logging.Field{Key: "agent_type_id", Value: at.ID},
				logging.Field{Key: "slug", Value: at.Slug},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}()
}

// loadAgentTypeForWrite loads an agent type by ID and enforces tenant scoping
// + the mode-qualified write check. Returns ErrNotFound on tenant mismatch;
// a scope deny surfaces as ErrIdentityModeWriteForbidden (403 with the missing
// scope named) — the required scope derives from the STORED identity mode,
// since widening an autonomous agent's grants equals creating it with them
// (delegated-agent-identity ADR, Decision 1).
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
	if err := s.authz.checkAgentTypeModeWrite(ctx, at.IdentityMode.OrDefault()); err != nil {
		return nil, err
	}
	return at, nil
}

// applyAgentTypeUpdates copies non-nil fields from req onto at.
func applyAgentTypeUpdates(at *AgentType, req UpdateAgentTypeRequest) {
	if req.Guardrails != nil {
		at.Guardrails = *req.Guardrails
	}
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
	if req.Environments != nil {
		at.Environments = *req.Environments
	}
}

// DeleteAgentType initiates teardown of a managed agent type. Enforces tenant
// scoping against ctx claims (mismatch returns ErrNotFound) and the
// mode-qualified write check on the STORED identity mode — deleting an agent
// type is a mutation of the same tier as creating it, so it needs
// agents:types:{storedMode}:write (delegated-agent-identity ADR, Decision 1).
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
	if err = s.authz.checkAgentTypeModeWrite(ctx, at.IdentityMode.OrDefault()); err != nil {
		return err
	}
	if err = refuseDelegatedAutonomousWrite(ctx, *at); err != nil {
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
		if err = s.repo.SetAgentTypeState(ctx, id, fromState, AgentTypeStateDeleting); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "set agent type deleting")
			return fmt.Errorf("set agent type deleting: %w", err)
		}
	}

	namespace := s.cfg.TenantNamespaceFn(at.TenantID)
	if err = s.k8s.DeprovisionAgentType(ctx, *at, namespace); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "deprovision k8s resources")
		return fmt.Errorf("deprovision agent type k8s resources: %w", err)
	}

	// Runs and their history stay. Unfinished runs end here.
	cancelled, err := s.runRepo.CancelRunsByAgentType(ctx, id)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "cancel runs for agent type")
		return fmt.Errorf("cancel runs for agent type: %w", err)
	}
	for _, run := range cancelled {
		s.cleanupCancelledRun(ctx, run, namespace)
	}

	if err = s.repo.SoftDeleteAgentType(ctx, id); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "soft delete agent type")
		return fmt.Errorf("soft delete agent type: %w", err)
	}
	if s.hook != nil {
		s.hook()
	}
	return nil
}

// cleanupCancelledRun records and publishes the run's terminal state, revokes its delegation and deletes its
// sandbox pod. All of it is best-effort. Agent deletion must not wedge on it.
func (s *AgentTypeService) cleanupCancelledRun(ctx context.Context, run CancelledRun, namespace string) {
	payload := []byte(`{"state":"` + string(run.State) + `"}`)
	at := eventTime()
	seqNo, err := s.runRepo.AppendRunEvent(ctx, RunEvent{RunID: run.ID, EventType: "run_state", Payload: payload, CreatedAt: at})
	if err != nil {
		s.logger.Warn(ctx, "failed to record state of cancelled run",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	}
	if s.pubsub != nil {
		s.pubsub.Publish(run.ID, PushedEvent{Type: "run_state", Payload: payload, SequenceNo: seqNo, At: at})
	}
	if run.DelegationJTI != nil && *run.DelegationJTI != "" && s.delegation != nil {
		s.delegation.RevokeBestEffort(ctx, *run.DelegationJTI, run.ID)
	}
	if delErr := s.k8s.DeleteSandbox(ctx, run.ID, namespace); delErr != nil {
		s.logger.Warn(ctx, "failed to delete sandbox for cancelled run",
			logging.Field{Key: "run_id", Value: run.ID},
			logging.Field{Key: "error", Value: delErr.Error()},
		)
	}
}

// WaitForStatefulSetReady blocks until the StatefulSet for the agent type is ready.
func (s *AgentTypeService) WaitForStatefulSetReady(ctx context.Context, slug, namespace string) error {
	return s.k8s.WaitForStatefulSetReady(ctx, slug, namespace)
}

// modelIDsEqual returns true when both pointers hold equal values (both nil, or
// both non-nil with the same string content).
func modelIDsEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
