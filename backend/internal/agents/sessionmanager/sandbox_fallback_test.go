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
// ProvisionSandbox does, so the fallback reads a realistic workspace.
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

// A session-manager restart empties the in-memory session map. Without the disk
// fallback that is indistinguishable from an agent deleting its own descriptor,
// and orbit-shell is told the run has no sandbox, which sends the agent's
// command to a local shell.
func TestSandbox_FallsBackToDiskAfterRestart(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute)
	writeDescriptor(t, workspaces, "run-1", "10.0.0.5:50051", "tok-abc")

	// No session registered: exactly the state after a restart.
	sandbox, ok := reg.Sandbox("run-1")
	if !ok {
		t.Fatal("Sandbox reported no sandbox; a restart must not look like the agent deleting its descriptor")
	}
	if sandbox.Endpoint != "10.0.0.5:50051" || sandbox.ExecToken != "tok-abc" {
		t.Errorf("descriptor = %+v, want endpoint 10.0.0.5:50051 and token tok-abc", sandbox)
	}
}

// The fallback only ever adds an answer. A run that never had a sandbox still
// reports none, which is what legitimately routes pre-sandbox work locally.
func TestSandbox_NoDescriptorStillReportsNone(t *testing.T) {
	reg := NewRegistry(discardLogger(), t.TempDir(), NoopNotifier{}, time.Minute)
	if _, ok := reg.Sandbox("run-absent"); ok {
		t.Error("Sandbox reported a sandbox for a run that has no descriptor")
	}
}

// Half a descriptor is unusable: an endpoint with no token cannot authenticate,
// so it must not read as a sandbox.
func TestSandbox_HalfWrittenDescriptorReportsNone(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute)

	writeDescriptor(t, workspaces, "endpoint-only", "10.0.0.5:50051", "")
	if _, ok := reg.Sandbox("endpoint-only"); ok {
		t.Error("an endpoint with no token read as a usable sandbox")
	}

	writeDescriptor(t, workspaces, "token-only", "", "tok-abc")
	if _, ok := reg.Sandbox("token-only"); ok {
		t.Error("a token with no endpoint read as a usable sandbox")
	}
}

// A caller aimed at a run this pod does not host is a 404, not a 500: the
// refusal is the run-membership check working, and burying it among Kubernetes
// failures loses the signal.
func TestHandleProvisionSandbox_UnhostedRunIsNotFound(t *testing.T) {
	reg := NewRegistry(discardLogger(), t.TempDir(), NoopNotifier{}, time.Minute)
	c := &Controller{registry: reg, logger: discardLogger()}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /internal/session/{runID}/provision-sandbox", c.handleProvisionSandbox)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/internal/session/run-elsewhere/provision-sandbox", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d. Body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
}
