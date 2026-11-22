package agents

import (
	"context"
	"errors"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/token"
)

var errPodGone = errors.New("pods \"bot-0\" not found")

// A run whose pod IP cannot be read is released to try again.
func TestStartClaimed_PodTargetFailureReleasesTheRun(t *testing.T) {
	f, _, k8s, sm := schedulerService(t)
	k8s.podIPErr = errPodGone
	run, err := startAndSchedule(startCtx(), f.run, StartRunRequest{AgentTypeID: "at-1"})
	if err != nil {
		t.Fatalf("startAndSchedule: %v", err)
	}
	if run.State != RunStatePending || len(sm.closedSessions) != 0 {
		t.Errorf("state = %s, closed = %d, want pending and 0", run.State, len(sm.closedSessions))
	}
}

// Terminate ends the run when its pod is gone.
func TestTerminateRun_PodGone(t *testing.T) {
	svc, repo, k8s, sm := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.podIPErr = errPodGone
	if err := svc.TerminateRun(context.Background(), run.ID); err != nil {
		t.Fatalf("TerminateRun: %v", err)
	}
	if len(sm.closedSessions) != 0 {
		t.Errorf("closed sessions = %d, want 0", len(sm.closedSessions))
	}
	assertTerminated(t, repo, run.ID)
}

// A message or an interrupt to a run whose pod IP cannot be read fails.
func TestSendAndInterrupt_PodTargetFailure(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRuns: 5})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.podIPErr = errPodGone
	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")
	if err := svc.run.SendMessage(ctx, "run-1", SendMessageRequest{Content: "hi"}); !errors.Is(err, errPodGone) {
		t.Errorf("SendMessage = %v, want the pod error", err)
	}
	if err := svc.run.InterruptRun(ctx, "run-1"); !errors.Is(err, errPodGone) {
		t.Errorf("InterruptRun = %v, want the pod error", err)
	}
}
