package agents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/google/uuid"
)

// ── Environments ────────────────────────────────────────────────────────────

// ErrInvalidEnvironment is returned when an environment is malformed (empty
// name, bad egress host, or invalid connector). Handlers map it to a 400.
var ErrInvalidEnvironment = errors.New("invalid environment")

// ErrEnvironmentNameTaken is returned when an environment name collides within a
// tenant. Handlers map it to a 409.
var ErrEnvironmentNameTaken = errors.New("environment name already taken")

// Environment is a tenant-scoped, UI-managed bundle of egress hosts and
// connectors an agent type can reference. Assigning an environment expands into
// the agent's effective grant: EgressHosts compile to egress:<host>:connect and
// Connectors to connection:<name>:<action> (whose adapters auto-bring their own
// egress). It is the reusable composition layer over the GitOps-registered
// connections — edited once, every referencing agent re-reconciles. Not a CRD:
// see docs/adr/2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md.
type Environment struct {
	ID          string      `json:"id"`
	TenantID    string      `json:"tenant_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	EgressHosts []string    `json:"egress_hosts"`
	Connectors  []Connector `json:"connectors"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// Validate checks the environment is well-formed: a non-empty name, egress hosts
// that follow the same IPv4/DNS pattern rules as a connectivity whitelist, and
// well-formed connectors. Mirrors Connectivity.Validate/ValidateConnectors so a
// bad bundle is rejected at config time, not at grant-reconcile time.
func (e Environment) Validate() error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("environment name must not be empty")
	}
	if err := (Connectivity{Mode: ConnectivityWhitelist, Hosts: e.EgressHosts}).Validate(); err != nil {
		return err
	}
	return ValidateConnectors(e.Connectors)
}

// EgressScopes compiles the environment's egress hosts into egress:<host>:connect
// scopes. Additive — applied to a referencing agent regardless of its own
// connectivity mode.
func (e Environment) EgressScopes() []string {
	return Connectivity{Mode: ConnectivityWhitelist, Hosts: e.EgressHosts}.EgressScopes()
}

// ── Request types ──────────────────────────────────────────────────────────

// CreateEnvironmentRequest is the create payload. TenantID is derived from the
// verified claims, never the body.
type CreateEnvironmentRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	EgressHosts []string    `json:"egress_hosts"`
	Connectors  []Connector `json:"connectors"`
}

// UpdateEnvironmentRequest is a full replacement of the editable fields — the
// settings form always submits the complete bundle.
type UpdateEnvironmentRequest struct {
	ID          string      `json:"-"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	EgressHosts []string    `json:"egress_hosts"`
	Connectors  []Connector `json:"connectors"`
}

// ── Service ────────────────────────────────────────────────────────────────

// EnvironmentServiceConfig holds runtime parameters for EnvironmentService.
type EnvironmentServiceConfig struct {
	TenantNamespaceFn func(tenantID string) string // maps tenant ID → k8s namespace
}

// EnvironmentService manages environment CRUD and propagates edits to the grants
// of every agent type that references the changed environment (the reference
// model — an environment is governed config, not a per-agent snapshot).
type EnvironmentService struct {
	repo   EnvironmentRepository
	atRepo AgentTypeRepository
	k8s    k8sProvisioner
	cfg    EnvironmentServiceConfig
	logger logging.Logger
}

// NewEnvironmentService constructs an EnvironmentService.
func NewEnvironmentService(
	repo EnvironmentRepository,
	atRepo AgentTypeRepository,
	k8s k8sProvisioner,
	cfg EnvironmentServiceConfig,
	logger logging.Logger,
) *EnvironmentService {
	return &EnvironmentService{repo: repo, atRepo: atRepo, k8s: k8s, cfg: cfg, logger: logger}
}

// CreateEnvironment registers a new environment for the caller's tenant.
func (s *EnvironmentService) CreateEnvironment(ctx context.Context, req CreateEnvironmentRequest) (*Environment, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.CreateEnvironment")
	defer span.End()

	env := Environment{
		ID:          uuid.NewString(),
		TenantID:    tenantFromContext(ctx),
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		EgressHosts: trimNonEmpty(req.EgressHosts),
		Connectors:  req.Connectors,
	}
	if err := env.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidEnvironment, err)
	}
	if err := s.repo.CreateEnvironment(ctx, env); err != nil {
		if errors.Is(err, ErrEnvironmentNameTaken) {
			return nil, ErrEnvironmentNameTaken
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "create environment")
		return nil, fmt.Errorf("create environment: %w", err)
	}
	return &env, nil
}

// GetEnvironment returns an environment by ID, tenant-scoped from ctx.
func (s *EnvironmentService) GetEnvironment(ctx context.Context, id string) (*Environment, error) {
	return s.repo.GetEnvironment(ctx, tenantFromContext(ctx), id)
}

// ListEnvironments returns all environments for the caller's tenant.
func (s *EnvironmentService) ListEnvironments(ctx context.Context) ([]Environment, error) {
	return s.repo.ListEnvironments(ctx, tenantFromContext(ctx))
}

// UpdateEnvironment fully replaces an environment's editable fields and
// re-reconciles every agent type that references it so the change propagates.
func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, req UpdateEnvironmentRequest) (*Environment, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.UpdateEnvironment",
		trace.WithAttributes(attribute.String("environment_id", req.ID)))
	defer span.End()

	tenantID := tenantFromContext(ctx)
	existing, err := s.repo.GetEnvironment(ctx, tenantID, req.ID)
	if err != nil {
		return nil, err // ErrNotFound bubbles to a 404
	}

	env := Environment{
		ID:          existing.ID,
		TenantID:    tenantID,
		Name:        strings.TrimSpace(req.Name),
		Description: req.Description,
		EgressHosts: trimNonEmpty(req.EgressHosts),
		Connectors:  req.Connectors,
		CreatedAt:   existing.CreatedAt,
	}
	if err := env.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidEnvironment, err)
	}
	if err := s.repo.UpdateEnvironment(ctx, env); err != nil {
		if errors.Is(err, ErrEnvironmentNameTaken) {
			return nil, ErrEnvironmentNameTaken
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "update environment")
		return nil, fmt.Errorf("update environment: %w", err)
	}

	affected, listErr := s.atRepo.ListAgentTypesByEnvironment(ctx, tenantID, env.ID)
	if listErr != nil {
		s.logger.Warn(ctx, "list agents for environment re-reconcile failed",
			logging.Field{Key: "environment_id", Value: env.ID},
			logging.Field{Key: "error", Value: listErr.Error()},
		)
	} else {
		s.reReconcileAgents(ctx, tenantID, affected)
	}
	return &env, nil
}

// DeleteEnvironment removes an environment and re-reconciles the agents that
// referenced it (their environment_id is cleared by ON DELETE SET NULL). The
// affected agents are captured BEFORE the delete, since afterward they can no
// longer be found by environment.
func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.DeleteEnvironment",
		trace.WithAttributes(attribute.String("environment_id", id)))
	defer span.End()

	tenantID := tenantFromContext(ctx)
	affected, listErr := s.atRepo.ListAgentTypesByEnvironment(ctx, tenantID, id)
	if listErr != nil {
		span.RecordError(listErr)
		span.SetStatus(codes.Error, "list agents for environment")
		return fmt.Errorf("list agents for environment: %w", listErr)
	}
	if err := s.repo.DeleteEnvironment(ctx, tenantID, id); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrNotFound
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete environment")
		return fmt.Errorf("delete environment: %w", err)
	}
	// The captured agents still carry environment_id in-struct; the scope
	// resolver tolerates the now-missing environment and recomputes their grants
	// without its contribution.
	s.reReconcileAgents(ctx, tenantID, affected)
	return nil
}

// reReconcileAgents reconciles each agent's policy grant in the background so an
// environment edit/delete is not delayed by policy-service round-trips.
func (s *EnvironmentService) reReconcileAgents(ctx context.Context, tenantID string, ats []AgentType) {
	if len(ats) == 0 {
		return
	}
	detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	namespace := s.cfg.TenantNamespaceFn(tenantID)
	go func() {
		for _, at := range ats {
			if err := s.k8s.EnsureAgentGrant(detachedCtx, at, namespace); err != nil {
				s.logger.Warn(detachedCtx, "re-reconcile agent grant after environment change failed",
					logging.Field{Key: "agent_type_id", Value: at.ID},
					logging.Field{Key: "error", Value: err.Error()},
				)
			}
		}
	}()
}

// trimNonEmpty trims each string and drops the empties — used to clean egress
// host lists submitted from the UI.
func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
