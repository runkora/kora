package agents

import (
	"context"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/clients/sessionmanager"
	"github.com/clear-route/orbit/internal/platform/token"
)

func startWaitService(t *testing.T) (*testServiceFacade, *mockK8s, *mockSMClient) {
	t.Helper()
	f, repo, k8s, sm := newTestService(t)
	f.run.cfg.StartBudget = 300 * time.Millisecond
	f.run.cfg.StartPollInterval = 5 * time.Millisecond
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t1", Slug: "bot", MaxConcurrentRunsPerPod: 5})
	return f, k8s, sm
}

func startCtx() context.Context {
	return withPrincipalCtx(context.Background(), "t1", token.PrincipalTypeHuman, "user:1")
}

// A run started while no pod is Ready begins once one is.
func TestStartRun_WaitsForReadyPod(t *testing.T) {
	f, k8s, sm := startWaitService(t)
	k8s.readyOrdinalsSeq = [][]int{nil, nil, {0}}

	if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if k8s.readyCalls < 3 || len(sm.sessions) != 1 {
		t.Errorf("readyCalls=%d sessions=%d, want >=3 and 1", k8s.readyCalls, len(sm.sessions))
	}
}

// A pod that never turns Ready fails the start with a reason naming the cause.
func TestStartRun_PodNeverReady_FailsWithReason(t *testing.T) {
	f, k8s, sm := startWaitService(t)
	k8s.readyOrdinalsSeq = [][]int{nil}

	_, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err == nil || !strings.Contains(err.Error(), "agent pod not ready after") {
		t.Fatalf("err = %v, want agent pod not ready", err)
	}
	if len(sm.sessions) != 0 {
		t.Errorf("sessions = %d, want 0", len(sm.sessions))
	}
}

type scriptedSM struct {
	*mockSMClient
	errs  []error
	calls int
}

func (s *scriptedSM) CreateSession(ctx context.Context, url string, req sessionmanager.CreateSessionRequest) error {
	s.calls++
	if s.calls <= len(s.errs) {
		return s.errs[s.calls-1]
	}
	return s.mockSMClient.CreateSession(ctx, url, req)
}

// Transport errors and 503s are retried until the pod answers.
func TestStartRun_RetriesSessionOnTransportErrors(t *testing.T) {
	f, _, sm := startWaitService(t)
	script := &scriptedSM{mockSMClient: sm, errs: []error{
		&net.DNSError{Err: "no such host", Name: "bot-0.bot", IsNotFound: true},
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		&sessionmanager.StatusError{StatusCode: 503, Err: fmt.Errorf("%w: status 503", sessionmanager.ErrUnexpectedStatus)},
	}}
	f.run.sm = script

	if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if script.calls != 4 {
		t.Errorf("CreateSession calls = %d, want 4", script.calls)
	}
}

// A rejection by the pod is not retried, even when its body mentions 503.
func TestStartRun_DoesNotRetryRejectedSession(t *testing.T) {
	f, _, sm := startWaitService(t)
	script := &scriptedSM{mockSMClient: sm, errs: []error{
		&sessionmanager.StatusError{StatusCode: 400, Err: fmt.Errorf("%w: status 400: status 503", sessionmanager.ErrUnexpectedStatus)},
	}}
	f.run.sm = script

	if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err == nil {
		t.Fatal("want error")
	}
	if script.calls != 1 {
		t.Errorf("CreateSession calls = %d, want 1", script.calls)
	}
}

// A conflict on a retry means an earlier attempt landed.
func TestStartRun_ConflictOnRetryIsSuccess(t *testing.T) {
	f, _, sm := startWaitService(t)
	script := &scriptedSM{mockSMClient: sm, errs: []error{
		&net.DNSError{Err: "no such host", Name: "bot-0.bot", IsNotFound: true},
		fmt.Errorf("%w: status 409", sessionmanager.ErrSessionConflict),
	}}
	f.run.sm = script

	run, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.State != RunStateActive {
		t.Errorf("state = %s, want active", run.State)
	}
}

// A conflict on the first attempt is a real duplicate and fails.
func TestStartRun_ConflictOnFirstAttemptFails(t *testing.T) {
	f, _, sm := startWaitService(t)
	f.run.sm = &scriptedSM{mockSMClient: sm, errs: []error{sessionmanager.ErrSessionConflict}}

	if _, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"}); err == nil {
		t.Fatal("want error")
	}
}

// The session retries stop at the budget and the error names the cause.
func TestStartRun_SessionRetryBudgetExpires(t *testing.T) {
	f, _, sm := startWaitService(t)
	errs := make([]error, 1000)
	for i := range errs {
		errs[i] = &net.DNSError{Err: "no such host", Name: "bot-0.bot", IsNotFound: true}
	}
	f.run.sm = &scriptedSM{mockSMClient: sm, errs: errs}

	_, err := f.run.StartRun(startCtx(), StartRunRequest{AgentTypeID: "at-1"})
	if err == nil || !strings.Contains(err.Error(), "agent pod not ready after") || !strings.Contains(err.Error(), "no such host") {
		t.Fatalf("err = %v", err)
	}
}
