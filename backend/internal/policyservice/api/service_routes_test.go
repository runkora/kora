package api

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// permissiveVocab claims every scope exists, so only validation can refuse.
type permissiveVocab struct{ vocabulary.Repository }

func (permissiveVocab) Exists(context.Context, string, string) (bool, error) { return true, nil }

func (permissiveVocab) Upsert(context.Context, *vocabulary.ScopeEntry) (bool, error) {
	return false, nil
}

// TestRegisterRoutes_RefusesUnscopedTenantRoutes keeps authenticated-only routes
// to platform services, even when the tenant declares an empty scope itself.
func TestRegisterRoutes_RefusesUnscopedTenantRoutes(t *testing.T) {
	tests := []struct {
		name  string
		route routes.RouteScope
		decls []routes.ScopeDeclaration
	}{
		{"empty scope", routes.RouteScope{PathPrefix: "/v1/thing", Method: "GET"}, nil},
		{"empty scope with marker", routes.RouteScope{PathPrefix: "/v1/thing", Method: "GET", AuthenticatedOnly: true}, nil},
		{"empty prefix", routes.RouteScope{PathPrefix: "", Method: "*", RequiredScope: "thing:read"}, nil},
		{"empty scope declared by the tenant", routes.RouteScope{PathPrefix: "/v1/thing", Method: "GET"}, []routes.ScopeDeclaration{{Scope: ""}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{vocabRepo: permissiveVocab{}, routeStore: routes.NewStore()}
			err := s.RegisterRoutes(context.Background(), "tenant-svc", "tenant-1", []routes.RouteScope{tc.route}, tc.decls)
			if !errors.Is(err, ErrInvalidRegistration) {
				t.Fatalf("err = %v, want ErrInvalidRegistration", err)
			}
			if _, tenant := s.routeStore.RoutesForTenant("tenant-1"); len(tenant) != 0 {
				t.Errorf("route stored after rejection: %+v", tenant)
			}
		})
	}
}

func TestMarkAuthenticatedOnly(t *testing.T) {
	tests := []struct {
		name   string
		tenant string
		in     routes.RouteScope
		want   bool
	}{
		{"global empty scope is marked", "", routes.RouteScope{PathPrefix: "/a", Method: "GET"}, true},
		{"caller marker on a scoped route is cleared", "", routes.RouteScope{PathPrefix: "/b", Method: "GET", RequiredScope: "b:read", AuthenticatedOnly: true}, false},
		{"tenant route is never marked", "tenant-1", routes.RouteScope{PathPrefix: "/c", Method: "GET", AuthenticatedOnly: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := []routes.RouteScope{tc.in}
			got := markAuthenticatedOnly(tc.tenant, in)
			if got[0].AuthenticatedOnly != tc.want {
				t.Errorf("AuthenticatedOnly = %v, want %v", got[0].AuthenticatedOnly, tc.want)
			}
			if in[0] != tc.in {
				t.Error("input slice was modified")
			}
		})
	}
}
