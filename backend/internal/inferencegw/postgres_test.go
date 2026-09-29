package inferencegw

import (
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/clear-route/orbit/cmd/inference-gateway/migrations"
	"github.com/clear-route/orbit/internal/platform/logging"
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

// insertUsage writes rows the way pre-migration binaries did: agent_slug unset.
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

// runBackfill executes the UPDATE from the agent_slug migration, so the test
// exercises the shipped SQL rather than a copy of it.
func runBackfill(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, "20260929055417_add_inference_usage_agent_slug.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	for block := range strings.SplitSeq(string(body), "-- +goose StatementBegin") {
		stmt, _, _ := strings.Cut(block, "-- +goose StatementEnd")
		if strings.HasPrefix(strings.TrimSpace(stmt), "UPDATE") {
			if _, err := pool.Exec(context.Background(), stmt); err != nil {
				t.Fatalf("backfill: %v", err)
			}
			return
		}
	}
	t.Fatal("no UPDATE statement in the agent_slug migration")
}

func slugsByPrincipal(t *testing.T, pool *pgxpool.Pool, tenant string) map[string]*string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT principal_type || ' ' || principal_id, agent_slug FROM inference_usage WHERE tenant_id = $1`, tenant)
	if err != nil {
		t.Fatalf("query slugs: %v", err)
	}
	defer rows.Close()
	out := map[string]*string{}
	for rows.Next() {
		var key string
		var slug *string
		if err := rows.Scan(&key, &slug); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out[key] = slug
	}
	return out
}

const testNS = "spiffe://orbit.test/tenant/orbit-tenant-example/agent/"

func TestPostgres_AgentSlugBackfill(t *testing.T) {
	_, pool, tenant := testUsageRepo(t)
	insertUsage(t, pool,
		usageRow{tenant, testNS + "probe", "AGENT", 1, 0, 1, 0, time.Hour},
		usageRow{tenant, testNS + "token-probe", "AGENT", 1, 0, 1, 0, time.Hour},
		usageRow{tenant, testNS + "probe/sandbox/run-1", "AGENT", 1, 0, 1, 0, time.Hour},
		usageRow{tenant, "spiffe://orbit.test/tenant/orbit-tenant-example/service/probe", "SERVICE", 1, 0, 1, 0, time.Hour},
		usageRow{tenant, testNS + "probe", "SERVICE", 1, 0, 1, 0, time.Hour},
		usageRow{tenant, "CgNkZXYSCmxvY2FsLWxkYXA", "HUMAN", 1, 0, 1, 0, time.Hour},
	)
	runBackfill(t, pool)

	want := map[string]string{
		"AGENT " + testNS + "probe":                                             "probe",
		"AGENT " + testNS + "token-probe":                                       "token-probe",
		"AGENT " + testNS + "probe/sandbox/run-1":                               "probe",
		"SERVICE spiffe://orbit.test/tenant/orbit-tenant-example/service/probe": "",
		"SERVICE " + testNS + "probe":                                           "",
		"HUMAN CgNkZXYSCmxvY2FsLWxkYXA":                                         "",
	}
	got := slugsByPrincipal(t, pool, tenant)
	for key, w := range want {
		g, ok := got[key]
		if !ok {
			t.Errorf("row %q missing", key)
			continue
		}
		if (w == "" && g != nil) || (w != "" && (g == nil || *g != w)) {
			t.Errorf("agent_slug for %q = %v, want %q", key, g, w)
		}
	}
}

func TestPostgres_WriteUsageRecordsAgentSlug(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	svc := &Service{usage: repo, logger: logging.NewLoggerWithOutput(io.Discard)}

	for _, p := range []struct{ id, typ string }{
		{testNS + "token-probe/sandbox/run-9", "AGENT"},
		{"CgNkZXYSCmxvY2FsLWxkYXA", "HUMAN"},
	} {
		rec := InferenceUsageRecord{
			ID: uuid.NewString(), TenantID: tenant, PrincipalID: p.id, PrincipalType: p.typ,
			APIDialect: "openai", ModelID: "m", Status: "success", CreatedAt: time.Now(),
		}
		if err := svc.writeUsage(context.Background(), rec); err != nil {
			t.Fatalf("writeUsage: %v", err)
		}
	}

	got := slugsByPrincipal(t, pool, tenant)
	if g := got["AGENT "+testNS+"token-probe/sandbox/run-9"]; g == nil || *g != "token-probe" {
		t.Errorf("agent row agent_slug = %v, want token-probe", g)
	}
	if g := got["HUMAN CgNkZXYSCmxvY2FsLWxkYXA"]; g != nil {
		t.Errorf("human row agent_slug = %q, want NULL", *g)
	}
}

func TestPostgres_AgentTypeUsage_KeysOnSlug(t *testing.T) {
	repo, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	otherTenant := "test-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM inference_usage WHERE tenant_id = $1`, otherTenant)
	})

	insertUsage(t, pool,
		usageRow{tenant, testNS + "probe", "AGENT", 10, 5, 15, 0.10, time.Hour},
		usageRow{tenant, testNS + "probe", "AGENT", 20, 10, 30, 0.20, 2 * time.Hour},
		usageRow{tenant, testNS + "token-probe", "AGENT", 100, 50, 150, 1.00, time.Hour},
		usageRow{tenant, testNS + "pr_be", "AGENT", 7, 0, 7, 0.07, time.Hour},
		usageRow{tenant, testNS + "probe", "SERVICE", 3, 0, 3, 0.03, time.Hour},
		usageRow{tenant, testNS + "probe", "AGENT", 500, 0, 500, 5, 8 * 24 * time.Hour},
		usageRow{otherTenant, testNS + "probe", "AGENT", 400, 0, 400, 4, time.Hour},
	)
	runBackfill(t, pool)

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

// TestPostgres_AgentTypeUsage_UsesSlugIndex checks the per-agent query can be
// served by idx_inference_usage_agent_slug. Sequential scans are disabled so a
// tiny test table does not make the planner prefer one.
func TestPostgres_AgentTypeUsage_UsesSlugIndex(t *testing.T) {
	_, pool, tenant := testUsageRepo(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SET enable_seqscan = off"); err != nil {
		t.Fatalf("set: %v", err)
	}
	defer func() { _, _ = conn.Exec(ctx, "RESET enable_seqscan") }()

	rows, err := conn.Query(ctx, `EXPLAIN SELECT COALESCE(SUM(total_tokens), 0) FROM inference_usage
		WHERE tenant_id = $1 AND agent_slug = $2 AND created_at >= NOW() - INTERVAL '7 days'`, tenant, "probe")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if !strings.Contains(plan.String(), "idx_inference_usage_agent_slug") {
		t.Errorf("plan does not use idx_inference_usage_agent_slug:\n%s", plan.String())
	}
}
