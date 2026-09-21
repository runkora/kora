package api

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/policyservice/routes"
	"github.com/clear-route/orbit/internal/policyservice/vocabulary"
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
		vocabRepo:       idempotentVocab{},
		routeStore:      routes.NewStore(),
		tenants:         fakeTenantLister{tenants: []string{"tenant-1"}},
		logger:          logging.NewLoggerWithOutput(io.Discard),
		recompileTimers: make(map[string]*time.Timer),
		lastWriteAt:     make(map[string]time.Time),
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
