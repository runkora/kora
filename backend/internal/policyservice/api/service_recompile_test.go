package api

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
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

// idempotentVocab is a vocabulary repository whose upserts change nothing,
// which is what a service re-registering an unchanged catalog produces.
type idempotentVocab struct{ vocabulary.Repository }

func (idempotentVocab) Upsert(context.Context, *vocabulary.ScopeEntry) (bool, error) {
	return false, nil
}

// TestRegisterRoutes_NoOpDoesNotRecompile covers the periodic re-registration
// every service performs. Recompiling each known tenant's bundle for a
// registration that moved nothing is load with no effect on the result.
func TestRegisterRoutes_NoOpDoesNotRecompile(t *testing.T) {
	declared := []routes.ScopeDeclaration{{
		Scope:        "inference:invoke",
		ResourceType: "inference",
		Action:       "invoke",
		PathSegments: []string{},
	}}
	declaredRoutes := []routes.RouteScope{{PathPrefix: "/v1/chat", Method: "POST", RequiredScope: "inference:invoke"}}

	s := &Service{
		vocabRepo:               idempotentVocab{},
		routeStore:              routes.NewStore(),
		tenants:                 fakeTenantLister{tenants: []string{"tenant-1"}},
		logger:                  logging.NewLoggerWithOutput(io.Discard),
		recompileTimers:         make(map[string]*time.Timer),
		lastWriteAt:             make(map[string]time.Time),
		lastKnownTenants:        []string{"tenant-1"},
		recompileAllHealthy:     true,
		gatewayRecompileHealthy: true,
	}
	// The catalog this service registered previously.
	s.routeStore.Register(&routes.Registration{Service: "inference-gateway", Routes: declaredRoutes, Scopes: declared})

	if err := s.RegisterRoutesWithPrune(context.Background(), "inference-gateway", "", declaredRoutes, declared, false); err != nil {
		t.Fatalf("RegisterRoutesWithPrune: %v", err)
	}

	if len(s.lastWriteAt) != 0 {
		t.Errorf("an unchanged registration marked %d tenants dirty, want 0", len(s.lastWriteAt))
	}
}

// TestGlobalStateChanged_TenantSetDrift covers a registry-only tenant: one
// that exists (an OrbitTenant CR) but holds no grant and no route, so it
// never shows up in a vocabulary or route diff. Without tracking the known
// tenant set separately, a periodic re-registration whose own catalog is
// unchanged would report changed=false and the new tenant's bundle — and the
// gateway bundle's document for it — would never compile until an unrelated
// write happened or the process restarted.
func TestGlobalStateChanged_TenantSetDrift(t *testing.T) {
	s := &Service{
		tenants:                 fakeTenantLister{tenants: []string{"tenant-1"}},
		lastKnownTenants:        []string{"tenant-1"},
		recompileAllHealthy:     true,
		gatewayRecompileHealthy: true,
	}

	changed, err := s.globalStateChanged(context.Background())
	if err != nil {
		t.Fatalf("globalStateChanged: %v", err)
	}
	if changed {
		t.Error("unchanged tenant set reported changed = true")
	}

	s.tenants = fakeTenantLister{tenants: []string{"tenant-1", "registry-only-tenant"}}
	changed, err = s.globalStateChanged(context.Background())
	if err != nil {
		t.Fatalf("globalStateChanged: %v", err)
	}
	if !changed {
		t.Error("a new tenant in the registry reported changed = false")
	}

	// The new snapshot is now the baseline: calling again with the same set
	// reports no further change.
	changed, err = s.globalStateChanged(context.Background())
	if err != nil {
		t.Fatalf("globalStateChanged: %v", err)
	}
	if changed {
		t.Error("the same tenant set reported changed = true on a second call")
	}
}

// TestGlobalStateChanged_RetriesAfterFailedRecompile covers a transient
// failure in the fire-and-forget recompile goroutines. Before a registration
// could skip recompiling on an unchanged catalog, every registration always
// attempted the recompile, so a failed attempt was retried by the next one
// regardless of what it changed. That retry has to survive the skip.
func TestGlobalStateChanged_RetriesAfterFailedRecompile(t *testing.T) {
	s := &Service{
		tenants:                 fakeTenantLister{tenants: []string{"tenant-1"}},
		lastKnownTenants:        []string{"tenant-1"},
		recompileAllHealthy:     true,
		gatewayRecompileHealthy: true,
	}

	if changed, err := s.globalStateChanged(context.Background()); err != nil || changed {
		t.Fatalf("changed = %v, err = %v; want false, nil before any failure", changed, err)
	}

	s.markRecompileAllHealth(false)
	if changed, err := s.globalStateChanged(context.Background()); err != nil || !changed {
		t.Fatalf("changed = %v, err = %v; want true, nil after scheduleRecompileAll failed", changed, err)
	}
	s.markRecompileAllHealth(true)

	s.markGatewayRecompileHealth(false)
	if changed, err := s.globalStateChanged(context.Background()); err != nil || !changed {
		t.Fatalf("changed = %v, err = %v; want true, nil after scheduleGatewayRecompile failed", changed, err)
	}
	s.markGatewayRecompileHealth(true)

	if changed, err := s.globalStateChanged(context.Background()); err != nil || changed {
		t.Fatalf("changed = %v, err = %v; want false, nil once both recompiles are healthy again", changed, err)
	}
}
