package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/policy"
)

// flushRecorder wraps httptest.ResponseRecorder and implements http.Flusher,
// which is required by the SSE handler.
type flushRecorder struct {
	*httptest.ResponseRecorder
}

func (fr *flushRecorder) Flush() {}

func newTestController(t *testing.T) (*Controller, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	return newTestControllerWithEval(t, &allowAllEvaluator{})
}

func newTestControllerWithEval(t *testing.T, eval policy.Evaluator) (*Controller, *mockRepository, *mockK8s, *mockSMClient) {
	t.Helper()
	facade, repo, k8s, sm := newTestServiceWithEval(t, eval)
	// Verifier is nil and routes is empty: handler tests invoke handlers
	// directly without going through Middleware(), so JWT verification and
	// scope enforcement are never reached. Tests that exercise tenant scoping
	// inject claims into the request context manually.
	c := NewController(facade.at, facade.run, nil, nil, nil, nil, eval, nil, "", logging.NewLoggerWithOutput(io.Discard))
	return c, repo, k8s, sm
}

// defaultTestTenant is the tenant ID injected into every test request's
// claims unless a test overrides it via withRequestTenant. The handler tests
// were written before the auth middleware existed and use "t-1" everywhere.
const defaultTestTenant = "t-1"

// withTenantRequest decorates a request with verified PlatformClaims for the
// given tenant, mirroring what AuthMiddleware would store in a live request.
func withTenantRequest(req *http.Request, tenantID string) *http.Request {
	return req.WithContext(withTenantCtx(req.Context(), tenantID))
}

func postJSON(t *testing.T, handler http.HandlerFunc, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func getRequest(t *testing.T, handler http.HandlerFunc, queryParams ...string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/"
	if len(queryParams) > 0 {
		url += "?" + queryParams[0]
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func deleteRequest(t *testing.T, handler http.HandlerFunc, id string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, "/"+id, nil)
	req.SetPathValue("id", id)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// ── Health ────────────────────────────────────────────────────────────────────

func TestHandleHealth(t *testing.T) {
	c, _, _, _ := newTestController(t)
	w := getRequest(t, c.handleHealth)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

// ── Agent Types ───────────────────────────────────────────────────────────────

func TestHandleCreateAgentType_Created(t *testing.T) {
	c, _, _, _ := newTestController(t)

	w := postJSON(t, c.handleCreateAgentType, map[string]any{
		"tenant_id": "t-1",
		"name":      "My Bot",
		"slug":      "my-bot",
		"model_id":  "anthropic/claude-sonnet-5",
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleCreateAgentType_MissingFields(t *testing.T) {
	c, _, _, _ := newTestController(t)

	// tenant_id comes from the verified platform claims, not the body; name,
	// slug and model_id are required.
	cases := []map[string]any{
		{"slug": "bot", "model_id": "anthropic/claude-sonnet-5"}, // missing name
		{"name": "Bot", "model_id": "anthropic/claude-sonnet-5"}, // missing slug
		{"name": "Bot", "slug": "bot"},                           // missing model_id
	}
	for _, body := range cases {
		w := postJSON(t, c.handleCreateAgentType, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%v: status = %d, want 400", body, w.Code)
		}
	}
}

func TestHandleCreateAgentType_SlugConflict(t *testing.T) {
	c, _, _, _ := newTestController(t)

	body := map[string]any{"tenant_id": "t-1", "name": "Bot", "slug": "dup", "model_id": "anthropic/claude-sonnet-5"}
	postJSON(t, c.handleCreateAgentType, body)
	w := postJSON(t, c.handleCreateAgentType, body)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandleCreateAgentType_K8sError(t *testing.T) {
	// Provisioning is async: the HTTP handler returns 201 immediately even if
	// K8s provisioning will fail in the background goroutine.
	c, _, k8s, _ := newTestController(t)
	k8s.provisionAgentTypeErr = errors.New("k8s down")

	w := postJSON(t, c.handleCreateAgentType, map[string]any{
		"tenant_id": "t-1", "name": "Bot", "slug": "bot", "model_id": "anthropic/claude-sonnet-5",
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201", w.Code)
	}
}

func TestHandleListAgentTypes_OK(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	w := getRequest(t, c.handleListAgentTypes, "tenant_id=t-1")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	types, _ := resp["agent_types"].([]any)
	if len(types) != 1 {
		t.Errorf("expected 1 agent type, got %d", len(types))
	}
}

func TestHandleListAgentTypes_NoClaims_Returns200WithEmptyList(t *testing.T) {
	// tenant_id is no longer read from the query string — the handler derives it
	// from verified claims. Without claims, tenantFromContext returns "" and the
	// repo finds no matching agent types. The handler returns 200 with [].
	// Live traffic cannot reach this path: AuthMiddleware rejects requests
	// lacking a valid token with 401 before the handler executes.
	c, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c.handleListAgentTypes(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleGetAgentType_Found(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-types/at-1", nil)
	req.SetPathValue("id", at.ID)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleGetAgentType(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleGetAgentType_NotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-types/missing", nil)
	req.SetPathValue("id", "missing")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleGetAgentType(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleDeleteAgentType_NoContent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})

	w := deleteRequest(t, c.handleDeleteAgentType, at.ID)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestHandleDeleteAgentType_NotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)
	w := deleteRequest(t, c.handleDeleteAgentType, "missing")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Runs ──────────────────────────────────────────────────────────────────────

func TestHandleStartRun_Created(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot", MaxConcurrentRunsPerPod: 5})

	w := postJSON(t, c.handleStartRun, map[string]any{
		"tenant_id":     "t-1",
		"agent_type_id": at.ID,
	})
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want 201; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleStartRun_MissingFields(t *testing.T) {
	c, _, _, _ := newTestController(t)

	// tenant_id no longer comes from the request body — only agent_type_id is
	// a client-controlled required field.
	cases := []map[string]any{
		{}, // missing agent_type_id
	}
	for _, body := range cases {
		w := postJSON(t, c.handleStartRun, body)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body=%v: status = %d, want 400", body, w.Code)
		}
	}
}

func TestHandleStartRun_AgentTypeNotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)

	w := postJSON(t, c.handleStartRun, map[string]any{
		"tenant_id":     "t-1",
		"agent_type_id": "nonexistent",
	})
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleListRuns_OK(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	w := getRequest(t, c.handleListRuns, "tenant_id=t-1")
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	runs, _ := resp["runs"].([]any)
	if len(runs) != 1 {
		t.Errorf("expected 1 run, got %d", len(runs))
	}
}

func TestHandleListRuns_NoClaims_Returns200WithEmptyList(t *testing.T) {
	// Same shape as TestHandleListAgentTypes_NoClaims_Returns200WithEmptyList:
	// without verified claims tenantFromContext returns "" and the repo
	// matches nothing. AuthMiddleware ensures unauthenticated traffic never
	// reaches this handler.
	c, _, _, _ := newTestController(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	c.handleListRuns(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleGetRun_Found(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-1", nil)
	req.SetPathValue("id", run.ID)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleGetRun(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
}

func TestHandleGetRun_NotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/missing", nil)
	req.SetPathValue("id", "missing")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleGetRun(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleTerminateRun_NoContent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	w := deleteRequest(t, c.handleTerminateRun, run.ID)
	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
}

func TestHandleTerminateRun_NotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)
	w := deleteRequest(t, c.handleTerminateRun, "missing")
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleSendMessage_Accepted(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/messages",
		bytes.NewBufferString(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleSendMessage(w, req)

	if w.Code != http.StatusAccepted {
		t.Errorf("status = %d, want 202; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleSendMessage_RunStarting(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	run := seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStatePending})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/messages",
		bytes.NewBufferString(`{"content":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", run.ID)
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleSendMessage(w, req)

	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "run is starting") || !strings.Contains(w.Body.String(), `"code":"conflict"`) {
		t.Errorf("status = %d, body = %s, want 409 run is starting", w.Code, w.Body.String())
	}
}

func TestHandleSendMessage_EmptyContent(t *testing.T) {
	c, _, _, _ := newTestController(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/run-1/messages",
		bytes.NewBufferString(`{"content":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "run-1")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleSendMessage(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleSendMessage_RunNotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/missing/messages",
		bytes.NewBufferString(`{"content":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "missing")
	req = withTenantRequest(req, defaultTestTenant)
	w := httptest.NewRecorder()
	c.handleSendMessage(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Internal callbacks ────────────────────────────────────────────────────────

// seedCallbackRun seeds an agent type (slug "bot") and a run owned by it, then
// returns a request decorated with matching AGENT claims for tenant "t-1".
// Mirrors a legitimate session-manager callback for its own run.
func callbackReq(method, target, body string) *http.Request {
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, target, http.NoBody)
	} else {
		req = httptest.NewRequest(method, target, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.SetPathValue("id", "run-1")
	return req.WithContext(withAgentCtx(req.Context(), "t-1", "bot"))
}

func TestHandleRunStateCallback_NoContent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateStarting})

	w := httptest.NewRecorder()
	c.handleRunStateCallback(w, callbackReq(http.MethodPost, "/internal/runs/run-1/state", `{"from":"starting","to":"active"}`))

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleRunStateCallback_Conflict(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	// Wrong `from` state — triggers CAS conflict.
	w := httptest.NewRecorder()
	c.handleRunStateCallback(w, callbackReq(http.MethodPost, "/internal/runs/run-1/state", `{"from":"pending","to":"active"}`))

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandlePushEvent_NoContent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	w := httptest.NewRecorder()
	c.handlePushEvent(w, callbackReq(http.MethodPost, "/internal/runs/run-1/events", `{"type":"text_delta","payload":{"delta":"hi"}}`))

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleSetRunSummary_NoContent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	w := httptest.NewRecorder()
	c.handleSetRunSummary(w, callbackReq(http.MethodPost, "/internal/runs/run-1/summary", `{"summary":"Audit the repo for exposed secrets"}`))

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
	got, err := repo.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Summary == nil || *got.Summary != "Audit the repo for exposed secrets" {
		t.Errorf("summary = %v, want the persisted label", got.Summary)
	}
}

func TestHandleSetRunSummary_SetOnce(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	// First summary wins; a second callback (e.g. after a session resume) must not overwrite it.
	c.handleSetRunSummary(httptest.NewRecorder(), callbackReq(http.MethodPost, "/internal/runs/run-1/summary", `{"summary":"First label"}`))
	c.handleSetRunSummary(httptest.NewRecorder(), callbackReq(http.MethodPost, "/internal/runs/run-1/summary", `{"summary":"Second label"}`))

	got, err := repo.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Summary == nil || *got.Summary != "First label" {
		t.Errorf("summary = %v, want the first label to win (set-once)", got.Summary)
	}
}

func TestHandleSetRunSummary_MissingRunNotFound(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	// No run seeded: authorizeCallback loads the run to verify ownership, so a
	// missing run is rejected at the gate with 404 — consistent with the other
	// internal callbacks — before the summary write is ever reached.

	w := httptest.NewRecorder()
	c.handleSetRunSummary(w, callbackReq(http.MethodPost, "/internal/runs/run-1/summary", `{"summary":"task"}`))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a missing run; body: %s", w.Code, w.Body.String())
	}
}

func TestHandleSetRunSummary_EmptyIsNoop(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	w := httptest.NewRecorder()
	c.handleSetRunSummary(w, callbackReq(http.MethodPost, "/internal/runs/run-1/summary", `{"summary":""}`))

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204; body: %s", w.Code, w.Body.String())
	}
	got, err := repo.GetRun(context.Background(), "run-1")
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if got.Summary != nil {
		t.Errorf("summary = %v, want nil for an empty payload", *got.Summary)
	}
}

func TestHandleProvisionSandbox_OK(t *testing.T) {
	c, repo, k8s, _ := newTestController(t)
	at := seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: at.ID, State: RunStateActive})
	k8s.sandboxEndpoint = "10.0.0.5:50051"

	w := httptest.NewRecorder()
	c.handleProvisionSandbox(w, callbackReq(http.MethodPost, "/internal/runs/run-1/provision-sandbox", ""))

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "10.0.0.5:50051") {
		t.Errorf("endpoint not in response: %s", w.Body.String())
	}
}

func TestHandleProvisionSandbox_NotFound(t *testing.T) {
	c, _, _, _ := newTestController(t)

	// Valid agent claims, but the run does not exist → 404 (existence not leaked).
	req := httptest.NewRequest(http.MethodPost, "/internal/runs/missing/provision-sandbox", http.NoBody)
	req.SetPathValue("id", "missing")
	req = req.WithContext(withAgentCtx(req.Context(), "t-1", "bot"))
	w := httptest.NewRecorder()
	c.handleProvisionSandbox(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

// ── Internal callback authorization (docs/adr/2026-06-04-orbit-agents-authorization-model.md) ─────────────────────────────────

// internalCallbackDenied asserts every internal callback handler rejects the
// given request context with 403. ctxFn decorates the request context with the
// (mis-matched or missing) claims under test.
func TestInternalCallbacks_RejectUnauthorizedAgents(t *testing.T) {
	cases := []struct {
		name  string
		ctxFn func(ctx context.Context) context.Context
	}{
		{"no claims", func(ctx context.Context) context.Context { return ctx }},
		{"non-agent principal", func(ctx context.Context) context.Context { return withTenantCtx(ctx, "t-1") }},
		{"wrong tenant", func(ctx context.Context) context.Context { return withAgentCtx(ctx, "t-2", "bot") }},
		{"wrong agent slug", func(ctx context.Context) context.Context { return withAgentCtx(ctx, "t-1", "other") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, repo, _, _ := newTestController(t)
			seedAgentType(t, repo, AgentType{ID: "at-1", TenantID: "t-1", Slug: "bot"})
			seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

			req := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/events",
				bytes.NewBufferString(`{"type":"text_delta","payload":{"delta":"x"}}`))
			req.Header.Set("Content-Type", "application/json")
			req.SetPathValue("id", "run-1")
			req = req.WithContext(tc.ctxFn(req.Context()))
			w := httptest.NewRecorder()
			c.handlePushEvent(w, req)

			if w.Code != http.StatusForbidden {
				t.Errorf("push event: status = %d, want 403; body: %s", w.Code, w.Body.String())
			}

			// The summary callback must enforce the same agent authorization.
			sumReq := httptest.NewRequest(http.MethodPost, "/internal/runs/run-1/summary",
				bytes.NewBufferString(`{"summary":"x"}`))
			sumReq.Header.Set("Content-Type", "application/json")
			sumReq.SetPathValue("id", "run-1")
			sumReq = sumReq.WithContext(tc.ctxFn(sumReq.Context()))
			sumW := httptest.NewRecorder()
			c.handleSetRunSummary(sumW, sumReq)

			if sumW.Code != http.StatusForbidden {
				t.Errorf("set summary: status = %d, want 403; body: %s", sumW.Code, sumW.Body.String())
			}
		})
	}
}

// ── SSE stream ────────────────────────────────────────────────────────────────

func TestHandleStreamEvents_ReplaysPersisted(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRun(t, repo, Run{ID: "run-1", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})
	repo.AppendRunEvent(context.Background(), RunEvent{ //nolint:errcheck
		RunID:     "run-1",
		EventType: "message_complete",
		Payload:   []byte(`{"text":"hello world"}`),
	})

	ctx, cancel := context.WithTimeout(withTenantCtx(context.Background(), defaultTestTenant), 100*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-1/events", nil).WithContext(ctx)
	req.SetPathValue("id", "run-1")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c.handleStreamEvents(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "message_complete") {
		t.Errorf("expected message_complete in SSE replay, got: %s", body)
	}
	if !strings.Contains(body, "hello world") {
		t.Errorf("expected payload in SSE replay, got: %s", body)
	}
}

func TestHandleStreamEvents_DeliversPushedEvent(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRun(t, repo, Run{ID: "run-2", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	ctx, cancel := context.WithTimeout(withTenantCtx(context.Background(), defaultTestTenant), 200*time.Millisecond)
	defer cancel()

	// Publish an event shortly after the handler connects.
	go func() {
		time.Sleep(10 * time.Millisecond)
		c.runs.PublishEvent(context.Background(), "run-2", PushedEvent{
			Type:    "text_delta",
			Payload: []byte(`{"delta":"hi"}`),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-2/events", nil).WithContext(ctx)
	req.SetPathValue("id", "run-2")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c.handleStreamEvents(w, req)

	body := w.Body.String()
	if !strings.Contains(body, "text_delta") {
		t.Errorf("expected text_delta in pushed event, got: %s", body)
	}
}

// TestHandleStreamEvents_DeduplicatesDurableEventFromLiveChannel proves Fix 3:
// a durable event (SequenceNo > 0) that arrives on the pubsub channel while the
// DB replay is in progress must not be emitted a second time.
func TestHandleStreamEvents_DeduplicatesDurableEventFromLiveChannel(t *testing.T) {
	c, repo, _, _ := newTestController(t)
	seedRun(t, repo, Run{ID: "run-dedup", TenantID: "t-1", AgentTypeID: "at-1", State: RunStateActive})

	// Append a durable event — the mock assigns it SequenceNo=1.
	repo.AppendRunEvent(context.Background(), RunEvent{ //nolint:errcheck
		RunID:     "run-dedup",
		EventType: "message_complete",
		Payload:   []byte(`{"text":"hello"}`),
	})

	ctx, cancel := context.WithTimeout(withTenantCtx(context.Background(), defaultTestTenant), 200*time.Millisecond)
	defer cancel()

	// Simulate the race: the event is fanned out to this subscriber while the DB
	// replay that already covered it is still in flight. Publishing straight to
	// the hub is the write PublishEvent has already done by the time it fans out.
	// Without the dedup guard this would write message_complete twice.
	go func() {
		time.Sleep(5 * time.Millisecond)
		c.runs.pubsub.Publish("run-dedup", PushedEvent{
			Type:       "message_complete",
			SequenceNo: 1, // same seqNo the DB replay emits
			Payload:    []byte(`{"text":"hello"}`),
		})
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/run-dedup/events", nil).WithContext(ctx)
	req.SetPathValue("id", "run-dedup")
	w := &flushRecorder{ResponseRecorder: httptest.NewRecorder()}
	c.handleStreamEvents(w, req)

	body := w.Body.String()
	count := strings.Count(body, "message_complete")
	if count != 1 {
		t.Errorf("expected message_complete exactly once, got %d; body:\n%s", count, body)
	}
}

// bearerScopes answers only for the request's bearer and counts lookups.
type bearerScopes struct {
	held  []string
	calls int
}

func (b *bearerScopes) CallerEffectiveScopes(_ context.Context, bearer string) ([]string, error) {
	b.calls++
	if bearer != "caller-token" {
		return nil, errors.New("no caller bearer token")
	}
	return b.held, nil
}

// can_edit on a list read applies the scope ceiling with the request's bearer,
// asking for the caller's scopes once however many autonomous agents there are.
func TestHandleListAgentTypes_CanEditUsesRequestBearer(t *testing.T) {
	facade, repo, _, _ := newTestServiceWithEval(t, &allowAllEvaluator{})
	scopes := &bearerScopes{held: []string{"platform:grants:write"}}
	facade.at.SetCallerScopes(scopes)
	c := NewController(facade.at, facade.run, nil, nil, nil, nil, &allowAllEvaluator{}, nil, "", logging.NewLoggerWithOutput(io.Discard))
	seedAgentType(t, repo, AgentType{ID: "at-in", TenantID: "t-1", Slug: "within", IdentityMode: IdentityModeAutonomous, GrantScopes: []string{"platform:grants:write"}})
	seedAgentType(t, repo, AgentType{ID: "at-out", TenantID: "t-1", Slug: "beyond", IdentityMode: IdentityModeAutonomous, GrantScopes: []string{"platform:roles:write"}})

	req := withTenantRequest(httptest.NewRequest(http.MethodGet, "/", nil), "t-1")
	req.Header.Set("Authorization", "Bearer caller-token")
	w := httptest.NewRecorder()
	c.handleListAgentTypes(w, req)

	var body struct {
		AgentTypes []AgentType `json:"agent_types"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := map[string]bool{"within": true, "beyond": false}
	for _, at := range body.AgentTypes {
		if at.CanEdit == nil || *at.CanEdit != want[at.Slug] {
			t.Errorf("%s can_edit = %v, want %v", at.Slug, at.CanEdit, want[at.Slug])
		}
	}
	if scopes.calls != 1 {
		t.Errorf("caller scope lookups = %d, want 1", scopes.calls)
	}
}
