package agents

import (
	"context"
	"errors"
	"io"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// coversEvaluator answers Check and Query from a fixed set of granted scopes
// with the platform covers hierarchy, the same rule the rego applies.
type coversEvaluator struct {
	allowAllEvaluator
	granted []string
}

func (e coversEvaluator) allows(resource string, segments []string, action string) bool {
	required := policy.DeriveRequiredScope(resource, segments, action)
	return slices.ContainsFunc(e.granted, func(g string) bool { return policy.ScopeCovers(g, required) })
}

func (e coversEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	return policy.Result{Allow: e.allows(in.ResourceType, in.PathSegments, in.Action)}, nil
}

func (e coversEvaluator) Query(_ context.Context, in policy.QueryInput) ([]string, error) {
	var out []string
	for _, c := range in.Candidates {
		if e.allows(in.ResourceType, c.PathSegments, in.Action) {
			out = append(out, c.ID)
		}
	}
	return out, nil
}

func TestCheckRun_PerAgentDelegatedAndOwner(t *testing.T) {
	coder := AgentType{ID: "at-coder", Slug: "coder"}
	reviewer := AgentType{ID: "at-rev", Slug: "reviewer", IdentityMode: IdentityModeDelegated}
	mine := AgentType{ID: "at-mine", TenantID: "t1", Slug: "mine", CreatedBy: "alice"}
	tests := []struct {
		name    string
		granted []string
		caller  string
		at      AgentType
		action  string
		allowed bool
	}{
		{"per-agent read reads", []string{"agents:runs:coder:read"}, "bob", coder, "read", true},
		{"per-agent read does not write", []string{"agents:runs:coder:read"}, "bob", coder, "write", false},
		{"per-agent write writes", []string{"agents:runs:coder:write"}, "bob", coder, "write", true},
		{"another agent's scope", []string{"agents:runs:reviewer:read", "agents:runs:reviewer:write"}, "bob", coder, "write", false},
		{"parent covers", []string{"agents:runs:write"}, "bob", coder, "write", true},
		{"delegated write starts a delegated agent", []string{"agents:runs:delegated:write"}, "bob", reviewer, "write", true},
		{"delegated write does not read", []string{"agents:runs:delegated:write"}, "bob", reviewer, "read", false},
		{"delegated write does not start an autonomous agent", []string{"agents:runs:delegated:write"}, "bob", coder, "write", false},
		{"owner with the mode write scope reads", []string{"agents:types:autonomous:write"}, "alice", mine, "read", true},
		{"owner with the mode write scope writes", []string{"agents:types:autonomous:write"}, "alice", mine, "write", true},
		{"owner with the parent type write scope writes", []string{"agents:types:write"}, "alice", mine, "write", true},
		{"owner with the other mode's scope", []string{"agents:types:delegated:write"}, "alice", mine, "write", false},
		{"owner without a type write scope", nil, "alice", mine, "read", false},
		{"another caller is not the owner", []string{"agents:types:autonomous:write"}, "bob", mine, "read", false},
		{"the owner's subject in another tenant", []string{"agents:types:autonomous:write"}, "alice", AgentType{ID: "at-x", TenantID: "t2", Slug: "mine", CreatedBy: "alice"}, "read", false},
		{"no created_by matches no caller", []string{"agents:types:write"}, "", AgentType{Slug: "legacy"}, "read", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAuthz(coversEvaluator{granted: tt.granted})
			err := a.checkRun(withRunCallerCtx(context.Background(), "t1", tt.caller), tt.at, tt.action)
			if tt.allowed && err != nil {
				t.Errorf("checkRun = %v, want allowed", err)
			}
			if !tt.allowed && !errors.Is(err, ErrForbidden) {
				t.Errorf("checkRun = %v, want ErrForbidden", err)
			}
		})
	}
}

func TestAgentTypeReads_PerAgentAndOwner(t *testing.T) {
	types := func() []AgentType {
		return []AgentType{
			{ID: "at-coder", Slug: "coder"},
			{ID: "at-other", Slug: "other"},
			{ID: "at-mine", TenantID: "t1", Slug: "mine", CreatedBy: "alice"},
		}
	}
	tests := []struct {
		name     string
		granted  []string
		caller   string
		wantType []string
		wantRuns []string
	}{
		{"per-agent scopes", []string{"agents:types:coder:read", "agents:runs:coder:read"}, "bob", []string{"at-coder"}, []string{"at-coder"}},
		{"owner with per-agent scopes", []string{"agents:types:coder:read", "agents:runs:coder:read", "agents:types:autonomous:write"}, "alice", []string{"at-coder", "at-mine"}, []string{"at-coder", "at-mine"}},
		{"owner who lost the mode write scope", []string{"agents:types:coder:read", "agents:runs:coder:read"}, "alice", []string{"at-coder"}, []string{"at-coder"}},
		{"parents", []string{"agents:types:read", "agents:runs:read"}, "bob", []string{"at-coder", "at-other", "at-mine"}, []string{"at-coder", "at-other", "at-mine"}},
		{"nothing", nil, "bob", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := NewAuthz(coversEvaluator{granted: tt.granted})
			ctx := withRunCallerCtx(context.Background(), "t1", tt.caller)
			got, err := a.filterAgentTypes(ctx, types())
			if err != nil {
				t.Fatalf("filterAgentTypes: %v", err)
			}
			var ids []string
			for _, at := range got {
				ids = append(ids, at.ID)
				if err := a.checkAgentType(ctx, at, "read"); err != nil {
					t.Errorf("checkAgentType(%s) = %v, want allowed like the list", at.ID, err)
				}
			}
			for _, at := range types() {
				if !slices.Contains(tt.wantType, at.ID) && !errors.Is(a.checkAgentType(ctx, at, "read"), ErrForbidden) {
					t.Errorf("checkAgentType(%s) allowed, want ErrForbidden like the list", at.ID)
				}
			}
			if !slices.Equal(ids, tt.wantType) {
				t.Errorf("readable types = %v, want %v", ids, tt.wantType)
			}
			runs, err := a.readableRunAgentTypes(ctx, types())
			if err != nil {
				t.Fatalf("readableRunAgentTypes: %v", err)
			}
			slices.Sort(runs)
			want := slices.Clone(tt.wantRuns)
			slices.Sort(want)
			if !slices.Equal(runs, want) {
				t.Errorf("readable run agents = %v, want %v", runs, want)
			}
		})
	}
}

// D3: a caller reads the runs they started without a read scope, but acting on them still needs write.
func TestStarterReadsOwnRun(t *testing.T) {
	svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{})
	seedAutonomousRun(t, repo, "run-own", "alice", RunStateCompleted)
	seedRun(t, repo, Run{ID: "run-other", TenantID: "t1", AgentTypeID: "at-auto", State: RunStateCompleted, CreatedBy: "bob"})
	ctx := withRunCallerCtx(context.Background(), "t1", "alice")

	if _, err := svc.run.GetRun(ctx, "run-own"); err != nil {
		t.Errorf("starter GetRun = %v, want allowed", err)
	}
	if _, err := svc.run.GetRun(ctx, "run-other"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRun of another caller's run = %v, want ErrNotFound", err)
	}
	if err := svc.run.TerminateRun(ctx, "run-own"); !errors.Is(err, ErrNotFound) {
		t.Errorf("starter TerminateRun without write = %v, want ErrNotFound", err)
	}

	page, err := svc.run.ListRuns(ctx, "", RunListQuery{Limit: 10})
	if err != nil || len(page.Runs) != 1 || page.Runs[0].ID != "run-own" {
		t.Errorf("ListRuns = %+v, %v; want only run-own", page.Runs, err)
	}
	seedAgentType(t, repo, AgentType{ID: "at-x", TenantID: "t1", Slug: "x"})
	page, err = svc.run.ListRuns(ctx, "at-x", RunListQuery{Limit: 10})
	if err != nil || len(page.Runs) != 0 {
		t.Errorf("ListRuns of another agent = %+v, %v; want none", page.Runs, err)
	}
}

// Only a HUMAN session reads its own runs without a read scope (D3).
func TestNonHumanDoesNotReadOwnRunsWithoutScope(t *testing.T) {
	for _, pt := range []token.PrincipalType{token.PrincipalTypePAT, token.PrincipalTypeAgent, token.PrincipalTypeService} {
		t.Run(string(pt), func(t *testing.T) {
			svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{})
			seedAutonomousRun(t, repo, "run-own", "alice", RunStateCompleted)
			ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: "alice"},
				Kora:             token.KoraClaims{TenantID: "t1", PrincipalType: pt, PatID: "pat-1"},
			})
			if _, err := svc.run.GetRun(ctx, "run-own"); !errors.Is(err, ErrNotFound) {
				t.Errorf("GetRun = %v, want ErrNotFound", err)
			}
			page, err := svc.run.ListRuns(ctx, "", RunListQuery{Limit: 10})
			if err != nil || len(page.Runs) != 0 {
				t.Errorf("ListRuns = %+v, %v; want none", page.Runs, err)
			}
		})
	}
}

// Every run read path applies the per-agent read check to autonomous runs: a caller
// with no run scope reads nothing of a run it did not start, whatever its principal type.
func TestRunReadPaths_AutonomousRunNeedsReadScope(t *testing.T) {
	principals := []token.PrincipalType{token.PrincipalTypeHuman, token.PrincipalTypePAT, token.PrincipalTypeAgent, token.PrincipalTypeService}
	for _, pt := range principals {
		t.Run(string(pt), func(t *testing.T) {
			svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{})
			seedAutonomousRun(t, repo, "run-alice", "alice", RunStateCompleted)
			ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: "bob"},
				Kora:             token.KoraClaims{TenantID: "t1", PrincipalType: pt, PatID: "pat-1"},
			})
			paths := []struct {
				name string
				call func() error
			}{
				{"get", func() error { _, err := svc.run.GetRun(ctx, "run-alice"); return err }},
				{"message", func() error { return svc.run.SendMessage(ctx, "run-alice", "hi") }},
				{"terminate", func() error { return svc.run.TerminateRun(ctx, "run-alice") }},
				{"agent run stats", func() error { _, err := svc.run.AgentRunStats(ctx, "helper", "7d", time.Now()); return err }},
			}
			for _, p := range paths {
				if err := p.call(); !errors.Is(err, ErrNotFound) {
					t.Errorf("%s = %v, want ErrNotFound", p.name, err)
				}
			}
			page, err := svc.run.ListRuns(ctx, "", RunListQuery{Limit: 10})
			if err != nil || len(page.Runs) != 0 {
				t.Errorf("list = %+v, %v; want none", page.Runs, err)
			}
			if _, err := svc.run.RunOutcomes(ctx, time.Now().Add(-time.Hour), time.Now()); err != nil || len(repo.outcomesScope.AgentTypeIDs) != 0 {
				t.Errorf("outcomes scope = %+v, %v; want no agent types", repo.outcomesScope, err)
			}
			if _, err := svc.run.TenantRunStats(ctx, "7d", time.Now()); err != nil || len(repo.statsScope.AgentTypeIDs) != 0 {
				t.Errorf("tenant stats scope = %+v, %v; want no agent types", repo.statsScope, err)
			}
			if want := pt == token.PrincipalTypeHuman; repo.statsScope.Own != want {
				t.Errorf("own-run scope = %v, want %v for %s", repo.statsScope.Own, want, pt)
			}
		})
	}
}

// Owner access is for people: an agent or service whose subject matches created_by gets none.
func TestOwnerAllowed_PeopleOnly(t *testing.T) {
	mine := AgentType{ID: "at-mine", TenantID: "t1", Slug: "mine", CreatedBy: "alice"}
	a := NewAuthz(coversEvaluator{granted: []string{"agents:types:write"}})
	for _, tt := range []struct {
		pt   token.PrincipalType
		want bool
	}{
		{token.PrincipalTypeHuman, true},
		{token.PrincipalTypePAT, true},
		{token.PrincipalTypeAgent, false},
		{token.PrincipalTypeService, false},
		{token.PrincipalTypeSystem, false},
	} {
		ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "alice"},
			Kora:             token.KoraClaims{TenantID: "t1", PrincipalType: tt.pt, PatID: "pat-1"},
		})
		if got, err := a.ownerAllowed(ctx, mine); err != nil || got != tt.want {
			t.Errorf("%s: ownerAllowed = %v, %v; want %v", tt.pt, got, err, tt.want)
		}
	}
}

// D37: an agent type created under delegation records the person behind the
// delegation as created_by. Without delegation it records the caller.
func TestCreateAgentType_CreatorIsThePerson(t *testing.T) {
	tests := []struct {
		name       string
		delegation *policy.DelegationClaims
		want       string
	}{
		{"direct caller", nil, "agent-subject"},
		{"under delegation", &policy.DelegationClaims{Sub: "alice", ActSub: "agent-subject"}, "alice"},
		{"delegation without a subject", &policy.DelegationClaims{ActSub: "agent-subject"}, "agent-subject"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo := newModeTestService(t, "agents:types:delegated:write")
			ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
				RegisteredClaims: jwt.RegisteredClaims{Subject: "agent-subject"},
				Kora:             token.KoraClaims{TenantID: "t-1", PrincipalType: token.PrincipalTypeHuman},
			})
			if tt.delegation != nil {
				ctx = policy.WithDelegation(ctx, tt.delegation)
			}
			at, err := svc.CreateAgentType(ctx, CreateAgentTypeRequest{Name: "Bot", Slug: "bot", IdentityMode: IdentityModeDelegated})
			if err != nil {
				t.Fatalf("CreateAgentType: %v", err)
			}
			if got := repo.agentTypes[at.ID].CreatedBy; got != tt.want {
				t.Errorf("created_by = %q, want %q", got, tt.want)
			}
		})
	}
}

// blockingK8s never finishes provisioning, so only CreateAgentType itself can have run the hook.
type blockingK8s struct {
	*mockK8s
	release chan struct{}
}

func (b blockingK8s) ProvisionAgentType(context.Context, AgentType, string) error {
	<-b.release
	return nil
}

// N3: the new slug's scopes are republished before CreateAgentType returns,
// without waiting for provisioning.
func TestCreateAgentType_RepublishesScopesBeforeProvisioning(t *testing.T) {
	repo := newMockRepository()
	k8s := blockingK8s{mockK8s: &mockK8s{}, release: make(chan struct{})}
	t.Cleanup(func() { close(k8s.release) })
	svc := NewAgentTypeService(repo, repo, k8s, NewAuthz(&scopedEvaluator{granted: []string{"agents:types:delegated:write"}}),
		AgentTypeServiceConfig{MaxConcurrentRunsPerPod: 5, TenantNamespaceFn: func(id string) string { return "orbit-tenant-" + id }},
		logging.NewLoggerWithOutput(io.Discard))
	var calls atomic.Int32
	svc.SetAgentTypeHook(func() { calls.Add(1) })

	if _, err := svc.CreateAgentType(withTenantCtx(context.Background(), "t-1"), CreateAgentTypeRequest{Name: "Bot", Slug: "bot", IdentityMode: IdentityModeDelegated}); err != nil {
		t.Fatalf("CreateAgentType: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("hook calls before CreateAgentType returned = %d, want 1", got)
	}
}

// can_run on the detail read answers with the run-start check, so triggers can ask it for the caller.
func TestGetAgentType_CanRun(t *testing.T) {
	tests := []struct {
		name    string
		granted []string
		caller  string
		want    bool
	}{
		{"per-agent write", []string{"agents:types:read", "agents:runs:mine:write"}, "bob", true},
		{"parent write", []string{"agents:types:read", "agents:runs:write"}, "bob", true},
		{"read only", []string{"agents:types:read", "agents:runs:mine:read"}, "bob", false},
		{"another agent's write", []string{"agents:types:read", "agents:runs:other:write"}, "bob", false},
		{"owner with the mode write scope", []string{"agents:types:autonomous:write"}, "alice", true},
	}
	// The detail read itself needs read access, so run write alone reads nothing.
	t.Run("run write without read", func(t *testing.T) {
		svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: []string{"agents:runs:mine:write"}})
		seedAgentType(t, repo, AgentType{ID: "at-mine", TenantID: "t1", Slug: "mine", CreatedBy: "alice"})
		if _, err := svc.at.GetAgentType(withRunCallerCtx(context.Background(), "t1", "bob"), "at-mine"); !errors.Is(err, ErrNotFound) {
			t.Errorf("GetAgentType = %v, want ErrNotFound", err)
		}
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _, _ := newTestServiceWithEval(t, coversEvaluator{granted: tt.granted})
			seedAgentType(t, repo, AgentType{ID: "at-mine", TenantID: "t1", Slug: "mine", CreatedBy: "alice"})

			got, err := svc.at.GetAgentType(withRunCallerCtx(context.Background(), "t1", tt.caller), "at-mine")
			if err != nil {
				t.Fatalf("GetAgentType: %v", err)
			}
			if got.CanRun == nil || *got.CanRun != tt.want {
				t.Errorf("can_run = %v, want %v", got.CanRun, tt.want)
			}
		})
	}
}
