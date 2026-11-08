package policy

import (
	"context"
	"testing"

	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/storage/inmem"
)

const budgetKeyTestAgent = "spiffe://runkora.test/tenant/t/agent/coder"

// budgetKeyEngine returns an engine whose tenant bundle carries data.budgets
// keyed TYPE:id, with an AGENT and a HUMAN entry sharing one ID.
func budgetKeyEngine(t *testing.T) *engine {
	t.Helper()
	data := map[string]any{"budgets": map[string]any{
		"AGENT:" + budgetKeyTestAgent: map[string]any{
			"effective_limits": map[string]any{"monthly": 1.0},
			"categories":       []any{map[string]any{"source": "grant", "id": "g-agent", "limit_usd": 1.0, "reset_cadence": "monthly"}},
		},
		"HUMAN:" + budgetKeyTestAgent: map[string]any{
			"effective_limits": map[string]any{"monthly": 100.0},
			"categories":       []any{map[string]any{"source": "grant", "id": "g-human", "limit_usd": 100.0, "reset_cadence": "monthly"}},
		},
	}}
	prep := func(q string) rego.PreparedEvalQuery {
		pq, err := rego.New(rego.Query(q), rego.Store(inmem.NewFromObject(data))).PrepareForEval(context.Background())
		if err != nil {
			t.Fatalf("prepare %q: %v", q, err)
		}
		return pq
	}
	e := &engine{bundles: map[string]*tenantBundle{}}
	e.bundles["t1"] = &tenantBundle{categoriesQuery: prep(budgetCategoriesQuery), allBudgetsQuery: prep(allBudgetsQuery), roleBudgetsQuery: prep(roleBudgetsQuery)}
	return e
}

func TestGetBudgetCategories_FiltersByPrincipalType(t *testing.T) {
	e := budgetKeyEngine(t)
	for typ, wantID := range map[string]string{"AGENT": "g-agent", "HUMAN": "g-human"} {
		cats, err := e.GetBudgetCategories(context.Background(), "t1", typ, budgetKeyTestAgent)
		if err != nil || len(cats) != 1 || cats[0].ID != wantID {
			t.Errorf("%s: cats = %+v, err = %v, want only %s", typ, cats, err, wantID)
		}
	}
	if cats, _ := e.GetBudgetCategories(context.Background(), "t1", "PAT", budgetKeyTestAgent); len(cats) != 0 {
		t.Errorf("PAT must not see another type's categories, got %+v", cats)
	}
}

func TestListBudgets_SplitsAtFirstColon(t *testing.T) {
	e := budgetKeyEngine(t)
	got, _, err := e.ListBudgets(context.Background(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(got), got)
	}
	if pb, ok := got[BudgetKey{Type: "AGENT", ID: budgetKeyTestAgent}]; !ok || pb.EffectiveLimits["monthly"] != 1 {
		t.Errorf("AGENT spiffe entry missing or wrong: %+v", got)
	}
	if pb, ok := got[BudgetKey{Type: "HUMAN", ID: budgetKeyTestAgent}]; !ok || pb.EffectiveLimits["monthly"] != 100 {
		t.Errorf("HUMAN entry missing or wrong: %+v", got)
	}
}
