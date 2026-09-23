package agents

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// GetSandbox reports what the run has without creating anything. The
// session-manager asks after a restart, when its own memory is empty.
func TestGetSandbox_ReturnsTheLiveSandbox(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.lookupSandbox = Sandbox{Endpoint: "10.0.0.5:50051", ExecToken: "exec-abc"}
	k8s.lookupFound = true

	got, err := svc.run.GetSandbox(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Endpoint != "10.0.0.5:50051" || got.ExecToken != "exec-abc" {
		t.Errorf("sandbox = %+v", got)
	}
	if k8s.provisionSandboxCalls != 0 {
		t.Errorf("GetSandbox provisioned a pod (%d calls); it must only read", k8s.provisionSandboxCalls)
	}
}

// A run that never asked for a sandbox is not an error. orbit-shell routes
// pre-sandbox work locally on this answer.
func TestGetSandbox_NoSandboxIsItsOwnError(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.lookupFound = false

	if _, err := svc.run.GetSandbox(context.Background(), "run-1"); !errors.Is(err, ErrNoSandbox) {
		t.Fatalf("error = %v, want ErrNoSandbox", err)
	}
}

// A lookup failure is not "no sandbox": reporting one would send the agent's
// command to a local shell because Kubernetes was briefly unreachable.
func TestGetSandbox_LookupErrorIsNotNoSandbox(t *testing.T) {
	svc, repo, k8s, _ := newTestService(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.lookupErr = errors.New("api server unreachable")

	_, err := svc.run.GetSandbox(context.Background(), "run-1")
	if err == nil {
		t.Fatal("a lookup failure returned no error")
	}
	if errors.Is(err, ErrNoSandbox) {
		t.Fatal("a lookup failure reported ErrNoSandbox, which routes the agent to a local shell")
	}
}

// The route hands the exec token only to a caller authorizeCallback has bound
// to this run, and 404 distinguishes a run with no sandbox.
func TestHandleGetRunSandbox(t *testing.T) {
	t.Run("returns the sandbox", func(t *testing.T) {
		c, repo, k8s, _ := newTestController(t)
		seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
		seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
		k8s.lookupSandbox = Sandbox{Endpoint: "10.0.0.5:50051", ExecToken: "exec-abc"}
		k8s.lookupFound = true

		w := httptest.NewRecorder()
		c.handleGetRunSandbox(w, callbackReq(http.MethodGet, "/internal/runs/run-1/sandbox", ""))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
		}
		if body := w.Body.String(); !contains(body, "exec-abc") {
			t.Errorf("response carried no exec token: %s", body)
		}
	})

	t.Run("no sandbox is 404", func(t *testing.T) {
		c, repo, k8s, _ := newTestController(t)
		seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
		seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
		k8s.lookupFound = false

		w := httptest.NewRecorder()
		c.handleGetRunSandbox(w, callbackReq(http.MethodGet, "/internal/runs/run-1/sandbox", ""))

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
