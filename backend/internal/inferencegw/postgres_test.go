package inferencegw

import (
	"context"
	"io"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runkora/kora/backend/cmd/inference-gateway/migrations"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// Postgres integration tests for the inference usage repository. They run only
// when KORA_TEST_DB_URL points at a database with inference_usage migrated;
// otherwise they skip. Each test isolates itself under a random tenant.
//
//	KORA_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/inferencegw/ -run Postgres
func testUsageRepo(t *testing.T) (*PostgresInferenceUsageRepository, *pgxpool.Pool, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run inference usage Postgres integration tests")
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
