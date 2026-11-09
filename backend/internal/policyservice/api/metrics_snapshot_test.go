package api

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/bundle"
	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
)

// countingGrants answers CountAllByPrincipalType with a fixed, already
// cross-tenant map, standing in for the repository's own SQL aggregate.
type countingGrants struct {
	grants.Repository
	counts map[grants.PrincipalType]int
}

func (g countingGrants) CountAllByPrincipalType(context.Context) (map[grants.PrincipalType]int, error) {
	return g.counts, nil
}

// countingRoles answers CountAllByKind with a fixed custom-role count,
// standing in for the repository's own SQL aggregate.
type countingRoles struct {
	roles.Repository
	counts map[string]int
}

func (r countingRoles) CountAllByKind(context.Context) (map[string]int, error) {
	return r.counts, nil
}

// TestMetricsSnapshot_AggregatesAcrossTenants pins the tenant-aggregate
// contract MetricsSnapshot exists to uphold (metrics_snapshot.go:11-19):
// Stats() is deliberately scoped to one tenant per call to avoid leaking
// cross-tenant counts (service.go:360-362), and MetricsSnapshot is the
// mirror image that sums across every known tenant instead. This test
// pins both halves of that contract: the returned counts are genuine sums
// across tenants (not one tenant's figures, not a per-tenant list), and no
// tenant identifier ever appears in the result.
func TestMetricsSnapshot_AggregatesAcrossTenants(t *testing.T) {
	tenantIDs := []string{"tenant-a", "tenant-b", "tenant-c"}

	compiler := bundle.NewCompiler(bundle.Config{}, routes.NewStore())
	for _, tenantID := range tenantIDs {
		if _, err := compiler.Compile(tenantID, nil, nil, nil, nil, nil); err != nil {
			t.Fatalf("Compile(%s): %v", tenantID, err)
		}
	}
	bundleSrv := bundle.NewServer(compiler, nil, nil, nil, nil, nil, nil, logging.NewLoggerWithOutput(io.Discard))

	grantCounts := map[grants.PrincipalType]int{
		grants.PrincipalTypeAgent: 7,
		grants.PrincipalTypeHuman: 3,
	}
	roleCounts := map[string]int{"custom": 4}

	s := &Service{
		grantRepo: countingGrants{counts: grantCounts},
		roleRepo:  countingRoles{counts: roleCounts},
		tenants:   fakeTenantLister{tenants: tenantIDs},
		bundleSrv: bundleSrv,
		// tenant-a and tenant-b wrote after their bundle was compiled (stale);
		// tenant-c has no entry at all, i.e. never written since startup, so
		// it must not be counted stale.
		lastWriteAt: map[string]time.Time{
			"tenant-a": time.Now().Add(time.Hour),
			"tenant-b": time.Now().Add(time.Hour),
		},
	}

	snapshot, err := s.MetricsSnapshot(context.Background())
	if err != nil {
		t.Fatalf("MetricsSnapshot: %v", err)
	}

	// The grant and role breakdowns pass through the repositories' own
	// cross-tenant aggregates unchanged — MetricsSnapshot must not re-slice
	// them per tenant.
	if len(snapshot.GrantsByPrincipalType) != len(grantCounts) {
		t.Fatalf("GrantsByPrincipalType = %v, want %v", snapshot.GrantsByPrincipalType, grantCounts)
	}
	for pt, want := range grantCounts {
		if got := snapshot.GrantsByPrincipalType[pt]; got != want {
			t.Errorf("GrantsByPrincipalType[%s] = %d, want %d", pt, got, want)
		}
	}

	if got := snapshot.RolesByKind["custom"]; got != 4 {
		t.Errorf("RolesByKind[custom] = %d, want 4 (pass-through of the repository aggregate)", got)
	}
	// "reserved" is derived as one platform-admin role per known tenant, so
	// it must equal the tenant count, not any individual tenant's figure.
	if got := snapshot.RolesByKind["reserved"]; got != len(tenantIDs) {
		t.Errorf("RolesByKind[reserved] = %d, want %d (one per known tenant, summed)", got, len(tenantIDs))
	}

	// StaleBundles must be the COUNT of stale tenants (2), never a per-tenant
	// list and never just one tenant's staleness.
	if snapshot.StaleBundles != 2 {
		t.Errorf("StaleBundles = %d, want 2 (summed across tenant-a and tenant-b only)", snapshot.StaleBundles)
	}

	// The defining leak-prevention property: nothing in the serialized
	// output names a tenant. Stats() carries a tenant_id deliberately;
	// MetricsSnapshot must never carry one, under any field name.
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	for _, tenantID := range tenantIDs {
		if strings.Contains(string(encoded), tenantID) {
			t.Errorf("snapshot leaked tenant identifier %q: %s", tenantID, encoded)
		}
	}
}
