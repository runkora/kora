package triggers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"github.com/runkora/kora/backend/internal/platform/api"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/token"
)

func newTestController(t *testing.T) (*Controller, *testDeps, *Service) {
	t.Helper()
	svc, deps := newTestService(t)
	// A pass-through auth middleware: handler tests invoke handlers directly and
	// inject claims, while the Boot smoke test needs a non-nil middleware to wrap.
	passthrough := mw.NewMiddleware(func(next http.Handler) http.Handler { return next })
	c := NewController(svc, deps.sources, deps.triggers, passthrough, nil, nil, "", silentLogger())
	return c, deps, svc
}

// withTenant injects verified platform claims so tenantFromCtx resolves a tenant.
func withTenant(req *http.Request) *http.Request {
	ctx := token.WithClaims(req.Context(), &token.PlatformClaims{Kora: token.KoraClaims{TenantID: testTenant}})
	return req.WithContext(ctx)
}

// newRequest builds a request with path values applied. A nil body sends no
// payload, []byte passes through raw, and anything else is JSON-encoded.
func newRequest(t *testing.T, method, target string, pathVals map[string]string, body any) *http.Request {
	t.Helper()
	var rdr io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rdr = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		rdr = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, target, rdr)
	for k, v := range pathVals {
		req.SetPathValue(k, v)
	}
	return req
}

// invoke runs the handler and returns the recorder. Tests that must stay
// unauthenticated (ingest, missing claims) use it directly, so the absence of
// tenant claims is visible at the call site.
func invoke(h http.HandlerFunc, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

// call is the authenticated path: tenant claims injected, handler invoked.
func call(t *testing.T, h http.HandlerFunc, method, target string, pathVals map[string]string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return invoke(h, withTenant(newRequest(t, method, target, pathVals, body)))
}

// decode unmarshals the recorded response body into T.
func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return v
}

func TestHandleCreateSource(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleCreateSource, http.MethodPost, "/trigger-sources", nil,
		CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
	resp := decode[SourceResponse](t, w)
	if resp.Secret == "" || resp.WebhookPath == "" {
		t.Errorf("expected one-time secret + webhook path, got %+v", resp)
	}
}

func TestHandleCreateSource_ValidationError(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleCreateSource, http.MethodPost, "/trigger-sources", nil,
		CreateSourceRequest{Provider: ProviderGitHub}) // no name

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestHandleListSources(t *testing.T) {
	c, deps, _ := newTestController(t)
	mustSource(t, deps.sources)

	w := call(t, c.handleListSources, http.MethodGet, "/trigger-sources", nil, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := decode[struct {
		Sources []WebhookSource `json:"sources"`
	}](t, w)
	if len(body.Sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(body.Sources))
	}
}

func TestHandleIngest_Accepted(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")

	req := newRequest(t, http.MethodPost, "/webhooks/"+src.ID, map[string]string{"sourceID": src.ID}, body)
	req.Header.Set("X-Hub-Signature-256", githubSig(src.Secret, body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-GitHub-Delivery", "d-1")
	w := invoke(c.handleIngest, req)

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
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	body := prBody("opened", "main")

	req := newRequest(t, http.MethodPost, "/webhooks/"+src.ID, map[string]string{"sourceID": src.ID}, body)
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	req.Header.Set("X-GitHub-Event", "pull_request")
	w := invoke(c.handleIngest, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (the same answer an unknown source gets)", w.Code)
	}

	uw := invoke(c.handleIngest, newRequest(t, http.MethodPost, "/webhooks/nope", map[string]string{"sourceID": "nope"}, body))
	if uw.Body.String() != w.Body.String() {
		t.Errorf("bad signature answered %q, unknown source answered %q; they must be identical",
			w.Body.String(), uw.Body.String())
	}
}

func TestHandleIngest_UnknownSource(t *testing.T) {
	c, _, _ := newTestController(t)

	w := invoke(c.handleIngest, newRequest(t, http.MethodPost, "/webhooks/nope", map[string]string{"sourceID": "nope"}, []byte(`{}`)))

	// Missing source is reported as 404 (does not leak existence vs. disabled).
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestHandleCreateTrigger(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)

	w := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, CreateTriggerRequest{
		SourceID:       src.ID,
		Name:           "review",
		AgentTypeID:    "at-1",
		AgentSlug:      "code-reviewer",
		EventType:      "pull_request",
		PromptTemplate: "Review {{ pr.number }}",
	})

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandleGetSource(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)

	w := call(t, c.handleGetSource, http.MethodGet, "/trigger-sources/"+src.ID, map[string]string{"id": src.ID}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", w.Code)
	}

	// Unknown id → 404 via writeServiceError.
	w = call(t, c.handleGetSource, http.MethodGet, "/trigger-sources/nope", map[string]string{"id": "nope"}, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("get unknown: status = %d, want 404", w.Code)
	}
}

func TestHandleUpdateAndDeleteSource(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)

	w := call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/"+src.ID,
		map[string]string{"id": src.ID}, UpdateSourceRequest{Enabled: new(false)})
	if w.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200", w.Code)
	}
	if deps.repo.sources[src.ID].Enabled {
		t.Error("source should be disabled after update")
	}

	w = call(t, c.handleDeleteSource, http.MethodDelete, "/trigger-sources/"+src.ID, map[string]string{"id": src.ID}, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", w.Code)
	}
}

func TestHandleTriggerLifecycle(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	tr := mustTrigger(t, deps.triggers, src.ID, nil, "p")

	// List (filtered by agent_type_id).
	w := call(t, c.handleListTriggers, http.MethodGet, "/triggers?agent_type_id=at-1", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d, want 200", w.Code)
	}
	listBody := decode[struct {
		Triggers []Trigger `json:"triggers"`
	}](t, w)
	if len(listBody.Triggers) != 1 {
		t.Errorf("expected 1 trigger, got %d", len(listBody.Triggers))
	}

	// Get.
	w = call(t, c.handleGetTrigger, http.MethodGet, "/triggers/"+tr.ID, map[string]string{"id": tr.ID}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: status = %d, want 200", w.Code)
	}

	// Update.
	w = call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/"+tr.ID,
		map[string]string{"id": tr.ID}, UpdateTriggerRequest{Enabled: new(false)})
	if w.Code != http.StatusOK {
		t.Fatalf("update: status = %d, want 200", w.Code)
	}

	// Delete.
	w = call(t, c.handleDeleteTrigger, http.MethodDelete, "/triggers/"+tr.ID, map[string]string{"id": tr.ID}, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d, want 204", w.Code)
	}
}

func TestHandleListDeliveries(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d1", TenantID: testTenant, SourceID: src.ID, EventType: "pull_request", Status: StatusSkipped,
	})

	w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := decode[struct {
		Deliveries []Delivery `json:"deliveries"`
	}](t, w)
	if len(body.Deliveries) != 1 {
		t.Errorf("expected 1 delivery, got %d", len(body.Deliveries))
	}
}

// A replay answers 202 with the NEW delivery id: the original is never touched,
// and the caller follows the new one to see what it did.
func TestHandleRedeliver(t *testing.T) {
	c, deps, svc := newTestController(t)
	trig := mustManualTrigger(t, deps.triggers)
	fired, err := svc.FireTrigger(context.Background(), testTenant, trig.ID, "user:1",
		FireTriggerRequest{Variables: map[string]string{"env": "prod"}})
	if err != nil {
		t.Fatalf("FireTrigger: %v", err)
	}
	drainPending(t, svc, deps.repo)

	w := call(t, c.handleRedeliver, http.MethodPost, "/trigger-deliveries/"+fired.DeliveryID+"/redeliver",
		map[string]string{"id": fired.DeliveryID}, nil)

	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d (%s), want 202", w.Code, w.Body.String())
	}
	body := decode[RedeliverResponse](t, w)
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

	w := call(t, c.handleRedeliver, http.MethodPost, "/trigger-deliveries/tick/redeliver",
		map[string]string{"id": "tick"}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", w.Code, w.Body.String())
	}
}

// ─── Payload retention ───────────────────────────────────────────────────────

func TestHandleRetention_GetAndPut(t *testing.T) {
	c, _, _ := newTestController(t)

	// Unset: the inherited default, flagged as such.
	w := call(t, c.handleGetRetention, http.MethodGet, "/trigger-retention", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d (%s), want 200", w.Code, w.Body.String())
	}
	got := decode[RetentionSettings](t, w)
	if got.PayloadDays != DefaultPayloadRetentionDays || !got.IsDefault {
		t.Errorf("GET body = %+v, want the inherited default", got)
	}

	w = call(t, c.handleSetRetention, http.MethodPut, "/trigger-retention", nil, UpdateRetentionRequest{PayloadDays: 3})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (%s), want 200", w.Code, w.Body.String())
	}

	w = call(t, c.handleGetRetention, http.MethodGet, "/trigger-retention", nil, nil)
	got = decode[RetentionSettings](t, w)
	if got.PayloadDays != 3 || got.IsDefault {
		t.Errorf("GET after PUT = %+v, want 3 and not default", got)
	}
}

// An out-of-range window is a 400 that names the bounds, not a constraint
// violation surfacing as a 500.
func TestHandleRetention_OutOfBounds(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleSetRetention, http.MethodPut, "/trigger-retention", nil, UpdateRetentionRequest{PayloadDays: 365})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400", w.Code, w.Body.String())
	}
}

// ─── Event catalog ─────────────────────────────────────────────────────────

func TestHandleSourceEventTypes(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
		ID: "d1", TenantID: testTenant, SourceID: src.ID, Kind: KindWebhook,
		ProviderDeliveryID: "gh-1", EventType: "release", Status: StatusSkipped,
	})

	w := call(t, c.handleSourceEventTypes, http.MethodGet, "/trigger-sources/"+src.ID+"/event-types",
		map[string]string{"id": src.ID}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body.String())
	}
	got := decode[EventTypesResponse](t, w)
	if len(got.Declared) == 0 {
		t.Error("declared must list what the adapter models")
	}
	if len(got.Observed) != 1 || got.Observed[0] != "release" {
		t.Errorf("observed = %v, want the event this source actually sent", got.Observed)
	}
}

func TestHandleSourceEventTypes_UnknownSource(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleSourceEventTypes, http.MethodGet, "/trigger-sources/nope/event-types",
		map[string]string{"id": "nope"}, nil)

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
		w := call(t, c.handleGetTrigger, http.MethodGet, "/triggers/"+trig.ID, map[string]string{"id": trig.ID}, nil)
		return decode[map[string]any](t, w), w.Code
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
	return NewController(svc, deps.sources, deps.triggers, passthrough, eval, nil, "", silentLogger()), eval, deps, svc
}

// The regression this closes: holding triggers:write must not imply the ability
// to fire. Route-level enforcement cannot make that distinction, so if this
// handler check is ever removed the gap silently returns.
func TestHandleFireTrigger_DeniedWithoutPerTriggerScope(t *testing.T) {
	c, eval, _, _ := newFireController(t, false, nil)

	w := call(t, c.handleFireTrigger, http.MethodPost, "/triggers/trig-1/fire",
		map[string]string{"id": "trig-1"}, []byte(`{}`))

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

	w := call(t, c.handleFireTrigger, http.MethodPost, "/triggers/trig-1/fire",
		map[string]string{"id": "trig-1"}, []byte(`{}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — an evaluator error must never become an allow", w.Code)
	}
}

// A misconfigured controller (no evaluator wired) must refuse too.
func TestHandleFireTrigger_NilEvaluatorFailsClosed(t *testing.T) {
	c, _, _ := newTestController(t) // built with a nil evaluator

	w := call(t, c.handleFireTrigger, http.MethodPost, "/triggers/trig-1/fire",
		map[string]string{"id": "trig-1"}, []byte(`{}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when no evaluator is configured", w.Code)
	}
}

// An unauthenticated caller never reaches the evaluator.
func TestHandleFireTrigger_NoClaimsDenied(t *testing.T) {
	c, _, _, _ := newFireController(t, true, nil)

	w := invoke(c.handleFireTrigger, newRequest(t, http.MethodPost, "/triggers/trig-1/fire",
		map[string]string{"id": "trig-1"}, []byte(`{}`)))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 without claims", w.Code)
	}
}

// With the scope held, the fire proceeds to the service. A non-manual trigger is
// rejected there, which is enough to show authorization passed.
func TestHandleFireTrigger_AllowedWithPerTriggerScope(t *testing.T) {
	c, eval, deps, _ := newFireController(t, true, nil)
	src := mustSource(t, deps.sources)
	trig := mustTrigger(t, deps.triggers, src.ID, nil, "p")
	deps.minter.reset()

	w := call(t, c.handleFireTrigger, http.MethodPost, "/triggers/"+trig.ID+"/fire",
		map[string]string{"id": trig.ID}, []byte(`{}`))

	if w.Code == http.StatusForbidden {
		t.Fatalf("authorization should have passed, got 403")
	}
	if got := eval.scopeAsked(); got != "trigger:"+trig.ID+":fire" {
		t.Errorf("checked scope %q, want the specific trigger's fire scope", got)
	}
}

func TestHandleProviderEventTypes(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleProviderEventTypes, http.MethodGet, "/trigger-providers/github/event-types",
		map[string]string{"provider": ProviderGitHub}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body.String())
	}
	got := decode[EventTypesResponse](t, w)
	if !slices.Contains(got.Declared, "issue_comment") || !slices.Equal(got.Associations, []string{"OWNER", "MEMBER", "COLLABORATOR"}) {
		t.Errorf("response = %+v", got)
	}

	w = call(t, c.handleProviderEventTypes, http.MethodGet, "/trigger-providers/alertmanager/event-types",
		map[string]string{"provider": ProviderAlertmanager}, nil)
	if am := decode[EventTypesResponse](t, w); !slices.Equal(am.Declared, []string{"alert.firing", "alert.resolved"}) ||
		am.Associations == nil || len(am.Associations) != 0 {
		t.Errorf("alertmanager = %+v, want its two types and an empty association list", am)
	}

	w = call(t, c.handleProviderEventTypes, http.MethodGet, "/trigger-providers/gitlab/event-types",
		map[string]string{"provider": "gitlab"}, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown provider status = %d, want 404", w.Code)
	}
}

func TestHandleProviderFields(t *testing.T) {
	c, _, _ := newTestController(t)

	w := call(t, c.handleProviderFields, http.MethodGet, "/trigger-providers/alertmanager/fields?event_type=alert.firing",
		map[string]string{"provider": ProviderAlertmanager}, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", w.Code, w.Body.String())
	}
	got := decode[FieldsResponse](t, w)
	var pattern bool
	for _, f := range got.Fields {
		if f.Path == "commonLabels.<name>" && f.Pattern {
			pattern = true
		}
	}
	if !pattern {
		t.Errorf("fields = %+v, want the commonLabels pattern field", got.Fields)
	}

	w = call(t, c.handleProviderFields, http.MethodGet, "/trigger-providers/github/fields?event_type=issue_comment",
		map[string]string{"provider": ProviderGitHub}, nil)
	paths := map[string]bool{}
	for _, f := range decode[FieldsResponse](t, w).Fields {
		paths[f.Path] = true
	}
	for _, want := range []string{"comment.body", "issue.title", "sender.login", metaEventType} {
		if !paths[want] {
			t.Errorf("issue_comment fields are missing %s", want)
		}
	}

	w = call(t, c.handleProviderFields, http.MethodGet, "/trigger-providers/github/fields",
		map[string]string{"provider": ProviderGitHub}, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing event_type status = %d, want 400", w.Code)
	}

	w = call(t, c.handleProviderFields, http.MethodGet, "/trigger-providers/github/fields?event_type=%20",
		map[string]string{"provider": ProviderGitHub}, nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("blank event_type status = %d, want 400", w.Code)
	}

	w = call(t, c.handleProviderFields, http.MethodGet, "/trigger-providers/gitlab/fields?event_type=push",
		map[string]string{"provider": "gitlab"}, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("unknown provider status = %d, want 404", w.Code)
	}
}

func TestHandleListDeliveries_TimeAndPayload(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	now := time.Now()
	for i, id := range []string{"old", "new"} {
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{
			ID: id, TenantID: testTenant, SourceID: src.ID, EventType: "push", Status: StatusSkipped,
			Payload: []byte(`{"a":1}`), PayloadExcerpt: "x", ReceivedAt: now.Add(time.Duration(i-2) * time.Hour),
		})
	}
	list := func(query string) (int, []Delivery) {
		w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?"+query, nil, nil)
		if w.Code != http.StatusOK {
			return w.Code, nil
		}
		return w.Code, decode[struct {
			Deliveries []Delivery `json:"deliveries"`
		}](t, w).Deliveries
	}

	cut := url.QueryEscape(now.Add(-90 * time.Minute).UTC().Format(time.RFC3339))
	if code, got := list("received_after=" + cut); code != 200 || len(got) != 1 || got[0].ID != "new" {
		t.Errorf("received_after: %d %+v, want only new", code, got)
	}
	if code, got := list("received_before=" + cut); code != 200 || len(got) != 1 || got[0].ID != "old" {
		t.Errorf("received_before: %d %+v, want only old", code, got)
	}
	if _, got := list(""); len(got) != 2 || len(got[0].Payload) == 0 {
		t.Errorf("default listing = %+v, want payloads", got)
	}
	if _, got := list("omit_payload=true"); len(got) != 2 || len(got[0].Payload) != 0 || got[0].PayloadExcerpt != "x" {
		t.Errorf("omit_payload listing = %+v, want excerpt without payload", got)
	}
	for _, q := range []string{"received_after=yesterday", "received_before=2026-13-01T00:00:00Z"} {
		if code, _ := list(q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, code)
		}
	}
}
