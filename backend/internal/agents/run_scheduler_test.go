package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/clients/sessionmanager"
	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func schedulerService(t *testing.T) (*testServiceFacade, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	f, repo, k8s, sm := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t1", Slug: "bot", MaxConcurrentRuns: 5})
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

func (s *scriptedSM) CreateSession(ctx context.Context, pod sessionmanager.Pod, req sessionmanager.CreateSessionRequest) error {
	s.calls++
	if s.onCreate != nil {
		s.onCreate()
	}
	if s.calls <= len(s.errs) && s.errs[s.calls-1] != nil {
		return s.errs[s.calls-1]
	}
	return s.mockSMClient.CreateSession(ctx, pod, req)
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

// fakeWaker records the pods it grows and returns err.
type fakeWaker struct {
	err  error
	woke []string
	warm []bool
}

func (w *fakeWaker) WakePod(_ context.Context, _, name string, warm bool) error {
	w.woke = append(w.woke, name)
	w.warm = append(w.warm, warm)
	return w.err
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
			setup:     func(e *startClaimedEnv) { e.k8s.unreadyPods = true },
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "an agent at its total releases even past the start budget",
			setup: func(e *startClaimedEnv) {
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
				for i := range 5 {
					seedRun(t, e.repo, Run{ID: fmt.Sprintf("live-%d", i), TenantID: "t1", AgentTypeID: "at-1", State: RunStateIdle})
				}
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "a provisioning error releases",
			setup: func(e *startClaimedEnv) {
				e.k8s.noReadyPods = true
				e.k8s.provisionAgentTypeErr = errors.New("conflict")
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "a run that waited past the start budget stays queued while Ready pods are full",
			setup: func(e *startClaimedEnv) {
				e.sm.memory = fullPods("bot", 0)
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "past the start budget with no Ready pod fails with a reason",
			setup: func(e *startClaimedEnv) {
				e.k8s.noReadyPods = true
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-2 * time.Minute)
				e.repo.agentTypes["at-1"].ActivatedAt = time.Now().Add(-3 * time.Minute)
			},
			wantState: RunStateFailed, wantErr: "No agent pod could take the run within 1m0s."},
		{name: "past the start budget a pod that is not Ready yet keeps the run queued",
			setup: func(e *startClaimedEnv) {
				e.k8s.unreadyPods = true
				e.k8s.livePod = true
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
			},
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "past the start budget a failed pod lookup releases the run",
			setup: func(e *startClaimedEnv) {
				e.k8s.noReadyPods = true
				e.k8s.livePodErr = errors.New("api unavailable")
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "past the start budget with no Ready pod an agent at its total keeps the run queued",
			setup: func(e *startClaimedEnv) {
				e.k8s.unreadyPods = true
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
				// Starting runs count toward the total as well as active ones.
				for i := range 4 {
					seedRun(t, e.repo, Run{ID: fmt.Sprintf("live-%d", i), TenantID: "t1", AgentTypeID: "at-1", State: RunStateIdle})
				}
				seedRun(t, e.repo, Run{ID: "starting-0", TenantID: "t1", AgentTypeID: "at-1", State: RunStateStarting})
			},
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "provisioning time does not count against the start budget",
			setup: func(e *startClaimedEnv) {
				e.k8s.noReadyPods = true
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-5 * time.Minute)
				e.repo.agentTypes["at-1"].ActivatedAt = time.Now()
			},
			wantState: RunStatePending, wantReleased: 1, wantProvMiss: 1},
		{name: "an agent provisioning past its budget fails the run",
			setup: func(e *startClaimedEnv) {
				e.f.run.cfg.ProvisionBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-2 * time.Minute)
				e.repo.agentTypes["at-1"].State = AgentTypeStateProvisioning
			},
			wantState: RunStateFailed, wantErr: "The agent did not finish provisioning within 1m0s."},
		{name: "a deleted agent fails the run",
			setup: func(e *startClaimedEnv) {
				if err := e.repo.SoftDeleteAgentType(context.Background(), "at-1"); err != nil {
					t.Fatal(err)
				}
			},
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
				e.f.run.sm = &scriptedSM{mockSMClient: e.sm, onCreate: func() { _, _ = e.repo.TerminateRun(context.Background(), e.run.ID) }}
			},
			wantState: RunStateTerminated, wantSessions: 1, wantClosed: 1},
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
		{name: "a warm pod grows before its session",
			setup:     func(e *startClaimedEnv) { e.f.run.SetPodWaker(&fakeWaker{}) },
			wantState: RunStateActive, wantSessions: 1, wantMessages: 1},
		{name: "a grow past its bound releases the run with no session",
			setup:     func(e *startClaimedEnv) { e.f.run.SetPodWaker(&fakeWaker{err: errGrowTimeout}) },
			wantState: RunStatePending, wantReleased: 1},
		{name: "a just-bound pod gets the session once it serves its agent",
			setup:     func(e *startClaimedEnv) { e.sm.unboundChecks = 2 },
			wantState: RunStateActive, wantSessions: 1, wantMessages: 1},
		{name: "a pod that never serves its agent releases the run with no session",
			setup: func(e *startClaimedEnv) {
				e.sm.unboundChecks = 1 << 30
				ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
				t.Cleanup(cancel)
				e.ctx = ctx
			},
			wantState: RunStatePending, wantReleased: 1},
		{name: "a pod with a pending resize counts as full",
			setup: func(e *startClaimedEnv) {
				e.k8s.resizing = []int{0}
				e.f.run.cfg.StartBudget = time.Minute
				e.repo.runs[e.run.ID].CreatedAt = time.Now().Add(-time.Hour)
			},
			wantState: RunStatePending, wantReleased: 1},
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
	_, _ = repo.TerminateRun(context.Background(), "run-1")

	f.run.startClaimed(context.Background(), run, claimedAt)

	if errs := runErrors(t, repo, "run-1"); len(errs) != 0 {
		t.Errorf("run errors = %v, want none", errs)
	}
}

// A delegated run that fails to start loses its delegation.
func TestStartClaimed_DelegatedBudgetFailureRevokes(t *testing.T) {
	f, repo, k8s, _ := newTestService(t)
	k8s.noReadyPods = true
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

// firstMessageErrs makes the first sends fail with each error in turn, then succeed.
type firstMessageErrs struct {
	*mockSMClient
	errs []error
}

func (f *firstMessageErrs) SendMessage(ctx context.Context, pod sessionmanager.Pod, runID, content string, idle int, fromPerson bool, from *sessionmanager.From) error {
	f.mu.Lock()
	n := f.sendMessageCalls
	f.mu.Unlock()
	if n < len(f.errs) {
		f.mu.Lock()
		f.sendMessageCalls++
		f.mu.Unlock()
		return f.errs[n]
	}
	return f.mockSMClient.SendMessage(ctx, pod, runID, content, idle, fromPerson, from)
}

func TestStartClaimed_InitialMessageRetries(t *testing.T) {
	old := initialMessageBackoff
	initialMessageBackoff = time.Millisecond
	t.Cleanup(func() { initialMessageBackoff = old })
	boom := errors.New("pod unreachable")

	for _, tc := range []struct {
		name         string
		msg          string
		errs         []error
		cancel       bool
		wantState    RunState
		wantCalls    int
		wantClosed   int
		wantErrorLog bool
	}{
		{name: "fails once then succeeds", msg: "fix the build", errs: []error{boom},
			wantState: RunStateActive, wantCalls: 2},
		{name: "fails every time", msg: "fix the build", errs: []error{boom, boom, boom, boom},
			wantState: RunStateFailed, wantCalls: 3, wantClosed: 1, wantErrorLog: true},
		{name: "an empty message sends nothing", msg: "",
			wantState: RunStateActive},
		{name: "a canceled context ends the retries", msg: "fix the build", errs: []error{boom, boom, boom}, cancel: true,
			wantState: RunStateFailed, wantCalls: 1, wantClosed: 1, wantErrorLog: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, repo, _, sm := schedulerService(t)
			var logs bytes.Buffer
			f.run.logger = logging.NewLoggerWithOutput(&logs)
			f.run.sm = &firstMessageErrs{mockSMClient: sm, errs: tc.errs}
			run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1", InitialMessage: tc.msg})
			if err != nil {
				t.Fatalf("StartRun: %v", err)
			}
			ctx := context.Background()
			if tc.cancel {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			claimed, claimedAt := claimRun(repo, run.ID)
			f.run.startClaimed(ctx, claimed, claimedAt)

			if got := repo.runs[run.ID].State; got != tc.wantState {
				t.Errorf("state = %s, want %s", got, tc.wantState)
			}
			if sm.sendMessageCalls != tc.wantCalls || len(sm.closedSessions) != tc.wantClosed {
				t.Errorf("send calls = %d, closed = %d, want %d and %d", sm.sendMessageCalls, len(sm.closedSessions), tc.wantCalls, tc.wantClosed)
			}
			if tc.wantState == RunStateFailed {
				if errs := runErrors(t, repo, run.ID); len(errs) != 1 || errs[0]["code"] != "start_failed" {
					t.Errorf("run errors = %v, want one start_failed", errs)
				}
			}
			hasErrLog := false
			for line := range strings.SplitSeq(logs.String(), "\n") {
				if strings.Contains(line, `"level":"error"`) && strings.Contains(line, `"run_id":"`+run.ID+`"`) && strings.Contains(line, `"pod"`) {
					hasErrLog = true
				}
			}
			if hasErrLog != tc.wantErrorLog {
				t.Errorf("error log with run_id and pod = %v, want %v; logs: %s", hasErrLog, tc.wantErrorLog, logs.String())
			}
		})
	}
}

// The pod placement picked is the pod that grows, with the warm state from placement's pod list, before its
// session is created.
func TestStartClaimed_GrowsAssignedPod(t *testing.T) {
	f, repo, k8s, sm := schedulerService(t)
	k8s.readyPods = 2
	k8s.warm = []int{1}
	sm.memory = fullPods("bot", 0)
	w := &fakeWaker{}
	f.run.SetPodWaker(w)
	run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err != nil {
		t.Fatal(err)
	}
	claimed, claimedAt := claimRun(repo, run.ID)
	f.run.startClaimed(context.Background(), claimed, claimedAt)
	if !slices.Equal(w.woke, []string{"pod-1"}) || !slices.Equal(w.warm, []bool{true}) || len(sm.sessions) != 1 {
		t.Errorf("woke %v warm %v with %d sessions, want pod-1 woken as warm and one session", w.woke, w.warm, len(sm.sessions))
	}
}
