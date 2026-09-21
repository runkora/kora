package api

import (
	"context"
	"testing"
	"time"
)

// fakeTenantLister returns a fixed tenant set, standing in for the bundle
// server's union of the registry, tenants with grants and tenants with routes.
type fakeTenantLister struct {
	tenants []string
}

func (f fakeTenantLister) KnownTenantIDs(context.Context) ([]string, error) {
	return f.tenants, nil
}

// TestRecordWriteAll_CoversTenantsWithoutGrants pins the tenant set a global
// route registration marks dirty. A tenant whose principals hold roles alone
// has no grant rows, and listing tenants from the grant table left its bundle
// on whatever vocabulary existed when it was first compiled — none, on a fresh
// install, so every role in it resolved to zero scopes until a restart.
func TestRecordWriteAll_CoversTenantsWithoutGrants(t *testing.T) {
	roleOnlyTenant := "roles-but-no-grants"
	s := &Service{
		tenants:         fakeTenantLister{tenants: []string{"has-grants", roleOnlyTenant}},
		recompileTimers: make(map[string]*time.Timer),
		lastWriteAt:     make(map[string]time.Time),
	}

	s.recordWriteAll(context.Background())

	for _, tenantID := range []string{"has-grants", roleOnlyTenant} {
		if _, ok := s.lastWriteAt[tenantID]; !ok {
			t.Errorf("tenant %q was not marked dirty", tenantID)
		}
	}
}
