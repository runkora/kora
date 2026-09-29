package agents

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration test for listing a tenant's agent types within one
// environment. It runs only when ORBIT_TEST_DB_URL points at a database with
// the orbit-agents migrations applied; otherwise it skips. See
// drafts_postgres_test.go for the same harness.
//
//	ORBIT_TEST_DB_URL='postgres://orbit:orbit@localhost:5432/orbit?sslmode=disable' \
//	  go test ./internal/agents/ -run Postgres_ListAgentTypesByEnvironment
func testEnvironmentRepo(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("ORBIT_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set ORBIT_TEST_DB_URL to run environment Postgres integration tests")
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
		_, _ = pool.Exec(ctx, `DELETE FROM agent_types WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM environments WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

// TestPostgres_ListAgentTypesByEnvironment covers the projection against a real
// database. pgx errors when the selected columns and the scan destinations
// disagree, and the projection here had omitted mcp_tools and identity_mode,
// so every call against a non-empty result failed. A mock cannot catch that:
// only the database reports the field descriptions the scan is matched to.
func TestPostgres_ListAgentTypesByEnvironment(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()

	env := Environment{TenantID: tenant, Name: "staging", EgressHosts: []string{"api.github.com"}}
	if err := r.CreateEnvironment(ctx, env); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	// CreateEnvironment generates the id in place on a copy, so read it back.
	envs, err := r.ListEnvironments(ctx, tenant)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("got %d environments, want 1", len(envs))
	}
	env = envs[0]

	at := AgentType{
		TenantID:                tenant,
		Name:                    "Builder",
		Slug:                    "builder-" + uuid.NewString()[:8],
		SystemPrompt:            "build things",
		Framework:               AgentFrameworkClaudeCode,
		GrantScopes:             []string{"inference:invoke"},
		MaxConcurrentRunsPerPod: 1,
		State:                   AgentTypeStateActive,
		ModelID:                 new("bedrock/claude-sonnet-4-5"),
		MCPTools:                []string{"mcp:orbit:use"},
		EnvironmentID:           &env.ID,
		IdentityMode:            IdentityModeDelegated,
	}
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}

	got, err := r.ListAgentTypesByEnvironment(ctx, tenant, env.ID)
	if err != nil {
		t.Fatalf("ListAgentTypesByEnvironment: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d agent types, want 1", len(got))
	}
	if got[0].Slug != at.Slug {
		t.Errorf("slug = %q, want %q", got[0].Slug, at.Slug)
	}
	if len(got[0].MCPTools) != 1 || got[0].MCPTools[0] != "mcp:orbit:use" {
		t.Errorf("mcp_tools = %v, want [mcp:orbit:use]", got[0].MCPTools)
	}
	if got[0].IdentityMode != IdentityModeDelegated {
		t.Errorf("identity_mode = %q, want %q", got[0].IdentityMode, IdentityModeDelegated)
	}
	if got[0].EnvironmentID == nil || *got[0].EnvironmentID != env.ID {
		t.Errorf("environment_id = %v, want %q", got[0].EnvironmentID, env.ID)
	}
}
