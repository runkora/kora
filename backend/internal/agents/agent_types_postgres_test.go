package agents

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"
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
		ID:                      uuid.NewString(),
		TenantID:                tenant,
		Name:                    "Round Trip",
		Slug:                    "round-trip-" + uuid.NewString()[:8],
		SystemPrompt:            "be brief",
		Framework:               AgentFrameworkClaudeCode,
		GrantScopes:             []string{"inference:invoke"},
		Connectors:              []Connector{},
		MCPTools:                []string{"mcp:kora:agents.list:use"},
		MaxConcurrentRunsPerPod: 2,
		State:                   AgentTypeStateActive,
		ModelID:                 new("bedrock/claude-sonnet-4-5"),
		CreatedBy:               "alice",
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
	got.MaxConcurrentRunsPerPod = 3
	if err := r.UpdateAgentType(ctx, *got); err != nil {
		t.Fatalf("UpdateAgentType: %v", err)
	}
	updated, err := r.GetAgentTypeBySlug(ctx, tenant, at.Slug)
	if err != nil {
		t.Fatalf("GetAgentTypeBySlug: %v", err)
	}
	if updated.SystemPrompt != "be thorough" || updated.MaxConcurrentRunsPerPod != 3 || updated.CreatedBy != "alice" {
		t.Errorf("after update got prompt %q, max runs %d and creator %q, want %q, 3 and alice", updated.SystemPrompt, updated.MaxConcurrentRunsPerPod, updated.CreatedBy, "be thorough")
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
