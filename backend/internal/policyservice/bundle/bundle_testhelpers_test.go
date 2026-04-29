package bundle_test

import (
	"context"
	"testing"

	"github.com/open-policy-agent/opa/v1/rego"
)

// testTenantID is the tenant used across both per-tenant and gateway bundle test suites.
const testTenantID = "test-tenant"

// evalPolicy evaluates a prepared OPA query and returns (allow, reason).
// Shared by authz_test.go and authz_gateway_test.go.
func evalPolicy(t *testing.T, pq rego.PreparedEvalQuery, input map[string]any) (bool, string) {
	t.Helper()
	rs, err := pq.Eval(context.Background(), rego.EvalInput(input))
	if err != nil {
		t.Fatalf("rego eval: %v", err)
	}
	if len(rs) == 0 || len(rs[0].Bindings) == 0 {
		return false, "no_result"
	}
	decision, ok := rs[0].Bindings["x"].(map[string]any)
	if !ok {
		return false, "no_result"
	}
	allow, _ := decision["allow"].(bool)
	reason, _ := decision["reason"].(string)
	return allow, reason
}
