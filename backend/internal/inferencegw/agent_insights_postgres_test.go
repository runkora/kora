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
	slug, model string
	delegator   string // empty for a row without a delegator
	cost        float64
	at          time.Time
}

// insertAgentRows writes agent traffic with agent_slug set, the way the gateway does.
func insertAgentRows(t *testing.T, pool *pgxpool.Pool, tenant string, rows ...agentRow) {
	t.Helper()
	for _, r := range rows {
		var delegator any
		if r.delegator != "" {
			delegator = r.delegator
		}
		if r.model == "" {
			r.model = "m"
		}
		_, err := pool.Exec(context.Background(), `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, agent_slug, model_id, status,
			    prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd, created_at, delegator_id)
			VALUES ($1, $2, $3, 'AGENT', $4, $5, 'success', 1, 0, 1, $6, $7, $8)`,
			uuid.NewString(), tenant, testNS+r.slug, r.slug, r.model, r.cost, r.at, delegator)
		if err != nil {
			t.Fatalf("insert agent row: %v", err)
		}
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func sum(vs []float64) (n float64) {
	for _, v := range vs {
		n += v
	}
	return n
}

// Every case of the visibility predicate, and the rest of the rollups agreeing
// with it: models and trend never include a row the usage list hides.
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
		agentRow{slug: "coder", model: "m1", cost: 1, at: hourAgo}, // an autonomous agent: no delegator
		agentRow{slug: "coder", model: "m2", cost: 2, at: twoHoursAgo},
		agentRow{slug: "notes", delegator: "alice", cost: 0.5, at: hourAgo}, // a delegated agent: each row names its person
		agentRow{slug: "notes", delegator: "bob", cost: 0.7, at: hourAgo},
		agentRow{slug: "gone", cost: 9, at: hourAgo},                           // an agent deleted since: its rows still count
		agentRow{slug: "reborn", delegator: "bob", cost: 0.3, at: twoHoursAgo}, // a slug recreated as autonomous: each row is judged by its own delegator
		agentRow{slug: "reborn", cost: 0.4, at: hourAgo},
		agentRow{slug: "coder", cost: 4, at: now.Add(-8 * 24 * time.Hour)}, // outside the window
	)
	insertAgentRows(t, pool, other, agentRow{slug: "coder", cost: 100, at: hourAgo})
	insertUsage(t, pool, usageRow{tenant, "human-1", "HUMAN", 500, 0, 500, 50, time.Hour}) // no agent slug: never counted

	cases := []struct {
		name   string
		caller usageCaller
		want   map[string]float64
	}{
		{"admin sees every row", usageCaller{TenantID: tenant, Subject: "root", Admin: true}, map[string]float64{"coder": 3, "notes": 1.2, "gone": 9, "reborn": 0.7}},
		{"the owner sees their own delegated rows", usageCaller{TenantID: tenant, Subject: "alice"}, map[string]float64{"coder": 3, "notes": 0.5, "gone": 9, "reborn": 0.4}},
		{"a stranger sees no delegated row", usageCaller{TenantID: tenant, Subject: "carol"}, map[string]float64{"coder": 3, "gone": 9, "reborn": 0.4}},
		{"an empty subject sees no delegated row", usageCaller{TenantID: tenant}, map[string]float64{"coder": 3, "gone": 9, "reborn": 0.4}},
		// A PAT carries no roles claim, so an admin's PAT is a person here.
		{"a PAT of an admin is not an admin", callerFromClaims(claimsWithRoles(tenant, "alice", "")), map[string]float64{"coder": 3, "notes": 0.5, "gone": 9, "reborn": 0.4}},
	}
	w := usagewindow.For(usagewindow.Period7d, now)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := usageQuery{Caller: tc.caller, Window: w}
			rows, err := repo.AgentUsage(ctx, q)
			if err != nil {
				t.Fatalf("AgentUsage: %v", err)
			}
			usage, models := foldUsage(rows)
			var total, modelSum float64
			for _, u := range usage {
				total += u.EstimatedCostUSD
				if want, ok := tc.want[u.AgentSlug]; !ok || !near(u.EstimatedCostUSD, want) {
					t.Errorf("%s = %v, want %v", u.AgentSlug, u.EstimatedCostUSD, want)
				}
			}
			if len(usage) != len(tc.want) {
				t.Errorf("%d agents, want %d", len(usage), len(tc.want))
			}
			for _, m := range models {
				modelSum += m.CostUSD
			}
			_, buckets, err := repo.AgentSpendBuckets(ctx, q)
			if err != nil {
				t.Fatalf("AgentSpendBuckets: %v", err)
			}
			if !near(modelSum, total) || !near(sum(buckets), total) {
				t.Errorf("models %v and trend %v must equal the usage total %v", modelSum, sum(buckets), total)
			}
		})
	}

	t.Run("the previous window excludes another person's delegated spend", func(t *testing.T) {
		insertAgentRows(t, pool, tenant,
			agentRow{slug: "notes", delegator: "bob", cost: 6, at: now.Add(-9 * 24 * time.Hour)},
			agentRow{slug: "coder", cost: 2, at: now.Add(-9 * 24 * time.Hour)},
		)
		previous, _, err := repo.AgentSpendBuckets(ctx, usageQuery{Caller: usageCaller{TenantID: tenant, Subject: "alice"}, Window: w})
		// The coder rows of 8 and 9 days ago (4 and 2) are alice's to see; bob's 6 is not.
		if err != nil || !near(previous, 6) {
			t.Errorf("previous = %v, %v; want 6", previous, err)
		}
	})

	t.Run("one agent follows the same predicate", func(t *testing.T) {
		for subject, want := range map[string]float64{"carol": 0, "bob": 0.7} {
			rows, err := repo.AgentUsage(ctx, usageQuery{Caller: usageCaller{TenantID: tenant, Subject: subject}, Window: w, Slug: "notes"})
			usage, _ := foldUsage(rows)
			var got float64
			for _, u := range usage {
				got += u.EstimatedCostUSD
			}
			if err != nil || !near(got, want) {
				t.Errorf("%s sees %v of notes, %v; want %v", subject, got, err, want)
			}
		}
	})
}

func TestPostgres_AgentRollups_Buckets(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	w := usagewindow.For(usagewindow.Period7d, time.Now())
	insertAgentRows(t, pool, tenant,
		agentRow{slug: "coder", cost: 3, at: w.Start.Add(2*24*time.Hour + time.Hour)}, // the third bucket
		agentRow{slug: "coder", cost: 1, at: w.Start.Add(2*24*time.Hour + 2*time.Hour)},
		agentRow{slug: "coder", cost: 5, at: w.Start.Add(-time.Second)}, // just before the window
		agentRow{slug: "coder", cost: 2, at: w.PreviousStart()},
		agentRow{slug: "coder", cost: 100, at: w.PreviousStart().Add(-time.Second)}, // before the previous window
	)
	previous, buckets, err := repo.AgentSpendBuckets(context.Background(), usageQuery{Caller: usageCaller{TenantID: tenant, Admin: true}, Window: w})
	if err != nil || len(buckets) != w.Buckets() || !near(buckets[2], 4) || !near(sum(buckets), 4) || !near(previous, 7) {
		t.Errorf("previous %v buckets %v, %v; want previous 7 and 4 in bucket 2", previous, buckets, err)
	}
}

func TestPostgres_AgentBudgetSpend(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	now := time.Now().UTC()
	week, month := periodStart(cadenceWeekly, now), periodStart(cadenceMonthly, now)
	rows := []agentRow{
		{slug: "coder", cost: 1, at: now},
		{slug: "coder", cost: 2, at: week.Add(-time.Hour)}, // rows either side of each boundary
		{slug: "coder", cost: 4, at: month.Add(-time.Hour)},
		{slug: "notes", delegator: "alice", cost: 8, at: now},
		{slug: "notes", delegator: "bob", cost: 16, at: now},
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
	if !near(alice["coder"].Week, wantWeek) || !near(alice["coder"].Month, wantMonth) {
		t.Errorf("coder = %+v, want week %v month %v", alice["coder"], wantWeek, wantMonth)
	}
	// Spend on a delegated agent is the caller's own: bob's 16 is not alice's to see.
	if !near(alice["notes"].Month, 8) || !near(spend(usageCaller{TenantID: tenant, Admin: true})["notes"].Month, 24) {
		t.Errorf("notes spend: alice %+v", alice["notes"])
	}
}

// The rollups must run in pgx.QueryExecModeExec so Postgres plans them with the
// real window; a prepared statement's generic plan reads most of the table
// through an index. The guard checks that no prepared statement is left on the
// connection, which does not depend on the planner.
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

	now := time.Now()
	q := usageQuery{Caller: usageCaller{TenantID: "test-" + uuid.NewString(), Subject: "alice"}, Window: usagewindow.For(usagewindow.Period7d, now)}
	_, usageErr := repo.AgentUsage(ctx, q)
	_, _, bucketErr := repo.AgentSpendBuckets(ctx, q)
	_, spendErr := repo.AgentBudgetSpend(ctx, q.Caller, []string{"coder"}, now, now)
	if usageErr != nil || bucketErr != nil || spendErr != nil {
		t.Fatalf("rollups: %v %v %v", usageErr, bucketErr, spendErr)
	}
	var prepared int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`, pgx.QueryExecModeExec).Scan(&prepared); err != nil || prepared != 0 {
		t.Errorf("%d prepared statements left on the connection (%v): the rollups must use pgx.QueryExecModeExec", prepared, err)
	}
}
