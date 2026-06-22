package inferencegw

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── GET /analytics/agents/{agentTypeId} ───────────────────────────────────────

func TestHandleAnalyticsAgent_Returns200WithKPIs(t *testing.T) {
	t.Parallel()
	usage := newMockUsageRepo()
	usage.agentAnalyticsResp = AgentAnalyticsKPIs{
		TotalTokens:         1000,
		PromptTokens:        600,
		CompletionTokens:    200,
		CacheReadTokens:     150,
		CacheCreationTokens: 50,
		EstimatedCostUSD:    0.0123,
	}
	svc := &Service{usage: usage, logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/agents/agent-type-1", nil)
	req.SetPathValue("agentTypeId", "agent-type-1")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("tenant-1", "user-1")))
	w := httptest.NewRecorder()
	c.handleAnalyticsAgent(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var got AgentAnalyticsKPIs
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.TotalTokens != 1000 {
		t.Errorf("TotalTokens = %d, want 1000", got.TotalTokens)
	}
	if got.PromptTokens != 600 {
		t.Errorf("PromptTokens = %d, want 600", got.PromptTokens)
	}
	if got.CompletionTokens != 200 {
		t.Errorf("CompletionTokens = %d, want 200", got.CompletionTokens)
	}
	if got.CacheReadTokens != 150 {
		t.Errorf("CacheReadTokens = %d, want 150", got.CacheReadTokens)
	}
	if got.CacheCreationTokens != 50 {
		t.Errorf("CacheCreationTokens = %d, want 50", got.CacheCreationTokens)
	}
	if got.EstimatedCostUSD != 0.0123 {
		t.Errorf("EstimatedCostUSD = %v, want 0.0123", got.EstimatedCostUSD)
	}
}

func TestHandleAnalyticsAgent_MissingClaims_Returns401(t *testing.T) {
	t.Parallel()
	svc := &Service{usage: newMockUsageRepo(), logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/agents/agent-type-1", nil)
	req.SetPathValue("agentTypeId", "agent-type-1")
	w := httptest.NewRecorder()
	c.handleAnalyticsAgent(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnauthorized, w.Body)
	}
}

func TestHandleAnalyticsAgent_MissingAgentTypeId_Returns400(t *testing.T) {
	t.Parallel()
	svc := &Service{usage: newMockUsageRepo(), logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/agents/", nil)
	// agentTypeId path value intentionally not set — simulates empty path param
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("tenant-1", "user-1")))
	w := httptest.NewRecorder()
	c.handleAnalyticsAgent(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body)
	}
}

func TestHandleAnalyticsAgent_RepoError_Returns500(t *testing.T) {
	t.Parallel()
	usage := newMockUsageRepo()
	usage.agentAnalyticsErr = errTestInternal
	svc := &Service{usage: usage, logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/agents/agent-type-1", nil)
	req.SetPathValue("agentTypeId", "agent-type-1")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("tenant-1", "user-1")))
	w := httptest.NewRecorder()
	c.handleAnalyticsAgent(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusInternalServerError, w.Body)
	}
}
