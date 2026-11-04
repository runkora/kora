package agents

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres integration tests for environments and their agent links. They run
// only when KORA_TEST_DB_URL points at a database with the kora-agents
// migrations applied. drafts_postgres_test.go uses the same harness.
//
//	KORA_TEST_DB_URL='postgres://kora:kora@localhost:5432/kora?sslmode=disable' \
//	  go test ./internal/agents/ -run Postgres_

func testEnvironmentRepo(t *testing.T) (*PostgresRepository, string) {
	t.Helper()
	dsn := os.Getenv("KORA_TEST_DB_URL")
	if dsn == "" {
		t.Skip("set KORA_TEST_DB_URL to run environment Postgres integration tests")
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
		_, _ = pool.Exec(ctx, `DELETE FROM runs WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM agent_types WHERE tenant_id=$1`, tenant)
		_, _ = pool.Exec(ctx, `DELETE FROM environments WHERE tenant_id=$1`, tenant)
		pool.Close()
	})
	return NewPostgresRepository(pool), tenant
}

func testEnvironment(t *testing.T, r *PostgresRepository, tenant, name string) Environment {
	t.Helper()
	env := Environment{
		ID:           uuid.NewString(),
		TenantID:     tenant,
		Slug:         name,
		Name:         name + " services",
		Description:  name + " sandbox",
		SandboxImage: "acme-" + name,
		Size:         EnvironmentSize{CPU: "4", Memory: "8Gi", Disk: "20Gi"},
		EgressHosts:  []string{name + ".example.com"},
		Connectors:   []Connector{{Connection: "github", Actions: []string{"contents:read"}}},
	}
	if err := r.CreateEnvironment(context.Background(), env); err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	return env
}

func testAgentType(tenant string, envs ...AgentEnvironment) AgentType {
	return AgentType{
		ID:                      uuid.NewString(),
		TenantID:                tenant,
		Name:                    "Builder",
		Slug:                    "builder-" + uuid.NewString()[:8],
		SystemPrompt:            "build things",
		Framework:               AgentFrameworkClaudeCode,
		GrantScopes:             []string{"inference:invoke"},
		MaxConcurrentRunsPerPod: 1,
		State:                   AgentTypeStateActive,
		ModelID:                 new("bedrock/claude-sonnet-4-5"),
		MCPTools:                []string{"mcp:kora:use"},
		Environments:            envs,
		IdentityMode:            IdentityModeDelegated,
	}
}

// An environment round-trips every column, including its image and size.
func TestPostgres_EnvironmentRoundTrip(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	env := testEnvironment(t, r, tenant, "go")

	got, err := r.GetEnvironment(ctx, tenant, env.ID)
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	if got.SandboxImage != "acme-go" || got.Size != env.Size || got.EgressHosts[0] != "go.example.com" || got.Connectors[0].Connection != "github" {
		t.Errorf("environment = %+v, want %+v", got, env)
	}

	env.Size.Disk = "30Gi"
	env.SandboxImage = "kora-base"
	if err := r.UpdateEnvironment(ctx, env); err != nil {
		t.Fatalf("UpdateEnvironment: %v", err)
	}
	got, _ = r.GetEnvironment(ctx, tenant, env.ID)
	if got.Size.Disk != "30Gi" || got.SandboxImage != "kora-base" {
		t.Errorf("after update: %+v", got)
	}

	// The column defaults to kora-base.
	var def string
	if err := r.pool.QueryRow(ctx, `INSERT INTO environments (id, tenant_id, name) VALUES ($1,$2,'defaulted') RETURNING sandbox_image`, uuid.NewString(), tenant).Scan(&def); err != nil || def != "kora-base" {
		t.Errorf("column default = %q (%v), want kora-base", def, err)
	}
	if _, err := r.GetEnvironment(ctx, "other-tenant", env.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another tenant's read = %v, want ErrNotFound", err)
	}
}

// The slug and the name for people are separate columns, an update changes only
// the name, and a row the previous release wrote without a name reads its slug.
func TestPostgres_EnvironmentSlugAndName(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	env := testEnvironment(t, r, tenant, "go")

	got, err := r.GetEnvironment(ctx, tenant, env.ID)
	if err != nil {
		t.Fatalf("GetEnvironment: %v", err)
	}
	if got.Slug != "go" || got.Name != "go services" {
		t.Errorf("slug, name = %q, %q; want go, go services", got.Slug, got.Name)
	}

	env.Name = "Go"
	env.Slug = "ignored"
	if err := r.UpdateEnvironment(ctx, env); err != nil {
		t.Fatalf("UpdateEnvironment: %v", err)
	}
	got, _ = r.GetEnvironment(ctx, tenant, env.ID)
	if got.Slug != "go" || got.Name != "Go" {
		t.Errorf("after update slug, name = %q, %q; want go, Go", got.Slug, got.Name)
	}

	legacy := uuid.NewString()
	if _, err := r.pool.Exec(ctx, `INSERT INTO environments (id, tenant_id, name) VALUES ($1,$2,'legacy')`, legacy, tenant); err != nil {
		t.Fatalf("insert without display_name: %v", err)
	}
	got, err = r.GetEnvironment(ctx, tenant, legacy)
	if err != nil || got.Slug != "legacy" || got.Name != "legacy" {
		t.Errorf("legacy row = %+v (%v), want its slug as the name", got, err)
	}

	dup := env
	dup.ID = uuid.NewString()
	dup.Slug = "go"
	dup.Name = "Another name"
	if err := r.CreateEnvironment(ctx, dup); !errors.Is(err, ErrEnvironmentSlugTaken) {
		t.Errorf("duplicate slug = %v, want ErrEnvironmentSlugTaken", err)
	}
}

// An agent's environments are stored as links with one default, read back
// default first, and replaced on update. The projection must match the scanner.
func TestPostgres_AgentEnvironments(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	goEnv := testEnvironment(t, r, tenant, "go")
	nodeEnv := testEnvironment(t, r, tenant, "node")

	at := testAgentType(tenant,
		AgentEnvironment{EnvironmentID: nodeEnv.ID},
		AgentEnvironment{EnvironmentID: goEnv.ID, IsDefault: true},
	)
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	got, err := r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if len(got.Environments) != 2 || got.Environments[0] != (AgentEnvironment{EnvironmentID: goEnv.ID, IsDefault: true}) {
		t.Errorf("environments = %+v, want the go default first", got.Environments)
	}
	if got.IdentityMode != IdentityModeDelegated || got.MCPTools[0] != "mcp:kora:use" {
		t.Errorf("agent type = %+v", got)
	}

	byEnv, err := r.ListAgentTypesByEnvironment(ctx, tenant, nodeEnv.ID)
	if err != nil {
		t.Fatalf("ListAgentTypesByEnvironment: %v", err)
	}
	if len(byEnv) != 1 || byEnv[0].ID != at.ID {
		t.Errorf("agents for node = %+v, want the builder", byEnv)
	}

	at.Environments = []AgentEnvironment{{EnvironmentID: nodeEnv.ID, IsDefault: true}}
	if err := r.UpdateAgentType(ctx, at); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	got, _ = r.GetAgentType(ctx, at.ID)
	if len(got.Environments) != 1 || got.Environments[0].EnvironmentID != nodeEnv.ID {
		t.Errorf("after update: %+v, want node only", got.Environments)
	}
	if byEnv, _ := r.ListAgentTypesByEnvironment(ctx, tenant, goEnv.ID); len(byEnv) != 0 {
		t.Errorf("go is still attached to %d agents", len(byEnv))
	}

	// Two defaults break the agent's unique default index.
	at.Environments = []AgentEnvironment{{EnvironmentID: nodeEnv.ID, IsDefault: true}, {EnvironmentID: goEnv.ID, IsDefault: true}}
	if err := r.UpdateAgentType(ctx, at); err == nil {
		t.Error("an agent with two default environments was stored")
	}
	got, _ = r.GetAgentType(ctx, at.ID)
	if len(got.Environments) != 1 {
		t.Errorf("a failed update changed the links: %+v", got.Environments)
	}
}

// An attached environment cannot be deleted. Once detached it can, and a run
// that ran in it keeps no dangling reference.
func TestPostgres_DeleteAttachedEnvironment(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	env := testEnvironment(t, r, tenant, "go")
	at := testAgentType(tenant, AgentEnvironment{EnvironmentID: env.ID, IsDefault: true})
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	run := Run{ID: uuid.NewString(), TenantID: tenant, AgentTypeID: at.ID}
	if err := r.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if err := r.SetRunEnvironment(ctx, run.ID, &env.ID); err != nil {
		t.Fatalf("SetRunEnvironment: %v", err)
	}
	got, err := r.GetRun(ctx, run.ID)
	if err != nil || got.EnvironmentID == nil || *got.EnvironmentID != env.ID {
		t.Fatalf("run environment = %v (%v), want %s", got.EnvironmentID, err, env.ID)
	}

	if err := r.DeleteEnvironment(ctx, tenant, env.ID); !errors.Is(err, ErrEnvironmentInUse) {
		t.Fatalf("delete attached = %v, want ErrEnvironmentInUse", err)
	}
	at.Environments = nil
	if err := r.UpdateAgentType(ctx, at); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	if err := r.DeleteEnvironment(ctx, tenant, env.ID); err != nil {
		t.Fatalf("delete detached: %v", err)
	}
	got, _ = r.GetRun(ctx, run.ID)
	if got.EnvironmentID != nil {
		t.Errorf("run environment = %v after the delete, want nil", *got.EnvironmentID)
	}
}

// ListEnvironmentsByIDs keeps the order of the IDs and leaves out IDs the tenant has no environment for.
func TestPostgres_ListEnvironmentsByIDs(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	a := testEnvironment(t, r, tenant, "a")
	b := testEnvironment(t, r, tenant, "b")
	other, otherTenant := testEnvironmentRepo(t)
	foreign := testEnvironment(t, other, otherTenant, "c")

	got, err := r.ListEnvironmentsByIDs(ctx, tenant, []string{b.ID, "missing", foreign.ID, a.ID})
	if err != nil {
		t.Fatalf("ListEnvironmentsByIDs: %v", err)
	}
	if len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Errorf("got %+v, want b then a", got)
	}
	if got, err := r.ListEnvironmentsByIDs(ctx, tenant, nil); err != nil || len(got) != 0 {
		t.Errorf("no ids = %v, %v", got, err)
	}
}
