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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/runkora/kora/backend/internal/platform/api"
	agentsclient "github.com/runkora/kora/backend/internal/platform/clients/agents"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
	"github.com/runkora/kora/backend/internal/platform/policy"
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

func TestHandleCreateSource_NameTaken(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.repo.createSourceErr = ErrSourceNameTaken

	w := call(t, c.handleCreateSource, http.MethodPost, "/trigger-sources", nil,
		CreateSourceRequest{Name: "gh", Provider: ProviderGitHub})

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), ErrSourceNameTaken.Error()) {
		t.Errorf("body = %s, want the name-taken message", w.Body.String())
	}
}

func TestSourceWriteError(t *testing.T) {
	taken := &pgconn.PgError{Code: "23505", ConstraintName: sourceNameKey}
	if err := sourceWriteError("create webhook source", taken); !errors.Is(err, ErrSourceNameTaken) {
		t.Errorf("duplicate name = %v, want ErrSourceNameTaken", err)
	}
	other := &pgconn.PgError{Code: "23505", ConstraintName: "webhook_sources_pkey"}
	if err := sourceWriteError("create webhook source", other); errors.Is(err, ErrSourceNameTaken) || !errors.As(err, new(*pgconn.PgError)) {
		t.Errorf("other unique violation = %v, want it wrapped unchanged", err)
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

// scopeSetEvaluator allows exactly the scopes it holds.
type scopeSetEvaluator struct {
	policy.Evaluator
	held map[string]bool
}

func (e scopeSetEvaluator) Check(_ context.Context, in policy.CheckInput) (policy.Result, error) {
	scope := strings.Join(append(append([]string{in.ResourceType}, in.PathSegments...), in.Action), ":")
	return policy.Result{Allow: e.held[scope]}, nil
}

// The fire route carries no route scope. triggers:write must neither be needed nor be enough.
func TestBoot_FireIsAuthorizedByTriggerScopeAlone(t *testing.T) {
	routes := []routescopes.RouteScope{
		{PathPrefix: "/api/v1/triggers", Method: http.MethodPost, RequiredScope: "triggers:write"},
		{PathPrefix: "/api/v1/trigger-fires", Method: http.MethodPost, RequiredScope: ""},
	}
	tests := []struct {
		name     string
		held     []string
		path     string
		wantCode int
	}{
		{"per-trigger scope fires", []string{"trigger:{id}:fire"}, "/trigger-fires/{id}", http.StatusAccepted},
		{"triggers:write alone cannot fire", []string{"triggers:write"}, "/trigger-fires/{id}", http.StatusForbidden},
		{"per-trigger scope cannot create", []string{"trigger:{id}:fire"}, "/triggers", http.StatusForbidden},
		{"old fire path is gone", []string{"trigger:{id}:fire", "triggers:write"}, "/triggers/{id}/fire", http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, deps := newTestService(t)
			trig, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, validReq(KindManual))
			if err != nil {
				t.Fatalf("CreateTrigger: %v", err)
			}
			held := map[string]bool{}
			for _, sc := range tc.held {
				held[strings.ReplaceAll(sc, "{id}", trig.ID)] = true
			}
			passthrough := mw.NewMiddleware(func(next http.Handler) http.Handler { return next })
			c := NewController(svc, deps.sources, deps.triggers, passthrough, scopeSetEvaluator{held: held}, routes, "/api/v1", silentLogger())
			mux := api.NewMux(context.Background(), silentLogger())
			mux.SetBasePath("")
			c.Boot(mux)

			req := withTenant(newRequest(t, http.MethodPost, strings.ReplaceAll(tc.path, "{id}", trig.ID), nil, []byte(`{}`)))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.wantCode {
				t.Fatalf("status = %d (body %s), want %d", w.Code, w.Body.String(), tc.wantCode)
			}
		})
	}
}

// The run-access question goes to kora-agents with the caller's own bearer and delegation, never the trigger principal's.
func TestBoot_CreateAsksWithCallerBearer(t *testing.T) {
	svc, deps := newTestService(t)
	routes := []routescopes.RouteScope{{PathPrefix: "/api/v1/triggers", Method: http.MethodPost, RequiredScope: "triggers:write"}}
	passthrough := mw.NewMiddleware(func(next http.Handler) http.Handler { return next })
	c := NewController(svc, deps.sources, deps.triggers, passthrough, scopeSetEvaluator{held: map[string]bool{"triggers:write": true}}, routes, "/api/v1", silentLogger())
	mux := api.NewMux(context.Background(), silentLogger())
	mux.SetBasePath("")
	c.Boot(mux)

	req := withTenant(newRequest(t, http.MethodPost, "/triggers", nil, validReq(KindManual)))
	req.Header.Set("Authorization", "Bearer caller-tok")
	req.Header.Set("X-Kora-Delegation-Token", "caller-delegation")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d (body %s), want 201", w.Code, w.Body.String())
	}
	deps.agents.mu.Lock()
	defer deps.agents.mu.Unlock()
	if !slices.Equal(deps.agents.callerBearers, []string{"Bearer caller-tok"}) {
		t.Errorf("caller bearers = %v, want the caller's own", deps.agents.callerBearers)
	}
	// A delegated caller's delegation goes with it, so kora-agents evaluates the delegated run.
	if !slices.Equal(deps.agents.callerDelegs, []string{"caller-delegation"}) {
		t.Errorf("caller delegations = %v, want the caller's own", deps.agents.callerDelegs)
	}
}

func TestHandleCreateTrigger_BindForbidden(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.agents.atNoRun = true

	w := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, validReq(KindManual))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", w.Code, w.Body.String())
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

// A full page carries a next_cursor the handler accepts back; a value it did not
// issue is a 400.
func TestHandleListDeliveries_Cursor(t *testing.T) {
	c, deps, _ := newTestController(t)
	for _, id := range []string{"d1", "d2"} {
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{ID: id, TenantID: testTenant, Status: StatusSkipped})
	}
	type page struct {
		Deliveries []Delivery `json:"deliveries"`
		NextCursor *string    `json:"next_cursor"`
	}

	first := decode[page](t, call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?limit=1", nil, nil))
	if len(first.Deliveries) != 1 || first.NextCursor == nil {
		t.Fatalf("first page = %+v, want one delivery and a next_cursor", first)
	}
	if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?limit=1&cursor="+url.QueryEscape(*first.NextCursor), nil, nil); w.Code != http.StatusOK {
		t.Errorf("issued cursor: status %d, want 200", w.Code)
	}
	last := decode[page](t, call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?limit=5", nil, nil))
	if last.NextCursor != nil {
		t.Errorf("last page next_cursor = %q, want absent", *last.NextCursor)
	}
	for _, bad := range []string{"not-base64!", "bm8tc2VwYXJhdG9y"} {
		if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?cursor="+bad, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("cursor %q: status %d, want 400", bad, w.Code)
		}
	}
	if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?received_before=2026-11-01T00:00:00Z", nil, nil); w.Code != http.StatusBadRequest {
		t.Errorf("received_before: status %d, want 400", w.Code)
	}
}

// status= narrows the page. The tenant-wide list takes delivery statuses and a
// trigger scope takes trigger statuses; anything else is a 400.
func TestHandleListDeliveries_Status(t *testing.T) {
	c, deps, _ := newTestController(t)
	for id, status := range map[string]string{"d-failed": StatusFailed, "d-skipped": StatusSkipped} {
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{ID: id, TenantID: testTenant, Status: status})
	}
	body := decode[struct {
		Deliveries []Delivery `json:"deliveries"`
	}](t, call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?status=failed", nil, nil))
	if len(body.Deliveries) != 1 || body.Deliveries[0].ID != "d-failed" {
		t.Errorf("status=failed = %+v, want only d-failed", body.Deliveries)
	}
	for _, q := range []string{"status=bogus", "status=superseded", "status=started", "status=filtered"} {
		if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?"+q, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("tenant-wide %s: status %d, want 400", q, w.Code)
		}
	}
	for _, status := range TriggerStatuses {
		if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?trigger_id=t1&status="+status, nil, nil); w.Code != http.StatusOK {
			t.Errorf("trigger-scoped %s: status %d, want 200", status, w.Code)
		}
	}
	for _, status := range []string{"dispatched", "joined", "duplicate", "bogus"} {
		if w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?trigger_id=t1&status="+status, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("trigger-scoped %s: status %d, want 400", status, w.Code)
		}
	}
}

// agent_type_id lists the deliveries of the agent's triggers; an agent with none
// gets an empty page, and it does not combine with trigger_id.
func TestHandleListDeliveries_AgentScope(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	mine := mustTrigger(t, deps.triggers, src.ID, nil, "mine")
	for id, trig := range map[string]string{"d-mine": mine.ID, "d-other": "trg-other"} {
		_, _ = deps.repo.InsertDelivery(context.Background(), &Delivery{ID: id, TenantID: testTenant, TriggerID: &trig, Status: StatusDispatched})
	}
	list := func(query string) (int, []Delivery) {
		w := call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries?"+query, nil, nil)
		return w.Code, decode[struct {
			Deliveries []Delivery `json:"deliveries"`
		}](t, w).Deliveries
	}
	if code, got := list("agent_type_id=" + mine.AgentTypeID); code != http.StatusOK || len(got) != 1 || got[0].ID != "d-mine" {
		t.Errorf("agent scope = %d %+v, want only d-mine", code, got)
	}
	if code, got := list("agent_type_id=at-none"); code != http.StatusOK || len(got) != 0 {
		t.Errorf("agent with no triggers = %d %+v, want an empty page", code, got)
	}
	if code, _ := list("agent_type_id=" + mine.AgentTypeID + "&trigger_id=" + mine.ID); code != http.StatusBadRequest {
		t.Errorf("agent and trigger together: status %d, want 400", code)
	}
}

// Every listed trigger carries stats, zero-filled when it has no activity, the
// tenant's stats sit beside the list, and ?window= picks the span.
func TestHandleListTriggers_Stats(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	busy := mustTrigger(t, deps.triggers, src.ID, nil, "busy")
	quiet := mustTrigger(t, deps.triggers, src.ID, nil, "quiet")
	p50 := 120.0
	deps.repo.triggerStats = map[string]TriggerStats{busy.ID: {Deliveries: 3, Started: 2}}
	deps.repo.tenantStats = TenantTriggerStats{Deliveries: 4, Started: 2, DispatchP50Ms: &p50}

	list := func(query string) map[string]*TriggerStats {
		body := decode[struct {
			Triggers []Trigger          `json:"triggers"`
			Stats    TenantTriggerStats `json:"stats"`
		}](t, call(t, c.handleListTriggers, http.MethodGet, "/triggers"+query, nil, nil))
		if body.Stats.Deliveries != 4 || body.Stats.DispatchP50Ms == nil || *body.Stats.DispatchP50Ms != p50 {
			t.Errorf("%q tenant stats = %+v", query, body.Stats)
		}
		got := map[string]*TriggerStats{}
		for _, tr := range body.Triggers {
			got[tr.ID] = tr.Stats
		}
		return got
	}
	if st := list("")[busy.ID]; st == nil || st.Deliveries != 3 || st.Started != 2 {
		t.Errorf("busy stats = %+v, want the repository's", st)
	}
	for _, tc := range []struct {
		query   string
		window  StatsWindow
		buckets int
	}{
		{"", StatsWindow24h, 13},
		{"?window=24h", StatsWindow24h, 13},
		{"?window=7d", StatsWindow7d, 15},
	} {
		st := list(tc.query)[quiet.ID]
		if st == nil || st.Deliveries != 0 || len(st.Buckets) != tc.buckets || st.LastFiredAt != nil {
			t.Errorf("%q quiet stats = %+v, want zeros and %d empty buckets", tc.query, st, tc.buckets)
		}
		if deps.repo.statsWindow != tc.window {
			t.Errorf("%q repository read %+v, want %s", tc.query, deps.repo.statsWindow, tc.window.Name)
		}
	}
	for _, bad := range []string{"30d", "1h", "24H", "all"} {
		if w := call(t, c.handleListTriggers, http.MethodGet, "/triggers?window="+bad, nil, nil); w.Code != http.StatusBadRequest {
			t.Errorf("window=%s: status %d, want 400", bad, w.Code)
		}
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

// Replaying an event delivery needs events:read: the replay puts the event in a run.
func TestHandleRedeliver_EventNeedsEventsRead(t *testing.T) {
	c, deps, svc := newTestController(t)
	mustEventTrigger(t, deps, eventReq("on-failure"))
	if err := svc.HandleEvent(context.Background(), envelope(t, testTenant, "agents.run.failed.v1", 0, nil, nil)); err != nil {
		t.Fatal(err)
	}
	id := deps.repo.deliveries[0].ID
	redeliver := func() *httptest.ResponseRecorder {
		return call(t, c.handleRedeliver, http.MethodPost, "/trigger-deliveries/"+id+"/redeliver", map[string]string{"id": id}, nil)
	}
	c.eval = &mockEvaluator{allow: false}
	if w := redeliver(); w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "events:read") {
		t.Fatalf("without events:read: status = %d (%s), want 403 naming events:read", w.Code, w.Body.String())
	}
	if n := len(deps.repo.deliveries); n != 1 {
		t.Errorf("deliveries = %d, want the refused replay to record nothing", n)
	}
	c.eval = &mockEvaluator{allow: true}
	if w := redeliver(); w.Code != http.StatusAccepted {
		t.Errorf("with events:read: status = %d (%s), want 202", w.Code, w.Body.String())
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

	w := call(t, c.handleFireTrigger, http.MethodPost, "/trigger-fires/trig-1",
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

	w := call(t, c.handleFireTrigger, http.MethodPost, "/trigger-fires/trig-1",
		map[string]string{"id": "trig-1"}, []byte(`{}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 — an evaluator error must never become an allow", w.Code)
	}
}

// A misconfigured controller (no evaluator wired) must refuse too.
func TestHandleFireTrigger_NilEvaluatorFailsClosed(t *testing.T) {
	c, _, _ := newTestController(t) // built with a nil evaluator

	w := call(t, c.handleFireTrigger, http.MethodPost, "/trigger-fires/trig-1",
		map[string]string{"id": "trig-1"}, []byte(`{}`))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 when no evaluator is configured", w.Code)
	}
}

// An unauthenticated caller never reaches the evaluator.
func TestHandleFireTrigger_NoClaimsDenied(t *testing.T) {
	c, _, _, _ := newFireController(t, true, nil)

	w := invoke(c.handleFireTrigger, newRequest(t, http.MethodPost, "/trigger-fires/trig-1",
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

	w := call(t, c.handleFireTrigger, http.MethodPost, "/trigger-fires/"+trig.ID,
		map[string]string{"id": trig.ID}, []byte(`{}`))

	if w.Code == http.StatusForbidden {
		t.Fatalf("authorization should have passed, got 403")
	}
	if got := eval.scopeAsked(); got != "trigger:"+trig.ID+":fire" {
		t.Errorf("checked scope %q, want the specific trigger's fire scope", got)
	}
}

func TestTriggerReads_CanFire(t *testing.T) {
	tests := []struct {
		name    string
		allow   bool
		evalErr error
		want    bool
	}{
		{"fire scope held", true, nil, true},
		{"fire scope missing", false, nil, false},
		{"evaluator error reads as no", true, errors.New("bundle unavailable"), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _, deps, _ := newFireController(t, tc.allow, tc.evalErr)
			src := mustSource(t, deps.sources)
			trig := mustTrigger(t, deps.triggers, src.ID, nil, "p")

			w := call(t, c.handleListTriggers, http.MethodGet, "/triggers", nil, nil)
			list := decode[struct {
				Triggers []Trigger `json:"triggers"`
			}](t, w)
			if len(list.Triggers) != 1 || list.Triggers[0].CanFire != tc.want {
				t.Errorf("list can_fire = %+v, want %v", list.Triggers, tc.want)
			}

			w = call(t, c.handleGetTrigger, http.MethodGet, "/triggers/"+trig.ID, map[string]string{"id": trig.ID}, nil)
			if got := decode[Trigger](t, w); got.CanFire != tc.want {
				t.Errorf("get can_fire = %v, want %v", got.CanFire, tc.want)
			}
		})
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
	if !slices.Contains(got.Declared, "issue_comment") || got.Category != CategoryWebhook ||
		got.Senders == nil || !slices.Equal(roleValuesOf(got.Senders.Roles), []string{"OWNER", "MEMBER", "COLLABORATOR"}) {
		t.Errorf("response = %+v", got)
	}

	w = call(t, c.handleProviderEventTypes, http.MethodGet, "/trigger-providers/alertmanager/event-types",
		map[string]string{"provider": ProviderAlertmanager}, nil)
	if am := decode[EventTypesResponse](t, w); !slices.Equal(am.Declared, []string{"alert.firing", "alert.resolved"}) ||
		am.Senders != nil {
		t.Errorf("alertmanager = %+v, want its two types and no senders", am)
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
	if _, got := list(""); len(got) != 2 || len(got[0].Payload) == 0 {
		t.Errorf("default listing = %+v, want payloads", got)
	}
	if _, got := list("omit_payload=true"); len(got) != 2 || len(got[0].Payload) != 0 || got[0].PayloadExcerpt != "x" {
		t.Errorf("omit_payload listing = %+v, want excerpt without payload", got)
	}
	for _, q := range []string{"received_after=yesterday", "received_after=2026-13-01T00:00:00Z"} {
		if code, _ := list(q); code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, code)
		}
	}
}

// A routed create the caller lacks authority for is a 403 that lists the scopes.
func TestHandleCreateTrigger_RoutedMissingScopes(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.agents.atSlugs = map[string]string{"at-a": "coder", "at-b": "reviewer", "at-c": "security"}
	deps.eval.deny = map[string]bool{"inference:liquid:d1:use": true}
	src := mustSource(t, deps.sources)

	w := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, routedReq(src.ID, nil))

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"missing_scopes":["inference:liquid:d1:use"]`) {
		t.Errorf("body = %s, want the missing scope listed", w.Body.String())
	}
}

// A decision model outside the catalog is a 400 naming the model, on create and update.
func TestHandleRoutedTrigger_UnknownModel(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.agents.atSlugs = map[string]string{"at-a": "coder", "at-b": "reviewer", "at-c": "security"}
	src := mustSource(t, deps.sources)
	tr := mustRouted(t, deps, src.ID, nil)
	deps.policy.unknownScopes = map[string]bool{"inference:liquid:missing:use": true}

	create := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil,
		routedReq(src.ID, func(r *CreateTriggerRequest) { r.Routing.Model = "liquid/missing" }))
	update := call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/"+tr.ID, map[string]string{"id": tr.ID}, UpdateTriggerRequest{
		Routing: routingReq(func(r *RoutingConfig) { r.Model = "liquid/missing" }),
	})

	for name, w := range map[string]*httptest.ResponseRecorder{"create": create, "update": update} {
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s status = %d, want 400 (body: %s)", name, w.Code, w.Body.String())
		}
		if !strings.Contains(w.Body.String(), `routing.model \"liquid/missing\" is not a model in this tenant's catalog`) {
			t.Errorf("%s body = %s, want the model named", name, w.Body.String())
		}
	}
}

func TestHandleRoutingCandidates(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.agents.agentTypes = []agentsclient.AgentType{{ID: "at-a", Slug: "coder", Name: "Coder", CanRun: true}}

	req := withTenant(newRequest(t, http.MethodGet, "/triggers/routing/candidates", nil, nil))
	req.Header.Set("Authorization", "Bearer caller-token")
	w := invoke(c.handleRoutingCandidates, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body: %s)", w.Code, w.Body.String())
	}
	got := decode[struct {
		AgentTypes []RoutingCandidate `json:"agent_types"`
	}](t, w)
	if len(got.AgentTypes) != 1 || !got.AgentTypes[0].Runnable || got.AgentTypes[0].Name != "Coder" {
		t.Errorf("candidates = %+v", got.AgentTypes)
	}
}

// A failed revocation answers with a fixed message and the trigger id, never the grant ids.
func TestHandleUpdateTrigger_RevokeFailureHidesGrants(t *testing.T) {
	c, deps, _ := newTestController(t)
	deps.agents.atSlugs = map[string]string{"at-a": "coder", "at-b": "reviewer", "at-c": "security"}
	src := mustSource(t, deps.sources)
	tr := mustRouted(t, deps, src.ID, nil)
	deps.policy.failDelete = true

	w := call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/"+tr.ID, map[string]string{"id": tr.ID}, UpdateTriggerRequest{
		Routing: routingReq(func(r *RoutingConfig) { r.Candidates = []string{"at-a", "at-b"} }),
	})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d (body: %s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "previous grant could not be revoked") || !strings.Contains(body, tr.ID) || strings.Contains(body, "grant-") {
		t.Errorf("body = %s, want the fixed message and trigger id only", body)
	}
}

// An event delivery's stored event is returned only to a caller holding events:read.
func TestHandleListDeliveries_EventPayloadNeedsEventsRead(t *testing.T) {
	c, deps, _ := newTestController(t)
	_, _ = deps.repo.InsertEventDelivery(context.Background(), &Delivery{
		ID: "d-ev", TenantID: testTenant, Kind: KindEvent, EventID: "e-1", Status: StatusSkipped,
		Payload: []byte(`{"koraprincipalname":"Ada"}`), PayloadExcerpt: `{"koraprincipalname":"Ada"}`,
	})
	list := func(c *Controller) Delivery {
		body := decode[struct {
			Deliveries []Delivery `json:"deliveries"`
		}](t, call(t, c.handleListDeliveries, http.MethodGet, "/trigger-deliveries", nil, nil))
		if len(body.Deliveries) != 1 {
			t.Fatalf("deliveries = %d, want 1", len(body.Deliveries))
		}
		return body.Deliveries[0]
	}
	_ = deps.repo.InsertDeliveryKey(context.Background(), &DeliveryKey{DeliveryID: "d-ev", TenantID: testTenant, TriggerID: "t-1", Key: "Ada"})
	_ = deps.repo.InsertDeliveryRun(context.Background(), &DeliveryRun{ID: "r-1", TenantID: testTenant, DeliveryID: "d-ev", TriggerID: "t-1",
		Status: RunStatusFailed, Error: new("upstream said Ada")})
	_ = deps.repo.InsertDeliveryRun(context.Background(), &DeliveryRun{ID: "r-2", TenantID: testTenant, DeliveryID: "d-ev", TriggerID: "t-2",
		Status: RunStatusFiltered, Error: new(reasonChainDepth)})
	d := list(c)
	if d.Payload != nil || d.PayloadExcerpt != "" || d.EventID != "e-1" || len(d.CoalesceKeys) != 0 {
		t.Errorf("without events:read: %+v, want the delivery without its event", d)
	}
	if len(d.Runs) != 2 {
		t.Fatalf("runs = %d, want both outcomes listed", len(d.Runs))
	}
	for _, run := range d.Runs {
		if run.Status == RunStatusFailed && run.Error != nil {
			t.Errorf("failed outcome kept its upstream error %q", *run.Error)
		}
		if run.Status == RunStatusFiltered && (run.Error == nil || *run.Error != reasonChainDepth) {
			t.Errorf("filtered outcome lost its reason: %v", run.Error)
		}
	}
	c.eval = &mockEvaluator{allow: true}
	if d := list(c); len(d.Payload) == 0 || d.PayloadExcerpt == "" {
		t.Errorf("with events:read: %+v, want the event", d)
	}
	if stored, _ := deps.repo.GetDelivery(context.Background(), testTenant, "d-ev"); len(stored.Payload) == 0 {
		t.Error("the stored event must stay for redelivery")
	}
}
