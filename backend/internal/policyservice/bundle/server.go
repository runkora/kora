package bundle

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// Server serves per-tenant OPA bundles at GET /v1/policy/bundles/{tenant_id}.
// On startup it compiles all tenants' bundles from the current DB state.
// On every grant or role write the policy service recompiles the affected tenant's bundle.
type Server struct {
	compiler  *Compiler
	grantRepo grants.Repository
	vocabRepo vocabulary.Repository
	roleRepo  roles.Repository
	logger    logging.Logger
}

// NewServer creates a bundle Server wired to the provided repositories.
func NewServer(compiler *Compiler, grantRepo grants.Repository, vocabRepo vocabulary.Repository, roleRepo roles.Repository, logger logging.Logger) *Server {
	return &Server{
		compiler:  compiler,
		grantRepo: grantRepo,
		vocabRepo: vocabRepo,
		roleRepo:  roleRepo,
		logger:    logger,
	}
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
	tenantGrants, err := s.grantRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list grants for tenant %s: %w", tenantID, err)
	}

	if vocabEntries == nil {
		vocabEntries, err = s.vocabRepo.List(ctx, "")
		if err != nil {
			return fmt.Errorf("list vocabulary: %w", err)
		}
	}

	roleEntries, err := s.roleRepo.ListByTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("list role scopes for tenant %s: %w", tenantID, err)
	}

	_, err = s.compiler.Compile(tenantID, tenantGrants, vocabEntries, roleEntries)
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
