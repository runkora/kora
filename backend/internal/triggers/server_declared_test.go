package triggers

import (
	"context"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/declarative/v1alpha1"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// errorCode is the code of a recorded error response.
func errorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	return decode[struct {
		Code string `json:"code"`
	}](t, w).Code
}

func TestHandlers_DeclaredRowsAreReadOnly(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := projectedSource(t, deps, "uid-s")
	res := mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeProjected)
	tr := &Trigger{ID: res.ID}
	srcPath, trPath := map[string]string{"id": src.ID}, map[string]string{"id": tr.ID}

	for name, w := range map[string]*httptest.ResponseRecorder{
		"source update":  call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/x", srcPath, UpdateSourceRequest{RotateSecret: true}),
		"source delete":  call(t, c.handleDeleteSource, http.MethodDelete, "/trigger-sources/x", srcPath, nil),
		"trigger update": call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/x", trPath, UpdateTriggerRequest{PromptTemplate: new("edited")}),
		"trigger delete": call(t, c.handleDeleteTrigger, http.MethodDelete, "/triggers/x", trPath, nil),
	} {
		if got := w.Code; got != http.StatusConflict {
			t.Errorf("%s: status = %d, want 409", name, got)
		}
		if code := errorCode(t, w); code != "managed_by_resource" {
			t.Errorf("%s: code = %q, want managed_by_resource", name, code)
		}
	}

	// Clearing an auto-pause stays available on a declared trigger.
	paused := time.Now()
	deps.repo.triggers[tr.ID].Enabled = false
	deps.repo.triggers[tr.ID].AutoPausedAt = &paused
	w := call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/x", trPath, UpdateTriggerRequest{Enabled: new(true)})
	if w.Code != http.StatusOK {
		t.Fatalf("clear auto-pause: status = %d (body: %s)", w.Code, w.Body.String())
	}
	if got := deps.repo.triggers[tr.ID]; !got.Enabled || got.AutoPausedAt != nil {
		t.Errorf("trigger after clearing the pause = %+v", got)
	}
}

func TestHandlers_LockedTenant(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := mustSource(t, deps.sources)
	tr, err := deps.triggers.CreateTrigger(context.Background(), testTenant, nil, CreateTriggerRequest{
		SourceID: src.ID, Name: "rev", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	locked := openTenants{locked: map[string]bool{testTenant: true}}
	deps.sources.tenants, deps.triggers.tenants = locked, locked
	srcPath, trPath := map[string]string{"id": src.ID}, map[string]string{"id": tr.ID}

	for name, w := range map[string]*httptest.ResponseRecorder{
		"source create":  call(t, c.handleCreateSource, http.MethodPost, "/trigger-sources", nil, CreateSourceRequest{Name: "gh2", Provider: ProviderGitHub}),
		"source disable": call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/x", srcPath, UpdateSourceRequest{Enabled: new(false)}),
		"source delete":  call(t, c.handleDeleteSource, http.MethodDelete, "/trigger-sources/x", srcPath, nil),
		"trigger create": call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, CreateTriggerRequest{SourceID: src.ID, Name: "rev2", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p"}),
		"trigger update": call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/x", trPath, UpdateTriggerRequest{Name: new("renamed")}),
		"trigger delete": call(t, c.handleDeleteTrigger, http.MethodDelete, "/triggers/x", trPath, nil),
	} {
		if got := w.Code; got != http.StatusForbidden {
			t.Errorf("%s: status = %d, want 403", name, got)
		}
		if code := errorCode(t, w); code != "api_authoring_disabled" {
			t.Errorf("%s: code = %q, want api_authoring_disabled", name, code)
		}
	}

	// Secret rotation on an API source stays available.
	w := call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/x", srcPath, UpdateSourceRequest{RotateSecret: true})
	if w.Code != http.StatusOK {
		t.Errorf("rotate in a locked tenant: status = %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandlers_APITriggerCannotTakeDeclaredName(t *testing.T) {
	c, deps, _ := newTestController(t)
	ctx := context.Background()
	src := projectedSource(t, deps, "uid-s")
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeProjected)
	apiSrc := mustSource(t, deps.sources)

	w := call(t, c.handleCreateTrigger, http.MethodPost, "/triggers", nil, CreateTriggerRequest{
		SourceID: apiSrc.ID, Name: "review-on-pr", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("create with a declared name: status = %d (body: %s)", w.Code, w.Body.String())
	}

	other, err := deps.triggers.CreateTrigger(ctx, testTenant, nil, CreateTriggerRequest{
		SourceID: apiSrc.ID, Name: "mine", AgentTypeID: "at-1", EventType: "pull_request", PromptTemplate: "p",
	})
	if err != nil {
		t.Fatalf("seed API trigger: %v", err)
	}
	w = call(t, c.handleUpdateTrigger, http.MethodPatch, "/triggers/x", map[string]string{"id": other.ID}, UpdateTriggerRequest{Name: new("review-on-pr")})
	if w.Code != http.StatusConflict {
		t.Errorf("rename to a declared name: status = %d (body: %s)", w.Code, w.Body.String())
	}
}

func TestHandlers_ListsCarrySource(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := projectedSource(t, deps, "uid-s")
	mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeProjected)

	sources := decode[struct {
		Sources []map[string]any `json:"sources"`
	}](t, call(t, c.handleListSources, http.MethodGet, "/trigger-sources", nil, nil))
	if len(sources.Sources) != 1 || sources.Sources[0]["source"] != declarative.SourceCRD {
		t.Errorf("sources = %v", sources.Sources)
	}
	triggers := decode[struct {
		Triggers []map[string]any `json:"triggers"`
	}](t, call(t, c.handleListTriggers, http.MethodGet, "/triggers", nil, nil))
	if len(triggers.Triggers) != 1 || triggers.Triggers[0]["source"] != declarative.SourceCRD {
		t.Errorf("triggers = %v", triggers.Triggers)
	}
}

// Manual fires stay available on declared triggers and in locked tenants.
func TestFireDeclaredTriggerInLockedTenant(t *testing.T) {
	svc, deps := newTestService(t)
	spec := v1alpha1.TriggerSpec{Kind: KindManual, Agent: "code-reviewer", PromptTemplate: "p", Enabled: true}
	tr := mustApply(t, deps.triggers, triggerItem(t, "uid-t", spec, agentRefs("")), declarative.OutcomeProjected)
	locked := openTenants{locked: map[string]bool{testTenant: true}}
	deps.sources.tenants, deps.triggers.tenants = locked, locked
	if _, err := svc.FireTrigger(context.Background(), testTenant, tr.ID, "user-1", FireTriggerRequest{}); err != nil {
		t.Errorf("fire: %v", err)
	}
}

// No response carries a declared source's signing secret or its ciphertext.
func TestHandlers_DeclaredSecretNeverReturned(t *testing.T) {
	c, deps, _ := newTestController(t)
	stored := projectedSource(t, deps, "uid-s")
	src := stored
	path := map[string]string{"id": src.ID}
	for name, w := range map[string]*httptest.ResponseRecorder{
		"list":   call(t, c.handleListSources, http.MethodGet, "/trigger-sources", nil, nil),
		"get":    call(t, c.handleGetSource, http.MethodGet, "/trigger-sources/x", path, nil),
		"rotate": call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/x", path, UpdateSourceRequest{RotateSecret: true}),
	} {
		body := w.Body.String()
		if strings.Contains(body, "s3cret") || strings.Contains(body, stored.SecretCiphertext) {
			t.Errorf("%s response carries the signing secret: %s", name, body)
		}
	}
}

// In a locked tenant a declared item reports the lock first, as agents and roles do.
func TestHandlers_LockedTenantBeforeManaged(t *testing.T) {
	c, deps, _ := newTestController(t)
	src := projectedSource(t, deps, "uid-s")
	res := mustApply(t, deps.triggers, triggerItem(t, "uid-t", webhookSpec("code-reviewer"), agentRefs(src.ID)), declarative.OutcomeProjected)
	locked := openTenants{locked: map[string]bool{testTenant: true}}
	deps.sources.tenants, deps.triggers.tenants = locked, locked

	for name, w := range map[string]*httptest.ResponseRecorder{
		"source rotate":  call(t, c.handleUpdateSource, http.MethodPatch, "/trigger-sources/x", map[string]string{"id": src.ID}, UpdateSourceRequest{RotateSecret: true}),
		"source delete":  call(t, c.handleDeleteSource, http.MethodDelete, "/trigger-sources/x", map[string]string{"id": src.ID}, nil),
		"trigger delete": call(t, c.handleDeleteTrigger, http.MethodDelete, "/triggers/x", map[string]string{"id": res.ID}, nil),
	} {
		if code := errorCode(t, w); w.Code != http.StatusForbidden || code != "api_authoring_disabled" {
			t.Errorf("%s: %d %s, want 403 api_authoring_disabled", name, w.Code, code)
		}
	}
}
