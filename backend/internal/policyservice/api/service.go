package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// ErrInvalidPrincipalType is returned when principal_type is not a known value.
var ErrInvalidPrincipalType = errors.New("principal_type must be AGENT, HUMAN, or SERVICE")

// ScopeVocabularyError is returned when a grant includes a scope that is not
// registered in the scope vocabulary.
type ScopeVocabularyError struct {
	Scope string
}

func (e *ScopeVocabularyError) Error() string {
	return fmt.Sprintf("scope %q is not in the scope vocabulary", e.Scope)
}

// recompileDebounce is the delay before a bundle recompilation fires. Multiple
// writes within this window are coalesced into a single recompilation.
const recompileDebounce = 500 * time.Millisecond

// Service orchestrates grant, role, and vocabulary management, including the
// bundle recompilation side-effect triggered on every write.
type Service struct {
	grantRepo  grants.Repository
	vocabRepo  vocabulary.Repository
	roleRepo   roles.Repository
	bundleSrv  *bundle.Server
	routeStore *routes.Store
	logger     logging.Logger

	// recompileMu protects recompileTimers and lastWriteAt from concurrent map access.
	recompileMu     sync.Mutex
	recompileTimers map[string]*time.Timer
	lastWriteAt     map[string]time.Time
}

// NewService creates a Service wired to the provided repositories and bundle server.
func NewService(
	grantRepo grants.Repository,
	vocabRepo vocabulary.Repository,
	roleRepo roles.Repository,
	bundleSrv *bundle.Server,
	routeStore *routes.Store,
	logger logging.Logger,
) *Service {
	return &Service{
		grantRepo:       grantRepo,
		vocabRepo:       vocabRepo,
		roleRepo:        roleRepo,
		bundleSrv:       bundleSrv,
		routeStore:      routeStore,
		logger:          logger,
		recompileTimers: make(map[string]*time.Timer),
		lastWriteAt:     make(map[string]time.Time),
	}
}

// BundleHandler returns the HTTP handler that serves compiled OPA bundles to
// enforcement points. Registered by the Controller at GET /bundles/{tenant_id}.
func (s *Service) BundleHandler() http.Handler {
	return s.bundleSrv
}

func (s *Service) CreateGrant(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, scopes []string) (*grants.Grant, error) {
	switch principalType {
	case grants.PrincipalTypeAgent, grants.PrincipalTypeHuman, grants.PrincipalTypeService:
	default:
		return nil, ErrInvalidPrincipalType
	}

	for _, scope := range scopes {
		exists, err := s.vocabRepo.Exists(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("vocabulary check: %w", err)
		}
		if !exists {
			return nil, &ScopeVocabularyError{Scope: scope}
		}
	}

	g := &grants.Grant{
		TenantID:      tenantID,
		PrincipalType: principalType,
		PrincipalID:   principalID,
		Scopes:        scopes,
	}
	if err := s.grantRepo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}

	s.recordWrite(g.TenantID)
	s.scheduleRecompile(ctx, g.TenantID)
	return g, nil
}

func (s *Service) GetGrant(ctx context.Context, id string) (*grants.Grant, error) {
	return s.grantRepo.GetByID(ctx, id)
}

func (s *Service) DeleteGrant(ctx context.Context, id string, tenantID string) error {
	if err := s.grantRepo.Delete(ctx, id, tenantID); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return nil
}

func (s *Service) ListGrants(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, limit, offset int) ([]*grants.Grant, error) {
	if principalID != "" {
		return s.grantRepo.ListByPrincipal(ctx, tenantID, principalType, principalID, limit, offset)
	}
	return s.grantRepo.ListByTenant(ctx, tenantID, limit, offset)
}

func (s *Service) ListVocabulary(ctx context.Context, resourceType string) ([]*vocabulary.ScopeEntry, error) {
	return s.vocabRepo.List(ctx, resourceType)
}

// ListVocabularyWithUsage returns all scopes with grant and role usage counts
// scoped to a specific tenant. Reserved roles (tenant-admin, platform-admin)
// are not stored in the DB, so their scope usage is counted in-memory.
func (s *Service) ListVocabularyWithUsage(ctx context.Context, resourceType, tenantID string) ([]*vocabulary.ScopeEntryWithUsage, error) {
	entries, err := s.vocabRepo.ListWithUsage(ctx, resourceType, tenantID)
	if err != nil {
		return nil, err
	}

	// Build a set of scopes used by reserved roles and count how many
	// reserved roles reference each scope.
	reservedCounts := make(map[string]int)
	for _, scopes := range reservedRoleScopes {
		for _, s := range scopes {
			reservedCounts[s]++
		}
	}

	// Add reserved role counts to the DB-sourced role_count.
	for _, e := range entries {
		if extra, ok := reservedCounts[e.Scope]; ok {
			e.RoleCount += extra
		}
	}

	return entries, nil
}

// Stats returns aggregate counts across grants, roles, vocabulary, routes,
// and bundle compilation status for a tenant.
func (s *Service) Stats(ctx context.Context, tenantID string) (*statsResponse, error) {
	grantTotal, grantByType, err := s.grantRepo.CountByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count grants: %w", err)
	}

	customRoleCount, err := s.roleRepo.CountByTenant(ctx, tenantID)
	if err != nil {
		return nil, fmt.Errorf("count roles: %w", err)
	}

	vocabByType, err := s.vocabRepo.CountByResourceType(ctx)
	if err != nil {
		return nil, fmt.Errorf("count vocabulary: %w", err)
	}
	vocabTotal := 0
	for _, c := range vocabByType {
		vocabTotal += c
	}

	allRoutes := s.routeStore.ListAll()
	routeTotal := 0
	for _, reg := range allRoutes {
		routeTotal += len(reg.Routes)
	}

	// Bundle status: only return status for the requested tenant to avoid
	// leaking other tenant identifiers and bundle timestamps.
	tenantBundles := []tenantBundleStatus{}
	cached := s.bundleSrv.Compiler().Get(tenantID)
	if cached != nil {
		status := "current"
		s.recompileMu.Lock()
		if lastWrite, ok := s.lastWriteAt[tenantID]; ok && lastWrite.After(cached.CompiledAt) {
			status = "stale"
		}
		s.recompileMu.Unlock()
		tenantBundles = append(tenantBundles, tenantBundleStatus{
			TenantID:       tenantID,
			LastCompiledAt: cached.CompiledAt.Format("2006-01-02T15:04:05Z07:00"),
			Status:         status,
		})
	}

	return &statsResponse{
		Grants: grantStats{
			Total:           grantTotal,
			ByPrincipalType: grantByType,
		},
		Roles: roleStats{
			Total:    len(reservedRoles) + customRoleCount,
			Reserved: len(reservedRoles),
			Custom:   customRoleCount,
		},
		Vocabulary: vocabStats{
			Total:          vocabTotal,
			ByResourceType: vocabByType,
		},
		Routes: routeStats{
			Total: routeTotal,
		},
		Bundles: bundleStats{
			Tenants: tenantBundles,
		},
	}, nil
}

func (s *Service) GetScope(ctx context.Context, scope string) (*vocabulary.ScopeEntry, error) {
	return s.vocabRepo.GetByScope(ctx, scope)
}

// reservedRoles are the hardcoded platform admin roles that cannot be managed
// via the API — they are compiled directly into every bundle.
var reservedRoles = map[string]bool{
	"tenant-admin":   true,
	"platform-admin": true,
}

// reservedRoleScopes mirrors bundle.defaultRoleScopes so ListRoles can include
// them in API responses without importing the bundle package.
var reservedRoleScopes = map[string][]string{
	"tenant-admin":   {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:agents:read"},
	"platform-admin": {"platform:grants:read", "platform:grants:write", "platform:roles:read", "platform:roles:write", "platform:agents:read", "platform:tenants:manage", "platform:catalog:read", "platform:catalog:write"},
}

// UpsertRole creates or replaces the scope mapping for a role within a tenant.
// All scopes must exist in the vocabulary. Reserved platform admin roles cannot
// be modified.
func (s *Service) UpsertRole(ctx context.Context, tenantID, role string, scopes []string) (*roles.RoleEntry, error) {
	if reservedRoles[role] {
		return nil, &ReservedRoleError{Role: role}
	}
	for _, scope := range scopes {
		exists, err := s.vocabRepo.Exists(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("vocabulary check: %w", err)
		}
		if !exists {
			return nil, &ScopeVocabularyError{Scope: scope}
		}
	}
	entry := &roles.RoleEntry{TenantID: tenantID, Role: role, Scopes: scopes}
	if err := s.roleRepo.Upsert(ctx, entry); err != nil {
		return nil, fmt.Errorf("upsert role: %w", err)
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return entry, nil
}

// GetRole returns the scope mapping for a specific role within a tenant.
func (s *Service) GetRole(ctx context.Context, tenantID, role string) (*roles.RoleEntry, error) {
	return s.roleRepo.GetByRole(ctx, tenantID, role)
}

// ListRoles returns all role→scope mappings for a tenant, including the
// hardcoded reserved roles (tenant-admin, platform-admin) prepended on
// the first page so the dashboard can display them.
func (s *Service) ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]*roles.RoleEntry, error) {
	custom, err := s.roleRepo.ListByTenant(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}

	// Only prepend reserved roles on the first page.
	if offset > 0 {
		return custom, nil
	}

	reserved := make([]*roles.RoleEntry, 0, len(reservedRoleScopes))
	for _, name := range []string{"tenant-admin", "platform-admin"} {
		reserved = append(reserved, &roles.RoleEntry{
			ID:       "reserved-" + name,
			TenantID: tenantID,
			Role:     name,
			Scopes:   reservedRoleScopes[name],
		})
	}
	return append(reserved, custom...), nil
}

// DeleteRole removes the scope mapping for a role within a tenant.
func (s *Service) DeleteRole(ctx context.Context, tenantID, role string) error {
	if reservedRoles[role] {
		return &ReservedRoleError{Role: role}
	}
	if err := s.roleRepo.Delete(ctx, tenantID, role); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return nil
}

// ListRoutes returns all registered route→scope mappings across all services and tenants.
func (s *Service) ListRoutes() []*routes.Registration {
	return s.routeStore.ListAll()
}

// RegisterRoutes stores a service's route→scope mappings and upserts any declared
// scopes into the vocabulary. Scope declarations are always upserted — services
// are the authoritative owners of their scopes.
//
// For tenant-scoped registrations (tenantID != ""), any required scopes not
// covered by the service's own declarations must already exist in the vocabulary.
// Global registrations (tenantID == "") trigger recompilation for all tenants.
func (s *Service) RegisterRoutes(ctx context.Context, service, tenantID string, routeScopes []routes.RouteScope, scopeDecls []routes.ScopeDeclaration) error {
	// Upsert declared scopes into vocabulary first so that the existence check
	// below sees the freshly declared entries.
	incomingScopes := make([]string, len(scopeDecls))
	for i := range scopeDecls {
		incomingScopes[i] = scopeDecls[i].Scope
		if err := s.vocabRepo.Upsert(ctx, &vocabulary.ScopeEntry{
			Scope:        scopeDecls[i].Scope,
			ResourceType: scopeDecls[i].ResourceType,
			PathSegments: scopeDecls[i].PathSegments,
			Action:       scopeDecls[i].Action,
			Description:  scopeDecls[i].Description,
			RegisteredBy: service,
		}); err != nil {
			return fmt.Errorf("upsert scope %q: %w", scopeDecls[i].Scope, err)
		}
	}

	// For global registrations, reconcile owned scopes: any scope previously
	// registered by this service that is no longer in the catalog means the
	// virtual model was deleted. Remove it from vocab and cascade to grants/roles.
	if tenantID == "" {
		deleted, err := s.vocabRepo.DeleteOrphans(ctx, service, incomingScopes)
		if err != nil {
			return fmt.Errorf("prune orphan scopes for %q: %w", service, err)
		}
		for _, scope := range deleted {
			if err := s.grantRepo.PurgeScopeFromAll(ctx, scope); err != nil {
				s.logger.Error(ctx, "purge deleted scope from grants",
					logging.Field{Key: "scope", Value: scope},
					logging.Field{Key: "error", Value: err})
			}
			if err := s.roleRepo.PurgeScopeFromAll(ctx, scope); err != nil {
				s.logger.Error(ctx, "purge deleted scope from roles",
					logging.Field{Key: "scope", Value: scope},
					logging.Field{Key: "error", Value: err})
			}
		}
	}

	if tenantID != "" {
		for _, r := range routeScopes {
			exists, err := s.vocabRepo.Exists(ctx, r.RequiredScope)
			if err != nil {
				return fmt.Errorf("vocabulary check: %w", err)
			}
			if !exists {
				return &ScopeVocabularyError{Scope: r.RequiredScope}
			}
		}
	}

	s.routeStore.Register(&routes.Registration{
		Service:  service,
		TenantID: tenantID,
		Routes:   routeScopes,
		Scopes:   scopeDecls,
	})

	if tenantID == "" {
		s.recordWriteAll(ctx)
		go s.scheduleRecompileAll(context.WithoutCancel(ctx))
	} else {
		s.recordWrite(tenantID)
		s.scheduleRecompile(ctx, tenantID)
	}

	return nil
}

// recordWrite marks the current time as the last policy-affecting write for a
// tenant. Used by Stats to determine bundle staleness.
func (s *Service) recordWrite(tenantID string) {
	s.recompileMu.Lock()
	s.lastWriteAt[tenantID] = time.Now()
	s.recompileMu.Unlock()
}

// recordWriteAll marks a write for all active tenants (used after global route
// registration changes).
func (s *Service) recordWriteAll(ctx context.Context) {
	tenantIDs, err := s.grantRepo.ListDistinctTenants(ctx)
	if err != nil {
		return
	}
	now := time.Now()
	s.recompileMu.Lock()
	for _, tid := range tenantIDs {
		s.lastWriteAt[tid] = now
	}
	s.recompileMu.Unlock()
}

// scheduleRecompile debounces bundle recompilation for a tenant. Multiple writes
// within recompileDebounce are coalesced into a single recompilation, preventing
// goroutine explosion during bulk provisioning.
func (s *Service) scheduleRecompile(ctx context.Context, tenantID string) {
	detachedCtx := context.WithoutCancel(ctx)
	s.recompileMu.Lock()
	defer s.recompileMu.Unlock()

	if t, ok := s.recompileTimers[tenantID]; ok {
		t.Stop()
	}
	s.recompileTimers[tenantID] = time.AfterFunc(recompileDebounce, func() {
		s.recompileMu.Lock()
		delete(s.recompileTimers, tenantID)
		s.recompileMu.Unlock()

		if err := s.bundleSrv.Recompile(detachedCtx, tenantID, nil); err != nil {
			s.logger.Error(detachedCtx, "bundle recompile failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	})
}

// scheduleRecompileAll schedules recompilation for all active tenants.
// Called after a global route registration change.
func (s *Service) scheduleRecompileAll(ctx context.Context) {
	tenantIDs, err := s.grantRepo.ListDistinctTenants(ctx)
	if err != nil {
		s.logger.Error(ctx, "recompile all: failed to list tenants",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	vocab, err := s.vocabRepo.List(ctx, "")
	if err != nil {
		s.logger.Error(ctx, "recompile all: failed to list vocabulary",
			logging.Field{Key: "error", Value: err.Error()},
		)
		return
	}
	for _, tenantID := range tenantIDs {
		if err := s.bundleSrv.Recompile(ctx, tenantID, vocab); err != nil {
			s.logger.Error(ctx, "recompile all: bundle recompile failed",
				logging.Field{Key: "tenant_id", Value: tenantID},
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	}
}
