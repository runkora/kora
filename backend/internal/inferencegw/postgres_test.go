package inferencegw

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for the inference usage repository. They run only
// when ORBIT_TEST_DB_URL points at a database with inference_usage migrated;
// otherwise they skip. Each test isolates itself under a random tenant.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/inferencegw/ -run Postgres
func testUsageRepo(t *testing.T) (*PostgresInferenceUsageRepository, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run inference usage Postgres integration tests")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("database unreachable: %v", err)
	}
	tenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM inference_usage WHERE tenant_id = $1`, tenant)
		pool.Close()
	})
	return NewPostgresInferenceUsageRepository(pool), pool, tenant
}

type usageRow struct {
	tenant, principalID, principalType string
	prompt, completion, total          int
	cost                               float64
	age                                time.Duration
}

func insertUsage(t *testing.T, pool *pgxpool.Pool, rows ...usageRow) {
	t.Helper()
	for _, r := range rows {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO inference_usage (id, tenant_id, principal_id, principal_type, model_id, status,
			    prompt_tokens, completion_tokens, total_tokens, estimated_cost_usd, created_at)
			VALUES ($1, $2, $3, $4, 'm', 'success', $5, $6, $7, $8, $9)`,
			uuid.NewString(), r.tenant, r.principalID, r.principalType,
			r.prompt, r.completion, r.total, r.cost, time.Now().Add(-r.age))
		if err != nil {
			t.Fatalf("insert usage: %v", err)
		}
	}
}

func TestPostgres_AgentTypeUsage_KeysOnSlugSegment(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	otherTenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inference_usage WHERE tenant_id = $1`, otherTenant)
	})

	const ns = "spiffe://orbit.test/tenant/orbit-tenant-example/agent/"
	insertUsage(t, pool,
		usageRow{tenant, ns + "probe", "AGENT", 10, 5, 15, 0.10, time.Hour},
		usageRow{tenant, ns + "probe", "AGENT", 20, 10, 30, 0.20, 2 * time.Hour},
		usageRow{tenant, ns + "token-probe", "AGENT", 100, 50, 150, 1.00, time.Hour},
		// Sandbox-shaped principal: never recorded (identity normalises it), so it must not count.
		usageRow{tenant, ns + "probe/sandbox/run-1", "AGENT", 1000, 0, 1000, 9, time.Hour},
		// Wildcard-shaped slug and non-agent principal ending in the same path.
		usageRow{tenant, ns + "pr_be", "AGENT", 7, 0, 7, 0.07, time.Hour},
		usageRow{tenant, "spiffe://orbit.test/tenant/orbit-tenant-example/agent/probe", "SERVICE", 3, 0, 3, 0.03, time.Hour},
		// Outside the 7-day window and in another tenant.
		usageRow{tenant, ns + "probe", "AGENT", 500, 0, 500, 5, 8 * 24 * time.Hour},
		usageRow{otherTenant, ns + "probe", "AGENT", 400, 0, 400, 4, time.Hour},
	)

	tests := []struct {
		slug       string
		wantTotal  int64
		wantPrompt int64
		wantCost   float64
	}{
		{"probe", 45, 30, 0.30},
		{"token-probe", 150, 100, 1.00},
		{"pr_be", 7, 7, 0.07},
		{"pr%", 0, 0, 0},
		{"missing", 0, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.slug, func(t *testing.T) {
			got, err := repo.GetAgentTypeUsage(ctx, tenant, tc.slug)
			if err != nil {
				t.Fatalf("GetAgentTypeUsage: %v", err)
			}
			if got.AgentSlug != tc.slug || got.TotalTokens != tc.wantTotal || got.PromptTokens != tc.wantPrompt {
				t.Errorf("got %+v, want slug %q total %d prompt %d", got, tc.slug, tc.wantTotal, tc.wantPrompt)
			}
			if diff := got.EstimatedCostUSD - tc.wantCost; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("cost = %v, want %v", got.EstimatedCostUSD, tc.wantCost)
			}
		})
	}

	list, err := repo.ListAgentTypeUsage(ctx, tenant)
	if err != nil {
		t.Fatalf("ListAgentTypeUsage: %v", err)
	}
	gotTotals := map[string]int64{}
	for _, u := range list {
		gotTotals[u.AgentSlug] = u.TotalTokens
	}
	wantTotals := map[string]int64{"probe": 45, "token-probe": 150, "pr_be": 7}
	if len(gotTotals) != len(wantTotals) {
		t.Fatalf("list = %v, want %v", gotTotals, wantTotals)
	}
	for slug, want := range wantTotals {
		if gotTotals[slug] != want {
			t.Errorf("list[%q] = %d, want %d", slug, gotTotals[slug], want)
		}
	}
}
