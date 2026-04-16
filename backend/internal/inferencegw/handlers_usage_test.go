package inferencegw

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── test wiring ───────────────────────────────────────────────────────────────

// newUsageController builds a Controller wired to a Service backed by repo.
// verifier is nil — handleListUsage does not verify tokens itself; it reads
// claims already injected by AuthMiddleware (simulated via ctxWithClaims).
func newUsageController(t *testing.T, repo *mockInferenceUsageRepository) *Controller {
	t.Helper()
	svc := newTestUsageService(t, repo)
	return &Controller{
		svc:    svc,
		logger: logging.NewLoggerWithOutput(io.Discard),
	}
}

// getUsage performs a GET /usage with the given query params and JWT claims
// already injected into the request context (simulating AuthMiddleware).
func getUsage(
	t *testing.T,
	c *Controller,
	claims *token.PlatformClaims,
	params map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	q := url.Values{}
	for k, v := range params {
		q.Set(k, v)
	}
	req := httptest.NewRequest(http.MethodGet, "/usage?"+q.Encode(), nil)
	// Simulate what AuthMiddleware does: inject verified claims into context.
	req = req.WithContext(token.WithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	c.handleListUsage(w, req)
	return w
}

// decodeUsageResponse decodes the listUsageResponse JSON body from the recorder.
func decodeUsageResponse(t *testing.T, w *httptest.ResponseRecorder) listUsageResponse {
	t.Helper()
	var resp listUsageResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
	return resp
}

// decodeAPIError decodes a generic error envelope for asserting 4xx/5xx shapes.
func decodeAPIError(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return body
}

// humanClaimsForHandler returns minimal PAT-agnostic claims for handler tests.
func humanClaimsForHandler(tenantID, principalID string) *token.PlatformClaims {
	return makeClaims(tenantID, principalID, token.PrincipalTypeHuman, "")
}

// ── GET /usage — success paths ────────────────────────────────────────────────

func TestHandleListUsage_NoRecords_Returns200WithEmptySlice(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	resp := decodeUsageResponse(t, w)
	if resp.Data == nil {
		t.Error("Data: got nil, want empty slice (never null in JSON)")
	}
	if len(resp.Data) != 0 {
		t.Errorf("Data: got %d records, want 0", len(resp.Data))
	}
}

func TestHandleListUsage_WithRecords_ReturnsTotalAndData(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.records = []InferenceUsageRecord{
		{ID: "r1", TenantID: "t1", PrincipalID: "u1"},
	}
	repo.total = 99
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	resp := decodeUsageResponse(t, w)
	if len(resp.Data) != 1 {
		t.Errorf("Data length: got %d, want 1", len(resp.Data))
	}
	if resp.Total != 99 {
		t.Errorf("Total: got %d, want 99", resp.Total)
	}
}

func TestHandleListUsage_DefaultPagination(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeUsageResponse(t, w)
	if resp.Limit != 50 {
		t.Errorf("Limit: got %d, want 50 (default)", resp.Limit)
	}
	if resp.Offset != 0 {
		t.Errorf("Offset: got %d, want 0 (default)", resp.Offset)
	}
}

func TestHandleListUsage_CustomLimitAndOffset(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"limit":  "10",
		"offset": "20",
	})

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeUsageResponse(t, w)
	if resp.Limit != 10 {
		t.Errorf("Limit: got %d, want 10", resp.Limit)
	}
	if resp.Offset != 20 {
		t.Errorf("Offset: got %d, want 20", resp.Offset)
	}
}

func TestHandleListUsage_TokenIDFilter_PassedToRepo(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	const patID = "pat-uuid-xyz"
	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"token_id": patID,
	})

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	if repo.lastFilter.TokenID == nil || *repo.lastFilter.TokenID != patID {
		t.Errorf("TokenID filter: got %v, want pointer to %q", repo.lastFilter.TokenID, patID)
	}
}

// ── GET /usage — auth / claim errors ──────────────────────────────────────────

func TestHandleListUsage_NoClaims_Returns401(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	// No claims injected — simulates AuthMiddleware not running or failing.
	req := httptest.NewRequest(http.MethodGet, "/usage", nil)
	w := httptest.NewRecorder()
	c.handleListUsage(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusUnauthorized, w.Body)
	}
}

// ── GET /usage — bad query parameter validation ────────────────────────────────

func TestHandleListUsage_InvalidFrom_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"from": "not-a-timestamp",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_InvalidTo_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"to": "not-a-timestamp",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_InvalidLimit_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"limit": "banana",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_ZeroLimit_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"limit": "0",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_NegativeOffset_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"offset": "-1",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── GET /usage — invalid enum parameters ─────────────────────────────────────

func TestHandleListUsage_InvalidProvider_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"provider": "invalid-provider",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_InvalidAPIDialect_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"api_dialect": "invalid-dialect",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_InvalidStatus_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"status": "invalid-status",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ── GET /usage — repo / service errors ────────────────────────────────────────

func TestHandleListUsage_RepoError_Returns500(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.listErr = errors.New("postgres went away")
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusInternalServerError, w.Body)
	}
}

// ── buildFilter — TokenID predicate ──────────────────────────────────────────

// These tests extend the existing buildFilter suite in handlers_openai_test.go.

func TestBuildFilter_TokenID(t *testing.T) {
	t.Parallel()
	tid := "pat-uuid-abc"
	where, args := buildFilter(InferenceUsageFilter{TokenID: &tid})
	found := false
	for _, a := range args {
		if a == "pat-uuid-abc" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("args %v should contain %q", args, tid)
	}
	if where == "" {
		t.Error("WHERE clause should be non-empty when TokenID is set")
	}
}

func TestBuildFilter_TokenIDWithTenantAndPrincipal(t *testing.T) {
	t.Parallel()
	tid := "t1"
	pid := "u1"
	tokID := "pat-uuid-xyz"
	where, args := buildFilter(InferenceUsageFilter{
		TenantID:    tid,
		PrincipalID: &pid,
		TokenID:     &tokID,
	})
	if len(args) != 3 {
		t.Errorf("expected 3 args for tenant+principal+token_id, got %d: %v", len(args), args)
	}
	_ = where
}
