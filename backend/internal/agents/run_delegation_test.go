package agents

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/brand"
	identityclient "github.com/runkora/kora/backend/internal/platform/clients/identity"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeDelegationIssuer is a test double for the identity client's delegation
// endpoints.
type fakeDelegationIssuer struct {
	mu            sync.Mutex
	minted        []identityclient.CreateDelegationRequest
	mintedBearers []string
	mintErr       error
	revoked       []string
	revokeBearers []string
	revokeErr     error
}

func (f *fakeDelegationIssuer) CreateDelegation(_ context.Context, bearer string, req identityclient.CreateDelegationRequest) (*identityclient.DelegationIssuance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	f.minted = append(f.minted, req)
	f.mintedBearers = append(f.mintedBearers, bearer)
	return &identityclient.DelegationIssuance{
		JTI:              "jti-" + req.RunID,
		RunID:            req.RunID,
		AgentPrincipalID: req.AgentPrincipalID,
	}, nil
}

func (f *fakeDelegationIssuer) RevokeDelegation(_ context.Context, bearer, jti string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.revokeErr != nil {
		return f.revokeErr
	}
	f.revoked = append(f.revoked, jti)
	f.revokeBearers = append(f.revokeBearers, bearer)
	return nil
}

func (f *fakeDelegationIssuer) revokedJTIs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.revoked...)
}

// withPrincipalCtx builds claims for a given principal type in the tenant.
func withPrincipalCtx(ctx context.Context, tenantID string, pt token.PrincipalType, sub string) context.Context {
	return token.WithClaims(ctx, &token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: sub},
		Kora: token.KoraClaims{
			TenantID:      tenantID,
			PrincipalType: pt,
		},
	})
}

// newDelegationTestBroker wires a broker around the fake issuer with a static
// service token source.
func newDelegationTestBroker(issuer *fakeDelegationIssuer) *DelegationBroker {
	return NewDelegationBroker(issuer, func(context.Context) (string, error) {
		return "svc-token", nil
	}, "orbit.test", logging.NewLoggerWithOutput(io.Discard))
}

func seedDelegatedAgentType(t *testing.T, repo *mockRepository, id, tenantID, slug string) AgentType {
	t.Helper()
	return seedAgentType(t, repo, AgentType{
		ID:                      id,
		TenantID:                tenantID,
		Slug:                    slug,
		IdentityMode:            IdentityModeDelegated,
		MaxConcurrentRunsPerPod: 5,
	})
}

// ── StartRun gating + mint ────────────────────────────────────────────────────

func TestStartRun_Delegated_PrincipalGating(t *testing.T) {
	tests := []struct {
		name          string
		principalType token.PrincipalType
		bearer        string
		wantErr       error
		wantStart     bool
	}{
		{"HUMAN may start", token.PrincipalTypeHuman, "human-bearer", nil, true},
		{"PAT may start", token.PrincipalTypePAT, "pat-bearer", nil, true},
		{"AGENT refused", token.PrincipalTypeAgent, "agent-bearer", ErrDelegatedRunPrincipal, false},
		{"SERVICE refused", token.PrincipalTypeService, "svc-bearer", ErrDelegatedRunPrincipal, false},
		{"SYSTEM refused", token.PrincipalTypeSystem, "sys-bearer", ErrDelegatedRunPrincipal, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			issuer := &fakeDelegationIssuer{}
			f.run.SetDelegationBroker(newDelegationTestBroker(issuer))
			at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")

			ctx := withPrincipalCtx(context.Background(), "t1", tc.principalType, "principal-1")
			ctx = WithCallerBearer(ctx, tc.bearer)

			run, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if len(repo.runs) != 0 {
					t.Errorf("expected no run rows after refused start, got %d", len(repo.runs))
				}
				if len(issuer.minted) != 0 {
					t.Errorf("expected no mint for refused principal, got %d", len(issuer.minted))
				}
				return
			}
			if err != nil {
				t.Fatalf("StartRun: %v", err)
			}
			if !tc.wantStart {
				t.Fatal("test table inconsistency")
			}
			// The mint authenticated transport as the SERVICE token (the pod's
			// identity-proxy overwrites Authorization on egress anyway) and
			// carried the CALLER's bearer as the RFC 8693 subject_token, naming
			// the agent's SPIFFE principal for the tenant namespace.
			if len(issuer.minted) != 1 {
				t.Fatalf("expected 1 mint, got %d", len(issuer.minted))
			}
			if got, want := issuer.mintedBearers[0], "svc-token"; got != want {
				t.Errorf("mint transport bearer = %q, want the service token %q", got, want)
			}
			if got, want := issuer.minted[0].SubjectToken, tc.bearer; got != want {
				t.Errorf("mint subject_token = %q, want the caller's bearer %q", got, want)
			}
			wantPrincipal := "spiffe://orbit.test/tenant/orbit-tenant-t1/agent/reviewer"
			if got := issuer.minted[0].AgentPrincipalID; got != wantPrincipal {
				t.Errorf("mint agent principal = %q, want %q", got, wantPrincipal)
			}
			if got := issuer.minted[0].RunID; got != run.ID {
				t.Errorf("mint run id = %q, want %q", got, run.ID)
			}
			// The JTI is persisted on the run.
			wantJTI := "jti-" + run.ID
			if run.DelegationJTI == nil || *run.DelegationJTI != wantJTI {
				t.Errorf("returned run DelegationJTI = %v, want %q", run.DelegationJTI, wantJTI)
			}
			stored, err := repo.GetRun(ctx, run.ID)
			if err != nil {
				t.Fatalf("GetRun: %v", err)
			}
			if stored.DelegationJTI == nil || *stored.DelegationJTI != wantJTI {
				t.Errorf("stored run DelegationJTI = %v, want %q", stored.DelegationJTI, wantJTI)
			}
		})
	}
}

func TestStartRun_Delegated_MintFailureFailsStart(t *testing.T) {
	f, repo, _, sm := newTestService(t)
	issuer := &fakeDelegationIssuer{mintErr: errors.New("identity unreachable")}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))
	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")

	ctx := WithCallerBearer(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "u1"), "bearer-1")
	_, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if err == nil {
		t.Fatal("expected StartRun to fail when the mint fails (fail closed)")
	}
	if len(sm.sessions) != 0 {
		t.Error("no session must be created when the mint fails")
	}
	// The created run row is failed synchronously, not left pending.
	for _, r := range repo.runs {
		if r.State != RunStateFailed {
			t.Errorf("run %s state = %s, want failed", r.ID, r.State)
		}
	}
}

func TestStartRun_Delegated_NoBrokerFailsClosed(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")

	ctx := WithCallerBearer(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "u1"), "bearer-1")
	_, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if !errors.Is(err, ErrDelegationUnavailable) {
		t.Fatalf("err = %v, want ErrDelegationUnavailable", err)
	}
}

func TestStartRun_Delegated_NoBearerFailsClosed(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	f.run.SetDelegationBroker(newDelegationTestBroker(&fakeDelegationIssuer{}))
	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")

	// Claims present but no raw bearer lifted into ctx.
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "u1")
	_, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if !errors.Is(err, ErrDelegationUnavailable) {
		t.Fatalf("err = %v, want ErrDelegationUnavailable", err)
	}
}

func TestStartRun_Delegated_SessionFailureRevokesMint(t *testing.T) {
	f, repo, _, sm := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))
	sm.createSessionErr = errors.New("session-manager unreachable")
	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")

	ctx := WithCallerBearer(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "u1"), "bearer-1")
	run, err := startAndSchedule(ctx, f.run, StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatalf("startAndSchedule: %v", err)
	}
	if run.State != RunStateFailed {
		t.Fatalf("state = %s, want failed", run.State)
	}
	if len(issuer.minted) != 1 {
		t.Fatalf("expected 1 mint before the session failure, got %d", len(issuer.minted))
	}
	wantJTI := "jti-" + issuer.minted[0].RunID
	if got := issuer.revokedJTIs(); len(got) != 1 || got[0] != wantJTI {
		t.Errorf("revoked = %v, want [%s] (a failed start must not leave a live delegation)", got, wantJTI)
	}
}

func TestStartRun_Autonomous_NeverMints(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))
	at := seedAgentType(t, repo, AgentType{ID: "at-auto", TenantID: "t1", Slug: "auto", MaxConcurrentRunsPerPod: 5})

	// Even a SERVICE principal may start an autonomous run (authz permitting).
	ctx := withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeService, "svc-1")
	run, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if len(issuer.minted) != 0 {
		t.Errorf("autonomous run must not mint a delegation, got %d mints", len(issuer.minted))
	}
	if run.DelegationJTI != nil {
		t.Errorf("autonomous run DelegationJTI = %v, want nil", run.DelegationJTI)
	}
}

// ── Terminal-state revocation ─────────────────────────────────────────────────

func TestTransition_TerminalStatesRevokeDelegation(t *testing.T) {
	terminalStates := []RunState{RunStateCompleted, RunStateFailed, RunStateBudgetExhausted}
	for _, terminal := range terminalStates {
		t.Run(string(terminal), func(t *testing.T) {
			f, repo, _, _ := newTestService(t)
			issuer := &fakeDelegationIssuer{}
			f.run.SetDelegationBroker(newDelegationTestBroker(issuer))

			jti := "jti-run-1"
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive, DelegationJTI: &jti})

			if err := f.run.Transition(context.Background(), "run-1", RunStateActive, terminal); err != nil {
				t.Fatalf("Transition: %v", err)
			}
			if got := issuer.revokedJTIs(); len(got) != 1 || got[0] != jti {
				t.Errorf("revoked = %v, want [%s]", got, jti)
			}
			// The revoke authenticated with kora-agents' own service token.
			if len(issuer.revokeBearers) != 1 || issuer.revokeBearers[0] != "svc-token" {
				t.Errorf("revoke bearer = %v, want [svc-token]", issuer.revokeBearers)
			}
		})
	}
}

func TestTransition_NonTerminalStateDoesNotRevoke(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))

	jti := "jti-run-1"
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive, DelegationJTI: &jti})

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateIdle); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if got := issuer.revokedJTIs(); len(got) != 0 {
		t.Errorf("revoked = %v, want none on a non-terminal transition", got)
	}
}

func TestTransition_TerminalState_AutonomousRunNoRevoke(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))

	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive})

	if err := f.run.Transition(context.Background(), "run-1", RunStateActive, RunStateCompleted); err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if got := issuer.revokedJTIs(); len(got) != 0 {
		t.Errorf("revoked = %v, want none for an autonomous run", got)
	}
}

func TestTerminateRun_RevokesDelegation(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))

	jti := "jti-run-term"
	seedRun(t, repo, Run{ID: "run-term", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive, DelegationJTI: &jti})

	if err := f.TerminateRun(context.Background(), "run-term"); err != nil {
		t.Fatalf("TerminateRun: %v", err)
	}
	if got := issuer.revokedJTIs(); len(got) != 1 || got[0] != jti {
		t.Errorf("revoked = %v, want [%s]", got, jti)
	}
	if _, ok := repo.runs["run-term"]; ok {
		t.Error("run row should be deleted after termination")
	}
}

func TestTerminateRun_RevocationFailureDoesNotWedgeTermination(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{revokeErr: errors.New("identity unreachable")}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))

	jti := "jti-run-term"
	seedRun(t, repo, Run{ID: "run-term", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive, DelegationJTI: &jti})

	if err := f.TerminateRun(context.Background(), "run-term"); err != nil {
		t.Fatalf("TerminateRun must succeed despite revoke failure, got: %v", err)
	}
	if _, ok := repo.runs["run-term"]; ok {
		t.Error("run row should be deleted even when the revoke fails")
	}
}

func TestReaper_RevokesDelegationOnReapedRuns(t *testing.T) {
	repo := newMockRepository()
	issuer := &fakeDelegationIssuer{}
	logger := logging.NewLoggerWithOutput(io.Discard)
	runSvc, err := NewRunService(repo, repo, &mockSMClient{}, &mockK8s{}, NewPubSub(), NewAuthz(&allowAllEvaluator{}), repo, &mockK8s{}, &mockK8s{}, repo, RunServiceConfig{
		MaxConcurrentRunsPerPod: 5,
		TenantNamespaceFn:       func(tenantID string) string { return "orbit-tenant-" + tenantID },
		StaleRunTimeout:         time.Millisecond,
		ReaperInterval:          5 * time.Millisecond,
		Brand:                   brand.MustNew("kora"),
	}, logger)
	if err != nil {
		t.Fatal(err)
	}
	runSvc.SetDelegationBroker(newDelegationTestBroker(issuer))

	jti := "jti-stale"
	stale := Run{ID: "run-stale", TenantID: "t1", AgentTypeID: "at-1", State: RunStateActive, DelegationJTI: &jti, UpdatedAt: time.Now().Add(-time.Hour)}
	repo.runs[stale.ID] = &stale

	ctx := t.Context()
	go runSvc.StartReaper(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := issuer.revokedJTIs(); len(got) == 1 && got[0] == jti {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("reaper did not revoke the stale run's delegation; revoked = %v", issuer.revokedJTIs())
}

func TestDeleteAgentType_RevokesDelegationsOfForceDeletedRuns(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.at.SetDelegationBroker(newDelegationTestBroker(issuer))

	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")
	jti := "jti-run-del"
	seedRun(t, repo, Run{ID: "run-del", TenantID: "t1", AgentTypeID: at.ID, State: RunStateActive, DelegationJTI: &jti})

	if err := f.DeleteAgentType(context.Background(), at.ID); err != nil {
		t.Fatalf("DeleteAgentType: %v", err)
	}
	if got := issuer.revokedJTIs(); len(got) != 1 || got[0] != jti {
		t.Errorf("revoked = %v, want [%s]", got, jti)
	}
}
