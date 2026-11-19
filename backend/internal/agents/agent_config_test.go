package agents

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
)

func digestAgent() AgentType {
	model := "kora/sonnet"
	return AgentType{
		ID: "at-1", TenantID: "t1", Slug: "bot",
		SystemPrompt: "You review pull requests.",
		Framework:    AgentFrameworkClaudeCode,
		ModelID:      &model,
		GrantScopes:  []string{"inference:invoke", "events:read"},
		MCPTools:     []string{"mcp:github:get_pr:use", "mcp:argocd:sync:use"},
		Connectors:   []Connector{{Connection: "github", Actions: []string{"issues:write", "contents:read"}}},
		Environments: []AgentEnvironment{{EnvironmentID: "env-b"}, {EnvironmentID: "env-a", IsDefault: true}},
	}
}

func digestAccess() *EffectiveAccess {
	return &EffectiveAccess{
		Hosts:           []string{"b.example.com", "a.example.com"},
		Connectors:      []Connector{{Connection: "github", Actions: []string{"contents:read"}}},
		ConnectorEgress: []ConnectorEgress{{Connection: "github", Hosts: []string{"github.com", "api.github.com"}}},
	}
}

// digestGuardrails resolves at's guardrails as the session request does, with fixed sandbox settings.
func digestGuardrails(at AgentType) sessionmanager.RunGuardrails {
	return runGuardrails(SandboxGuardrails{ProvisionAttempts: 3}, at.Guardrails)
}

func TestAgentConfigDigest_StableAndOrderFree(t *testing.T) {
	want := agentConfigDigest(digestAgent(), digestAccess(), digestGuardrails(digestAgent()), true, "Kora")
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(want) {
		t.Fatalf("digest %q is not sha256 hex", want)
	}
	if got := agentConfigDigest(digestAgent(), digestAccess(), digestGuardrails(digestAgent()), true, "Kora"); got != want {
		t.Errorf("the same config hashed to %s and %s", want, got)
	}

	// Lists whose order has no meaning do not move the digest.
	at, access := digestAgent(), digestAccess()
	at.MCPTools = []string{"mcp:argocd:sync:use", "mcp:github:get_pr:use"}
	at.GrantScopes = []string{"events:read", "inference:invoke"}
	at.Connectors[0].Actions = []string{"contents:read", "issues:write"}
	at.Environments = []AgentEnvironment{{EnvironmentID: "env-a", IsDefault: true}, {EnvironmentID: "env-b"}}
	access.Hosts = []string{"a.example.com", "b.example.com"}
	access.ConnectorEgress[0].Hosts = []string{"api.github.com", "github.com"}
	if got := agentConfigDigest(at, access, digestGuardrails(at), true, "Kora"); got != want {
		t.Errorf("reordered lists changed the digest")
	}
}

func TestAgentConfigDigest_ChangesWithEachInput(t *testing.T) {
	base := agentConfigDigest(digestAgent(), digestAccess(), digestGuardrails(digestAgent()), true, "Kora")
	other := "kora/opus"
	for _, tc := range []struct {
		name   string
		change func(at *AgentType, access **EffectiveAccess, mount *bool, product *string)
	}{
		{"system prompt", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) { at.SystemPrompt += " Be brief." }},
		{"model", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) { at.ModelID = &other }},
		{"no model", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) { at.ModelID = nil }},
		{"framework", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) { at.Framework = "other" }},
		{"mcp tools", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.MCPTools = append(at.MCPTools, "mcp:github:merge:use")
		}},
		{"connectors", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.Connectors[0].Resources = []string{"kora"}
		}},
		{"grant scopes", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.GrantScopes = append(at.GrantScopes, "runs:read")
		}},
		{"connectivity", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.Connectivity = Connectivity{Mode: ConnectivityWhitelist, Hosts: []string{"c.example.com"}}
		}},
		{"environments", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.Environments = at.Environments[:1]
		}},
		{"default environment", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			at.Environments[0].IsDefault, at.Environments[1].IsDefault = true, false
		}},
		{"guardrails", func(at *AgentType, _ **EffectiveAccess, _ *bool, _ *string) {
			warn := DefaultRepeatedCallWarn + 1
			at.Guardrails.RepeatedCallWarn = &warn
		}},
		{"no brief", func(_ *AgentType, access **EffectiveAccess, _ *bool, _ *string) { *access = nil }},
		{"brief hosts", func(_ *AgentType, access **EffectiveAccess, _ *bool, _ *string) {
			(*access).Hosts = []string{"a.example.com"}
		}},
		{"brief open egress", func(_ *AgentType, access **EffectiveAccess, _ *bool, _ *string) { (*access).Open = true }},
		{"brief connectors", func(_ *AgentType, access **EffectiveAccess, _ *bool, _ *string) {
			(*access).Connectors[0].Actions = []string{"issues:write"}
		}},
		{"brief connector hosts", func(_ *AgentType, access **EffectiveAccess, _ *bool, _ *string) {
			(*access).ConnectorEgress[0].Hosts = []string{"github.com"}
		}},
		{"sandbox mount", func(_ *AgentType, _ **EffectiveAccess, mount *bool, _ *string) { *mount = false }},
		{"product", func(_ *AgentType, _ **EffectiveAccess, _ *bool, product *string) { *product = "Other" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at, access, mount, product := digestAgent(), digestAccess(), true, "Kora"
			tc.change(&at, &access, &mount, &product)
			if agentConfigDigest(at, access, digestGuardrails(at), mount, product) == base {
				t.Errorf("changing the %s kept the digest", tc.name)
			}
		})
	}
}

// Fields that are not config, like the agent's name or state, do not move the digest.
func TestAgentConfigDigest_IgnoresNonConfigFields(t *testing.T) {
	base := agentConfigDigest(digestAgent(), digestAccess(), digestGuardrails(digestAgent()), true, "Kora")
	at := digestAgent()
	at.Name, at.Description, at.State, at.MaxConcurrentRuns = "Renamed", "New text", AgentTypeStateProvisioning, 9
	if agentConfigDigest(at, digestAccess(), digestGuardrails(at), true, "Kora") != base {
		t.Error("a non-config field changed the digest")
	}
}

// Two runs of one agent store the same digest though their briefs name different workspaces.
func TestStartClaimed_RecordsAgentConfig(t *testing.T) {
	f, repo, _, sm := schedulerService(t)
	f.run.cfg.SandboxMount = true
	f.run.SetScopeResolver(NewScopeResolver(nil, nil))

	var digests, prompts []string
	for range 2 {
		run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1", InitialMessage: "fix the build"})
		if err != nil {
			t.Fatalf("StartRun: %v", err)
		}
		claimed, claimedAt := claimRun(repo, run.ID)
		f.run.startClaimed(context.Background(), claimed, claimedAt)
		stored := repo.runs[run.ID]
		if stored.AgentConfig == nil {
			t.Fatal("the started run has no agent_config")
		}
		digests = append(digests, *stored.AgentConfig)
		prompts = append(prompts, sm.sessions[len(sm.sessions)-1].SystemPrompt)
	}
	if prompts[0] == prompts[1] {
		t.Fatal("the two runs' briefs are identical, so the workspace dir is not exercised")
	}
	if digests[0] != digests[1] {
		t.Errorf("two runs of one agent stored digests %s and %s", digests[0], digests[1])
	}
}

// A failed digest write leaves the run queued and creates no session.
func TestStartClaimed_AgentConfigWriteFailureReleases(t *testing.T) {
	f, repo, _, sm := schedulerService(t)
	repo.setRunAgentConfigErr = errors.New("connection reset")
	run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	claimed, claimedAt := claimRun(repo, run.ID)
	f.run.startClaimed(context.Background(), claimed, claimedAt)
	if repo.runs[run.ID].State != RunStatePending || repo.released != 1 || len(sm.sessions) != 0 {
		t.Errorf("state=%s released=%d sessions=%d, want pending, 1, 0", repo.runs[run.ID].State, repo.released, len(sm.sessions))
	}
}
