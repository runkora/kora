package sessionmanager

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

func discardLogger() logging.Logger { return logging.NewLoggerWithOutput(io.Discard) }

// writeDescriptor lays down the on-disk sandbox descriptor the way
// ProvisionSandbox does.
func writeDescriptor(t *testing.T, workspaces, runID, endpoint, token string) {
	t.Helper()
	dir := filepath.Join(workspaces, runID, ".sandbox-exec")
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
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute, testBrand)
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

// lookupNotifier stands in for kora-agents, which holds the sandbox pod and so
// can answer after this pod's memory is gone.
type lookupNotifier struct {
	NoopNotifier
	sandbox Sandbox
	found   bool
	err     error
	calls   int
}

func (n *lookupNotifier) GetSandbox(_ context.Context, _ string) (Sandbox, bool, error) {
	n.calls++
	return n.sandbox, n.found, n.err
}

func controllerWithNotifier(t *testing.T, n Notifier) (*Controller, string) {
	t.Helper()
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, n, time.Minute, testBrand)
	c := &Controller{
		registry: reg,
		logger:   discardLogger(),
		cfg:      ControllerConfig{WorkspacesBasePath: workspaces, SelfPort: 8090},
	}
	return c, workspaces
}

// A restart empties the session map, and kora-shell only asks when its own
// workspace copy is missing. kora-agents holds the sandbox pod, so it is the
// one place that still knows.
func TestGetSandbox_AsksKoraAgentsAfterRestart(t *testing.T) {
	n := &lookupNotifier{sandbox: Sandbox{Endpoint: "10.0.0.5:50051", ExecToken: "exec-abc"}, found: true}
	c, workspaces := controllerWithNotifier(t, n)
	tok, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}

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
	if n.calls != 1 {
		t.Errorf("GetSandbox calls = %d, want 1", n.calls)
	}
}

// The regression that made the previous fallback useless: the workspace
// descriptor cannot be the answer, because it is the same file kora-shell has
// already failed to find. Only kora-agents decides.
func TestGetSandbox_WorkspaceDescriptorAloneDoesNotAnswer(t *testing.T) {
	n := &lookupNotifier{found: false}
	c, workspaces := controllerWithNotifier(t, n)
	tok, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	writeDescriptor(t, workspaces, "run-1", "10.0.0.5:50051", "exec-abc")

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-1", tok))

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; a descriptor on disk must not stand in for kora-agents. Body: %s", w.Code, w.Body.String())
	}
}

// A lookup that fails is not a run without a sandbox. 404 is what kora-shell
// reads as permission to run the command locally, so an unreachable
// kora-agents must stop the command rather than relax where it runs.
func TestGetSandbox_LookupFailureIsNotA404(t *testing.T) {
	n := &lookupNotifier{err: errors.New("kora-agents unreachable")}
	c, workspaces := controllerWithNotifier(t, n)
	tok, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, getSandboxReq("run-1", tok))

	if w.Code == http.StatusNotFound {
		t.Fatal("a failed lookup answered 404, which sends the agent's command to a local shell")
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503; body: %s", w.Code, w.Body.String())
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
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute, testBrand)

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

	r := httptest.NewRequest(http.MethodPost, "/internal/session/run-elsewhere/provision-sandbox", strings.NewReader("{}"))
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

// A second create request for a live run must not invalidate the token its
// subprocess already holds, which an overwrite would.
func TestMintRunToken_ReusesAnExistingToken(t *testing.T) {
	workspaces := t.TempDir()

	first, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := mintRunToken(workspaces, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("token changed on the second call: %q then %q", first, second)
	}
	if !verifyRunToken(workspaces, "run-1", first) {
		t.Error("the original token stopped verifying")
	}
}

// A request that cannot produce a session must not leave a token behind. An
// unsupported framework is rejected before anything is written for the run.
func TestCreateSession_UnsupportedFrameworkWritesNothing(t *testing.T) {
	c, workspaces := sandboxTestController(t)

	body := `{"run_id":"run-bad","agent_type_id":"at-1","tenant_id":"t-1","model":"m","framework":"nope"}`
	r := httptest.NewRequest(http.MethodPost, "/session", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	c.handleCreateSession(w, r)

	if w.Code != http.StatusBadRequest && w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want a 4xx; body: %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(workspaces, "run-bad")); !os.IsNotExist(err) {
		t.Errorf("a rejected request left %s behind (err=%v)", filepath.Join(workspaces, "run-bad"), err)
	}
}

// The run ID becomes a path segment when the token is read, and on the
// internal routes it arrives from the URL rather than a validated body.
func TestRunToken_RejectsTraversingRunIDs(t *testing.T) {
	workspaces := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspaces, "secret"), []byte("not-a-token"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, runID := range []string{"", ".", "..", "../secret", "a/b", "../../etc/passwd"} {
		if _, err := mintRunToken(workspaces, runID); err == nil {
			t.Errorf("mintRunToken(%q) succeeded", runID)
		}
		if verifyRunToken(workspaces, runID, "not-a-token") {
			t.Errorf("verifyRunToken(%q) verified", runID)
		}
	}
}

// The handler refuses an unusable run id before it reaches the token read.
func TestGetSandbox_RejectsTraversingRunID(t *testing.T) {
	c, _ := sandboxTestController(t)

	r := httptest.NewRequest(http.MethodGet, "/internal/session/x/sandbox", nil)
	r.SetPathValue("runID", "../../etc")
	r.Header.Set(runTokenHeader, "anything")

	w := httptest.NewRecorder()
	c.handleGetSandbox(w, r)

	if w.Code == http.StatusOK {
		t.Fatalf("status = 200 for a traversing run id; body: %s", w.Body.String())
	}
}

// The descriptor path is built from a run ID that arrives in the URL, so it
// gets the same treatment as the token path.
func TestSandboxFromDisk_RejectsTraversingRunIDs(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute, testBrand)
	writeDescriptor(t, workspaces, "victim", "10.0.0.9:50051", "exec-victim")

	for _, runID := range []string{"", ".", "..", "../victim", "a/b"} {
		if _, ok := reg.sandboxFromDisk(runID); ok {
			t.Errorf("sandboxFromDisk(%q) returned a descriptor", runID)
		}
	}
}
