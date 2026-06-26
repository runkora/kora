package sessionmanager

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func postSessionRaw(t *testing.T, ctrl *Controller, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/session", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	ctrl.handleCreateSession(rec, req)
	return rec
}

func newBareController(t *testing.T, cfg ControllerConfig) *Controller {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	// nil registry — these tests never reach registry.Create, so it's safe.
	return &Controller{logger: logger, cfg: cfg}
}

// ── unsupported framework error code ──────────────────────────────────────────

func TestHandleCreateSession_UnsupportedFrameworkReturnsInvalidField(t *testing.T) {
	ctrl := newBareController(t, ControllerConfig{})

	rec := postSessionRaw(t, ctrl, map[string]any{
		"run_id":        "run-bad-1",
		"agent_type_id": "at-1",
		"tenant_id":     "tenant-1",
		"framework":     "unknown-framework",
	})

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid_field") {
		t.Errorf("body = %q, want error code 'invalid_field'", rec.Body.String())
	}
}

// ── InferenceURL per-run path unit tests ──────────────────────────────────────
//
// The per-run URL is constructed inside handleCreateSession then stored in
// SessionConfig.InferenceURL. We verify the formula by inspecting the URL
// that a minimal stub runner would receive — but since registry.Create calls
// runner.Start which needs a real binary, we instead test the URL formula
// directly via the helper that server.go already uses: fmt.Sprintf.

func TestCodexInferenceURLFormula(t *testing.T) {
	tests := []struct {
		base  string
		runID string
		want  string
	}{
		{"http://gateway.test", "run-abc", "http://gateway.test/run/run-abc"},
		{"http://gateway.test/", "run-xyz", "http://gateway.test/run/run-xyz"},
		{"http://gw.svc.cluster.local:8081", "run-1", "http://gw.svc.cluster.local:8081/run/run-1"},
	}
	for _, tc := range tests {
		got := inferenceURLForRun(tc.base, tc.runID)
		if got != tc.want {
			t.Errorf("inferenceURLForRun(%q, %q) = %q, want %q", tc.base, tc.runID, got, tc.want)
		}
	}
}
