package api

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/bundle"
	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/roles"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/tenantbudget"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
)

// ErrInvalidPrincipalType is returned when principal_type is not a known value.
var ErrInvalidPrincipalType = errors.New("principal_type must be AGENT, HUMAN, PAT, or SERVICE")

// ScopeVocabularyError is returned when a grant includes a scope that is not
// registered in the scope vocabulary.
type ScopeVocabularyError struct {
	Scope string
}

func (e *ScopeVocabularyError) Error() string {
	return fmt.Sprintf("scope %q is not in the scope vocabulary", e.Scope)
}

// isStructurallyValidEgressScope reports whether scope is a well-formed egress
// connectivity-posture scope. Egress scopes name a destination HOST (data, not
// a catalog capability), so the grant path validates them structurally instead
// of against the bounded scope vocabulary. Accepted shapes (see ADR
// 2026-06-06-jit-credential-brokering-egress-gateway, Amendment 2026-06-07):
//
//	egress:connect              open — any host (no host segment)
//	egress:<host>:connect       exact host, or wildcard "*" / "*.suffix"
//
// The host segment must be non-empty and free of whitespace, "/", and ":" — a
// colon would denote an IPv6 literal (a v1 non-goal) or a malformed scope. The
// host's DNS validity is not enforced here: a host that never matches simply
// authorizes nothing, and the rego matcher (_host_matches) is the runtime gate.
func isStructurallyValidEgressScope(scope string) bool {
	if scope == "egress:connect" {
		return true
	}
	rest, ok := strings.CutPrefix(scope, "egress:")
	if !ok {
		return false
	}
	host, ok := strings.CutSuffix(rest, ":connect")
	if !ok {
		return false
	}
	return host != "" && !strings.ContainsAny(host, " \t:/")
}

// qualifiedConnectionScopeBase recognizes a repository/resource-qualified
// connection scope — connection:<name>:<category>:<qualifier>:<verb> — and
// returns its unqualified base (the qualifier segment removed). Qualifiers are
// grant data, not vocabulary (they name upstream resources — repositories,
// namespaces — an open set), so the grant path validates them structurally: the
// scope is grantable when its base exists in the vocabulary and the qualifier
// segment is a well-formed, lowercase resource name. See
// docs/adr/2026-07-22-connection-scope-qualifiers.md.
func qualifiedConnectionScopeBase(scope string) (string, bool) {
	parts := strings.Split(scope, ":")
	if len(parts) < 5 || parts[0] != "connection" {
		return "", false
	}
	qualifier := parts[len(parts)-2]
	if qualifier == "" || qualifier != strings.ToLower(qualifier) || strings.ContainsAny(qualifier, " \t/*") {
		return "", false
	}
	base := make([]string, 0, len(parts)-1)
	base = append(base, parts[:len(parts)-2]...)
	base = append(base, parts[len(parts)-1])
	return strings.Join(base, ":"), true
}

// recompileDebounce is the delay before a bundle recompilation fires. Multiple
// writes within this window are coalesced into a single recompilation.
const recompileDebounce = 500 * time.Millisecond

// gatewayRecompileDebounce is slightly longer than per-tenant debounce because
// the gateway bundle loads all tenant data and is more expensive to compile.
const gatewayRecompileDebounce = 1 * time.Second

// Service orchestrates grant, role, vocabulary, and bundle management, including the
// bundle recompilation side-effect triggered on every write.
type Service struct {
	grantRepo        grants.Repository
	vocabRepo        vocabulary.Repository
	roleRepo         roles.Repository
	tenantBudgetRepo tenantbudget.Repository
	bundleSrv        *bundle.Server
	routeStore       *routes.Store
	logger           logging.Logger

	// recompileMu protects recompileTimers and lastWriteAt from concurrent map access.
	recompileMu     sync.Mutex
	recompileTimers map[string]*time.Timer
	lastWriteAt     map[string]time.Time

	gatewayRecompileTimer *time.Timer
}

// NewService creates a Service wired to the provided repositories and bundle server.
func NewService(
	grantRepo grants.Repository,
	vocabRepo vocabulary.Repository,
	roleRepo roles.Repository,
	tenantBudgetRepo tenantbudget.Repository,
	bundleSrv *bundle.Server,
	routeStore *routes.Store,
	logger logging.Logger,
) *Service {
	return &Service{
		grantRepo:        grantRepo,
		vocabRepo:        vocabRepo,
		roleRepo:         roleRepo,
		tenantBudgetRepo: tenantBudgetRepo,
		bundleSrv:        bundleSrv,
		routeStore:       routeStore,
		logger:           logger,
		recompileTimers:  make(map[string]*time.Timer),
		lastWriteAt:      make(map[string]time.Time),
	}
}

// BundleHandler returns the HTTP handler that serves compiled OPA bundles to
// enforcement points. Registered by the Controller at GET /bundles/{tenant_id}.
func (s *Service) BundleHandler() http.Handler {
	return s.bundleSrv
}

// GatewayBundleHandler returns the HTTP handler that serves the cross-tenant
// gateway bundle. Registered by the Controller at GET /bundles/gateway.
func (s *Service) GatewayBundleHandler() http.HandlerFunc {
	return s.bundleSrv.ServeGatewayHTTP
}

// CreateGrant persists a grant after validating principal type and scopes.
// delegationRequired stamps the fail-closed delegated-agent marker on the grant
// (delegated-agent-identity ADR, Decision 2); false for ordinary grants.
func (s *Service) CreateGrant(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, scopes []string, budget *grants.BudgetAssignment, delegationRequired bool) (*grants.Grant, error) {
	switch principalType {
	case grants.PrincipalTypeAgent, grants.PrincipalTypeHuman, grants.PrincipalTypePAT, grants.PrincipalTypeService:
	default:
		return nil, ErrInvalidPrincipalType
	}

	for _, scope := range scopes {
		// Egress connectivity-posture scopes (egress:<host>:connect) name a
		// destination HOST — open-ended data, not a bounded catalog capability —
		// so they are validated structurally rather than against the scope
		// vocabulary (there is no finite set of hostnames to pre-register).
		// See ADR 2026-06-06-jit-credential-brokering-egress-gateway
		// (Amendment 2026-06-07).
		if isStructurallyValidEgressScope(scope) {
			continue
		}
		exists, err := s.vocabRepo.Exists(ctx, scope)
		if err != nil {
			return nil, fmt.Errorf("vocabulary check: %w", err)
		}
		if !exists {
			// Resource-qualified connection scopes are grant data over an open
			// upstream set — validated structurally against their unqualified
			// vocabulary base, mirroring egress host scopes above.
			if base, ok := qualifiedConnectionScopeBase(scope); ok {
				exists, err = s.vocabRepo.Exists(ctx, base)
				if err != nil {
					return nil, fmt.Errorf("vocabulary check: %w", err)
				}
			}
		}
		if !exists {
			return nil, &ScopeVocabularyError{Scope: scope}
		}
	}

	g := &grants.Grant{
		TenantID:           tenantID,
		PrincipalType:      principalType,
		PrincipalID:        principalID,
		Scopes:             scopes,
		Budget:             budget,
		DelegationRequired: delegationRequired,
	}
	if err := s.grantRepo.Create(ctx, g); err != nil {
		return nil, fmt.Errorf("create grant: %w", err)
	}

	s.recordWrite(g.TenantID)
	s.scheduleRecompile(ctx, g.TenantID)
	s.scheduleGatewayRecompile(ctx)
	return g, nil
}

func (s *Service) GetGrant(ctx context.Context, id string) (*grants.Grant, error) {
	return s.grantRepo.GetByID(ctx, id)
}

// UpsertTenantBudget creates or replaces the tenant's rollup spending cap and
// schedules a bundle recompile so enforcement picks it up.
func (s *Service) UpsertTenantBudget(ctx context.Context, tenantID string, limitUSD float64, cadence string) (*tenantbudget.TenantBudget, error) {
	tb := &tenantbudget.TenantBudget{TenantID: tenantID, LimitUSD: limitUSD, ResetCadence: cadence}
	if err := s.tenantBudgetRepo.Upsert(ctx, tb); err != nil {
		return nil, err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return tb, nil
}

// GetTenantBudget returns the tenant's rollup spending cap.
func (s *Service) GetTenantBudget(ctx context.Context, tenantID string) (*tenantbudget.TenantBudget, error) {
	return s.tenantBudgetRepo.Get(ctx, tenantID)
}

// DeleteTenantBudget removes the tenant's rollup spending cap and schedules a
// bundle recompile.
func (s *Service) DeleteTenantBudget(ctx context.Context, tenantID string) error {
	if err := s.tenantBudgetRepo.Delete(ctx, tenantID); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	return nil
}

// UpdateGrantBudget replaces the budget assignment on an existing grant in
// place. The grant ID is preserved so the period's spend-attribution counters
// remain attached. A nil budget removes the assignment.
func (s *Service) UpdateGrantBudget(ctx context.Context, id, tenantID string, budget *grants.BudgetAssignment) (*grants.Grant, error) {
	if err := s.grantRepo.UpdateBudget(ctx, id, tenantID, budget); err != nil {
		return nil, err
	}
	g, err := s.grantRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
	return g, nil
}

func (s *Service) DeleteGrant(ctx context.Context, id string, tenantID string) error {
	if err := s.grantRepo.Delete(ctx, id, tenantID); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
	return nil
}

func (s *Service) ListGrants(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, limit, offset int) ([]*grants.Grant, int, error) {
	return s.grantRepo.List(ctx, tenantID, principalType, principalID, limit, offset)
}

func (s *Service) ListVocabulary(ctx context.Context, resourceType string) ([]*vocabulary.ScopeEntry, error) {
	return s.vocabRepo.List(ctx, resourceType)
}

// ListVocabularyWithUsage returns all scopes with grant and role usage counts
// scoped to a specific tenant. The admin role is not stored, so its usage is
// added here.
func (s *Service) ListVocabularyWithUsage(ctx context.Context, resourceType, tenantID string) ([]*vocabulary.ScopeEntryWithUsage, error) {
	entries, err := s.vocabRepo.ListWithUsage(ctx, resourceType, tenantID)
	if err != nil {
		return nil, err
	}
	admin, err := s.adminScopes(ctx)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if slices.Contains(admin, e.Scope) {
			e.RoleCount++
		}
	}
	return entries, nil
}

// adminScopes derives the platform-admin scopes from the current vocabulary.
func (s *Service) adminScopes(ctx context.Context) ([]string, error) {
	vocab, err := s.vocabRepo.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list vocabulary: %w", err)
	}
	return bundle.AdminScopes(vocab), nil
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
			Total:    1 + customRoleCount,
			Reserved: 1,
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

// EffectiveScopes returns the union of all scopes a principal holds, derived from
// its OIDC roles (reserved + custom) and any explicit grants matching its type + id.
// Used by the effective-scopes endpoint (PAT scope selector UI) and by the grant
// anti-escalation ceiling.
//
// Roles are credited only for HUMAN principals — the edge (authz_gateway.rego)
// grants role-based authz to HUMAN only, so crediting a non-HUMAN token's `roles`
// claim here would let it assign, via the ceiling, scopes it cannot itself exercise.
// Grants apply to every principal type (an AGENT orchestrating the platform uses
// its own grants regardless of type).
func (s *Service) EffectiveScopes(ctx context.Context, tenantID, principalType, principalID string, principalRoles []string) ([]string, error) {
	scopeSet := make(map[string]struct{})

	if grants.PrincipalType(principalType) == grants.PrincipalTypeHuman {
		if err := s.addRoleScopes(ctx, tenantID, principalRoles, scopeSet); err != nil {
			return nil, err
		}
	}
	if err := s.addGrantScopes(ctx, tenantID, grants.PrincipalType(principalType), principalID, scopeSet); err != nil {
		return nil, err
	}

	// Expand family-level scopes (e.g. "inference:anthropic:use") to also include
	// any more-specific vocabulary entries they cover (e.g. "inference:anthropic:claude-sonnet-4-6:use").
	// This lets users create PATs with finer-grained scopes when they hold a
	// parent scope that implies access to all children. Requires are expanded
	// too, so the ceiling matches what the bundle grants.
	allVocab, err := s.vocabRepo.List(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("list vocabulary for scope expansion: %w", err)
	}
	expandScopeFamily(allVocab, scopeSet)
	for _, sc := range bundle.ExpandRequires(slices.Collect(maps.Keys(scopeSet)), allVocab) {
		scopeSet[sc] = struct{}{}
	}

	result := make([]string, 0, len(scopeSet))
	for sc := range scopeSet {
		result = append(result, sc)
	}
	sort.Strings(result)
	return result, nil
}

// addRoleScopes populates scopeSet with scopes derived from the principal's OIDC roles.
func (s *Service) addRoleScopes(ctx context.Context, tenantID string, principalRoles []string, scopeSet map[string]struct{}) error {
	for _, role := range principalRoles {
		role = strings.TrimSpace(role)
		if role == "" {
			continue
		}
		if role == bundle.PlatformAdminRole {
			admin, err := s.adminScopes(ctx)
			if err != nil {
				return err
			}
			for _, sc := range admin {
				scopeSet[sc] = struct{}{}
			}
			continue
		}
		entry, err := s.roleRepo.GetByRole(ctx, tenantID, role)
		if err != nil {
			if !errors.Is(err, roles.ErrRoleNotFound) {
				return fmt.Errorf("get role scopes: %w", err)
			}
			continue
		}
		for _, sc := range entry.Scopes {
			scopeSet[sc] = struct{}{}
		}
	}
	return nil
}

// addGrantScopes populates scopeSet with scopes from explicit grants held by the
// principal (matched by its type + id).
func (s *Service) addGrantScopes(ctx context.Context, tenantID string, principalType grants.PrincipalType, principalID string, scopeSet map[string]struct{}) error {
	grantList, _, err := s.grantRepo.List(ctx, tenantID, principalType, principalID, 0, 0)
	if err != nil {
		return fmt.Errorf("list grants: %w", err)
	}
	for _, g := range grantList {
		for _, sc := range g.Scopes {
			scopeSet[sc] = struct{}{}
		}
	}
	return nil
}

// expandScopeFamily expands each scope in scopeSet to also include any
// vocabulary entries covered by that scope (parent implies children).
func expandScopeFamily(allVocab []*vocabulary.ScopeEntry, scopeSet map[string]struct{}) {
	initial := make([]string, 0, len(scopeSet))
	for sc := range scopeSet {
		initial = append(initial, sc)
	}
	for _, entry := range allVocab {
		for _, granted := range initial {
			if scopeCovers(granted, entry.Scope) {
				scopeSet[entry.Scope] = struct{}{}
				break
			}
		}
	}
}

// scopeCovers mirrors the Rego scope_covers rule: a covers b when they share
// the same resource type (first segment) and action (last segment), and a's
// path segments (middle) are a prefix of b's path segments.
func scopeCovers(a, b string) bool {
	if a == b {
		return true
	}
	partsA := strings.Split(a, ":")
	partsB := strings.Split(b, ":")
	if len(partsA) < 2 || len(partsB) < 2 {
		return false
	}
	if partsA[0] != partsB[0] || partsA[len(partsA)-1] != partsB[len(partsB)-1] {
		return false
	}
	pathA := partsA[1 : len(partsA)-1]
	pathB := partsB[1 : len(partsB)-1]
	if len(pathA) > len(pathB) {
		return false
	}
	for i, seg := range pathA {
		if pathB[i] != seg {
			return false
		}
	}
	return true
}

// UpsertRole creates or replaces the scope mapping for a role within a tenant.
// All scopes must exist in the vocabulary. Reserved platform admin roles cannot
// be modified.
func (s *Service) UpsertRole(ctx context.Context, tenantID, role string, scopes []string, budget *roles.BudgetAssignment) (*roles.RoleEntry, error) {
	if role == bundle.PlatformAdminRole {
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
	entry := &roles.RoleEntry{TenantID: tenantID, Role: role, Scopes: scopes, Budget: budget}
	if err := s.roleRepo.Upsert(ctx, entry); err != nil {
		return nil, fmt.Errorf("upsert role: %w", err)
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
	return entry, nil
}

// GetRole returns the scope mapping for a specific role within a tenant.
func (s *Service) GetRole(ctx context.Context, tenantID, role string) (*roles.RoleEntry, error) {
	return s.roleRepo.GetByRole(ctx, tenantID, role)
}

// ListRoles returns all role→scope mappings for a tenant, with the derived
// admin role prepended on the first page so the dashboard can display it.
func (s *Service) ListRoles(ctx context.Context, tenantID string, limit, offset int) ([]*roles.RoleEntry, error) {
	custom, err := s.roleRepo.ListByTenant(ctx, tenantID, limit, offset)
	if err != nil {
		return nil, err
	}

	// Only prepend reserved roles on the first page.
	if offset > 0 {
		return custom, nil
	}

	admin, err := s.adminScopes(ctx)
	if err != nil {
		return nil, err
	}
	reserved := &roles.RoleEntry{
		ID:       "reserved-" + bundle.PlatformAdminRole,
		TenantID: tenantID,
		Role:     bundle.PlatformAdminRole,
		Scopes:   admin,
	}
	return append([]*roles.RoleEntry{reserved}, custom...), nil
}

// DeleteRole removes the scope mapping for a role within a tenant.
func (s *Service) DeleteRole(ctx context.Context, tenantID, role string) error {
	if role == bundle.PlatformAdminRole {
		return &ReservedRoleError{Role: role}
	}
	if err := s.roleRepo.Delete(ctx, tenantID, role); err != nil {
		return err
	}
	s.recordWrite(tenantID)
	s.scheduleRecompile(ctx, tenantID)
	s.scheduleGatewayRecompile(ctx)
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
	return s.RegisterRoutesWithPrune(ctx, service, tenantID, routeScopes, scopeDecls, true)
}

// RegisterRoutesWithPrune is RegisterRoutes with explicit control over the
// destructive orphan-prune. With prune=false, a global registration upserts the
// declared scopes but does NOT run DeleteOrphans/PurgeScopeFromAll — the
// tool-gateway uses this to publish newly-discovered tools while its
// introspection keep-list is still incomplete, so a partial list cannot revoke
// live mcp scopes from grants and roles. Tenant registrations never prune, so
// prune is a no-op for them.
func (s *Service) RegisterRoutesWithPrune(ctx context.Context, service, tenantID string, routeScopes []routes.RouteScope, scopeDecls []routes.ScopeDeclaration, prune bool) error {
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
			Requires:     scopeDecls[i].Requires,
		}); err != nil {
			return fmt.Errorf("upsert scope %q: %w", scopeDecls[i].Scope, err)
		}
	}

	// For global registrations, reconcile owned scopes: any scope previously
	// registered by this service that is no longer in the catalog means the
	// virtual model was deleted. Remove it from vocab and cascade to grants/roles.
	if tenantID == "" && prune {
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
	s.scheduleGatewayRecompile(ctx)

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

// scheduleGatewayRecompile debounces gateway bundle recompilation. Multiple
// policy changes within the debounce window trigger only one gateway recompile.
func (s *Service) scheduleGatewayRecompile(ctx context.Context) {
	detachedCtx := context.WithoutCancel(ctx)
	s.recompileMu.Lock()
	defer s.recompileMu.Unlock()

	if s.gatewayRecompileTimer != nil {
		s.gatewayRecompileTimer.Stop()
	}
	s.gatewayRecompileTimer = time.AfterFunc(gatewayRecompileDebounce, func() {
		s.recompileMu.Lock()
		s.gatewayRecompileTimer = nil
		s.recompileMu.Unlock()

		if err := s.bundleSrv.RecompileGateway(detachedCtx); err != nil {
			s.logger.Error(detachedCtx, "gateway bundle recompile failed",
				logging.Field{Key: "error", Value: err.Error()},
			)
		}
	})
}

// scheduleRecompileAll recompiles all tenant bundles after a global route change.
// Must be called in a goroutine — it blocks until all tenants are recompiled.
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
