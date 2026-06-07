package inferencegw

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── test wiring ───────────────────────────────────────────────────────────────

func newQuotasController(t *testing.T, repo *mockQuotaRepository) *QuotasController {
	t.Helper()
	return &QuotasController{
		repo:   repo,
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

func quotaRequest(t *testing.T, method, path string, body any) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	claims := humanClaimsForHandler("tenant1", "user1")
	return req.WithContext(token.WithClaims(req.Context(), claims))
}

func quotaRequestWithPath(t *testing.T, method, path string, body any, pathValues map[string]string) *http.Request {
	t.Helper()
	req := quotaRequest(t, method, path, body)
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	return req
}

// ── GET /groups ───────────────────────────────────────────────────────────────

func TestHandleListGroups_Empty(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodGet, "/quotas/groups", nil)
	w := httptest.NewRecorder()
	c.handleListGroups(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := resp["data"].([]any)
	if len(data) != 0 {
		t.Errorf("data length = %d, want 0", len(data))
	}
}

func TestHandleListGroups_RepoError(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{listGroupsErr: errTestInternal}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodGet, "/quotas/groups", nil)
	w := httptest.NewRecorder()
	c.handleListGroups(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ── POST /groups ──────────────────────────────────────────────────────────────

func TestHandleCreateGroup_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodPost, "/quotas/groups", createQuotaGroupRequest{Name: "dev-team", MonthlyBudgetUSD: 50.0})
	w := httptest.NewRecorder()
	c.handleCreateGroup(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := resp["data"].(map[string]any)
	if data["name"] != "dev-team" {
		t.Errorf("name = %v, want dev-team", data["name"])
	}
}

func TestHandleCreateGroup_MissingName(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodPost, "/quotas/groups", createQuotaGroupRequest{MonthlyBudgetUSD: 50.0})
	w := httptest.NewRecorder()
	c.handleCreateGroup(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateGroup_ZeroBudget(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodPost, "/quotas/groups", createQuotaGroupRequest{Name: "team", MonthlyBudgetUSD: 0})
	w := httptest.NewRecorder()
	c.handleCreateGroup(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleCreateGroup_RepoError(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{createGroupErr: errTestInternal}
	c := newQuotasController(t, repo)

	req := quotaRequest(t, http.MethodPost, "/quotas/groups", createQuotaGroupRequest{Name: "team", MonthlyBudgetUSD: 10.0})
	w := httptest.NewRecorder()
	c.handleCreateGroup(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}

// ── GET /groups/{id} ──────────────────────────────────────────────────────────

func TestHandleGetGroup_Found(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	repo.groups = []QuotaGroup{{ID: "g1", TenantID: "tenant1", Name: "dev", MonthlyBudgetUSD: 100}}
	c := newQuotasController(t, repo)

	getReq := quotaRequestWithPath(t, http.MethodGet, "/quotas/groups/g1", nil, map[string]string{"id": "g1"})
	w := httptest.NewRecorder()
	c.handleGetGroup(w, getReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
}

func TestHandleGetGroup_NotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodGet, "/quotas/groups/missing", nil, map[string]string{"id": "missing"})
	w := httptest.NewRecorder()
	c.handleGetGroup(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── PUT /groups/{id} ─────────────────────────────────────────────────────────

func TestHandleUpdateGroupBudget_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	repo.groups = []QuotaGroup{{ID: "g1", TenantID: "tenant1", Name: "dev", MonthlyBudgetUSD: 50}}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/groups/g1", updateGroupBudgetRequest{MonthlyBudgetUSD: 100.0}, map[string]string{"id": "g1"})
	w := httptest.NewRecorder()
	c.handleUpdateGroupBudget(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleUpdateGroupBudget_NotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/groups/missing", updateGroupBudgetRequest{MonthlyBudgetUSD: 100.0}, map[string]string{"id": "missing"})
	w := httptest.NewRecorder()
	c.handleUpdateGroupBudget(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── DELETE /groups/{id} ───────────────────────────────────────────────────────

func TestHandleDeleteGroup_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	repo.groups = []QuotaGroup{{ID: "g1", TenantID: "tenant1", Name: "dev", MonthlyBudgetUSD: 50}}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/groups/g1", nil, map[string]string{"id": "g1"})
	w := httptest.NewRecorder()
	c.handleDeleteGroup(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleDeleteGroup_NotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/groups/missing", nil, map[string]string{"id": "missing"})
	w := httptest.NewRecorder()
	c.handleDeleteGroup(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── PUT /groups/{id}/members/{principalId} ────────────────────────────────────

func TestHandleSetMembership_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	repo.groups = []QuotaGroup{{ID: "g1", TenantID: "tenant1", Name: "dev", MonthlyBudgetUSD: 50}}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/groups/g1/members/user42", nil, map[string]string{"id": "g1", "principalId": "user42"})
	w := httptest.NewRecorder()
	c.handleSetMembership(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleSetMembership_GroupNotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/groups/missing/members/user42", nil, map[string]string{"id": "missing", "principalId": "user42"})
	w := httptest.NewRecorder()
	c.handleSetMembership(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── DELETE /groups/{id}/members/{principalId} ─────────────────────────────────

func TestHandleRemoveMembership_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	repo.groups = []QuotaGroup{{ID: "g1", TenantID: "tenant1", Name: "dev", MonthlyBudgetUSD: 50}}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/groups/g1/members/user42", nil, map[string]string{"id": "g1", "principalId": "user42"})
	w := httptest.NewRecorder()
	c.handleRemoveMembership(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleRemoveMembership_GroupNotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/groups/missing/members/user42", nil, map[string]string{"id": "missing", "principalId": "user42"})
	w := httptest.NewRecorder()
	c.handleRemoveMembership(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── GET /principals/{principalId} ─────────────────────────────────────────────

func TestHandleGetPrincipalQuota_Found(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{principalBudget: 75.0, principalHasLimit: true}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodGet, "/quotas/principals/usr_abc", nil, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleGetPrincipalQuota(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	data, _ := resp["data"].(map[string]any)
	if data["monthly_budget_usd"] != 75.0 {
		t.Errorf("monthly_budget_usd = %v, want 75.0", data["monthly_budget_usd"])
	}
}

func TestHandleGetPrincipalQuota_NotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{principalHasLimit: false}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodGet, "/quotas/principals/usr_abc", nil, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleGetPrincipalQuota(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── PUT /principals/{principalId} ─────────────────────────────────────────────

func TestHandleSetPrincipalQuota_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/principals/usr_abc", setPrincipalQuotaRequest{MonthlyBudgetUSD: 25.0}, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleSetPrincipalQuota(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
	if repo.principalBudget != 25.0 {
		t.Errorf("budget = %v, want 25.0", repo.principalBudget)
	}
}

func TestHandleSetPrincipalQuota_ZeroBudget(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/principals/usr_abc", setPrincipalQuotaRequest{MonthlyBudgetUSD: 0}, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleSetPrincipalQuota(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── DELETE /principals/{principalId} ──────────────────────────────────────────

func TestHandleClearPrincipalQuota_OK(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{principalBudget: 50.0, principalHasLimit: true}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/principals/usr_abc", nil, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleClearPrincipalQuota(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body)
	}
}

func TestHandleClearPrincipalQuota_NotFound(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{principalHasLimit: false}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/principals/usr_abc", nil, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleClearPrincipalQuota(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── empty-string path value guards ────────────────────────────────────────────

func TestHandleGetGroup_EmptyID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequest(t, http.MethodGet, "/quotas/groups/", nil) // no path value set
	w := httptest.NewRecorder()
	c.handleGetGroup(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateGroupBudget_EmptyID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequest(t, http.MethodPut, "/quotas/groups/", updateGroupBudgetRequest{MonthlyBudgetUSD: 10})
	w := httptest.NewRecorder()
	c.handleUpdateGroupBudget(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleDeleteGroup_EmptyID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequest(t, http.MethodDelete, "/quotas/groups/", nil)
	w := httptest.NewRecorder()
	c.handleDeleteGroup(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetMembership_EmptyGroupID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequestWithPath(t, http.MethodPut, "/quotas/groups//members/user42", nil, map[string]string{"principalId": "user42"}) // id not set
	w := httptest.NewRecorder()
	c.handleSetMembership(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRemoveMembership_EmptyGroupID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequestWithPath(t, http.MethodDelete, "/quotas/groups//members/user42", nil, map[string]string{"principalId": "user42"}) // id not set
	w := httptest.NewRecorder()
	c.handleRemoveMembership(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetPrincipalQuota_EmptyPrincipalID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequest(t, http.MethodPut, "/quotas/principals/", setPrincipalQuotaRequest{MonthlyBudgetUSD: 10})
	w := httptest.NewRecorder()
	c.handleSetPrincipalQuota(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleClearPrincipalQuota_EmptyPrincipalID(t *testing.T) {
	t.Parallel()
	c := newQuotasController(t, &mockQuotaRepository{})
	req := quotaRequest(t, http.MethodDelete, "/quotas/principals/", nil)
	w := httptest.NewRecorder()
	c.handleClearPrincipalQuota(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── GET /principals/{principalId} — repo error path ───────────────────────────

func TestHandleGetPrincipalQuota_RepoError(t *testing.T) {
	t.Parallel()
	repo := &mockQuotaRepository{getPrincipalErr: errTestInternal}
	c := newQuotasController(t, repo)

	req := quotaRequestWithPath(t, http.MethodGet, "/quotas/principals/usr_abc", nil, map[string]string{"principalId": "usr_abc"})
	w := httptest.NewRecorder()
	c.handleGetPrincipalQuota(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
}
