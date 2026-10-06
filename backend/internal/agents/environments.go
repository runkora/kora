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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/clear-route/orbit/internal/platform/logging"
	si "github.com/clear-route/orbit/internal/platform/sandboximages/v1alpha1"
	"github.com/google/uuid"
)

// ── Environments ────────────────────────────────────────────────────────────

// ErrInvalidEnvironment is returned when an environment is malformed. Handlers map it to a 400.
var ErrInvalidEnvironment = errors.New("invalid environment")

// ErrEnvironmentNameTaken is returned when an environment name collides within a
// tenant. Handlers map it to a 409.
var ErrEnvironmentNameTaken = errors.New("environment name already taken")

// ErrEnvironmentInUse is returned when an environment attached to an agent is
// deleted. Handlers map it to a 409.
var ErrEnvironmentInUse = errors.New("environment is attached to an agent")

// DefaultEnvironmentName names the built-in environment of an agent with none attached.
const DefaultEnvironmentName = "default"

// EnvironmentSize is a sandbox's CPU, memory and local disk in Kubernetes quantities.
type EnvironmentSize struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Disk   string `json:"disk"`
}

// EnvironmentSizes are the install's default and maximum environment sizes.
type EnvironmentSizes struct {
	Default EnvironmentSize `json:"default"`
	Max     EnvironmentSize `json:"max"`
}

// Validate checks every maximum is a positive quantity and the default fits within it.
func (s EnvironmentSizes) Validate() error {
	if err := s.Max.positive(); err != nil {
		return fmt.Errorf("maximum environment %w", err)
	}
	if err := s.Default.within(s.Max); err != nil {
		return fmt.Errorf("default environment %w", err)
	}
	return nil
}

// Environment is a prepackaged sandbox: a SandboxImage, a size, and the egress hosts and connectors its agents may use.
// See docs/adr/2026-10-03-sandbox-environments.md.
type Environment struct {
	ID          string `json:"id"`
	TenantID    string `json:"tenant_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	// SandboxImage names a SandboxImage in the tenant namespace. kora-base is the base image.
	SandboxImage string          `json:"sandbox_image"`
	Size         EnvironmentSize `json:"size"`
	EgressHosts  []string        `json:"egress_hosts"`
	Connectors   []Connector     `json:"connectors"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// DefaultEnvironment is the environment of an agent with none attached. It runs
// the base image at the default size with no egress or connectors.
func DefaultEnvironment(size EnvironmentSize) Environment {
	return Environment{
		Name:         DefaultEnvironmentName,
		Description:  si.BaseDescription,
		SandboxImage: si.BaseName,
		Size:         size,
		EgressHosts:  []string{},
		Connectors:   []Connector{},
	}
}

// Validate checks the environment is well-formed and its size within maxSize.
func (e Environment) Validate(maxSize EnvironmentSize) error {
	if strings.TrimSpace(e.Name) == "" {
		return fmt.Errorf("environment name must not be empty")
	}
	if errs := validation.IsDNS1123Label(e.SandboxImage); len(errs) > 0 {
		return fmt.Errorf("sandbox_image %q: %s", e.SandboxImage, strings.Join(errs, "; "))
	}
	if err := e.Size.within(maxSize); err != nil {
		return err
	}
	if err := (Connectivity{Mode: ConnectivityWhitelist, Hosts: e.EgressHosts}).Validate(); err != nil {
		return err
	}
	return ValidateConnectors(e.Connectors)
}

// within checks each dimension is a positive quantity no larger than its maximum.
func (s EnvironmentSize) within(maxSize EnvironmentSize) error {
	if err := s.positive(); err != nil {
		return err
	}
	for _, d := range []struct{ name, value, limit string }{
		{"cpu", s.CPU, maxSize.CPU},
		{"memory", s.Memory, maxSize.Memory},
		{"disk", s.Disk, maxSize.Disk},
	} {
		q := resource.MustParse(d.value)
		if limit, err := resource.ParseQuantity(d.limit); err == nil && q.Cmp(limit) > 0 {
			return fmt.Errorf("size.%s %s is above the maximum %s", d.name, d.value, d.limit)
		}
	}
	return nil
}

// positive checks each dimension is a quantity greater than zero.
func (s EnvironmentSize) positive() error {
	for _, d := range []struct{ name, value string }{{"cpu", s.CPU}, {"memory", s.Memory}, {"disk", s.Disk}} {
		q, err := resource.ParseQuantity(d.value)
		if err != nil {
			return fmt.Errorf("size.%s %q is not a quantity", d.name, d.value)
		}
		if q.Sign() <= 0 {
			return fmt.Errorf("size.%s must be greater than zero", d.name)
		}
	}
	return nil
}

// withDefaults fills each empty dimension from def.
func (s EnvironmentSize) withDefaults(def EnvironmentSize) EnvironmentSize {
	if s.CPU == "" {
		s.CPU = def.CPU
	}
	if s.Memory == "" {
		s.Memory = def.Memory
	}
	if s.Disk == "" {
		s.Disk = def.Disk
	}
	return s
}

// EgressScopes compiles the environment's egress hosts into egress:<host>:connect scopes.
func (e Environment) EgressScopes() []string {
	return Connectivity{Mode: ConnectivityWhitelist, Hosts: e.EgressHosts}.EgressScopes()
}

// ── Request types ──────────────────────────────────────────────────────────

// EnvironmentRequest is the create and update payload. An update replaces every field.
// The tenant comes from the verified claims, and an empty size dimension takes the install default.
type EnvironmentRequest struct {
	ID           string          `json:"-"`
	Name         string          `json:"name"`
	Description  string          `json:"description"`
	SandboxImage string          `json:"sandbox_image"`
	Size         EnvironmentSize `json:"size"`
	EgressHosts  []string        `json:"egress_hosts"`
	Connectors   []Connector     `json:"connectors"`
}

// ── Service ────────────────────────────────────────────────────────────────

// EnvironmentServiceConfig holds runtime parameters for EnvironmentService.
type EnvironmentServiceConfig struct {
	TenantNamespaceFn func(tenantID string) string // maps tenant ID → k8s namespace
	Sizes             EnvironmentSizes
}

// environmentStore is the storage the environment service uses.
type environmentStore interface {
	CreateEnvironment(ctx context.Context, env Environment) error
	GetEnvironment(ctx context.Context, tenantID, id string) (*Environment, error)
	ListEnvironments(ctx context.Context, tenantID string) ([]Environment, error)
	UpdateEnvironment(ctx context.Context, env Environment) error
	DeleteEnvironment(ctx context.Context, tenantID, id string) error
}

// agentsByEnvironment lists the agents an environment is attached to.
type agentsByEnvironment interface {
	ListAgentTypesByEnvironment(ctx context.Context, tenantID, environmentID string) ([]AgentType, error)
}

// sandboxImageLookup reads a SandboxImage in a tenant namespace.
type sandboxImageLookup interface {
	SandboxImage(ctx context.Context, namespace, name string) (*si.SandboxImage, error)
}

// sandboxImageReader reads and lists SandboxImages in a tenant namespace.
type sandboxImageReader interface {
	sandboxImageLookup
	SandboxImages(ctx context.Context, namespace string) ([]si.SandboxImage, error)
}

// grantReconciler recompiles an agent's policy grant.
type grantReconciler interface {
	EnsureAgentGrant(ctx context.Context, at AgentType, namespace string) error
}

// EnvironmentService manages environment CRUD and recompiles the grants of
// every agent an edited environment is attached to.
type EnvironmentService struct {
	repo   environmentStore
	agents agentsByEnvironment
	images sandboxImageReader
	grants grantReconciler
	cfg    EnvironmentServiceConfig
	logger logging.Logger
}

// NewEnvironmentService constructs an EnvironmentService.
func NewEnvironmentService(
	repo environmentStore,
	agents agentsByEnvironment,
	images sandboxImageReader,
	grants grantReconciler,
	cfg EnvironmentServiceConfig,
	logger logging.Logger,
) *EnvironmentService {
	return &EnvironmentService{repo: repo, agents: agents, images: images, grants: grants, cfg: cfg, logger: logger}
}

// CreateEnvironment registers a new environment for the caller's tenant.
func (s *EnvironmentService) CreateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.CreateEnvironment")
	defer span.End()

	env := s.fromRequest(tenantFromContext(ctx), uuid.NewString(), req)
	if err := s.validate(ctx, env); err != nil {
		return nil, err
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

// Sizes are the install's default and maximum environment sizes.
func (s *EnvironmentService) Sizes() EnvironmentSizes {
	return s.cfg.Sizes
}

// UpdateEnvironment replaces an environment's fields and recompiles the grants
// of the agents it is attached to.
func (s *EnvironmentService) UpdateEnvironment(ctx context.Context, req EnvironmentRequest) (*Environment, error) {
	ctx, span := agentsTracer.Start(ctx, "agents.UpdateEnvironment",
		trace.WithAttributes(attribute.String("environment_id", req.ID)))
	defer span.End()

	tenantID := tenantFromContext(ctx)
	existing, err := s.repo.GetEnvironment(ctx, tenantID, req.ID)
	if err != nil {
		return nil, err
	}
	env := s.fromRequest(tenantID, existing.ID, req)
	env.CreatedAt = existing.CreatedAt
	if err = s.validate(ctx, env); err != nil {
		return nil, err
	}
	if err = s.repo.UpdateEnvironment(ctx, env); err != nil {
		if errors.Is(err, ErrEnvironmentNameTaken) {
			return nil, ErrEnvironmentNameTaken
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "update environment")
		return nil, fmt.Errorf("update environment: %w", err)
	}

	affected, err := s.agents.ListAgentTypesByEnvironment(ctx, tenantID, env.ID)
	if err != nil {
		s.logger.Warn(ctx, "list agents for environment re-reconcile failed",
			logging.Field{Key: "environment_id", Value: env.ID},
			logging.Field{Key: "error", Value: err.Error()},
		)
	} else {
		s.reReconcileAgents(ctx, tenantID, affected)
	}
	return &env, nil
}

// DeleteEnvironment removes an environment no agent has attached.
func (s *EnvironmentService) DeleteEnvironment(ctx context.Context, id string) error {
	ctx, span := agentsTracer.Start(ctx, "agents.DeleteEnvironment",
		trace.WithAttributes(attribute.String("environment_id", id)))
	defer span.End()

	err := s.repo.DeleteEnvironment(ctx, tenantFromContext(ctx), id)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrEnvironmentInUse):
		return err
	default:
		span.RecordError(err)
		span.SetStatus(codes.Error, "delete environment")
		return fmt.Errorf("delete environment: %w", err)
	}
}

func (s *EnvironmentService) fromRequest(tenantID, id string, req EnvironmentRequest) Environment {
	connectors := req.Connectors
	if connectors == nil {
		connectors = []Connector{}
	}
	// A request that names no image gets the base image.
	image := strings.TrimSpace(req.SandboxImage)
	if image == "" {
		image = si.BaseName
	}
	return Environment{
		ID:           id,
		TenantID:     tenantID,
		Name:         strings.TrimSpace(req.Name),
		Description:  req.Description,
		SandboxImage: image,
		Size:         req.Size.withDefaults(s.cfg.Sizes.Default),
		EgressHosts:  trimNonEmpty(req.EgressHosts),
		Connectors:   connectors,
	}
}

// validate checks the fields and that the named SandboxImage is valid.
func (s *EnvironmentService) validate(ctx context.Context, env Environment) error {
	if err := env.Validate(s.cfg.Sizes.Max); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidEnvironment, err)
	}
	_, err := usableSandboxImage(ctx, s.images, s.cfg.TenantNamespaceFn(env.TenantID), env.SandboxImage, ErrInvalidEnvironment, ErrInvalidEnvironment)
	return err
}

// usableSandboxImage returns the named SandboxImage when it is registered and valid.
// A missing or rejected image wraps unusable, and an image not validated yet wraps pending.
func usableSandboxImage(ctx context.Context, images sandboxImageLookup, namespace, name string, unusable, pending error) (*si.SandboxImage, error) {
	img, err := images.SandboxImage(ctx, namespace, name)
	if errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("%w: sandbox image %q is not registered", unusable, name)
	}
	if err != nil {
		return nil, fmt.Errorf("look up sandbox image %q: %w", name, err)
	}
	switch {
	case img.Valid():
		return img, nil
	case img.Rejected():
		return nil, fmt.Errorf("%w: sandbox image %q is not valid: %s", unusable, name, sandboxImageProblem(img))
	default:
		return nil, fmt.Errorf("%w: sandbox image %q is not validated yet: %s", pending, name, sandboxImageProblem(img))
	}
}

// sandboxImageProblem is the operator's reason a SandboxImage is not valid. A
// success message from an earlier image does not count.
func sandboxImageProblem(img *si.SandboxImage) string {
	for _, c := range img.Status.Conditions {
		if c.Type == si.ConditionValid && c.Status == metav1.ConditionFalse && c.Message != "" {
			return c.Message
		}
	}
	return "the operator has not validated the current image"
}

// reReconcileAgents recompiles each agent's grant in the background. An
// environment edit does not wait on policy-service round trips.
func (s *EnvironmentService) reReconcileAgents(ctx context.Context, tenantID string, ats []AgentType) {
	if len(ats) == 0 {
		return
	}
	detachedCtx := trace.ContextWithRemoteSpanContext(context.Background(), trace.SpanContextFromContext(ctx))
	namespace := s.cfg.TenantNamespaceFn(tenantID)
	go func() {
		for _, at := range ats {
			if err := s.grants.EnsureAgentGrant(detachedCtx, at, namespace); err != nil {
				s.logger.Warn(detachedCtx, "re-reconcile agent grant after environment change failed",
					logging.Field{Key: "agent_type_id", Value: at.ID},
					logging.Field{Key: "error", Value: err.Error()},
				)
			}
		}
	}()
}

// trimNonEmpty trims each string and drops the empties.
func trimNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
