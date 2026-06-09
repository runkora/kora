package bundle

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/quotas"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// validTenantID matches alphanumeric IDs with hyphens, underscores, and dots.
// Rejects path traversal or arbitrary UTF-8 to prevent DoS via compilation for
// non-existent tenants.
var validTenantID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,254}$`)

// Server serves per-tenant OPA bundles at GET /v1/policy/bundles/{tenant_id}.
// On startup it compiles all tenants' bundles from the current DB state.
// On every grant or role write the policy service recompiles the affected tenant's bundle.
type Server struct {
	compiler  *Compiler
	grantRepo grants.Repository
	vocabRepo vocabulary.Repository
	roleRepo  roles.Repository
	quotaRepo quotas.Repository
	logger    logging.Logger
}

// NewServer creates a bundle Server wired to the provided repositories.
func NewServer(compiler *Compiler, grantRepo grants.Repository, vocabRepo vocabulary.Repository, roleRepo roles.Repository, quotaRepo quotas.Repository, logger logging.Logger) *Server {
	return &Server{
		compiler:  compiler,
		grantRepo: grantRepo,
		vocabRepo: vocabRepo,
		roleRepo:  roleRepo,
		quotaRepo: quotaRepo,
		logger:    logger,
	}
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
	grantTenantIDs, err := s.grantRepo.ListDistinctTenants(ctx)
	if err != nil {
		return fmt.Errorf("list tenants for gateway bundle: %w", err)
	}

	// Union grant-tenant IDs with route-tenant IDs so that tenants with
	// registered routes but no grants are still included in the bundle.
	// Without this, requests to those routes return deny:no_route_match
	// instead of the more accurate deny:invalid_token.
	tenantSet := make(map[string]struct{}, len(grantTenantIDs))
	for _, tid := range grantTenantIDs {
		tenantSet[tid] = struct{}{}
	}
	for _, tid := range s.compiler.RouteTenantIDs() {
		tenantSet[tid] = struct{}{}
	}
	tenantIDs := make([]string, 0, len(tenantSet))
	for tid := range tenantSet {
		tenantIDs = append(tenantIDs, tid)
	}

	allGrants := make(map[string][]*grants.Grant, len(tenantIDs))
	allRoles := make(map[string][]*roles.RoleEntry, len(tenantIDs))
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
	}

	_, err = s.compiler.CompileGateway(tenantIDs, allGrants, allRoles)
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
			http.Error(w, "gateway bundle not available", http.StatusNotFound)
			return
		}
		cached = s.compiler.GetGateway()
		if cached == nil {
			http.Error(w, "gateway bundle not available", http.StatusNotFound)
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
// Called after any grant, role, route, or quota change. vocabEntries may be nil to
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

	quotaData, err := s.quotaRepo.BundleData(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("load quota bundle data for tenant %s: %w", tenantID, err)
	}

	_, err = s.compiler.Compile(tenantID, tenantGrants, vocabEntries, roleEntries, quotaData)
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
		http.Error(w, "tenant_id required", http.StatusBadRequest)
		return
	}
	if !validTenantID.MatchString(tenantID) {
		http.Error(w, "invalid tenant_id format", http.StatusBadRequest)
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
			http.Error(w, "bundle not available", http.StatusNotFound)
			return
		}
		cached = s.compiler.Get(tenantID)
		if cached == nil {
			http.Error(w, "bundle not available", http.StatusNotFound)
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
