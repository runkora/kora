// Package routes provides the in-memory registration store for route→scope mappings.
// Services register their routes at startup; the policy service merges global and
// tenant-specific registrations into each tenant's OPA bundle at compile time.
package routes

import (
	"slices"
	"sync"
	"time"
)

// RouteScope maps an HTTP path prefix and method to the scope required to access it.
type RouteScope struct {
	PathPrefix    string `json:"path_prefix"`
	Method        string `json:"method"`
	RequiredScope string `json:"required_scope"`
	// AuthenticatedOnly marks a route that needs a valid token and no scope.
	// The service behind it holds the real check. Set from an empty
	// RequiredScope at registration.
	AuthenticatedOnly bool `json:"authenticated_only,omitempty"`
}

// ScopeDeclaration declares a scope that this service owns. The policy service
// upserts these into the vocabulary so grants and roles can reference them.
// ResourceType, PathSegments, and Action mirror the vocabulary.ScopeEntry fields.
type ScopeDeclaration struct {
	Scope        string   `json:"scope"`
	ResourceType string   `json:"resource_type"`
	PathSegments []string `json:"path"`
	Action       string   `json:"action"`
	Description  string   `json:"description,omitempty"`
	// Requires lists scopes this scope depends on (see vocabulary.ScopeEntry.Requires).
	// Absent or null is unknown and keeps the stored set; [] clears it.
	Requires []string `json:"requires,omitempty"`
}

// Registration is a single service's route and scope declaration.
// TenantID == "" means global — compiled into every tenant bundle.
// TenantID set means tenant-scoped — compiled into that tenant's bundle only.
type Registration struct {
	Service      string             `json:"service"`
	TenantID     string             `json:"tenant_id"`
	Routes       []RouteScope       `json:"routes"`
	Scopes       []ScopeDeclaration `json:"scopes,omitempty"`
	RegisteredAt time.Time          `json:"registered_at"`
}

type registrationKey struct {
	service  string
	tenantID string
}

// Store holds in-memory route registrations keyed by (service, tenant_id).
// All methods are safe for concurrent use. Registrations are lost on restart;
// services re-register at startup so this is the expected behavior.
type Store struct {
	mu            sync.RWMutex
	registrations map[registrationKey]*Registration
}

// NewStore creates an empty Store.
func NewStore() *Store {
	return &Store{
		registrations: make(map[registrationKey]*Registration),
	}
}

// Register records a service's routes and scopes, replacing any previous
// registration for the same service and tenant. It reports whether the routes
// or scopes differ from what was already registered, so a caller can tell a
// periodic re-registration that changed nothing from one that did.
func (s *Store) Register(reg *Registration) bool {
	key := registrationKey{service: reg.Service, tenantID: reg.TenantID}

	// Defensive copy so the caller cannot mutate store internals after Register returns.
	cpy := *reg
	cpy.Routes = make([]RouteScope, len(reg.Routes))
	copy(cpy.Routes, reg.Routes)
	if reg.Scopes != nil {
		cpy.Scopes = make([]ScopeDeclaration, len(reg.Scopes))
		copy(cpy.Scopes, reg.Scopes)
	}
	cpy.RegisteredAt = time.Now().UTC()

	s.mu.Lock()
	previous, existed := s.registrations[key]
	s.registrations[key] = &cpy
	s.mu.Unlock()

	if !existed {
		return true
	}
	// RegisteredAt moves on every call and says nothing about the content.
	return !slices.Equal(previous.Routes, cpy.Routes) ||
		!slices.EqualFunc(previous.Scopes, cpy.Scopes, sameScopeDeclaration)
}

// ListAll returns all registrations in the store, regardless of tenant.
// The returned slice is a snapshot — callers must not modify it.
func (s *Store) ListAll() []*Registration {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*Registration, 0, len(s.registrations))
	for _, reg := range s.registrations {
		result = append(result, reg)
	}
	return result
}

// GlobalRoutes returns only the global route scopes (TenantID == "").
// Used by the gateway bundle compiler to build the top-level routes document.
func (s *Store) GlobalRoutes() []RouteScope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]RouteScope, 0)
	for key, reg := range s.registrations {
		if key.tenantID == "" {
			result = append(result, reg.Routes...)
		}
	}
	return result
}

// AllTenantRoutes returns all tenant-scoped route registrations grouped by tenant ID.
// Global registrations (TenantID == "") are excluded.
// Used by the gateway bundle compiler to build per-tenant route documents.
func (s *Store) AllTenantRoutes() map[string][]RouteScope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string][]RouteScope)
	for key, reg := range s.registrations {
		if key.tenantID == "" {
			continue
		}
		result[key.tenantID] = append(result[key.tenantID], reg.Routes...)
	}
	return result
}

// RouteTenantIDs returns the distinct tenant IDs that have at least one
// tenant-scoped route registration (TenantID != "").
// Used by the gateway bundle compiler to include tenants that have routes but
// no grants, so requests to those routes return deny:invalid_token rather than
// the misleading deny:no_route_match.
func (s *Store) RouteTenantIDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]struct{})
	for key := range s.registrations {
		if key.tenantID != "" {
			seen[key.tenantID] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for tid := range seen {
		result = append(result, tid)
	}
	return result
}

// RoutesForTenant returns the global route scopes (tenant_id == "") and the
// route scopes registered for that tenant, kept apart so a global route can
// take precedence. Both slices are non-nil so they marshal to JSON [] rather
// than null.
func (s *Store) RoutesForTenant(tenantID string) (global, tenant []RouteScope) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	global, tenant = make([]RouteScope, 0), make([]RouteScope, 0)
	for key, reg := range s.registrations {
		switch key.tenantID {
		case "":
			global = append(global, reg.Routes...)
		case tenantID:
			tenant = append(tenant, reg.Routes...)
		}
	}
	return global, tenant
}

// sameScopeDeclaration compares two declarations by value. ScopeDeclaration
// holds slices, so it is not comparable and slices.Equal cannot be used on a
// slice of them.
func sameScopeDeclaration(a, b ScopeDeclaration) bool {
	return a.Scope == b.Scope &&
		a.ResourceType == b.ResourceType &&
		a.Action == b.Action &&
		a.Description == b.Description &&
		slices.Equal(a.PathSegments, b.PathSegments) &&
		slices.Equal(a.Requires, b.Requires)
}
