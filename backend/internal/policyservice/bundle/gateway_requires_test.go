package bundle_test

import (
	"testing"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
	"github.com/runkora/kora/backend/internal/policyservice/routes"
	"github.com/runkora/kora/backend/internal/policyservice/vocabulary"
)

// TestGateway_RequiresExpansion_RolesAndGrants pins the gateway bundle to the
// same requires expansion the per-tenant bundle applies: a role or grant that
// bundles a tool scope confers the tool's upstream scope at the edge.
func TestGateway_RequiresExpansion_RolesAndGrants(t *testing.T) {
	jwksSrv := startJWKSServer(t)

	vocab := []*vocabulary.ScopeEntry{
		{Scope: "inference:list", ResourceType: "inference", Action: "list"},
		{
			Scope:        "mcp:kora:models:list:use",
			ResourceType: "mcp",
			PathSegments: []string{"kora", "models", "list"},
			Action:       "use",
			Requires:     []string{"inference:list"},
		},
	}
	route := []routes.RouteScope{{PathPrefix: "/v1/models", Method: "GET", RequiredScope: "inference:list"}}

	t.Run("role holding the covering tool scope", func(t *testing.T) {
		pq := compileAndPrepareGateway(t, gatewayBundleOpts{
			jwksURL:      jwksSrv.URL + "/jwks",
			tenantIDs:    []string{testTenantID},
			vocab:        vocab,
			globalRoutes: route,
			roles: map[string][]*roles.RoleEntry{
				testTenantID: {{TenantID: testTenantID, Role: "engineer", Scopes: []string{"mcp:kora:use"}}},
			},
		})
		jwt := signTestJWT(t, "HUMAN", testTenantID, "user-1", map[string]any{
			"claims": map[string]any{"roles": "engineer"},
		})
		allow, reason := evalGateway(t, pq, "GET", "/v1/models", map[string]string{"authorization": "Bearer " + jwt})
		if !allow || reason != "role:engineer" {
			t.Fatalf("allow=%v reason=%q, want allow via role:engineer", allow, reason)
		}
	})

	t.Run("grant holding the covering tool scope", func(t *testing.T) {
		pq := compileAndPrepareGateway(t, gatewayBundleOpts{
			jwksURL:      jwksSrv.URL + "/jwks",
			tenantIDs:    []string{testTenantID},
			vocab:        vocab,
			globalRoutes: route,
			grants: map[string][]*grants.Grant{
				testTenantID: {{TenantID: testTenantID, PrincipalType: grants.PrincipalTypeHuman, PrincipalID: "user-2", Scopes: []string{"mcp:kora:use"}}},
			},
		})
		jwt := signTestJWT(t, "HUMAN", testTenantID, "user-2", nil)
		allow, reason := evalGateway(t, pq, "GET", "/v1/models", map[string]string{"authorization": "Bearer " + jwt})
		if !allow || reason != "grant:inference:list" {
			t.Fatalf("allow=%v reason=%q, want allow via grant:inference:list", allow, reason)
		}
	})
}
