package inferencegw

import (
	"context"
	"math"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/clear-route/orbit/internal/platform/usagewindow"
)

// Postgres integration tests for the agent rollups. They run only when
// ORBIT_TEST_DB_URL points at a migrated database (see testUsageRepo).

type agentRow struct {
	slug      string
	model     string
	delegator string // empty for a row without a delegator
	prompt    int
	cost      float64
	at        time.Time
}

// insertAgentRows writes agent traffic with agent_slug set, the way the gateway does.
func insertAgentRows(t *testing.T, pool *pgxpool.Pool, tenant string, rows ...agentRow) {
	t.Helper()
	for _, r := range rows {
		var delegator any
		if r.delegator != "" {
			delegator = r.delegator
		}
		model := r.model
		if model == "" {
			model = "m"
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, agent_slug, model_id, status,
			    prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd, created_at, delegator_id)
			VALUES ($1, $2, $3, 'AGENT', $4, $5, 'success', $6, 0, $6, $7, $8, $9)`,
			uuid.NewString(), tenant, testNS+r.slug, r.slug, model, r.prompt, r.cost, r.at, delegator)
		if err != nil {
			t.Fatalf("insert agent row: %v", err)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func costs(rows []agentUsageRow) map[string]float64 {
	usage, _ := foldUsage(rows)
	out := map[string]float64{}
	for _, u := range usage {
		out[u.AgentSlug] = u.EstimatedCostUSD
	}
	return out
}

func sum(vs ...float64) float64 {
	var n float64
	for _, v := range vs {
		n += v
	}
	return n
}

// Every case of the visibility predicate, and the rest of the rollups agreeing
// with it: totals, models and trend never include a row the usage list hides.
func TestPostgres_AgentRollups_Predicate(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	other := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inference_usage WHERE tenant_id = $1`, other)
	})

	now := time.Now()
	hourAgo, twoHoursAgo := now.Add(-time.Hour), now.Add(-2*time.Hour)
	insertAgentRows(t, pool, tenant,
		// An autonomous agent: every row has no delegator.
		agentRow{slug: "coder", model: "m1", prompt: 10, cost: 1.00, at: hourAgo},
		agentRow{slug: "coder", model: "m2", prompt: 20, cost: 2.00, at: twoHoursAgo},
		// A delegated agent: each row names the person who ran it.
		agentRow{slug: "notes", model: "m1", delegator: "alice", prompt: 5, cost: 0.50, at: hourAgo},
		agentRow{slug: "notes", model: "m1", delegator: "bob", prompt: 7, cost: 0.70, at: hourAgo},
		// An agent deleted since: its rows keep the slug and still count.
		agentRow{slug: "gone", prompt: 99, cost: 9.00, at: hourAgo},
		// A slug deleted and recreated as autonomous: each row is judged by its own delegator.
		agentRow{slug: "reborn", delegator: "bob", prompt: 3, cost: 0.30, at: twoHoursAgo},
		agentRow{slug: "reborn", prompt: 4, cost: 0.40, at: hourAgo},
		// Outside the window, and another tenant's.
		agentRow{slug: "coder", prompt: 1, cost: 4.00, at: now.Add(-8 * 24 * time.Hour)},
	)
	insertAgentRows(t, pool, other, agentRow{slug: "coder", prompt: 1000, cost: 100, at: hourAgo})
	// Rows with no agent slug never count: a person's own calls, a service.
	insertUsage(t, pool, usageRow{tenant, "human-1", "HUMAN", 500, 0, 500, 50, time.Hour})

	cases := []struct {
		name   string
		caller usageCaller
		want   map[string]float64
	}{
		{"admin sees every row", usageCaller{TenantID: tenant, Subject: "root", Admin: true},
			map[string]float64{"coder": 3, "notes": 1.2, "gone": 9, "reborn": 0.7}},
		{"the owner sees their own delegated rows", usageCaller{TenantID: tenant, Subject: "alice"},
			map[string]float64{"coder": 3, "notes": 0.5, "gone": 9, "reborn": 0.4}},
		{"another person sees theirs and not alice's", usageCaller{TenantID: tenant, Subject: "bob"},
			map[string]float64{"coder": 3, "notes": 0.7, "gone": 9, "reborn": 0.7}},
		{"a stranger sees no delegated row", usageCaller{TenantID: tenant, Subject: "carol"},
			map[string]float64{"coder": 3, "gone": 9, "reborn": 0.4}},
		{"an empty subject sees no delegated row", usageCaller{TenantID: tenant},
			map[string]float64{"coder": 3, "gone": 9, "reborn": 0.4}},
		// A PAT carries no roles claim, so an admin's PAT is not an admin here.
		{"a PAT of an admin is a person, not an admin", callerFromClaims(claimsWithRoles(tenant, "alice", "")),
			map[string]float64{"coder": 3, "notes": 0.5, "gone": 9, "reborn": 0.4}},
	}
	w := usagewindow.For(usagewindow.Period7d, now)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := usageQuery{Caller: tc.caller, Window: w}
			usage, err := repo.AgentUsage(ctx, q)
			if err != nil {
				t.Fatalf("AgentUsage: %v", err)
			}
			got := costs(usage)
			if len(got) != len(tc.want) {
				t.Fatalf("agents = %v, want %v", got, tc.want)
			}
			var total float64
			for slug, want := range tc.want {
				total += want
				if !near(got[slug], want) {
					t.Errorf("%s = %v, want %v", slug, got[slug], want)
				}
			}

			_, models := foldUsage(usage)
			var modelSum float64
			for _, m := range models {
				modelSum += m.CostUSD
			}
			_, buckets, err := repo.AgentSpendBuckets(ctx, q)
			if err != nil {
				t.Fatalf("AgentSpendBuckets: %v", err)
			}
			var trendSum float64
			for _, b := range buckets {
				trendSum += b
			}
			for name, v := range map[string]float64{"models": modelSum, "trend": trendSum} {
				if !near(v, total) {
					t.Errorf("%s total = %v, want %v: it must not include a row the usage list hides", name, v, total)
				}
			}
		})
	}

	t.Run("a non-admin's previous window excludes another person's delegated spend", func(t *testing.T) {
		insertAgentRows(t, pool, tenant,
			agentRow{slug: "notes", delegator: "bob", cost: 6, at: now.Add(-9 * 24 * time.Hour)},
			agentRow{slug: "coder", cost: 2, at: now.Add(-9 * 24 * time.Hour)},
		)
		previous, _, err := repo.AgentSpendBuckets(ctx, usageQuery{Caller: usageCaller{TenantID: tenant, Subject: "alice"}, Window: w})
		if err != nil {
			t.Fatalf("AgentSpendBuckets: %v", err)
		}
		// The 8d old coder row (4.00) and the 9d old one (2) are in the previous 7 days; bob's 6 is not alice's to see.
		if !near(previous, 6) {
			t.Errorf("previous = %v, want 6", previous)
		}
	})

	t.Run("one agent follows the same predicate", func(t *testing.T) {
		got, err := repo.AgentUsage(ctx, usageQuery{Caller: usageCaller{TenantID: tenant, Subject: "carol"}, Window: w, Slug: "notes"})
		if err != nil || len(got) != 0 {
			t.Errorf("a stranger's view of the delegated agent = %+v, %v; want nothing", got, err)
		}
		got, err = repo.AgentUsage(ctx, usageQuery{Caller: usageCaller{TenantID: tenant, Subject: "bob"}, Window: w, Slug: "notes"})
		if c := costs(got); err != nil || len(c) != 1 || !near(c["notes"], 0.7) {
			t.Errorf("the owner's view = %v, %v; want 0.7", c, err)
		}
	})
}

func TestPostgres_AgentRollups_BucketsAndModels(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	now := time.Now()
	w := usagewindow.For(usagewindow.Period7d, now)
	q := usageQuery{Caller: usageCaller{TenantID: tenant, Admin: true}, Window: w}

	// Bucket 2 is the third bucket of the window.
	insertAgentRows(t, pool, tenant,
		agentRow{slug: "coder", model: "big", cost: 3, at: w.Start.Add(2*24*time.Hour + time.Hour)},
		agentRow{slug: "coder", model: "small", cost: 1, at: w.Start.Add(2*24*time.Hour + 2*time.Hour)},
		agentRow{slug: "coder", model: "big", cost: 5, at: w.Start.Add(-time.Second)},
		agentRow{slug: "coder", model: "big", cost: 2, at: w.PreviousStart()},
		agentRow{slug: "coder", model: "big", cost: 100, at: w.PreviousStart().Add(-time.Second)},
	)
	previous, buckets, err := repo.AgentSpendBuckets(ctx, q)
	if err != nil {
		t.Fatalf("AgentSpendBuckets: %v", err)
	}
	// 5 just before the window and 2 at the start of the previous window, so 7 before it.
	if len(buckets) != w.Buckets() || !near(buckets[2], 4) || !near(sum(buckets...), 4) || !near(previous, 7) {
		t.Errorf("previous %v buckets %v, want previous 7 and 4 in bucket 2", previous, buckets)
	}

	usage, err := repo.AgentUsage(ctx, q)
	if err != nil {
		t.Fatalf("AgentUsage: %v", err)
	}
	_, models := foldUsage(usage)
	if len(models) != 2 || models[0].ModelID != "big" || !near(models[0].CostUSD, 3) || models[1].ModelID != "small" {
		t.Errorf("models = %+v, want big then small", models)
	}
}

func TestPostgres_AgentBudgetSpend(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	now := time.Now().UTC()
	week, month := periodStart(cadenceWeekly, now), periodStart(cadenceMonthly, now)

	// Rows at and just before each boundary, so both windows have rows on each
	// side whichever day of the week and month this runs on.
	rows := []agentRow{
		{slug: "coder", cost: 1, at: now},
		{slug: "coder", cost: 2, at: week.Add(-time.Hour)},
		{slug: "coder", cost: 4, at: month.Add(-time.Hour)},
		{slug: "notes", delegator: "alice", cost: 8, at: now},
		{slug: "notes", delegator: "bob", cost: 16, at: now},
		{slug: "other", cost: 32, at: now},
	}
	insertAgentRows(t, pool, tenant, rows...)
	var wantWeek, wantMonth float64
	for _, r := range rows[:3] {
		if !r.at.Before(week) {
			wantWeek += r.cost
		}
		if !r.at.Before(month) {
			wantMonth += r.cost
		}
	}

	spend := func(caller usageCaller) map[string]AgentBudgetSpend {
		got, err := repo.AgentBudgetSpend(context.Background(), caller, []string{"coder", "notes"}, week, month)
		if err != nil {
			t.Fatalf("AgentBudgetSpend: %v", err)
		}
		by := map[string]AgentBudgetSpend{}
		for _, b := range got {
			by[b.AgentSlug] = b
		}
		return by
	}

	alice := spend(usageCaller{TenantID: tenant, Subject: "alice"})
	if len(alice) != 2 || !near(alice["coder"].Week, wantWeek) || !near(alice["coder"].Month, wantMonth) {
		t.Errorf("alice = %+v, want coder week %v month %v and notes", alice, wantWeek, wantMonth)
	}
	// Spend on a delegated agent is the caller's own: bob's 16 is not alice's to see.
	if !near(alice["notes"].Month, 8) {
		t.Errorf("alice's notes spend = %+v, want 8", alice["notes"])
	}
	if admin := spend(usageCaller{TenantID: tenant, Admin: true}); !near(admin["notes"].Month, 24) {
		t.Errorf("admin's notes spend = %+v, want 24", admin["notes"])
	}
}

// The rollups must run with pgx.QueryExecModeExec so Postgres plans them with
// the real window. A prepared statement gets one generic plan that reads most
// of the table through an index, which took about 2 seconds for a 90d window at
// 150 agents where the plan chosen with the real values takes about 0.3. The
// guard does not depend on the planner: it checks that the queries leave no
// prepared statement on the connection.
func TestPostgres_AgentRollups_DoNotPrepareStatements(t *testing.T) {
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run inference usage Postgres integration tests")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 1 // one connection, so the statements checked are the ones the rollups ran on
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	repo := NewPostgresInferenceUsageRepository(pool)

	tenant := "test-" + uuid.NewString()
	now := time.Now()
	q := usageQuery{Caller: usageCaller{TenantID: tenant, Subject: "alice"}, Window: usagewindow.For(usagewindow.Period7d, now)}
	if _, err := repo.AgentUsage(ctx, q); err != nil {
		t.Fatalf("AgentUsage: %v", err)
	}
	if _, _, err := repo.AgentSpendBuckets(ctx, q); err != nil {
		t.Fatalf("AgentSpendBuckets: %v", err)
	}
	if _, err := repo.AgentBudgetSpend(ctx, q.Caller, []string{"coder"}, now, now); err != nil {
		t.Fatalf("AgentBudgetSpend: %v", err)
	}

	var prepared int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`, pgx.QueryExecModeExec).Scan(&prepared); err != nil {
		t.Fatalf("count prepared statements: %v", err)
	}
	if prepared != 0 {
		t.Errorf("%d prepared statements left on the connection: the rollups must use pgx.QueryExecModeExec", prepared)
	}
}
