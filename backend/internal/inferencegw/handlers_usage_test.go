package inferencegw

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

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

func TestHandleListUsage_WithRecords_ReturnsDataAndHasNextPage(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.records = []InferenceUsageRecord{
		{
			ID:          "r1",
			TenantID:    "t1",
			PrincipalID: "u1",
			CreatedAt:   time.Date(2024, 6, 1, 12, 0, 0, 123456000, time.UTC),
		},
	}
	repo.hasNextPage = true
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	resp := decodeUsageResponse(t, w)
	if len(resp.Data) != 1 {
		t.Errorf("Data length: got %d, want 1", len(resp.Data))
	}
	if !resp.HasNextPage {
		t.Error("HasNextPage: got false, want true")
	}
	if resp.NextCursor == nil {
		t.Error("NextCursor: got nil, want non-nil when HasNextPage is true")
	}
	// Verify the cursor round-trips through parseCursorParam without error.
	if resp.NextCursor != nil {
		f := InferenceUsageFilter{}
		apiErr, err := parseCursorParam(*resp.NextCursor, &f)
		if err != nil || apiErr != nil {
			t.Errorf("NextCursor %q does not round-trip: apiErr=%v err=%v", *resp.NextCursor, apiErr, err)
		}
		if f.CursorTime == nil || f.CursorID == nil {
			t.Errorf("NextCursor round-trip: CursorTime=%v CursorID=%v, want both non-nil", f.CursorTime, f.CursorID)
		}
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
	if resp.Limit != defaultUsageLimit {
		t.Errorf("Limit: got %d, want %d (default)", resp.Limit, defaultUsageLimit)
	}
}

func TestHandleListUsage_CustomLimit(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"limit": "10",
	})

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeUsageResponse(t, w)
	if resp.Limit != 10 {
		t.Errorf("Limit: got %d, want 10", resp.Limit)
	}
}

func TestHandleListUsage_NoNextPage_NextCursorIsNil(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	repo.hasNextPage = false
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), nil)

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusOK)
	}
	resp := decodeUsageResponse(t, w)
	if resp.HasNextPage {
		t.Error("HasNextPage: got true, want false")
	}
	if resp.NextCursor != nil {
		t.Errorf("NextCursor: got %q, want nil when HasNextPage is false", *resp.NextCursor)
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

func TestHandleListUsage_OverMaxLimit_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"limit": "5000",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusBadRequest, w.Body)
	}
}

func TestHandleListUsage_InvalidCursor_Returns400(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"cursor": "not-a-valid-cursor",
	})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleListUsage_ValidCursor_PassedToRepo(t *testing.T) {
	t.Parallel()
	repo := newMockUsageRepo()
	c := newUsageController(t, repo)

	// A well-formed cursor: RFC3339Nano timestamp + "|" + UUID.
	cursor := "2024-01-15T10:30:00.123456Z|550e8400-e29b-41d4-a716-446655440000"
	w := getUsage(t, c, humanClaimsForHandler("t1", "u1"), map[string]string{
		"cursor": cursor,
	})

	if w.Code != http.StatusOK {
		t.Errorf("status: got %d, want %d (body: %s)", w.Code, http.StatusOK, w.Body)
	}
	f := repo.LastFilter()
	if f.CursorTime == nil {
		t.Error("CursorTime: got nil, want non-nil after valid cursor")
	}
	if f.CursorID == nil || *f.CursorID != "550e8400-e29b-41d4-a716-446655440000" {
		t.Errorf("CursorID: got %v, want %q", f.CursorID, "550e8400-e29b-41d4-a716-446655440000")
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

// ── GET /usage/budget ──────────────────────────────────────────────────────────

func newPersonalBudgetController(t *testing.T, eval *stubEvaluator, counter SpendCounter) *Controller {
	t.Helper()
	svc := &Service{
		policy:        eval,
		usage:         newMockUsageRepo(),
		budgetCounter: counter,
		logger:        logging.NewLoggerWithOutput(io.Discard),
	}
	return &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}
}

func TestHandleGetPersonalBudget_NoLimit(t *testing.T) {
	t.Parallel()
	// OPA allows (no limit configured for this principal).
	eval := &stubEvaluator{budgetHasLimit: false}
	counter := &mockSpendCounter{spend: 0}
	c := newPersonalBudgetController(t, eval, counter)

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil)
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "u1")))
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var status BudgetStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.SpendLimitReached {
		t.Error("SpendLimitReached = true, want false")
	}
	if status.HasLimit {
		t.Error("HasLimit = true, want false (no budget configured)")
	}
	if status.CurrentSpendUSD != 0 {
		t.Errorf("CurrentSpendUSD = %v, want 0", status.CurrentSpendUSD)
	}
}

func TestHandleGetPersonalBudget_UnderLimit(t *testing.T) {
	t.Parallel()
	// OPA allows — spend is below the configured limit; limit details populated.
	eval := &stubEvaluator{budgetHasLimit: true, budgetEffectiveUSD: 100}
	counter := &mockSpendCounter{spend: 42.5}
	c := newPersonalBudgetController(t, eval, counter)

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil)
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "u1")))
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var status BudgetStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.CurrentSpendUSD != 42.5 {
		t.Errorf("CurrentSpendUSD = %v, want 42.5", status.CurrentSpendUSD)
	}
	if status.SpendLimitReached {
		t.Error("SpendLimitReached = true, want false")
	}
	if !status.HasLimit {
		t.Error("HasLimit = false, want true")
	}
	if status.MonthlyBudgetUSD != 100 {
		t.Errorf("MonthlyBudgetUSD = %v, want 100", status.MonthlyBudgetUSD)
	}
}

func TestHandleGetPersonalBudget_LimitReached(t *testing.T) {
	t.Parallel()
	// OPA denies — spend has hit or exceeded the limit.
	deny := false
	eval := &stubEvaluator{checkAllow: &deny, budgetHasLimit: true, budgetEffectiveUSD: 50}
	counter := &mockSpendCounter{spend: 50.0}
	c := newPersonalBudgetController(t, eval, counter)

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil)
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "u1")))
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var status BudgetStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !status.SpendLimitReached {
		t.Error("SpendLimitReached = false, want true")
	}
	if status.CurrentSpendUSD != 50.0 {
		t.Errorf("CurrentSpendUSD = %v, want 50.0", status.CurrentSpendUSD)
	}
	if !status.HasLimit {
		t.Error("HasLimit = false, want true")
	}
	if status.MonthlyBudgetUSD != 50 {
		t.Errorf("MonthlyBudgetUSD = %v, want 50", status.MonthlyBudgetUSD)
	}
}

func TestHandleGetPersonalBudget_SpendCounterError(t *testing.T) {
	t.Parallel()
	// Counter error → GetPersonalBudget fails open: 200 with empty BudgetStatus.
	eval := &stubEvaluator{}
	counter := &mockSpendCounter{getSpendErr: errTestInternal}
	c := newPersonalBudgetController(t, eval, counter)

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil)
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "u1")))
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (fail-open on counter error)", w.Code, http.StatusOK)
	}
	var status BudgetStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.HasLimit {
		t.Error("HasLimit = true, want false (fail-open → no limit info)")
	}
	if status.SpendLimitReached {
		t.Error("SpendLimitReached = true, want false (fail-open)")
	}
	if status.CurrentSpendUSD != 0 {
		t.Errorf("CurrentSpendUSD = %v, want 0 (fail-open)", status.CurrentSpendUSD)
	}
}

func TestHandleGetPersonalBudget_MissingClaims(t *testing.T) {
	t.Parallel()
	c := newPersonalBudgetController(t, &stubEvaluator{}, &mockSpendCounter{})

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil) // no claims
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestHandleGetPersonalBudget_NilCounter(t *testing.T) {
	t.Parallel()
	// When no spend counter is wired (nil), GetPersonalBudget returns an empty
	// BudgetStatus with no error — effectively "no budget configured".
	c := newPersonalBudgetController(t, &stubEvaluator{}, nil)

	req := httptest.NewRequest(http.MethodGet, "/usage/budget", nil)
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "u1")))
	w := httptest.NewRecorder()
	c.handleGetPersonalBudget(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var status BudgetStatus
	if err := json.NewDecoder(w.Body).Decode(&status); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if status.HasLimit {
		t.Error("HasLimit = true, want false (nil counter → no budget)")
	}
	if status.SpendLimitReached {
		t.Error("SpendLimitReached = true, want false (nil counter → no budget)")
	}
}
