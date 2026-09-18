package bundle

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"time"

	"github.com/clear-route/orbit/internal/platform/api"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/principalroles"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/tenantbudget"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// validTenantID matches alphanumeric IDs with hyphens, underscores, and dots.
// Rejects path traversal or arbitrary UTF-8 to prevent DoS via compilation for
// non-existent tenants.
var validTenantID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)

// tenantLister names the tenants that exist, whether or not they have policy
// rows yet. The operator-written tenant registry is the implementation.
type tenantLister interface {
	ActiveTenantIDs(ctx context.Context) ([]string, error)
}

// Server serves per-tenant OPA bundles at GET /v1/policy/bundles/{tenant_id}.
// On startup it compiles all tenants' bundles from the current DB state.
// On every grant or role write the policy service recompiles the affected tenant's bundle.
type Server struct {
	compiler           *Compiler
	grantRepo          grants.Repository
	vocabRepo          vocabulary.Repository
	roleRepo           roles.Repository
	tenantBudgetRepo   tenantbudget.Repository
	principalRolesRepo principalroles.Repository
	tenants            tenantLister
	logger             logging.Logger
}

// NewServer creates a bundle Server wired to the provided repositories.
func NewServer(compiler *Compiler, grantRepo grants.Repository, vocabRepo vocabulary.Repository, roleRepo roles.Repository, tenantBudgetRepo tenantbudget.Repository, principalRolesRepo principalroles.Repository, tenants tenantLister, logger logging.Logger) *Server {
	return &Server{
		compiler:           compiler,
		grantRepo:          grantRepo,
		vocabRepo:          vocabRepo,
		roleRepo:           roleRepo,
		tenantBudgetRepo:   tenantBudgetRepo,
		principalRolesRepo: principalRolesRepo,
		tenants:            tenants,
		logger:             logger,
	}
}

// KnownTenantIDs unions every tenant source: the registry, tenants with
// grants, and tenants with registered routes. A registered tenant with no
// rows is still a tenant; its reserved roles must reach the gateway bundle.
func (s *Server) KnownTenantIDs(ctx context.Context) ([]string, error) {
	set := map[string]struct{}{}
	registered, err := s.tenants.ActiveTenantIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list registered tenants: %w", err)
	}
	for _, tid := range registered {
		set[tid] = struct{}{}
	}
	grantTenantIDs, err := s.grantRepo.ListDistinctTenants(ctx)
	if err != nil {
		return nil, fmt.Errorf("list tenants with grants: %w", err)
	}
	for _, tid := range grantTenantIDs {
		set[tid] = struct{}{}
	}
	for _, tid := range s.compiler.RouteTenantIDs() {
		set[tid] = struct{}{}
	}
	return slices.Sorted(maps.Keys(set)), nil
}

// principalRolesForTenant loads the tenant's user→role membership snapshot for
// the delegator-leg data document. Fails open to an empty map with a log line:
// a membership read failure must not block bundle compilation — the delegator
// role leg simply resolves no roles until the next successful recompile
// (grants and direct-grant delegation remain fully enforced).
func (s *Server) principalRolesForTenant(ctx context.Context, tenantID string) map[string][]string {
	if s.principalRolesRepo == nil {
		return nil
	}
	pr, err := s.principalRolesRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		s.logger.Error(ctx, "bundle compile: failed to list principal roles",
			logging.Field{Key: "tenant_id", Value: tenantID},
			logging.Field{Key: "error", Value: err.Error()},
		)
		return nil
	}
	return pr
}

// Compiler returns the underlying bundle compiler. Used by the stats endpoint
// to read bundle compilation timestamps.
func (s *Server) Compiler() *Compiler { return s.compiler }

// BootstrapGateway pre-compiles the cross-tenant gateway bundle at startup so
// the OPA gateway pod does not receive a 404 on its first poll.
func (s *Server) BootstrapGateway(ctx context.Context) {
	if err := s.RecompileGateway(ctx); err != nil {
		s.logger.Error(ctx, "gateway bundle bootstrap failed", logging.Field{Key: "error", Value: err.Error()})
	}
}

// RecompileGateway rebuilds the cross-tenant gateway bundle from the current
// DB state. Called after any grant, role, or route change that affects the
// gateway enforcement point.
func (s *Server) RecompileGateway(ctx context.Context) error {
	tenantIDs, err := s.KnownTenantIDs(ctx)
	if err != nil {
		return fmt.Errorf("list tenants for gateway bundle: %w", err)
	}

	vocab, err := s.vocabRepo.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list vocabulary for gateway bundle: %w", err)
	}

	allGrants := make(map[string][]*grants.Grant, len(tenantIDs))
	allRoles := make(map[string][]*roles.RoleEntry, len(tenantIDs))
	allPrincipalRoles := make(map[string]map[string][]string, len(tenantIDs))
	for _, tid := range tenantIDs {
		tGrants, _, tErr := s.grantRepo.ListByTenant(ctx, tid, 0, 0)
		if tErr != nil {
			return fmt.Errorf("list grants for tenant %s: %w", tid, tErr)
		}
		allGrants[tid] = tGrants

		tRoles, tErr := s.roleRepo.ListByTenant(ctx, tid, 0, 0)
		if tErr != nil {
			return fmt.Errorf("list roles for tenant %s: %w", tid, tErr)
		}
		allRoles[tid] = tRoles

		allPrincipalRoles[tid] = s.principalRolesForTenant(ctx, tid)
	}

	_, err = s.compiler.CompileGateway(tenantIDs, allGrants, allRoles, vocab, allPrincipalRoles)
	return err
}

// ServeGatewayHTTP handles GET /v1/policy/bundles/gateway.
// Returns the compiled cross-tenant gateway bundle as application/gzip.
// No authentication is required — the OPA gateway polls this endpoint from
// inside the cluster using plain HTTP, matching the per-tenant bundle pattern.
func (s *Server) ServeGatewayHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	cached := s.compiler.GetGateway()
	if cached == nil {
		if err := s.RecompileGateway(ctx); err != nil {
			s.logger.Error(ctx, "gateway bundle: on-demand compile failed",
				logging.Field{Key: "error", Value: err.Error()},
			)
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("bundle_not_available", "gateway bundle not available", nil), err)
			return
		}
		cached = s.compiler.GetGateway()
		if cached == nil {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("bundle_not_available", "gateway bundle not available", nil), nil)
			return
		}
	}

	w.Header().Set("Content-Type", "application/vnd.openpolicyagent.bundle")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(cached.Data)))
	w.Header().Set("ETag", fmt.Sprintf("%q", cached.Revision))
	w.Header().Set("Last-Modified", cached.CompiledAt.Format(time.RFC1123))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cached.Data)
}

// Bootstrap pre-compiles bundles for all tenants that have at least one grant.
// Called once at startup so enforcement points do not receive a 404 on first poll.
func (s *Server) Bootstrap(ctx context.Context, tenantIDs []string) {
	vocab, err := s.vocabRepo.List(ctx, "")
	if err != nil {
		s.logger.Error(ctx, "bundle bootstrap: failed to list vocabulary", logging.Field{Key: "error", Value: err.Error()})
		return
	}
	for _, tenantID := range tenantIDs {
		if err := s.Recompile(ctx, tenantID, vocab); err != nil {
			s.logger.Error(ctx, "bundle bootstrap: failed to compile",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}

// Recompile rebuilds the bundle for a single tenant from the current DB state.
// Called after any grant, role, or route change. vocabEntries may be nil to
// trigger a fresh vocabulary load.
func (s *Server) Recompile(ctx context.Context, tenantID string, vocabEntries []*vocabulary.ScopeEntry) error {
	tenantGrants, _, err := s.grantRepo.ListByTenant(ctx, tenantID, 0, 0)
	if err != nil {
		return fmt.Errorf("list grants for tenant %s: %w", tenantID, err)
	}

	if vocabEntries == nil {
		vocabEntries, err = s.vocabRepo.List(ctx, "")
		if err != nil {
			return fmt.Errorf("list vocabulary: %w", err)
		}
	}

	roleEntries, err := s.roleRepo.ListByTenant(ctx, tenantID, 0, 0)
	if err != nil {
		return fmt.Errorf("list role scopes for tenant %s: %w", tenantID, err)
	}

	tb, err := s.tenantBudgetRepo.Get(ctx, tenantID)
	if err != nil && !errors.Is(err, tenantbudget.ErrTenantBudgetNotFound) {
		return fmt.Errorf("get tenant budget for tenant %s: %w", tenantID, err)
	}

	principalRoles := s.principalRolesForTenant(ctx, tenantID)

	_, err = s.compiler.Compile(tenantID, tenantGrants, vocabEntries, roleEntries, tb, principalRoles)
	return err
}

// ServeHTTP handles GET /v1/policy/bundles/{tenant_id}.
// Returns the compiled bundle as application/gzip. If no bundle exists yet
// for the tenant (no grants configured), returns 404.
// This is mounted by the policy-service cmd under the /v1/policy/ prefix.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tenantID := r.PathValue("tenant_id")
	if tenantID == "" {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("tenant_id_required", "tenant_id required", nil), nil)
		return
	}
	if !validTenantID.MatchString(tenantID) {
		api.ErrorResponse(ctx, w, s.logger, api.BadRequestError("invalid_tenant_id", "invalid tenant_id format", nil), nil)
		return
	}

	cached := s.compiler.Get(tenantID)
	if cached == nil {
		// Attempt on-demand compilation for tenants with no prior bundle.
		if err := s.Recompile(ctx, tenantID, nil); err != nil {
			s.logger.Error(ctx, "bundle serve: on-demand compile failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("bundle_not_available", "bundle not available", nil), err)
			return
		}
		cached = s.compiler.Get(tenantID)
		if cached == nil {
			api.ErrorResponse(ctx, w, s.logger, api.NotFoundError("bundle_not_available", "bundle not available", nil), nil)
			return
		}
	}

	w.Header().Set("Content-Type", "application/vnd.openpolicyagent.bundle")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(cached.Data)))
	w.Header().Set("ETag", fmt.Sprintf("%q", cached.Revision))
	w.Header().Set("Last-Modified", cached.CompiledAt.Format(time.RFC1123))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(cached.Data)
}
