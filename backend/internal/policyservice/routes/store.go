// Package routes provides the in-memory registration store for route→scope mappings.
// Services register their routes at startup; the policy service merges global and
// tenant-specific registrations into each tenant's OPA bundle at compile time.
package routes

import (
	"sync"
	"time"
)

// RouteScope maps an HTTP path prefix and method to the scope required to access it.
type RouteScope struct {
	PathPrefix    string `json:"path_prefix"`
	Method        string `json:"method"`
	RequiredScope string `json:"required_scope"`
}

// Registration is a single service's route declaration.
// TenantID == "" means global — compiled into every tenant bundle.
// TenantID set means tenant-scoped — compiled into that tenant's bundle only.
type Registration struct {
	Service      string       `json:"service"`
	TenantID     string       `json:"tenant_id"`
	Routes       []RouteScope `json:"routes"`
	RegisteredAt time.Time    `json:"registered_at"`
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

// Register stores (or replaces) the route registration for a service+tenant pair.
// Idempotent: re-registration replaces the previous entry.
func (s *Store) Register(reg *Registration) {
	key := registrationKey{service: reg.Service, tenantID: reg.TenantID}
	reg.RegisteredAt = time.Now().UTC()
	s.mu.Lock()
	s.registrations[key] = reg
	s.mu.Unlock()
}

// RoutesForTenant returns the merged route scopes for a tenant:
// all global registrations (tenant_id == "") plus all registrations for that
// specific tenant. Always returns a non-nil slice so it marshals to JSON []
// rather than null when there are no registrations.
func (s *Store) RoutesForTenant(tenantID string) []RouteScope {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]RouteScope, 0)
	for key, reg := range s.registrations {
		if key.tenantID == "" || key.tenantID == tenantID {
			result = append(result, reg.Routes...)
		}
	}
	return result
}
