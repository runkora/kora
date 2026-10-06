package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// A 409 from orbit-agents is a refused environment choice that carries its message.
func TestHTTPNotifier_EnvironmentRejected(t *testing.T) {
	var gotQuery, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":"conflict","message":"environment is not attached to this agent: \"rust\""}`))
	}))
	defer srv.Close()
	n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))

	_, err := n.ProvisionSandbox(context.Background(), "run-1", true, "rust", false)
	if !errors.Is(err, ErrEnvironmentRejected) || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("err = %v, want ErrEnvironmentRejected with the message", err)
	}
	if gotQuery != "ahead=true" || gotBody != `{"environment":"rust","replace":false}` {
		t.Errorf("query %q body %s, want ahead=true and the environment in the body", gotQuery, gotBody)
	}

	_, err = n.ProvisionSandbox(context.Background(), "run-1", false, "node", true)
	if !errors.Is(err, ErrEnvironmentRejected) {
		t.Fatalf("replace err = %v, want ErrEnvironmentRejected", err)
	}
	if gotBody != `{"environment":"node","replace":true}` {
		t.Errorf("replace body = %s", gotBody)
	}
}

// envNotifier refuses every provision and answers a replacement with a sandbox at a dead address.
type envNotifier struct {
	NoopNotifier
	mu          sync.Mutex
	calls       int
	replaced    string
	runErrors   int
	environment string
	// replaceCtxErr is the replacement context's error when the call began.
	replaceCtxErr error
}

func (n *envNotifier) ProvisionSandbox(ctx context.Context, _ string, _ bool, env string, replace bool) (Sandbox, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if replace {
		n.replaced = env
		n.replaceCtxErr = ctx.Err()
		return Sandbox{Endpoint: "127.0.0.1:1", ExecToken: "tok-new"}, nil
	}
	n.calls++
	n.environment = env
	return Sandbox{}, fmt.Errorf("%w: not attached", ErrEnvironmentRejected)
}

func (n *envNotifier) Notify(_ context.Context, _ string, ev Event) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if ev.Type == EventTypeRunError {
		n.runErrors++
	}
	return nil
}

// A refused choice is not retried and does not lose the sandbox.
func TestProvisionSandbox_RefusedEnvironmentIsNotALoss(t *testing.T) {
	n := &envNotifier{}
	reg, _ := newTestRegistry(t, n)
	setGuardrails(reg, fastGuardrails())
	hostSession(reg, "run-1")

	_, err := reg.ProvisionSandbox(context.Background(), "run-1", false, "rust", false)
	if !errors.Is(err, ErrEnvironmentRejected) {
		t.Fatalf("err = %v, want ErrEnvironmentRejected", err)
	}
	if n.calls != 1 || n.environment != "rust" {
		t.Errorf("calls = %d with %q, want one call naming rust", n.calls, n.environment)
	}
	if n.runErrors != 0 || reg.sessions["run-1"].sandboxLost.Load() {
		t.Error("a refused environment marked the sandbox lost")
	}
}

// A replacement writes the new sandbox's descriptor for orbit-shell.
func TestReplaceEnvironment_WritesDescriptor(t *testing.T) {
	n := &envNotifier{}
	reg, ws := newTestRegistry(t, n)
	setGuardrails(reg, fastGuardrails())
	hostSession(reg, "run-1")
	if err := os.MkdirAll(filepath.Join(ws, "run-1"), 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := reg.ProvisionSandbox(context.Background(), "run-1", false, "node", true); err != nil {
		t.Fatalf("replace: %v", err)
	}
	if n.replaced != "node" {
		t.Errorf("replaced into %q, want node", n.replaced)
	}
	token, err := os.ReadFile(filepath.Join(ws, "run-1", ".sandbox", "exec-token"))
	if err != nil || string(token) != "tok-new" {
		t.Errorf("exec token = %q (%v), want the new sandbox's", token, err)
	}
}

// The provision route takes the environment from the body. An empty body keeps
// the current one.
func TestHandleProvisionSandbox_PassesEnvironment(t *testing.T) {
	n := &envNotifier{}
	c, ws := sandboxTestController(t)
	c.registry.notifier = n
	setGuardrails(c.registry, fastGuardrails())
	hostSession(c.registry, "run-1")
	tok, err := mintRunToken(ws, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	for body, want := range map[string]string{`{"environment":"go"}`: "go", "{}": ""} {
		req := httptest.NewRequest(http.MethodPost, "/internal/session/run-1/provision-sandbox", strings.NewReader(body))
		req.SetPathValue("runID", "run-1")
		req.Header.Set(runTokenHeader, tok)
		w := httptest.NewRecorder()
		c.handleProvisionSandbox(w, req)
		if w.Code != http.StatusConflict || n.environment != want {
			t.Errorf("body %q: status %d, environment %q; want 409 and %q", body, w.Code, n.environment, want)
		}
		var apiErr struct{ Message string }
		_ = json.Unmarshal(w.Body.Bytes(), &apiErr)
		if apiErr.Message != "not attached" {
			t.Errorf("message = %q, want orbit-agents' message alone", apiErr.Message)
		}
	}
}

// A non-JSON 409 and a 500 both keep the status and body in the error.
func TestHTTPNotifier_SandboxCallErrorBody(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
	}{{http.StatusConflict, "plain conflict"}, {http.StatusInternalServerError, "boom"}} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		}))
		n := NewHTTPNotifier(srv.URL, logging.NewLoggerWithOutput(io.Discard))
		_, err := n.ProvisionSandbox(context.Background(), "run-1", false, "", false)
		srv.Close()
		if errors.Is(err, ErrEnvironmentRejected) || err == nil || !strings.Contains(err.Error(), tc.body) || !strings.Contains(err.Error(), fmt.Sprint(tc.status)) {
			t.Errorf("status %d: err = %v, want the status and body", tc.status, err)
		}
	}
}

// A replacement outlives the agent's request like a provision does.
func TestReplaceEnvironment_OutlivesTheRequest(t *testing.T) {
	n := &envNotifier{}
	reg, _ := newTestRegistry(t, n)
	hostSession(reg, "run-1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _ = reg.ProvisionSandbox(ctx, "run-1", false, "node", true)
	if n.replaceCtxErr != nil {
		t.Errorf("replacement ran on the cancelled request context: %v", n.replaceCtxErr)
	}
}

// A replacement shares the provision retries and the sandbox-lost handling.
func TestReplaceEnvironment_RetriesThenLosesTheSandbox(t *testing.T) {
	n := &failingNotifier{failures: 100}
	reg, _ := newTestRegistry(t, n)
	setGuardrails(reg, fastGuardrails())
	hostSession(reg, "run-1")

	_, err := reg.ProvisionSandbox(context.Background(), "run-1", false, "node", true)
	if !errors.Is(err, ErrSandboxLost) {
		t.Fatalf("err = %v, want ErrSandboxLost", err)
	}
	if n.calls != fastGuardrails().SandboxProvisionAttempts {
		t.Errorf("calls = %d, want %d attempts", n.calls, fastGuardrails().SandboxProvisionAttempts)
	}
}
