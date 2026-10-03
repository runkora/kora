package inferencegw

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// Postgres integration tests for the agent rollups. They run only when
// ORBIT_TEST_DB_URL points at a migrated database (see testUsageRepo).

type agentRow struct {
	slug      string
	model     string
	delegator string // empty for an autonomous row
	prompt    int
	cost      float64
	age       time.Duration
}

// insertAgentRows writes agent traffic with agent_slug set, the way the gateway does.
func insertAgentRows(t *testing.T, pool *pgxpool.Pool, tenant string, rows ...agentRow) {
	t.Helper()
	for _, r := range rows {
		var delegator any
		if r.delegator != "" {
			delegator = r.delegator
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, agent_slug, model_id, status,
			    prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd, created_at, delegator_id)
			VALUES ($1, $2, $3, 'AGENT', $4, $5, 'success', $6, 0, $6, $7, $8, $9)`,
			uuid.NewString(), tenant, testNS+r.slug, r.slug, r.model, r.prompt, r.cost, time.Now().Add(-r.age), delegator)
		if err != nil {
			t.Fatalf("insert agent row: %v", err)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// A delegated agent's spend reaches only the person who ran it. The totals of
// every other caller are the sum over their own visible slugs.
func TestPostgres_AgentRollups_DelegatedSpendIsPrivate(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	otherTenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inference_usage WHERE tenant_id = $1`, otherTenant)
	})

	insertAgentRows(t, pool, tenant,
		agentRow{slug: "coder", model: "m1", prompt: 10, cost: 1.00, age: time.Hour},
		agentRow{slug: "coder", model: "m2", prompt: 20, cost: 2.00, age: 2 * time.Hour},
		agentRow{slug: "notes", model: "m1", delegator: "alice", prompt: 5, cost: 0.50, age: time.Hour},
		agentRow{slug: "notes", model: "m1", delegator: "bob", prompt: 7, cost: 0.70, age: time.Hour},
		agentRow{slug: "secret", model: "m1", prompt: 99, cost: 9.00, age: time.Hour},
		agentRow{slug: "coder", model: "m1", prompt: 1, cost: 4.00, age: 8 * 24 * time.Hour}, // outside 7d
	)
	insertAgentRows(t, pool, otherTenant, agentRow{slug: "coder", model: "m1", prompt: 1000, cost: 100, age: time.Hour})
	// A human row and a service row never count: they have no agent_slug.
	insertUsage(t, pool, usageRow{tenant, "human-1", "HUMAN", 500, 0, 500, 50, time.Hour})

	w := usagewindow.For(usagewindow.Period7d, time.Now())

	cases := []struct {
		name      string
		scope     AgentUsageScope
		wantSlugs map[string]float64
	}{
		{"alice sees coder and her own notes", AgentUsageScope{TenantID: tenant, AllRows: []string{"coder"}, OwnRows: []string{"notes"}, Subject: "alice"},
			map[string]float64{"coder": 3.00, "notes": 0.50}},
		{"bob sees coder and his own notes", AgentUsageScope{TenantID: tenant, AllRows: []string{"coder"}, OwnRows: []string{"notes"}, Subject: "bob"},
			map[string]float64{"coder": 3.00, "notes": 0.70}},
		{"carol sees coder only", AgentUsageScope{TenantID: tenant, AllRows: []string{"coder"}, OwnRows: []string{"notes"}, Subject: "carol"},
			map[string]float64{"coder": 3.00}},
		{"an admin sees notes in full", AgentUsageScope{TenantID: tenant, AllRows: []string{"coder", "notes"}, Subject: "root"},
			map[string]float64{"coder": 3.00, "notes": 1.20}},
		{"no slugs sees nothing", AgentUsageScope{TenantID: tenant, AllRows: nil, OwnRows: nil, Subject: "alice"},
			map[string]float64{}},
		{"an empty subject sees no delegated row", AgentUsageScope{TenantID: tenant, OwnRows: []string{"notes"}, Subject: ""},
			map[string]float64{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			usage, err := repo.AgentUsage(ctx, tc.scope, w)
			if err != nil {
				t.Fatalf("AgentUsage: %v", err)
			}
			var sum float64
			got := map[string]float64{}
			for _, u := range usage {
				got[u.AgentSlug] = u.EstimatedCostUSD
				sum += u.EstimatedCostUSD
			}
			if len(got) != len(tc.wantSlugs) {
				t.Fatalf("slugs = %v, want %v", got, tc.wantSlugs)
			}
			var wantSum float64
			for slug, want := range tc.wantSlugs {
				wantSum += want
				if !near(got[slug], want) {
					t.Errorf("%s = %v, want %v", slug, got[slug], want)
				}
			}

			// The other aggregates agree with the per-agent figures.
			models, err := repo.AgentSpendByModel(ctx, tc.scope, w)
			if err != nil {
				t.Fatalf("AgentSpendByModel: %v", err)
			}
			var modelSum float64
			for _, m := range models {
				modelSum += m.CostUSD
			}
			buckets, err := repo.AgentSpendBuckets(ctx, tc.scope, w)
			if err != nil {
				t.Fatalf("AgentSpendBuckets: %v", err)
			}
			var bucketSum float64
			for _, b := range buckets.Buckets {
				bucketSum += b
			}
			for name, total := range map[string]float64{"agents": sum, "models": modelSum, "buckets": bucketSum} {
				if !near(total, wantSum) {
					t.Errorf("%s total = %v, want %v", name, total, wantSum)
				}
			}
		})
	}
}

func TestPostgres_AgentRollups_BucketsPreviousAndModels(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	scope := AgentUsageScope{TenantID: tenant, AllRows: []string{"coder"}}
	w := usagewindow.For(usagewindow.Period7d, time.Now())

	// One row mid-way through the third bucket from the origin, one
	// just before the window, one in the window before that and one beyond it.
	insertAgentRowAt := func(at time.Time, model string, cost float64) {
		t.Helper()
		_, err := pool.Exec(ctx, `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, agent_slug, model_id, status, estimated_cost_usd, created_at)
			VALUES ($1, $2, $3, 'AGENT', 'coder', $4, 'success', $5, $6)`,
			uuid.NewString(), tenant, testNS+"coder", model, cost, at)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
	insertAgentRowAt(w.Origin.Add(2*24*time.Hour+time.Hour), "big", 3)
	insertAgentRowAt(w.Origin.Add(2*24*time.Hour+2*time.Hour), "small", 1)
	insertAgentRowAt(w.Start.Add(-time.Second), "big", 5)
	insertAgentRowAt(w.Start.Add(-7*24*time.Hour), "big", 2)
	insertAgentRowAt(w.Start.Add(-7*24*time.Hour-time.Second), "big", 100)

	buckets, err := repo.AgentSpendBuckets(ctx, scope, w)
	if err != nil {
		t.Fatalf("AgentSpendBuckets: %v", err)
	}
	if len(buckets.Buckets) != w.Buckets() || !near(buckets.Buckets[2], 4) || !near(buckets.PreviousUSD, 7) {
		t.Errorf("buckets = %v previous = %v, want bucket 2 = 4 and previous = 7", buckets.Buckets, buckets.PreviousUSD)
	}
	for i, b := range buckets.Buckets {
		if i != 2 && b != 0 {
			t.Errorf("bucket %d = %v, want 0", i, b)
		}
	}

	models, err := repo.AgentSpendByModel(ctx, scope, w)
	if err != nil {
		t.Fatalf("AgentSpendByModel: %v", err)
	}
	if len(models) != 2 || models[0].ModelID != "big" || !near(models[0].CostUSD, 3) || models[1].ModelID != "small" {
		t.Errorf("models = %+v, want big then small", models)
	}
}

func TestPostgres_AgentBudgetSpend(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()

	now := time.Now().UTC()
	week := periodStart(cadenceWeekly, now)
	month := periodStart(cadenceMonthly, now)

	// Rows at and just before each boundary, so both windows have rows on each
	// side whichever day of the week and month this runs on.
	rows := []agentRow{
		{slug: "coder", model: "m", cost: 1, age: 0},
		{slug: "coder", model: "m", cost: 2, age: now.Sub(week) + time.Hour},
		{slug: "coder", model: "m", cost: 4, age: now.Sub(month) + time.Hour},
		{slug: "hidden", model: "m", cost: 8, age: 0},
	}
	insertAgentRows(t, pool, tenant, rows...)

	var wantWeek, wantMonth float64
	for _, r := range rows[:3] {
		at := now.Add(-r.age)
		if !at.Before(week) {
			wantWeek += r.cost
		}
		if !at.Before(month) {
			wantMonth += r.cost
		}
	}

	got, err := repo.AgentBudgetSpend(ctx, tenant, []string{"coder"}, week, month)
	if err != nil {
		t.Fatalf("AgentBudgetSpend: %v", err)
	}
	if len(got) != 1 || got[0].AgentSlug != "coder" {
		t.Fatalf("got %+v, want coder only", got)
	}
	if !near(got[0].WeekToDateUSD, wantWeek) || !near(got[0].MonthToDateUSD, wantMonth) {
		t.Errorf("week %v month %v, want week %v month %v (week %s month %s)", got[0].WeekToDateUSD, got[0].MonthToDateUSD, wantWeek, wantMonth, week, month)
	}
}

// A slug is matched exactly: a LIKE wildcard in it matches nothing, and a slug
// that is a prefix of another does not pick up the longer one.
func TestPostgres_AgentRollups_SlugsMatchExactly(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	insertAgentRows(t, pool, tenant,
		agentRow{slug: "probe", model: "m", prompt: 10, cost: 0.1, age: time.Hour},
		agentRow{slug: "token-probe", model: "m", prompt: 100, cost: 1, age: time.Hour},
		agentRow{slug: "pr_be", model: "m", prompt: 7, cost: 0.07, age: time.Hour},
	)
	w := usagewindow.For(usagewindow.Period7d, time.Now())
	for slug, want := range map[string]int64{"probe": 10, "token-probe": 100, "pr_be": 7, "pr%": 0, "missing": 0} {
		got, err := repo.AgentUsage(context.Background(), AgentUsageScope{TenantID: tenant, AllRows: []string{slug}}, w)
		if err != nil {
			t.Fatalf("AgentUsage: %v", err)
		}
		var total int64
		for _, u := range got {
			total += u.TotalTokens
		}
		if total != want {
			t.Errorf("%q: total tokens %d, want %d", slug, total, want)
		}
	}
}
