package agents

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// runsByID is a runGetter over fixed runs.
type runsByID map[string]Run

func (m runsByID) GetRun(_ context.Context, id string) (*Run, error) {
	if r, ok := m[id]; ok {
		return &r, nil
	}
	return nil, ErrNotFound
}

// sandboxAgentCtx is an AGENT caller whose per-run sandbox identity attests runID.
func sandboxAgentCtx(tenantID, slug, runID string) context.Context {
	return token.WithClaims(context.Background(), &token.PlatformClaims{
		Kora: token.KoraClaims{
			PrincipalType: token.PrincipalTypeAgent,
			TenantID:      tenantID,
			DisplayName:   slug,
			Claims: map[string]string{
				"spiffe_path": "/tenant/kora-tenant-" + tenantID + "/agent/" + slug + "/sandbox/" + runID,
				"run_id":      runID,
			},
		},
	})
}

// withRunPrefix is ctx as StripRunPrefix leaves it for a request under /run/{runID}.
func withRunPrefix(ctx context.Context, runID string) context.Context {
	out := ctx
	req := httptest.NewRequest(http.MethodGet, "/run/"+runID+"/api/v1/runs", http.NoBody).WithContext(ctx)
	mw.StripRunPrefix(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { out = r.Context() })).ServeHTTP(httptest.NewRecorder(), req)
	return out
}

func TestInheritedChain(t *testing.T) {
	runs := runsByID{
		"parent": {ID: "parent", TenantID: "t-1", AgentSlug: "triage", CausedBy: "ev-1", ChainDepth: 2, TriggerIDs: []string{"trg-a"}},
		"plain":  {ID: "plain", TenantID: "t-1", AgentSlug: "triage"},
		"manual": {ID: "manual", TenantID: "t-1", AgentSlug: "triage", TriggerIDs: []string{"trg-m"}},
	}
	inherited := runChain{causedBy: "ev-1", depth: 3, triggers: []string{"trg-a"}}
	tests := []struct {
		name   string
		ctx    context.Context
		target string
		want   runChain
	}{
		{name: "sandbox run", ctx: sandboxAgentCtx("t-1", "triage", "parent"), want: inherited},
		{name: "delegated run", ctx: policy.WithDelegation(withAgentCtx(context.Background(), "t-1", "triage"), &policy.DelegationClaims{RunID: "parent"}), want: inherited},
		{name: "a run in no trigger chain", ctx: sandboxAgentCtx("t-1", "triage", "plain")},
		{name: "a run a trigger started at depth 0", ctx: sandboxAgentCtx("t-1", "triage", "manual"), want: runChain{depth: 1, triggers: []string{"trg-m"}}},
		{name: "shared pod agent", ctx: withAgentCtx(context.Background(), "t-1", "triage")},
		{name: "shared pod agent through the run prefix", ctx: withRunPrefix(withAgentCtx(context.Background(), "t-1", "triage"), "parent"), want: inherited},
		{name: "run prefix of another agent's run", ctx: withRunPrefix(withAgentCtx(context.Background(), "t-1", "coder"), "parent")},
		{name: "run prefix of another tenant's run", ctx: withRunPrefix(withAgentCtx(context.Background(), "t-2", "triage"), "parent")},
		{name: "run prefix of a run in no trigger chain", ctx: withRunPrefix(withAgentCtx(context.Background(), "t-1", "triage"), "plain")},
		{name: "run prefix from a person", ctx: withRunPrefix(withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1"), "parent")},
		{name: "another agent's run", ctx: sandboxAgentCtx("t-1", "coder", "parent")},
		{name: "another tenant's run", ctx: sandboxAgentCtx("t-2", "triage", "parent")},
		{name: "unknown run", ctx: sandboxAgentCtx("t-1", "triage", "gone")},
		{name: "a message to its own run", ctx: sandboxAgentCtx("t-1", "triage", "parent"), target: "parent"},
		{name: "a person", ctx: withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inheritedChain(tt.ctx, runs, tt.target)
			if err != nil {
				t.Fatal(err)
			}
			if got.causedBy != tt.want.causedBy || got.depth != tt.want.depth || !slices.Equal(got.triggers, tt.want.triggers) {
				t.Errorf("inheritedChain = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// A run an agent starts from an attested run carries that run's chain one step deeper.
func TestStartRun_InheritsTheCallingRunsChain(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedAutonomousAgentType(t, repo, "at-1", "t1", "reviewer")
	repo.runs["parent"] = &Run{ID: "parent", TenantID: "t1", AgentTypeID: at.ID, AgentSlug: "reviewer", CausedBy: "ev-1", ChainDepth: 1, TriggerIDs: []string{"trg-a"}}

	run, err := f.run.StartRun(sandboxAgentCtx("t1", "reviewer", "parent"), StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	stored, _ := repo.GetRun(context.Background(), run.ID)
	if stored.CausedBy != "ev-1" || stored.ChainDepth != 2 || !slices.Equal(stored.TriggerIDs, []string{"trg-a"}) {
		t.Errorf("chain = %s %d %v, want ev-1 2 [trg-a]", stored.CausedBy, stored.ChainDepth, stored.TriggerIDs)
	}
}

// Only kora-triggers reads a run's chain, and only the chain of the named agent's run in the named tenant.
func TestHandleGetRunChain(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "triage"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", AgentSlug: "triage", State: RunStateActive,
		CausedBy: "ev-1", ChainDepth: 2, TriggerIDs: []string{"trg-a"}})

	consumer := principalClaims(token.PrincipalTypeSystem, "kora-triggers")
	inChain := `{"caused_by":"ev-1","chain_depth":2,"trigger_ids":["trg-a"]}`
	empty := `{"caused_by":"","chain_depth":0,"trigger_ids":[]}`
	cases := []struct {
		name   string
		query  string
		claims *token.PlatformClaims
		status int
		body   string
	}{
		{"the agent's run", "tenant=t-1&agent=triage", consumer, http.StatusOK, inChain},
		{"another agent", "tenant=t-1&agent=coder", consumer, http.StatusOK, empty},
		{"another tenant", "tenant=t-2&agent=triage", consumer, http.StatusOK, empty},
		{"other system component", "tenant=t-1&agent=triage", principalClaims(token.PrincipalTypeSystem, "egress-gateway"), http.StatusForbidden, ""},
		{"agent principal", "tenant=t-1&agent=triage", principalClaims(token.PrincipalTypeAgent, "kora-triggers"), http.StatusForbidden, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/internal/runs/run-1/chain?"+tc.query, http.NoBody)
			req.SetPathValue("id", "run-1")
			req = req.WithContext(token.WithClaims(req.Context(), tc.claims))
			w := httptest.NewRecorder()
			c.handleGetRunChain(w, req)
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.status, w.Body.String())
			}
			if got := strings.TrimSpace(w.Body.String()); tc.body != "" && got != tc.body {
				t.Errorf("body = %s, want %s", got, tc.body)
			}
		})
	}
}
