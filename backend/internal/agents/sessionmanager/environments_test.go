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

// A 409 from orbit-agents is a refused environment choice, carrying its message.
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

	_, err := n.ProvisionSandbox(context.Background(), "run-1", false, "rust")
	if !errors.Is(err, ErrEnvironmentRejected) || !strings.Contains(err.Error(), "not attached") {
		t.Fatalf("err = %v, want ErrEnvironmentRejected with the message", err)
	}
	if gotQuery != "environment=rust" {
		t.Errorf("query = %q, want environment=rust", gotQuery)
	}

	_, err = n.ReplaceEnvironment(context.Background(), "run-1", "node")
	if !errors.Is(err, ErrEnvironmentRejected) {
		t.Fatalf("replace err = %v, want ErrEnvironmentRejected", err)
	}
	if gotBody != `{"environment":"node"}` {
		t.Errorf("replace body = %s", gotBody)
	}
}

// envNotifier refuses every provision and replaces into a dead address.
type envNotifier struct {
	NoopNotifier
	mu          sync.Mutex
	calls       int
	replaced    string
	runErrors   int
	environment string
}

func (n *envNotifier) ProvisionSandbox(_ context.Context, _ string, _ bool, env string) (Sandbox, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.calls++
	n.environment = env
	return Sandbox{}, fmt.Errorf("%w: not attached", ErrEnvironmentRejected)
}

func (n *envNotifier) ReplaceEnvironment(_ context.Context, _ string, env string) (Sandbox, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.replaced = env
	return Sandbox{Endpoint: "127.0.0.1:1", ExecToken: "tok-new"}, nil
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

	_, err := reg.ProvisionSandbox(context.Background(), "run-1", false, "rust")
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

	if _, err := reg.ReplaceEnvironment(context.Background(), "run-1", "node"); err != nil {
		t.Fatalf("ReplaceEnvironment: %v", err)
	}
	if n.replaced != "node" {
		t.Errorf("replaced into %q, want node", n.replaced)
	}
	token, err := os.ReadFile(filepath.Join(ws, "run-1", ".sandbox", "exec-token"))
	if err != nil || string(token) != "tok-new" {
		t.Errorf("exec token = %q (%v), want the new sandbox's", token, err)
	}
}

// The provision route takes the environment from the body, and an empty body
// keeps the current one.
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

	for body, want := range map[string]string{`{"environment":"go"}`: "go", "": ""} {
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
