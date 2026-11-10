package agents

import (
	"context"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/runkora/kora/backend/cmd/kora-agents/migrations"
)

// TestPostgres_AgentTypeRoundTrip creates, reads and updates an agent type against
// the migrated schema, where tool_scopes no longer exists. The column lists are
// positional, so one that still names the dropped column fails here.
func TestPostgres_AgentTypeRoundTrip(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()

	var exists bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='agent_types' AND column_name='tool_scopes')`).Scan(&exists); err != nil {
		t.Fatalf("inspect schema: %v", err)
	}
	if exists {
		t.Fatal("agent_types.tool_scopes still exists; apply the kora-agents migrations")
	}

	at := AgentType{
		ID:                uuid.NewString(),
		TenantID:          tenant,
		Name:              "Round Trip",
		Slug:              "round-trip-" + uuid.NewString()[:8],
		SystemPrompt:      "be brief",
		Framework:         AgentFrameworkClaudeCode,
		GrantScopes:       []string{"inference:invoke"},
		Connectors:        []Connector{},
		MCPTools:          []string{"mcp:kora:agents.list:use"},
		MaxConcurrentRuns: 2,
		State:             AgentTypeStateActive,
		ModelID:           new("bedrock/claude-sonnet-4-5"),
		CreatedBy:         "alice",
	}
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}

	got, err := r.GetAgentType(ctx, at.ID)
	if err != nil {
		t.Fatalf("GetAgentType: %v", err)
	}
	if got.Slug != at.Slug || got.CreatedBy != "alice" || got.SystemPrompt != at.SystemPrompt || !slices.Equal(got.GrantScopes, at.GrantScopes) || !slices.Equal(got.MCPTools, at.MCPTools) {
		t.Errorf("read back %+v, want the created fields of %+v", got, at)
	}

	got.SystemPrompt = "be thorough"
	got.MaxConcurrentRuns = 3
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	updated, err := r.GetAgentTypeBySlug(ctx, tenant, at.Slug)
	if err != nil {
		t.Fatalf("GetAgentTypeBySlug: %v", err)
	}
	if updated.SystemPrompt != "be thorough" || updated.MaxConcurrentRuns != 3 || updated.CreatedBy != "alice" {
		t.Errorf("after update got prompt %q, max runs %d and creator %q, want %q, 3 and alice", updated.SystemPrompt, updated.MaxConcurrentRuns, updated.CreatedBy, "be thorough")
	}
}

func TestPostgres_ListActiveAgentTypes(t *testing.T) {
	r, tenant := testRunRepo(t)
	active := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	pending := seedRunAgentType(t, r, tenant, IdentityModeAutonomous)
	if err := r.SetAgentTypeState(context.Background(), pending.ID, AgentTypeStateActive, AgentTypeStateProvisioning); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListActiveAgentTypes(context.Background())
	if err != nil {
		t.Fatalf("ListActiveAgentTypes: %v", err)
	}
	var ids []string
	for _, at := range got {
		if at.TenantID == tenant {
			ids = append(ids, at.ID)
		}
	}
	if len(ids) != 1 || ids[0] != active.ID {
		t.Errorf("active agent types = %v, want only %s", ids, active.ID)
	}
}

// maxRunColumns reads both run cap columns of an agent type.
func maxRunColumns(t *testing.T, r *PostgresRepository, id string) (total, perPod int) {
	t.Helper()
	if err := r.pool.QueryRow(context.Background(),
		`SELECT max_concurrent_runs, max_concurrent_runs_per_pod FROM agent_types WHERE id=$1`, id).Scan(&total, &perPod); err != nil {
		t.Fatalf("read run caps: %v", err)
	}
	return total, perPod
}

// runMaxConcurrentRunsBackfill executes the UPDATE from the shipped migration rather than a copy of it.
func runMaxConcurrentRunsBackfill(t *testing.T, r *PostgresRepository) {
	t.Helper()
	body, err := fs.ReadFile(migrations.FS, "20261110093201_add_agent_max_concurrent_runs.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	up, _, _ := strings.Cut(string(body), "-- +goose Down")
	for line := range strings.SplitSeq(up, "\n") {
		if strings.HasPrefix(line, "UPDATE") {
			if _, err := r.pool.Exec(context.Background(), line); err != nil {
				t.Fatalf("backfill: %v", err)
			}
			return
		}
	}
	t.Fatal("no UPDATE statement in the max_concurrent_runs migration")
}

// An N-1 row gets the default total of 20, and the backfill copies its per-pod value into the total.
func TestPostgres_MaxConcurrentRunsBackfill(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	id := uuid.NewString()
	if _, err := r.pool.Exec(context.Background(),
		`INSERT INTO agent_types (id, tenant_id, name, slug, max_concurrent_runs_per_pod) VALUES ($1, $2, 'Old', $3, 7)`,
		id, tenant, "old-"+id[:8]); err != nil {
		t.Fatalf("insert N-1 row: %v", err)
	}
	if total, _ := maxRunColumns(t, r, id); total != 20 {
		t.Errorf("default max_concurrent_runs = %d, want 20", total)
	}
	runMaxConcurrentRunsBackfill(t, r)
	if total, perPod := maxRunColumns(t, r, id); total != 7 || perPod != 7 {
		t.Errorf("after backfill got total %d and per pod %d, want 7 and 7", total, perPod)
	}
}

// Creates and updates write the total to both columns, so N-1 binaries read the same cap.
func TestPostgres_MaxConcurrentRunsDualWrite(t *testing.T) {
	r, tenant := testEnvironmentRepo(t)
	ctx := context.Background()
	at := AgentType{ID: uuid.NewString(), TenantID: tenant, Name: "Dual", Slug: "dual-" + uuid.NewString()[:8],
		Framework: AgentFrameworkClaudeCode, MaxConcurrentRuns: 4, State: AgentTypeStateActive}
	if err := r.CreateAgentType(ctx, at); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	if total, perPod := maxRunColumns(t, r, at.ID); total != 4 || perPod != 4 {
		t.Errorf("after create got total %d and per pod %d, want 4 and 4", total, perPod)
	}
	at.MaxConcurrentRuns = 6
	if err := r.UpdateAgentType(ctx, at); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	if total, perPod := maxRunColumns(t, r, at.ID); total != 6 || perPod != 6 {
		t.Errorf("after update got total %d and per pod %d, want 6 and 6", total, perPod)
	}
}
