package agents

import (
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
)

// RunUsageTotals against real SQL; gated on ORBIT_TEST_DB_URL (see testRunRepo).
func TestPostgres_RunUsageTotals(t *testing.T) {
	r, tenant := testRunRepo(t)
	ctx := context.Background()
	t.Cleanup(func() { _, _ = r.pool.Exec(ctx, `DELETE FROM inference_usage WHERE tenant_id=$1`, tenant) })

	insert := func(tenantID, runID string, cost float64, in, out int) {
		t.Helper()
		if _, err := r.pool.Exec(ctx, `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, model_id, prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd, status, run_id)
			VALUES ($1, $2, 'p', 'AGENT', 'm', $3, $4, $3 + $4, $5, 'success', $6)`,
			uuid.NewString(), tenantID, in, out, cost, runID); err != nil {
			t.Fatalf("insert usage: %v", err)
		}
	}
	a, b, none := uuid.NewString(), uuid.NewString(), uuid.NewString()
	insert(tenant, a, 0.25, 100, 10)
	insert(tenant, a, 0.5, 200, 20)
	insert(tenant, b, 1, 5, 5)
	// Another tenant's row for run a must not count.
	insert("other-"+tenant, a, 9, 999, 999)
	t.Cleanup(func() { _, _ = r.pool.Exec(ctx, `DELETE FROM inference_usage WHERE tenant_id=$1`, "other-"+tenant) })

	got, err := r.RunUsageTotals(ctx, tenant, []string{a, b, none})
	if err != nil {
		t.Fatalf("RunUsageTotals: %v", err)
	}
	if u := got[a]; math.Abs(u.CostUSD-0.75) > 1e-9 || u.InputTokens != 300 || u.OutputTokens != 30 || u.RequestCount != 2 {
		t.Errorf("run a = %+v, want 0.75 cost, 300 in, 30 out, 2 requests", u)
	}
	if u := got[b]; u.RequestCount != 1 || u.CostUSD != 1 {
		t.Errorf("run b = %+v", u)
	}
	if _, ok := got[none]; ok {
		t.Errorf("run with no usage rows should be absent, got %+v", got[none])
	}
	if empty, err := r.RunUsageTotals(ctx, tenant, nil); err != nil || len(empty) != 0 {
		t.Errorf("no ids = %v, %v", empty, err)
	}
}
