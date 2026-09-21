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

// Sandbox answers from the session map, which is what limits it to runs this
// pod hosts. The /internal routes authenticate the caller as loopback in this
// container and never as a particular run, so answering from the workspace
// would let a co-hosted run read a sibling's endpoint and exec token.
func TestSandbox_ForeignRunWithADescriptorIsRefused(t *testing.T) {
	workspaces := t.TempDir()
	reg := NewRegistry(discardLogger(), workspaces, NoopNotifier{}, time.Minute)
	writeDescriptor(t, workspaces, "run-sibling", "10.0.0.9:50051", "tok-sibling")

	if _, ok := reg.Sandbox("run-sibling"); ok {
		t.Fatal("Sandbox answered for a run this pod does not host; that hands a sibling run its exec token")
	}
}

// sandboxFromDisk backs ProvisionSandbox's cache check, which runs behind a
// hosts() membership check. Both halves or neither: an endpoint whose token is
// missing cannot authenticate to the sandbox.
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
