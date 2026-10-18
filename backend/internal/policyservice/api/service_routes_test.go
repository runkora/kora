package api

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// emptyVocab knows no scope.
type emptyVocab struct{ vocabulary.Repository }

func (emptyVocab) Exists(context.Context, string) (bool, error) { return false, nil }

// TestRegisterRoutes_TenantRouteNeedsScope keeps authenticated-only routes to
// platform services: a tenant route with an empty scope fails the vocabulary check.
func TestRegisterRoutes_TenantRouteNeedsScope(t *testing.T) {
	tests := []struct {
		name  string
		route routes.RouteScope
	}{
		{"empty scope", routes.RouteScope{PathPrefix: "/v1/thing", Method: "GET"}},
		{"empty scope with marker", routes.RouteScope{PathPrefix: "/v1/thing", Method: "GET", AuthenticatedOnly: true}},
		{"empty prefix catch-all", routes.RouteScope{PathPrefix: "", Method: "*"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := &Service{vocabRepo: emptyVocab{}, routeStore: routes.NewStore()}
			err := s.RegisterRoutes(context.Background(), "tenant-svc", "tenant-1", []routes.RouteScope{tc.route}, nil)
			var scopeErr *ScopeVocabularyError
			if !errors.As(err, &scopeErr) {
				t.Fatalf("err = %v, want ScopeVocabularyError", err)
			}
			if _, tenant := s.routeStore.RoutesForTenant("tenant-1"); len(tenant) != 0 {
				t.Errorf("route stored after rejection: %+v", tenant)
			}
		})
	}
}

func TestMarkAuthenticatedOnly(t *testing.T) {
	tests := []struct {
		name string
		in   routes.RouteScope
		want bool
	}{
		{"empty scope is marked", routes.RouteScope{PathPrefix: "/a", Method: "GET"}, true},
		{"caller marker on a scoped route is cleared", routes.RouteScope{PathPrefix: "/b", Method: "GET", RequiredScope: "b:read", AuthenticatedOnly: true}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := []routes.RouteScope{tc.in}
			got := markAuthenticatedOnly(in)
			if got[0].AuthenticatedOnly != tc.want {
				t.Errorf("AuthenticatedOnly = %v, want %v", got[0].AuthenticatedOnly, tc.want)
			}
			if in[0] != tc.in {
				t.Error("input slice was modified")
			}
		})
	}
}
