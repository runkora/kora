package triggers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/api"
	mw "github.com/clear-route/orbit/internal/platform/middleware"
	"github.com/clear-route/orbit/internal/platform/token"
)

func newTestController(t *testing.T) (*Controller, *testDeps, *Service) {
	t.Helper()
	svc, deps := newTestService(t)
	// A pass-through auth middleware: handler tests invoke handlers directly and
	// inject claims, while the Boot smoke test needs a non-nil middleware to wrap.
	passthrough := mw.NewMiddleware(func(next http.Handler) http.Handler { return next })
	c := NewController(svc, passthrough, nil, nil, "", silentLogger())
	return c, deps, svc
}

// withTenant injects verified platform claims so tenantFromCtx resolves a tenant.
func withTenant(req *http.Request) *http.Request {
	ctx := token.WithClaims(req.Context(), &token.PlatformClaims{Orbit: token.OrbitClaims{TenantID: testTenant}})
	return req.WithContext(ctx)
}

func TestHandleCreateSource(t *testing.T) {
	c, _, _ := newTestController(t)
	raw, _ := json.Marshal(CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})
	req := withTenant(httptest.NewRequest(http.MethodPost, "/trigger-sources", bytes.NewReader(raw)))
	w := httptest.NewRecorder()

	c.handleCreateSource(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	var resp SourceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Secret == "" || resp.WebhookPath == "" {
		t.Errorf("expected one-time secret + webhook path, got %+v", resp)
	}
}

func TestHandleCreateSource_ValidationError(t *testing.T) {
	c, _, _ := newTestController(t)
	raw, _ := json.Marshal(CreateSourceRequest{Provider: ProviderGitHub}) // no name
	req := withTenant(httptest.NewRequest(http.MethodPost, "/trigger-sources", bytes.NewReader(raw)))
	w := httptest.NewRecorder()

	c.handleCreateSource(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleListSources(t *testing.T) {
	c, _, svc := newTestController(t)
	mustSource(t, svc)

	req := withTenant(httptest.NewRequest(http.MethodGet, "/trigger-sources", nil))
	w := httptest.NewRecorder()
	c.handleListSources(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Sources []WebhookSource `json:"sources"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(body.Sources))
	}
}

func TestHandleIngest_Accepted(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/"+src.ID, bytes.NewReader(body))
	req.SetPathValue("sourceID", src.ID)
	req.Header.Set("X-Hub-Signature-256", githubSig(src.Secret, body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "d-1")
	w := httptest.NewRecorder()

	c.handleIngest(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202 (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandleIngest_BadSignature(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/"+src.ID, bytes.NewReader(body))
	req.SetPathValue("sourceID", src.ID)
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()

	c.handleIngest(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestHandleIngest_UnknownSource(t *testing.T) {
	c, _, _ := newTestController(t)
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/nope", bytes.NewReader(body))
	req.SetPathValue("sourceID", "nope")
	w := httptest.NewRecorder()

	c.handleIngest(w, req)

	// Missing source is reported as 404 (does not leak existence vs. disabled).
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleCreateTrigger(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)

	raw, _ := json.Marshal(CreateTriggerRequest{
		SourceID:       src.ID,
		Name:           "review",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "pull_request",
		PromptTemplate: "Review {{ pr.number }}",
	})
	req := withTenant(httptest.NewRequest(http.MethodPost, "/triggers", bytes.NewReader(raw)))
	w := httptest.NewRecorder()

	c.handleCreateTrigger(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandleGetSource(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)

	req := withTenant(httptest.NewRequest(http.MethodGet, "/trigger-sources/"+src.ID, nil))
	req.SetPathValue("id", src.ID)
	w := httptest.NewRecorder()
	c.handleGetSource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", w.Code)
	}

	// Unknown id → 404 via writeServiceError.
	req = withTenant(httptest.NewRequest(http.MethodGet, "/trigger-sources/nope", nil))
	req.SetPathValue("id", "nope")
	w = httptest.NewRecorder()
	c.handleGetSource(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get unknown: status = %d, want 404", w.Code)
	}
}

func TestHandleUpdateAndDeleteSource(t *testing.T) {
	c, deps, svc := newTestController(t)
	src := mustSource(t, svc)

	raw, _ := json.Marshal(UpdateSourceRequest{Enabled: new(false)})
	req := withTenant(httptest.NewRequest(http.MethodPatch, "/trigger-sources/"+src.ID, bytes.NewReader(raw)))
	req.SetPathValue("id", src.ID)
	w := httptest.NewRecorder()
	c.handleUpdateSource(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200", w.Code)
	}
	if deps.repo.sources[src.ID].Enabled {
		t.Error("source should be disabled after update")
	}

	req = withTenant(httptest.NewRequest(http.MethodDelete, "/trigger-sources/"+src.ID, nil))
	req.SetPathValue("id", src.ID)
	w = httptest.NewRecorder()
	c.handleDeleteSource(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", w.Code)
	}
}

func TestHandleTriggerLifecycle(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)
	tr := mustTrigger(t, svc, src.ID, nil, "p")

	// List (filtered by agent_type_id).
	req := withTenant(httptest.NewRequest(http.MethodGet, "/triggers?agent_type_id=at-1", nil))
	w := httptest.NewRecorder()
	c.handleListTriggers(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", w.Code)
	}
	var listBody struct {
		Triggers []Trigger `json:"triggers"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listBody)
	if len(listBody.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(listBody.Triggers))
	}

	// Get.
	req = withTenant(httptest.NewRequest(http.MethodGet, "/triggers/"+tr.ID, nil))
	req.SetPathValue("id", tr.ID)
	w = httptest.NewRecorder()
	c.handleGetTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", w.Code)
	}

	// Update.
	raw, _ := json.Marshal(UpdateTriggerRequest{Enabled: new(false)})
	req = withTenant(httptest.NewRequest(http.MethodPatch, "/triggers/"+tr.ID, bytes.NewReader(raw)))
	req.SetPathValue("id", tr.ID)
	w = httptest.NewRecorder()
	c.handleUpdateTrigger(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200", w.Code)
	}

	// Delete.
	req = withTenant(httptest.NewRequest(http.MethodDelete, "/triggers/"+tr.ID, nil))
	req.SetPathValue("id", tr.ID)
	w = httptest.NewRecorder()
	c.handleDeleteTrigger(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", w.Code)
	}
}

func TestHandleListDeliveries(t *testing.T) {
	c, deps, svc := newTestController(t)
	src := mustSource(t, svc)
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d1", TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusSkipped,
	})

	req := withTenant(httptest.NewRequest(http.MethodGet, "/trigger-deliveries", nil))
	w := httptest.NewRecorder()
	c.handleListDeliveries(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var body struct {
		Deliveries []Delivery `json:"deliveries"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if len(body.Deliveries) != 1 {
		t.Errorf("expected 1 delivery, got %d", len(body.Deliveries))
	}
}

func TestController_BootAndMeta(t *testing.T) {
	c, _, _ := newTestController(t)
	if c.GroupedBy() != "" {
		t.Errorf("GroupedBy = %q, want empty", c.GroupedBy())
	}
	if c.Middleware() != nil {
		t.Error("Middleware should be nil (auth applied per-route)")
	}
	// Boot must register all routes on a Mux without panicking.
	mux := api.NewMux(context.Background(), silentLogger())
	mux.SetBasePath("")
	mux.SetMiddleware(nil)
	c.Boot(mux)
}
