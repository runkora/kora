package agents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/events"
	"github.com/runkora/kora/backend/internal/platform/token"
)

// An idle run an earlier release placed on an agent StatefulSet pod ends at the cutover with a run_error that
// gives the reason. An active run there finishes within the drain, and a run on a pool pod and an ended run
// are left alone.
func TestEndStatefulSetRuns(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedStatefulSetRun(t, repo, Run{ID: "idle-old", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateIdle})
	seedStatefulSetRun(t, repo, Run{ID: "active-old", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	seedStatefulSetRun(t, repo, Run{ID: "done-old", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateCompleted})
	seedRun(t, repo, Run{ID: "pooled", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	svc.run.EndStatefulSetRuns(context.Background())

	for id, want := range map[string]RunState{"idle-old": RunStateFailed, "active-old": RunStateActive, "done-old": RunStateCompleted, "pooled": RunStateActive} {
		if got := repo.runs[id].State; got != want {
			t.Errorf("run %s state = %s, want %s", id, got, want)
		}
	}
	for _, id := range []string{"idle-old"} {
		found := false
		for _, e := range repo.events[id] {
			found = found || (e.EventType == "run_error" && strings.Contains(string(e.Payload), statefulSetRunReason) && strings.Contains(string(e.Payload), events.RunErrorRunnerError))
		}
		if !found {
			t.Errorf("run %s has no run_error with the reason: %+v", id, repo.events[id])
		}
	}
}

// A message or an interrupt to a run with no pod name is refused as not accepting messages, with no
// session-manager call. Terminate ends it with no session to close.
func TestRunWithNoPodName(t *testing.T) {
	svc, repo, _, sm := newTestService(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedStatefulSetRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	ctx := withPrincipalCtx(context.Background(), "t-1", token.PrincipalTypeHuman, "user-1")
	if err := svc.run.SendMessage(ctx, "run-1", SendMessageRequest{Content: "hi"}); !errors.Is(err, ErrRunNotAcceptingMessages) {
		t.Errorf("SendMessage = %v, want ErrRunNotAcceptingMessages", err)
	}
	if err := svc.run.InterruptRun(ctx, "run-1"); !errors.Is(err, ErrRunNotAcceptingMessages) {
		t.Errorf("InterruptRun = %v, want ErrRunNotAcceptingMessages", err)
	}
	if err := svc.TerminateRun(context.Background(), "run-1"); err != nil {
		t.Errorf("TerminateRun = %v, want nil", err)
	}
	if len(sm.sentMessages) != 0 || len(sm.interruptedRuns) != 0 || len(sm.closedSessions) != 0 {
		t.Errorf("session-manager calls: messages %v, interrupts %v, closes %v; want none", sm.sentMessages, sm.interruptedRuns, sm.closedSessions)
	}
}

// The reaper ticker sweeps again, so an idle run an earlier replica places after the startup sweep also ends.
func TestEndStatefulSetRunsOnTheReaperTicker(t *testing.T) {
	svc, repo, _, _ := newTestService(t)
	svc.run.cfg.ReaperInterval = 10 * time.Millisecond
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	svc.run.EndStatefulSetRuns(context.Background())
	seedStatefulSetRun(t, repo, Run{ID: "late", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateIdle})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		svc.run.StartReaper(ctx)
		close(done)
	}()
	waitCached(t, func() bool {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		return repo.runs["late"].State == RunStateFailed
	})
	cancel()
	<-done
}
