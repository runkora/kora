package api

import (
	"context"
	"fmt"
	"maps"

	"github.com/clear-route/orbit/internal/policyservice/grants"
)

// MetricsSnapshot is the tenant-aggregate view consumed only by the
// observable-gauge callbacks in the policy-service metrics instrument set
// (internal/policyservice/metrics.go). It is the deliberate mirror image of
// Stats() (service.go:334-399): Stats() is scoped to exactly one tenant per
// call, specifically to avoid leaking cross-tenant counts through that
// endpoint (service.go:360-362). MetricsSnapshot instead sums across every
// known tenant and never returns a tenant ID — only counts cross the
// boundary, so the existing isolation is not weakened. See
// docs/adr/2026-10-07-domain-metrics-instrumentation.md.
type MetricsSnapshot struct {
	// GrantsByPrincipalType is the total grant count across all tenants,
	// broken down by principal type.
	GrantsByPrincipalType map[grants.PrincipalType]int
	// RolesByKind is the total role count across all tenants, broken down by
	// kind: "custom" (rows in policy_role_scopes, summed across tenants) and
	// "reserved" (the derived platform-admin role every known tenant has —
	// mirrors Stats()'s per-tenant Reserved: 1 constant, extended across all
	// known tenants; it is not stored, so no repository query can count it).
	RolesByKind map[string]int
	// StaleBundles is the count of known tenants whose cached compiled bundle
	// predates their last write — the same staleness check Stats() makes for
	// a single tenant (service.go:365-370), aggregated to a count only.
	StaleBundles int
}

// MetricsSnapshot aggregates grant, role, and bundle-staleness counts across
// every known tenant, for the policy-service observable gauges. This is a
// new, separate query path: it does not change Stats(), its HTTP handler, or
// its response shape in any way, and it never surfaces a tenant ID — only
// aggregate counts leave this method.
func (s *Service) MetricsSnapshot(ctx context.Context) (MetricsSnapshot, error) {
	grantsByType, err := s.grantRepo.CountAllByPrincipalType(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count all grants: %w", err)
	}

	customRoles, err := s.roleRepo.CountAllByKind(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("count all roles: %w", err)
	}

	tenantIDs, err := s.tenants.KnownTenantIDs(ctx)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("list known tenants: %w", err)
	}

	rolesByKind := make(map[string]int, len(customRoles)+1)
	maps.Copy(rolesByKind, customRoles)
	rolesByKind["reserved"] = len(tenantIDs)

	stale := 0
	s.recompileMu.Lock()
	for _, tenantID := range tenantIDs {
		cached := s.bundleSrv.Compiler().Get(tenantID)
		if cached == nil {
			continue
		}
		if lastWrite, ok := s.lastWriteAt[tenantID]; ok && lastWrite.After(cached.CompiledAt) {
			stale++
		}
	}
	s.recompileMu.Unlock()

	return MetricsSnapshot{
		GrantsByPrincipalType: grantsByType,
		RolesByKind:           rolesByKind,
		StaleBundles:          stale,
	}, nil
}
