package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// countingEnvironments counts the environment queries the resolver makes.
type countingEnvironments struct {
	environmentLookup
	calls int
}

func (c *countingEnvironments) ListEnvironmentsByIDs(ctx context.Context, tenantID string, ids []string) ([]Environment, error) {
	c.calls++
	return c.environmentLookup.ListEnvironmentsByIDs(ctx, tenantID, ids)
}

// readQueryEvaluator answers the candidate query from per-slug read grants.
type readQueryEvaluator struct{ scopedEvaluator }

func (e *readQueryEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	var allowed []string
	for _, c := range in.Candidates {
		if slices.Contains(e.granted, "agents:types:"+c.PathSegments[1]+":read") {
			allowed = append(allowed, c.ID)
		}
	}
	return allowed, nil
}

func seedBatchAgents(t *testing.T, repo *mockRepository) {
	t.Helper()
	repo.envs = map[string]*Environment{
		"env-1": {ID: "env-1", TenantID: "t-1", Connectors: []Connector{{Connection: "gh", Actions: []string{"pulls:write"}}}},
	}
	seedAgentType(t, repo, AgentType{
		ID: "at-a", TenantID: "t-1", Slug: "a", MCPTools: []string{"mcp:github:get_file_contents:use"},
		Connectors:   []Connector{{Connection: "gh", Actions: []string{"contents:read"}}},
		Environments: []AgentEnvironment{{EnvironmentID: "env-1"}},
	})
	seedAgentType(t, repo, AgentType{ID: "at-b", TenantID: "t-1", Slug: "b"})
	seedAgentType(t, repo, AgentType{
		ID: "at-c", TenantID: "t-1", Slug: "c",
		Connectors: []Connector{{Connection: "other", Actions: []string{"x:read"}}},
	})
}

func TestListAgentTypesByIDs_FiltersAndReadChecksEachType(t *testing.T) {
	svc, repo := newModeTestService(t)
	svc.authz = NewAuthz(&readQueryEvaluator{scopedEvaluator{granted: []string{"agents:types:a:read", "agents:types:b:read"}}})
	seedBatchAgents(t, repo)
	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")

	got, err := svc.ListAgentTypesByIDs(ctx, []string{"at-a", "at-c", "missing"}, false)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].ID != "at-a" {
		t.Fatalf("got %+v, want only at-a: at-c is unreadable and missing does not exist", got)
	}
	if got[0].EffectiveConnectors != nil {
		t.Errorf("effective connectors = %+v without include", got[0].EffectiveConnectors)
	}
}

func TestListAgentTypesByIDs_EffectiveAccessMergesEnvironmentsWithOneQuery(t *testing.T) {
	svc, repo := newModeTestService(t, "agents:types:read")
	seedBatchAgents(t, repo)
	envs := &countingEnvironments{environmentLookup: repo}
	svc.SetScopeResolver(NewScopeResolver(envs, nil))
	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")

	got, err := svc.ListAgentTypesByIDs(ctx, []string{"at-a", "at-b", "at-c"}, true)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if envs.calls != 1 {
		t.Errorf("environment queries = %d, want 1", envs.calls)
	}
	byID := map[string][]Connector{}
	for _, at := range got {
		byID[at.ID] = at.EffectiveConnectors
	}
	want := map[string][]Connector{
		"at-a": {{Connection: "gh", Actions: []string{"contents:read", "pulls:write"}, Resources: nil}},
		"at-b": {},
		"at-c": {{Connection: "other", Actions: []string{"x:read"}}},
	}
	for id, w := range want {
		if len(w) == 0 && len(byID[id]) == 0 {
			continue
		}
		if !reflect.DeepEqual(byID[id], w) {
			t.Errorf("%s connectors = %+v, want %+v", id, byID[id], w)
		}
	}
}

func TestHandleListAgentTypes_IDsAndInclude(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedBatchAgents(t, repo)

	w := getRequest(t, c.handleListAgentTypes, "ids=at-a,at-b&include=effective_access")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body)
	}
	var resp struct {
		AgentTypes []AgentType `json:"agent_types"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.AgentTypes) != 2 {
		t.Fatalf("got %d types, want 2", len(resp.AgentTypes))
	}

	for _, q := range []string{"include=effective_access", "ids=at-a&include=bogus"} {
		if w := getRequest(t, c.handleListAgentTypes, q); w.Code != http.StatusBadRequest {
			t.Errorf("%q: status = %d, want 400", q, w.Code)
		}
	}

	w = getRequest(t, c.handleListAgentTypes)
	resp.AgentTypes = nil
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.AgentTypes) != 3 {
		t.Errorf("unfiltered list = %d types, status %d", len(resp.AgentTypes), w.Code)
	}
}
