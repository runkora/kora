package inferencegw

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// newPricingOverrideController returns a CatalogController wired to the given
// mock, with only the pricingCatalog field set — sufficient for the override
// handler tests, which do not exercise auth middleware.
func newPricingOverrideController(t *testing.T, mock *mockPricingCatalogRepository) *CatalogController {
	t.Helper()
	return &CatalogController{
		pricingCatalog: mock,
		logger:         logging.NewLoggerWithOutput(io.Discard),
	}
}

// doPricingRequest fires a request directly at the named handler method,
// injecting the {provider} and {modelId} path values that http.ServeMux would
// normally parse.
func doPricingRequest(t *testing.T, method string, handler http.HandlerFunc, provider, modelID string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}
	req := httptest.NewRequest(method, "/pricing/"+provider+"/"+modelID+"/override", &buf)
	req.SetPathValue("provider", provider)
	req.SetPathValue("modelId", modelID)
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func decodeErrorBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(w.Body).Decode(&m); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	return m
}

// ── handleSetPricingOverride ──────────────────────────────────────────────────

func TestHandleSetPricingOverride_AllFields(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	body := setPricingOverrideRequest{
		InputCostPer1M:      new(3.0),
		OutputCostPer1M:     new(15.0),
		CacheReadCostPer1M:  new(0.3),
		CacheWriteCostPer1M: new(3.75),
	}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock", "us.anthropic.claude-sonnet-4-6-v1:0", body)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if mock.lastSetProvider != ProviderBedrock {
		t.Errorf("provider = %q, want %q", mock.lastSetProvider, ProviderBedrock)
	}
	// normalisePricingModelID strips the region prefix and version suffix so the
	// write targets the same bare ID stored in model_pricing (sourced from LiteLLM).
	if mock.lastSetModelID != "anthropic.claude-sonnet-4-6" {
		t.Errorf("modelID = %q, want %q", mock.lastSetModelID, "anthropic.claude-sonnet-4-6")
	}
	if mock.lastSetInput == nil || *mock.lastSetInput != 3.0 {
		t.Errorf("input = %v, want 3.0", mock.lastSetInput)
	}
	if mock.lastSetOutput == nil || *mock.lastSetOutput != 15.0 {
		t.Errorf("output = %v, want 15.0", mock.lastSetOutput)
	}
}

func TestHandleSetPricingOverride_PartialFields(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	// Only input cost provided — output/cache fields should be nil.
	body := setPricingOverrideRequest{InputCostPer1M: new(5.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "azure", "gpt-4o", body)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if mock.lastSetInput == nil || *mock.lastSetInput != 5.0 {
		t.Errorf("input = %v, want 5.0", mock.lastSetInput)
	}
	if mock.lastSetOutput != nil {
		t.Errorf("output = %v, want nil", mock.lastSetOutput)
	}
	if mock.lastSetCacheRead != nil {
		t.Errorf("cacheRead = %v, want nil", mock.lastSetCacheRead)
	}
	if mock.lastSetCacheWrite != nil {
		t.Errorf("cacheWrite = %v, want nil", mock.lastSetCacheWrite)
	}
}

func TestHandleSetPricingOverride_InvalidProvider(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	body := setPricingOverrideRequest{InputCostPer1M: new(1.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "mistral", "gpt-4o", body)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "invalid_provider" {
		t.Errorf("code = %q, want %q", code, "invalid_provider")
	}
	if mock.lastSetProvider != "" {
		t.Errorf("repo should not be called on invalid provider, got provider = %q", mock.lastSetProvider)
	}
}

func TestHandleSetPricingOverride_AllNilFields(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	// Empty body — no override fields set.
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock", "some-model", setPricingOverrideRequest{})

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "invalid_request" {
		t.Errorf("code = %q, want %q", code, "invalid_request")
	}
}

func TestHandleSetPricingOverride_NotFound(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{setOverrideErr: ErrModelPricingNotFound}
	c := newPricingOverrideController(t, mock)

	body := setPricingOverrideRequest{InputCostPer1M: new(1.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock", "nonexistent-model", body)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "model_pricing_not_found" {
		t.Errorf("code = %q, want %q", code, "model_pricing_not_found")
	}
}

func TestHandleSetPricingOverride_RepoError(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{setOverrideErr: errTestInternal}
	c := newPricingOverrideController(t, mock)

	body := setPricingOverrideRequest{InputCostPer1M: new(1.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock", "some-model", body)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "internal_error" {
		t.Errorf("code = %q, want %q", code, "internal_error")
	}
}

// ── handleClearPricingOverride ────────────────────────────────────────────────

func TestHandleClearPricingOverride_OK(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	w := doPricingRequest(t, http.MethodDelete, c.handleClearPricingOverride, "vertexai", "claude-sonnet-4-6@20250514", nil)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if mock.lastClearProvider != ProviderVertexAI {
		t.Errorf("provider = %q, want %q", mock.lastClearProvider, ProviderVertexAI)
	}
	if mock.lastClearModelID != "claude-sonnet-4-6@20250514" {
		t.Errorf("modelID = %q, want %q", mock.lastClearModelID, "claude-sonnet-4-6@20250514")
	}
}

func TestHandleClearPricingOverride_InvalidProvider(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	w := doPricingRequest(t, http.MethodDelete, c.handleClearPricingOverride, "mistral", "gpt-4o", nil)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "invalid_provider" {
		t.Errorf("code = %q, want %q", code, "invalid_provider")
	}
	if mock.lastClearProvider != "" {
		t.Errorf("repo should not be called on invalid provider, got provider = %q", mock.lastClearProvider)
	}
}

func TestHandleClearPricingOverride_NotFound(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{clearOverrideErr: ErrModelPricingNotFound}
	c := newPricingOverrideController(t, mock)

	w := doPricingRequest(t, http.MethodDelete, c.handleClearPricingOverride, "bedrock", "nonexistent-model", nil)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "model_pricing_not_found" {
		t.Errorf("code = %q, want %q", code, "model_pricing_not_found")
	}
}

func TestHandleClearPricingOverride_RepoError(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{clearOverrideErr: errTestInternal}
	c := newPricingOverrideController(t, mock)

	w := doPricingRequest(t, http.MethodDelete, c.handleClearPricingOverride, "bedrock", "some-model", nil)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	errBody := decodeErrorBody(t, w)
	if code, _ := errBody["code"].(string); code != "internal_error" {
		t.Errorf("code = %q, want %q", code, "internal_error")
	}
}

func TestHandleSetPricingOverride_ZeroValueField(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	// 0.0 is a valid override (e.g. a free model). The all-nil guard must not
	// reject a pointer to zero.
	body := setPricingOverrideRequest{InputCostPer1M: new(0.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock", "some-free-model", body)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if mock.lastSetInput == nil || *mock.lastSetInput != 0.0 {
		t.Errorf("input = %v, want 0.0", mock.lastSetInput)
	}
}

// ── alias provider normalisation ──────────────────────────────────────────────

// TestHandleSetPricingOverride_AliasNormalisedToCanonical verifies that a
// Bedrock alias (e.g. "bedrock:us") is normalised to the canonical "bedrock"
// key before the repository is called — alias rows must not be stored in
// model_pricing.
func TestHandleSetPricingOverride_AliasNormalisedToCanonical(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	body := setPricingOverrideRequest{InputCostPer1M: new(3.0)}
	w := doPricingRequest(t, http.MethodPut, c.handleSetPricingOverride, "bedrock:us", "us.anthropic.claude-sonnet-4-6-v1:0", body)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if mock.lastSetProvider != ProviderBedrock {
		t.Errorf("provider = %q, want %q (alias must be normalised)", mock.lastSetProvider, ProviderBedrock)
	}
}

func TestHandleClearPricingOverride_AliasNormalisedToCanonical(t *testing.T) {
	t.Parallel()
	mock := &mockPricingCatalogRepository{}
	c := newPricingOverrideController(t, mock)

	w := doPricingRequest(t, http.MethodDelete, c.handleClearPricingOverride, "bedrock:aus", "anthropic.claude-3-haiku-20240307-v1:0", nil)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if mock.lastClearProvider != ProviderBedrock {
		t.Errorf("provider = %q, want %q (alias must be normalised)", mock.lastClearProvider, ProviderBedrock)
	}
}
