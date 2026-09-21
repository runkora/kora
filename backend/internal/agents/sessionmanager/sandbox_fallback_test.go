package sessionmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func discardLogger() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

// writeDescriptor lays down the on-disk sandbox descriptor the way
// ProvisionSandbox does.
func writeDescriptor(t *testing.T, workspaces, runID, endpoint, token string) {
	t.Helper()
	dir := filepath.Join(workspaces, runID, ".sandbox")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if endpoint != "" {
		if err := os.WriteFile(filepath.Join(dir, "endpoint"), []byte(endpoint), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if token != "" {
		if err := os.WriteFile(filepath.Join(dir, "exec-token"), []byte(token), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func sandboxTestController(t *testing.T) (*Controller, string) {
	t.Helper()
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute)
	c := &Controller{
		registry: reg,
		logger:   discardLogger(),
		cfg:      ControllerConfig{WorkspacesBasePath: workspaces, SelfPort: 8090},
	}
	return c, workspaces
}

func getSandboxReq(runID, runToken string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/internal/session/"+runID+"/sandbox", nil)
	r.SetPathValue("runID", runID)
	if runToken != "" {
		r.Header.Set(runTokenHeader, runToken)
	}
	return r
}

// A restart empties the session map. Without the workspace fallback that is
// indistinguishable from an agent deleting its own descriptor, and orbit-shell
// runs the agent's command locally in this container.
func TestGetSandbox_FallsBackToDiskAfterRestart(t *testing.T) {
	c, workspaces := sandboxTestController(t)
	tok, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	writeDescriptor(t, workspaces, "run-1", "10.0.0.5:50051", "exec-abc")

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-1", tok))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var body struct {
		Endpoint  string `json:"endpoint"`
		ExecToken string `json:"exec_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Endpoint != "10.0.0.5:50051" || body.ExecToken != "exec-abc" {
		t.Errorf("descriptor = %+v", body)
	}
}

// The workspace fallback is only safe because the caller has proved which run
// it is. Loopback is shared by every subprocess in the pod, so a co-hosted run
// presenting its own token must not receive a sibling's exec token.
func TestGetSandbox_SiblingRunCannotReadAnotherRunsDescriptor(t *testing.T) {
	c, workspaces := sandboxTestController(t)
	siblingToken, err := mintRunToken(workspaces, "run-sibling")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := mintRunToken(workspaces, "run-victim"); err != nil {
		t.Fatal(err)
	}
	writeDescriptor(t, workspaces, "run-victim", "10.0.0.9:50051", "exec-victim")

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-victim", siblingToken))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
	if body := w.Body.String(); body != "" && (len(body) > 0 && containsAny(body, "10.0.0.9", "exec-victim")) {
		t.Errorf("response leaked the victim's descriptor: %s", body)
	}
}

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
	}
	return false
}

// A caller with no token is refused. Presenting nothing must not fall through
// to the loopback-only check the run token replaced.
func TestGetSandbox_NoTokenIsRefused(t *testing.T) {
	c, workspaces := sandboxTestController(t)
	if _, err := mintRunToken(workspaces, "run-1"); err != nil {
		t.Fatal(err)
	}
	writeDescriptor(t, workspaces, "run-1", "10.0.0.5:50051", "exec-abc")

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-1", ""))

	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body: %s", w.Code, w.Body.String())
	}
}

// A run that never asked for a sandbox still reports none, which is what
// legitimately routes pre-sandbox work to a local shell.
func TestGetSandbox_NoDescriptorIsNotFound(t *testing.T) {
	c, workspaces := sandboxTestController(t)
	tok, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-1", tok))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// sandboxFromDisk backs both the fallback and ProvisionSandbox's cache check.
// Both halves or neither: an endpoint whose token is missing cannot
// authenticate to the sandbox.
func TestSandboxFromDisk_RequiresBothHalves(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute)

	writeDescriptor(t, workspaces, "both", "10.0.0.5:50051", "tok-abc")
	got, ok := reg.sandboxFromDisk("both")
	if !ok || got.Endpoint != "10.0.0.5:50051" || got.ExecToken != "tok-abc" {
		t.Errorf("complete descriptor = %+v ok=%v", got, ok)
	}

	writeDescriptor(t, workspaces, "endpoint-only", "10.0.0.5:50051", "")
	if _, ok := reg.sandboxFromDisk("endpoint-only"); ok {
		t.Error("an endpoint with no token read as a usable sandbox")
	}

	writeDescriptor(t, workspaces, "token-only", "", "tok-abc")
	if _, ok := reg.sandboxFromDisk("token-only"); ok {
		t.Error("a token with no endpoint read as a usable sandbox")
	}

	if _, ok := reg.sandboxFromDisk("absent"); ok {
		t.Error("a run with no descriptor read as a usable sandbox")
	}
}

// A run this pod does not host is a 404 once the caller has proved its
// identity: the refusal is the membership check, distinct from the identity
// check that precedes it and from an ordinary Kubernetes failure.
func TestHandleProvisionSandbox_UnhostedRunIsNotFound(t *testing.T) {
	c, workspaces := sandboxTestController(t)
	tok, err := mintRunToken(workspaces, "run-elsewhere")
	if err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/internal/session/run-elsewhere/provision-sandbox", nil)
	r.SetPathValue("runID", "run-elsewhere")
	r.Header.Set(runTokenHeader, tok)

	w := httptest.NewRecorder()
	c.handleProvisionSandbox(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body: %s", w.Code, w.Body.String())
	}
}

// A run's token is only its own, and a missing token file never verifies.
func TestVerifyRunToken(t *testing.T) {
	workspaces := t.TempDir()
	a, err := mintRunToken(workspaces, "run-a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := mintRunToken(workspaces, "run-b")
	if err != nil {
		t.Fatal(err)
	}

	if !verifyRunToken(workspaces, "run-a", a) {
		t.Error("a run's own token did not verify")
	}
	if verifyRunToken(workspaces, "run-b", a) {
		t.Error("run-a's token verified against run-b")
	}
	if verifyRunToken(workspaces, "run-a", b) {
		t.Error("run-b's token verified against run-a")
	}
	if verifyRunToken(workspaces, "run-never-started", a) {
		t.Error("a run with no stored token verified")
	}
	if verifyRunToken(workspaces, "run-a", "") {
		t.Error("an empty token verified")
	}
}
