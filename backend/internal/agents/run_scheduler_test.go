package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"syscall"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func schedulerService(t *testing.T) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	f, repo, k8s, sm := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	return f, repo, k8s, sm
}

func startCtx() context.Context {
	return withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user:1")
}

// runErrors returns the run_error payloads recorded for a run.
func runErrors(t *testing.T, repo *mockRepository, runID string) []map[string]string {
	t.Helper()
	repo.mu.Lock()
	defer repo.mu.Unlock()
	var out []map[string]string
	for _, e := range repo.events[runID] {
		if e.EventType != "run_error" {
			continue
		}
		var p map[string]string
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// StartRun writes a queued pending run and touches neither the cluster nor a pod.
func TestStartRun_QueuesPendingRun(t *testing.T) {
	f, repo, k8s, sm := schedulerService(t)

	run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1", InitialMessage: "fix the build"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.CreatedAt.IsZero() {
		t.Error("the queued run has no created_at")
	}
	if stored := repo.runs[run.ID]; run.State != RunStatePending || stored.InitialMessage != "fix the build" {
		t.Errorf("state = %s, initial message = %q", run.State, stored.InitialMessage)
	}
	if len(repo.queued) != 1 || len(k8s.provisionedMissing) != 0 || len(sm.sessions) != 0 {
		t.Errorf("queued=%v provisions=%d sessions=%d, want one queued run and nothing else", repo.queued, len(k8s.provisionedMissing), len(sm.sessions))
	}
}

type scriptedSM struct {
	*mockSMClient
	errs  []error
	calls int
	// onCreate runs inside CreateSession, before it returns.
	onCreate func()
}

func (s *scriptedSM) CreateSession(ctx context.Context, url string, req sessionmanager.CreateSessionRequest) error {
	s.calls++
	if s.onCreate != nil {
		s.onCreate()
	}
	if s.calls <= len(s.errs) && s.errs[s.calls-1] != nil {
		return s.errs[s.calls-1]
	}
	return s.mockSMClient.CreateSession(ctx, url, req)
}

// setRunStateRepo fails the first SetRunState call with err. When commit is true
// the change is stored first, as when a write succeeds and its reply is lost.
type setRunStateRepo struct {
	*mockRepository
	err    error
	commit bool
	used   bool
}

func (r *setRunStateRepo) SetRunState(ctx context.Context, id string, from, to RunState) error {
	if r.used {
		return r.mockRepository.SetRunState(ctx, id, from, to)
	}
	r.used = true
	if r.commit {
		if err := r.mockRepository.SetRunState(ctx, id, from, to); err != nil {
			return err
		}
	}
	return r.err
}

// startClaimedEnv is what a startClaimed case can change before the attempt.
type startClaimedEnv struct {
	f    *testServiceFacade
	repo *mockRepository
	k8s  *mockK8s
	sm   *mockSMClient
	run  *Run
	ctx  context.Context
}

func TestStartClaimed(t *testing.T) {
	reset := errors.New("connection reset")
	for _, tc := range []struct {
		name  string
		setup func(e *startClaimedEnv)
		// The run's state, the call counts and the start_failed reason after one attempt.
		wantState    RunState
		wantReleased int
		wantSessions int
		wantClosed   int
		wantMessages int
		wantProvMiss int
		wantErr      string
	}{
		{name: "starts on a free pod and sends the initial message",
			wantState: RunStateActive, wantSessions: 1, wantMessages: 1},
		{name: "no ready pod creates missing resources and releases",
			setup:     func(e *startClaimedEnv) { e.k8s.noReadyPods = true },
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "full pods release without provisioning",
			setup:     func(e *startClaimedEnv) { e.repo.activeRunsOverride = map[int]int{0: 5} },
			wantState: RunStatePending, wantReleased: 1},
		{name: "a provisioning error releases",
			setup: func(e *startClaimedEnv) {
				e.k8s.noReadyPods = true
				e.k8s.provisionAgentTypeErr = errors.New("conflict")
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "past the start budget fails with a reason",
			setup: func(e *startClaimedEnv) {
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-2 * time.Minute)
			},
			wantState: RunStateFailed, wantErr: "No agent pod could take the run within 1m0s."},
		{name: "a deleted agent fails the run",
			setup:     func(e *startClaimedEnv) { delete(e.repo.agentTypes, "at-1") },
			wantState: RunStateFailed, wantErr: "The agent no longer exists."},
		{name: "a deleting agent fails the run",
			setup:     func(e *startClaimedEnv) { e.repo.agentTypes["at-1"].State = AgentTypeStateDeleting },
			wantState: RunStateFailed, wantErr: "The agent is being deleted."},
		{name: "a provisioning agent leaves the run queued",
			setup:     func(e *startClaimedEnv) { e.repo.agentTypes["at-1"].State = AgentTypeStateProvisioning },
			wantState: RunStatePending, wantReleased: 1},
		{name: "a run that left pending gets no session",
			setup:     func(e *startClaimedEnv) { e.repo.runs[e.run.ID].State = RunStateFailed },
			wantState: RunStateFailed},
		{name: "a run terminated during session create loses the session",
			setup: func(e *startClaimedEnv) {
				e.f.run.sm = &scriptedSM{mockSMClient: e.sm, onCreate: func() { _ = e.repo.DeleteRun(context.Background(), e.run.ID) }}
			},
			wantSessions: 1, wantClosed: 1},
		{name: "an attempt that times out during session create releases",
			setup: func(e *startClaimedEnv) {
				ctx, cancel := context.WithCancel(context.Background())
				e.ctx = ctx
				e.f.run.sm = &scriptedSM{mockSMClient: e.sm, errs: []error{context.Canceled}, onCreate: cancel}
			},
			wantState: RunStatePending, wantReleased: 1, wantClosed: 1},
		{name: "a failed activation closes the session and releases",
			setup:     func(e *startClaimedEnv) { e.f.run.repo = &setRunStateRepo{mockRepository: e.repo, err: reset} },
			wantState: RunStatePending, wantReleased: 1, wantSessions: 1, wantClosed: 1},
		{name: "an activation that committed despite an error keeps the session",
			setup: func(e *startClaimedEnv) {
				e.f.run.repo = &setRunStateRepo{mockRepository: e.repo, err: reset, commit: true}
			},
			wantState: RunStateActive, wantSessions: 1, wantMessages: 1},
		{name: "a session that cannot be closed after a failed activation fails the run",
			setup: func(e *startClaimedEnv) {
				e.sm.closeSessionErr = errors.New("pod unreachable")
				e.f.run.repo = &setRunStateRepo{mockRepository: e.repo, err: reset}
			},
			wantState: RunStateFailed, wantSessions: 1, wantErr: "The agent pod did not confirm the run."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, k8s, sm := schedulerService(t)
			run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1", InitialMessage: "fix the build"})
			if err != nil {
				t.Fatalf("StartRun: %v", err)
			}
			e := &startClaimedEnv{f: f, repo: repo, k8s: k8s, sm: sm, run: run, ctx: context.Background()}
			if tc.setup != nil {
				tc.setup(e)
			}

			claimed, claimedAt := claimRun(repo, run.ID)
			f.run.startClaimed(e.ctx, claimed, claimedAt)

			var state RunState
			if r, ok := repo.runs[run.ID]; ok {
				state = r.State
			}
			got := []int{repo.released, len(sm.sessions), len(sm.closedSessions), len(sm.sentMessages), len(k8s.provisionedMissing)}
			want := []int{tc.wantReleased, tc.wantSessions, tc.wantClosed, tc.wantMessages, tc.wantProvMiss}
			if state != tc.wantState || !slices.Equal(got, want) {
				t.Errorf("state=%s released,sessions,closed,messages,provisions=%v, want %s %v", state, got, tc.wantState, want)
			}
			var gotErr string
			if errs := runErrors(t, repo, run.ID); len(errs) == 1 && errs[0]["code"] == "start_failed" {
				gotErr = errs[0]["message"]
			}
			if gotErr != tc.wantErr {
				t.Errorf("start_failed reason = %q, want %q", gotErr, tc.wantErr)
			}
			if len(sm.sentMessages) == 1 && sm.sentMessages[0] != "fix the build" {
				t.Errorf("sent %q, want the stored initial message", sm.sentMessages[0])
			}
		})
	}
}

func TestStartClaimed_SessionErrors(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		wantState  RunState
		wantClosed int
	}{
		{"dns", &net.DNSError{Err: "no such host", Name: "bot-0.bot", IsNotFound: true}, RunStatePending, 1},
		{"refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, RunStatePending, 1},
		{"503", &sessionmanager.StatusError{StatusCode: 503, Err: fmt.Errorf("%w: status 503", sessionmanager.ErrUnexpectedStatus)}, RunStatePending, 1},
		{"400", &sessionmanager.StatusError{StatusCode: 400, Err: fmt.Errorf("%w: status 400", sessionmanager.ErrUnexpectedStatus)}, RunStateFailed, 1},
		{"conflict", fmt.Errorf("%w: status 409", sessionmanager.ErrSessionConflict), RunStateActive, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, _, sm := schedulerService(t)
			f.run.sm = &scriptedSM{mockSMClient: sm, errs: []error{tc.err}}

			run, err := startAndSchedule(startCtx(), f.run, StartRunRequest{AgentTypeID: "at-1"})
			if err != nil {
				t.Fatalf("startAndSchedule: %v", err)
			}
			if run.State != tc.wantState || len(sm.closedSessions) != tc.wantClosed {
				t.Errorf("state = %s, closed = %d, want %s and %d", run.State, len(sm.closedSessions), tc.wantState, tc.wantClosed)
			}
		})
	}
}

// A run terminated before the scheduler fails it gets no error event.
func TestFailStart_TerminatedRunGetsNoErrorEvent(t *testing.T) {
	f, repo, _, _ := schedulerService(t)
	f.run.cfg.StartBudget = time.Minute
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t1", AgentTypeID: "at-1", State: RunStatePending, CreatedAt: time.Now().Add(-time.Hour)})
	run, claimedAt := claimRun(repo, "run-1")
	_ = repo.DeleteRun(context.Background(), "run-1")

	f.run.startClaimed(context.Background(), run, claimedAt)

	if errs := runErrors(t, repo, "run-1"); len(errs) != 0 {
		t.Errorf("run errors = %v, want none", errs)
	}
}

// A delegated run that fails to start loses its delegation.
func TestStartClaimed_DelegatedBudgetFailureRevokes(t *testing.T) {
	f, repo, _, _ := newTestService(t)
	issuer := &fakeDelegationIssuer{}
	f.run.SetDelegationBroker(newDelegationTestBroker(issuer))
	at := seedDelegatedAgentType(t, repo, "at-del", "t1", "reviewer")
	ctx := WithCallerBearer(withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "u1"), "bearer-1")
	run, err := f.run.StartRun(ctx, StartRunRequest{AgentTypeID: at.ID})
	if err != nil {
		t.Fatal(err)
	}
	repo.runs[run.ID].CreatedAt = time.Now().Add(-time.Hour)

	claimed, claimedAt := claimRun(repo, run.ID)
	f.run.startClaimed(context.Background(), claimed, claimedAt)

	if repo.runs[run.ID].State != RunStateFailed || len(issuer.revokedJTIs()) != 1 {
		t.Errorf("state=%s revoked=%v, want failed and revoked", repo.runs[run.ID].State, issuer.revokedJTIs())
	}
}

// A poll claims no more runs than there are free slots, and the next poll takes the rest.
func TestPollQueue_ClaimsUpToFreeSlots(t *testing.T) {
	f, repo, _, sm := schedulerService(t)
	for range 3 {
		if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err != nil {
			t.Fatal(err)
		}
	}
	slots := make(chan struct{}, 2)

	f.run.pollQueue(context.Background(), slots)
	f.run.attempts.Wait()
	if len(sm.sessions) != 2 {
		t.Fatalf("one poll started %d runs, want 2", len(sm.sessions))
	}
	f.run.pollQueue(context.Background(), slots)
	f.run.attempts.Wait()
	f.run.reclaimStale(context.Background())
	if len(sm.sessions) != 3 || repo.reclaims != 1 {
		t.Errorf("sessions = %d, reclaims = %d, want 3 and 1", len(sm.sessions), repo.reclaims)
	}
}

// After shutdown, WaitScheduler returns only once the run start in flight has finished.
func TestWaitScheduler_WaitsForAttemptInFlight(t *testing.T) {
	f, _, _, sm := schedulerService(t)
	f.run.cfg.StartPollInterval = time.Millisecond
	if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatal(err)
	}
	creating, release := make(chan struct{}), make(chan struct{})
	f.run.sm = &scriptedSM{mockSMClient: sm, onCreate: func() { close(creating); <-release }}
	ctx, cancel := context.WithCancel(context.Background())

	f.run.StartScheduler(ctx)
	<-creating
	cancel()
	waited := make(chan struct{})
	go func() { f.run.WaitScheduler(context.Background()); close(waited) }()
	select {
	case <-waited:
		t.Fatal("WaitScheduler returned while a run start was in flight")
	default:
	}
	close(release)
	<-waited

	if len(sm.sessions) != 1 {
		t.Errorf("sessions = %d, want the start in flight to finish", len(sm.sessions))
	}
}
