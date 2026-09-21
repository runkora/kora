package inferencegw

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// ── GET /analytics/principals/{id} ────────────────────────────────────────────
//
// Admin drill-down added alongside docs/adr/2026-04-27-inference-analytics.md
// (amended 2026-07-20). Scope enforcement (platform:analytics:read) is applied
// by ScopeEnforcementMiddleware ahead of this handler and is exercised by the
// route-scope tests, not here; these tests cover path-value extraction, query
// parsing, and error propagation.

func TestHandleAnalyticsPrincipalByID(t *testing.T) {
	t.Parallel()
	usage := newMockUsageRepo()
	usage.analyticsResp = AnalyticsResponse{
		Scope:      "personal",
		PeriodDays: 30,
	}
	svc := &Service{usage: usage, logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/principals/target-principal?days=30&granularity=daily", nil)
	req.SetPathValue("id", "target-principal")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "admin-1")))
	w := httptest.NewRecorder()
	c.handleAnalyticsPrincipalByID(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body)
	}
	var got AnalyticsResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Scope != "personal" {
		t.Errorf("Scope = %q, want %q", got.Scope, "personal")
	}

	gotFilter := usage.lastAnalytics
	if gotFilter.TenantID != "t1" {
		t.Errorf("TenantID sent to repository = %q, want %q", gotFilter.TenantID, "t1")
	}
	if gotFilter.PrincipalID == nil || *gotFilter.PrincipalID != "target-principal" {
		t.Errorf("PrincipalID sent to repository = %v, want %q (the drill-down target, not the caller admin-1)", gotFilter.PrincipalID, "target-principal")
	}
}

func TestHandleAnalyticsPrincipalByID_MissingClaims(t *testing.T) {
	t.Parallel()
	svc := &Service{usage: newMockUsageRepo(), logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/principals/target-principal", nil)
	req.SetPathValue("id", "target-principal")
	w := httptest.NewRecorder()
	c.handleAnalyticsPrincipalByID(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusUnauthorized, w.Body)
	}
}

func TestHandleAnalyticsPrincipalByID_InvalidDays(t *testing.T) {
	t.Parallel()
	svc := &Service{usage: newMockUsageRepo(), logger: logging.NewLoggerWithOutput(io.Discard)}
	c := &Controller{svc: svc, logger: logging.NewLoggerWithOutput(io.Discard)}

	req := httptest.NewRequest(http.MethodGet, "/analytics/principals/target-principal?days=14", nil)
	req.SetPathValue("id", "target-principal")
	req = req.WithContext(token.WithClaims(req.Context(), humanClaimsForHandler("t1", "admin-1")))
	w := httptest.NewRecorder()
	c.handleAnalyticsPrincipalByID(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusBadRequest, w.Body)
	}
}

// ── GET /analytics/principals page size ──────────────────────────────────────

// TestParseAnalyticsPrincipalsFilter_Limit covers the page size accepted by
// GET /v1/analytics/principals. The repository clamps anything above
// maxPrincipalsLimit, so a larger value was answered with a smaller page than
// the caller asked for and nothing said so.
func TestParseAnalyticsPrincipalsFilter_Limit(t *testing.T) {
	cases := []struct {
		name      string
		raw       string
		wantLimit int
		wantErr   bool
	}{
		{name: "absent uses the default", raw: "", wantLimit: defaultPrincipalsLimit},
		{name: "within the ceiling", raw: "50", wantLimit: 50},
		{name: "at the ceiling", raw: "100", wantLimit: maxPrincipalsLimit},
		{name: "above the ceiling", raw: "101", wantErr: true},
		{name: "far above the ceiling", raw: "1000000", wantErr: true},
		{name: "zero", raw: "0", wantErr: true},
		{name: "negative", raw: "-1", wantErr: true},
		{name: "not a number", raw: "all", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := url.Values{}
			if tc.raw != "" {
				q.Set("limit", tc.raw)
			}

			f, apiErr, _ := parseAnalyticsPrincipalsFilter(q)

			if tc.wantErr {
				if apiErr == nil {
					t.Fatalf("limit=%q was accepted, want a 400", tc.raw)
				}
				return
			}
			if apiErr != nil {
				t.Fatalf("limit=%q rejected: %v", tc.raw, apiErr)
			}
			if f.Limit != tc.wantLimit {
				t.Errorf("limit = %d, want %d", f.Limit, tc.wantLimit)
			}
		})
	}
}
