package triggers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

// A bad signature answers exactly as an unknown source id does. A 401 here told
// a prober that the id they guessed is a real source and only the secret is
// missing, which is the single fact this unauthenticated endpoint should never
// give away. The reason is still recorded against the source, where the tenant
// that owns it can see it.
func TestHandleIngest_BadSignatureIsIndistinguishableFromAnUnknownSource(t *testing.T) {
	c, _, svc := newTestController(t)
	src := mustSource(t, svc)
	body := prBody("opened", "main")

	req := httptest.NewRequest(http.MethodPost, "/webhooks/"+src.ID, bytes.NewReader(body))
	req.SetPathValue("sourceID", src.ID)
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := httptest.NewRecorder()

	c.handleIngest(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (the same answer an unknown source gets)", w.Code)
	}

	unknown := httptest.NewRequest(http.MethodPost, "/webhooks/nope", bytes.NewReader(body))
	unknown.SetPathValue("sourceID", "nope")
	uw := httptest.NewRecorder()
	c.handleIngest(uw, unknown)
	if uw.Body.String() != w.Body.String() {
		t.Errorf("bad signature answered %q, unknown source answered %q; they must be identical",
			w.Body.String(), uw.Body.String())
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

// A replay answers 202 with the NEW delivery id: the original is never touched,
// and the caller follows the new one to see what it did.
func TestHandleRedeliver(t *testing.T) {
	c, deps, svc := newTestController(t)
	trig := mustManualTrigger(t, svc)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)

	req := withTenant(httptest.NewRequest(http.MethodPost, "/trigger-deliveries/"+fired.DeliveryID+"/redeliver", nil))
	req.SetPathValue("id", fired.DeliveryID)
	w := httptest.NewRecorder()
	c.handleRedeliver(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", w.Code, w.Body.String())
	}
	var body RedeliverResponse
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body.RedeliveryOf != fired.DeliveryID {
		t.Errorf("redelivery_of = %q, want %q", body.RedeliveryOf, fired.DeliveryID)
	}
	if body.DeliveryID == "" || body.DeliveryID == fired.DeliveryID {
		t.Errorf("delivery_id = %q, want a new delivery", body.DeliveryID)
	}
}

// A delivery that cannot be replayed is a 400 that says why, not a 500.
func TestHandleRedeliver_NotRedeliverable(t *testing.T) {
	c, deps, _ := newTestController(t)
	at := time.Now().UTC().Truncate(time.Minute)
	tid := "sched-1"
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "tick", TenantID: testTenant, TriggerID: &tid, Kind: KindScheduled,
		Status: StatusDispatched, ScheduledAt: &at,
	})

	req := withTenant(httptest.NewRequest(http.MethodPost, "/trigger-deliveries/tick/redeliver", nil))
	req.SetPathValue("id", "tick")
	w := httptest.NewRecorder()
	c.handleRedeliver(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", w.Code, w.Body.String())
	}
}

// ─── Payload retention ───────────────────────────────────────────────────────

func TestHandleRetention_GetAndPut(t *testing.T) {
	c, _, _ := newTestController(t)

	// Unset: the inherited default, flagged as such.
	w := httptest.NewRecorder()
	c.handleGetRetention(w, withTenant(httptest.NewRequest(http.MethodGet, "/trigger-retention", nil)))
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d (%s), want 200", w.Code, w.Body.String())
	}
	var got RetentionSettings
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.PayloadDays != DefaultPayloadRetentionDays || !got.IsDefault {
		t.Errorf("GET body = %+v, want the inherited default", got)
	}

	raw, _ := json.Marshal(UpdateRetentionRequest{PayloadDays: 3})
	w = httptest.NewRecorder()
	c.handleSetRetention(w, withTenant(httptest.NewRequest(http.MethodPut, "/trigger-retention", bytes.NewReader(raw))))
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s), want 200", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	c.handleGetRetention(w, withTenant(httptest.NewRequest(http.MethodGet, "/trigger-retention", nil)))
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got.PayloadDays != 3 || got.IsDefault {
		t.Errorf("GET after PUT = %+v, want 3 and not default", got)
	}
}

// An out-of-range window is a 400 that names the bounds, not a constraint
// violation surfacing as a 500.
func TestHandleRetention_OutOfBounds(t *testing.T) {
	c, _, _ := newTestController(t)
	raw, _ := json.Marshal(UpdateRetentionRequest{PayloadDays: 365})
	w := httptest.NewRecorder()

	c.handleSetRetention(w, withTenant(httptest.NewRequest(http.MethodPut, "/trigger-retention", bytes.NewReader(raw))))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", w.Code, w.Body.String())
	}
}

// ─── Event catalog ─────────────────────────────────────────────────────────

func TestHandleSourceEventTypes(t *testing.T) {
	c, deps, svc := newTestController(t)
	src := mustSource(t, svc)
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d1", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-1", EventType: "release", Status: StatusSkipped,
	})

	req := withTenant(httptest.NewRequest(http.MethodGet, "/trigger-sources/"+src.ID+"/event-types", nil))
	req.SetPathValue("id", src.ID)
	w := httptest.NewRecorder()
	c.handleSourceEventTypes(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body.String())
	}
	var got EventTypesResponse
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if len(got.Declared) == 0 {
		t.Error("declared must list what the adapter models")
	}
	if len(got.Observed) != 1 || got.Observed[0] != "release" {
		t.Errorf("observed = %v, want the event this source actually sent", got.Observed)
	}
}

func TestHandleSourceEventTypes_UnknownSource(t *testing.T) {
	c, _, _ := newTestController(t)
	req := withTenant(httptest.NewRequest(http.MethodGet, "/trigger-sources/nope/event-types", nil))
	req.SetPathValue("id", "nope")
	w := httptest.NewRecorder()

	c.handleSourceEventTypes(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// pending_count is always serialized, including at zero: "nothing is waiting" is
// the answer an operator most needs to trust, and an absent field reads as
// unknown rather than none.
func TestHandleGetTrigger_PendingCountAlwaysPresent(t *testing.T) {
	c, deps, svc := newTestController(t)
	trig := newAdmissionTrigger(t, deps, OverflowWait, 1, 32)

	get := func() (map[string]any, int) {
		req := withTenant(httptest.NewRequest(http.MethodGet, "/triggers/"+trig.ID, nil))
		req.SetPathValue("id", trig.ID)
		w := httptest.NewRecorder()
		c.handleGetTrigger(w, req)
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body, w.Code
	}

	body, code := get()
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	got, present := body["pending_count"]
	if !present {
		t.Fatal("pending_count must be present even at zero")
	}
	if got.(float64) != 0 {
		t.Errorf("pending_count = %v, want 0", got)
	}

	enqueue(t, svc, deps, trig, 2)

	body, _ = get()
	if body["pending_count"].(float64) != 2 {
		t.Errorf("pending_count = %v, want 2 waiting", body["pending_count"])
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

// ─── Phase 2: per-trigger fire authorization ─────────────────────────────────
//
// Firing was previously gated only by triggers:write, the same scope that
// creates and deletes triggers, so anyone who could manage triggers could fire
// any of them. These tests pin the narrower check closed.

// newFireController builds a controller whose evaluator answers as configured,
// so a fire test exercises the real authorization path.
func newFireController(t *testing.T, allow bool, evalErr error) (*Controller, *mockEvaluator, *testDeps, *Service) {
	t.Helper()
	svc, deps := newTestService(t)
	eval := &mockEvaluator{allow: allow, err: evalErr}
	passthrough := mw.NewMiddleware(func(next http.Handler) http.Handler { return next })
	return NewController(svc, passthrough, eval, nil, "", silentLogger()), eval, deps, svc
}

// The regression this closes: holding triggers:write must not imply the ability
// to fire. Route-level enforcement cannot make that distinction, so if this
// handler check is ever removed the gap silently returns.
func TestHandleFireTrigger_DeniedWithoutPerTriggerScope(t *testing.T) {
	c, eval, _, _ := newFireController(t, false, nil)

	req := withTenant(httptest.NewRequest(http.MethodPost, "/triggers/trig-1/fire", bytes.NewReader([]byte(`{}`))))
	req.SetPathValue("id", "trig-1")
	w := httptest.NewRecorder()

	c.handleFireTrigger(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — triggers:write must not imply fire", w.Code)
	}
	if got := eval.scopeAsked(); got != "trigger:trig-1:fire" {
		t.Errorf("checked scope %q, want trigger:trig-1:fire — the identifier precedes the action so prefix grants nest correctly", got)
	}
}

// An evaluator failure is not a permission decision. It must refuse rather than
// fall through to an allow.
func TestHandleFireTrigger_EvaluatorErrorFailsClosed(t *testing.T) {
	c, _, _, _ := newFireController(t, true, errors.New("bundle unavailable"))

	req := withTenant(httptest.NewRequest(http.MethodPost, "/triggers/trig-1/fire", bytes.NewReader([]byte(`{}`))))
	req.SetPathValue("id", "trig-1")
	w := httptest.NewRecorder()

	c.handleFireTrigger(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — an evaluator error must never become an allow", w.Code)
	}
}

// A misconfigured controller (no evaluator wired) must refuse too.
func TestHandleFireTrigger_NilEvaluatorFailsClosed(t *testing.T) {
	c, _, _ := newTestController(t) // built with a nil evaluator

	req := withTenant(httptest.NewRequest(http.MethodPost, "/triggers/trig-1/fire", bytes.NewReader([]byte(`{}`))))
	req.SetPathValue("id", "trig-1")
	w := httptest.NewRecorder()

	c.handleFireTrigger(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when no evaluator is configured", w.Code)
	}
}

// An unauthenticated caller never reaches the evaluator.
func TestHandleFireTrigger_NoClaimsDenied(t *testing.T) {
	c, _, _, _ := newFireController(t, true, nil)

	req := httptest.NewRequest(http.MethodPost, "/triggers/trig-1/fire", bytes.NewReader([]byte(`{}`)))
	req.SetPathValue("id", "trig-1")
	w := httptest.NewRecorder()

	c.handleFireTrigger(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without claims", w.Code)
	}
}

// With the scope held, the fire proceeds to the service. A non-manual trigger is
// rejected there, which is enough to show authorization passed.
func TestHandleFireTrigger_AllowedWithPerTriggerScope(t *testing.T) {
	c, eval, deps, svc := newFireController(t, true, nil)
	src := mustSource(t, svc)
	trig := mustTrigger(t, svc, src.ID, nil, "p")
	deps.minter.reset()

	req := withTenant(httptest.NewRequest(http.MethodPost, "/triggers/"+trig.ID+"/fire", bytes.NewReader([]byte(`{}`))))
	req.SetPathValue("id", trig.ID)
	w := httptest.NewRecorder()

	c.handleFireTrigger(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("authorization should have passed, got 403")
	}
	if got := eval.scopeAsked(); got != "trigger:"+trig.ID+":fire" {
		t.Errorf("checked scope %q, want the specific trigger's fire scope", got)
	}
}
