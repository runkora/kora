package egressgw

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── fixtures ──────────────────────────────────────────────────────────────────

func newTestAdminController(t *testing.T) (*AdminController, *memConnRepo) {
	t.Helper()
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	logger := logging.NewLoggerWithOutput(io.Discard)
	// verifier and eval are not exercised by direct handler calls — Middleware()
	// is not in the call path when handlers are invoked directly. Pass nil.
	return NewAdminController(repo, nil, nil, "", logger), repo
}

// withTenantClaims attaches verified claims for the given tenant onto the
// request — equivalent to AuthMiddleware having already run.
func withTenantClaims(t *testing.T, r *http.Request, tenant string) *http.Request {
	t.Helper()
	c := claimsFor(tenant, "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-9")
	return r.WithContext(token.WithClaims(r.Context(), c))
}

// validCreateRequest returns a complete connection-create payload.
func validCreateRequest() connectionRequest {
	return connectionRequest{
		Name:         "github",
		Adapter:      credentials.AdapterGitHubApp,
		BaseHost:     "github.com",
		SecretRef:    "acme/github/app.pem",
		ScopeCeiling: []string{"repo:read", "repo:write"},
		Config:       json.RawMessage(`{"app_id":"1","installation_id":42}`),
	}
}

func postJSON(t *testing.T, body any) *http.Request {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	return req
}

// ── handleCreate ──────────────────────────────────────────────────────────────

func TestHandleCreate_HappyPath(t *testing.T) {
	c, repo := newTestAdminController(t)

	req := withTenantClaims(t, postJSON(t, validCreateRequest()), "acme")
	w := httptest.NewRecorder()
	c.handleCreate(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body = %s", w.Code, w.Body.String())
	}

	var got credentials.Connection
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID == "" {
		t.Error("response missing ID — repo should have generated one")
	}
	if got.TenantID != "acme" {
		t.Errorf("TenantID = %q, want acme (taken from claims)", got.TenantID)
	}

	// The repo holds it under (tenant, name) and matches what the response showed.
	stored, err := repo.GetByName(context.Background(), "acme", "github")
	if err != nil {
		t.Fatalf("repo lookup: %v", err)
	}
	if stored.ID != got.ID {
		t.Errorf("stored ID %q != response ID %q", stored.ID, got.ID)
	}
}

func TestHandleCreate_NoClaims_Returns401(t *testing.T) {
	c, _ := newTestAdminController(t)
	req := postJSON(t, validCreateRequest()) // no claims attached
	w := httptest.NewRecorder()
	c.handleCreate(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleCreate_TenantTakenFromClaims_NotBody(t *testing.T) {
	c, repo := newTestAdminController(t)

	// Body has no TenantID field (the request type doesn't expose one), and
	// even if a hostile caller stuffed one in, it would be ignored. Verify
	// the row is filed under the claims tenant, not anything from the body.
	req := withTenantClaims(t, postJSON(t, validCreateRequest()), "acme")
	w := httptest.NewRecorder()
	c.handleCreate(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d", w.Code)
	}

	if _, err := repo.GetByName(context.Background(), "acme", "github"); err != nil {
		t.Errorf("not stored under claims tenant: %v", err)
	}
	if _, err := repo.GetByName(context.Background(), "other-tenant", "github"); err == nil {
		t.Error("stored under unexpected tenant — tenant must come from claims, not the body")
	}
}

func TestHandleCreate_ValidationFailures(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*connectionRequest)
		wantMsg string
	}{
		{"missing_name", func(r *connectionRequest) { r.Name = "" }, "name"},
		{"missing_adapter", func(r *connectionRequest) { r.Adapter = "" }, "adapter"},
		{"missing_base_host", func(r *connectionRequest) { r.BaseHost = "" }, "base_host"},
		{"missing_secret_ref", func(r *connectionRequest) { r.SecretRef = "" }, "secret_ref"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestAdminController(t)
			body := validCreateRequest()
			tc.mutate(&body)

			req := withTenantClaims(t, postJSON(t, body), "acme")
			w := httptest.NewRecorder()
			c.handleCreate(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.wantMsg) {
				t.Errorf("body should mention %q; got %s", tc.wantMsg, w.Body.String())
			}
		})
	}
}

func TestHandleCreate_MalformedJSON_Returns400(t *testing.T) {
	c, _ := newTestAdminController(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/connections", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleCreate(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

// ── handleList ────────────────────────────────────────────────────────────────

func TestHandleList_ReturnsTenantConnections(t *testing.T) {
	c, repo := newTestAdminController(t)

	// Two connections for acme, one for another tenant.
	for _, conn := range []*credentials.Connection{
		{ID: "1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp},
		{ID: "2", TenantID: "acme", Name: "k8s", Adapter: credentials.AdapterKubernetesBearer},
		{ID: "3", TenantID: "other", Name: "github", Adapter: credentials.AdapterGitHubApp},
	} {
		_ = repo.Create(context.Background(), conn)
	}

	req := withTenantClaims(t, httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil), "acme")
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var resp struct {
		Connections []*credentials.Connection `json:"connections"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Connections) != 2 {
		t.Errorf("got %d connections, want 2 (other tenant must not appear)", len(resp.Connections))
	}
	for _, conn := range resp.Connections {
		if conn.TenantID != "acme" {
			t.Errorf("returned connection from another tenant: %+v", conn)
		}
	}
}

func TestHandleList_EmptyTenant_ReturnsEmptyArray(t *testing.T) {
	c, _ := newTestAdminController(t)

	req := withTenantClaims(t, httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil), "acme")
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	// Body must contain "connections":[] (not null) so clients always see an array.
	if !strings.Contains(w.Body.String(), `"connections":[]`) {
		t.Errorf("body should serialize empty connections as []; got %s", w.Body.String())
	}
}

func TestHandleList_NoClaims_Returns401(t *testing.T) {
	c, _ := newTestAdminController(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections", nil)
	w := httptest.NewRecorder()
	c.handleList(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

// ── handleGet ─────────────────────────────────────────────────────────────────

func TestHandleGet_HappyPath(t *testing.T) {
	c, repo := newTestAdminController(t)
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/conn-1", nil)
	req.SetPathValue("id", "conn-1")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleGet(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var got credentials.Connection
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "conn-1" || got.Name != "github" {
		t.Errorf("got %+v, want id=conn-1 name=github", got)
	}
}

func TestHandleGet_NotFound_Returns404(t *testing.T) {
	c, _ := newTestAdminController(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/unknown", nil)
	req.SetPathValue("id", "unknown")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleGet(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleGet_OtherTenantsConnection_Returns404(t *testing.T) {
	c, repo := newTestAdminController(t)
	// Connection belongs to "other"; caller is "acme" — must look like 404.
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-other", TenantID: "other", Name: "github", Adapter: credentials.AdapterGitHubApp,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/connections/conn-other", nil)
	req.SetPathValue("id", "conn-other")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleGet(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 — cross-tenant lookup must not 200", w.Code)
	}
}

// ── handleDelete ──────────────────────────────────────────────────────────────

func TestHandleDelete_HappyPath(t *testing.T) {
	c, repo := newTestAdminController(t)
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-1", TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/conn-1", nil)
	req.SetPathValue("id", "conn-1")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleDelete(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want 204", w.Code)
	}
	if _, err := repo.GetByID(context.Background(), "conn-1", "acme"); err == nil {
		t.Error("connection still present after delete")
	}
}

func TestHandleDelete_NotFound_Returns404(t *testing.T) {
	c, _ := newTestAdminController(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/unknown", nil)
	req.SetPathValue("id", "unknown")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleDelete(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleDelete_OtherTenantsConnection_Returns404(t *testing.T) {
	c, repo := newTestAdminController(t)
	_ = repo.Create(context.Background(), &credentials.Connection{
		ID: "conn-other", TenantID: "other", Name: "github", Adapter: credentials.AdapterGitHubApp,
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/connections/conn-other", nil)
	req.SetPathValue("id", "conn-other")
	req = withTenantClaims(t, req, "acme")
	w := httptest.NewRecorder()
	c.handleDelete(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 — cross-tenant delete must not succeed", w.Code)
	}

	// Verify the connection is still present in the other tenant.
	if _, err := repo.GetByID(context.Background(), "conn-other", "other"); err != nil {
		t.Errorf("connection from other tenant was deleted via cross-tenant request: %v", err)
	}
}
