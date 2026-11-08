package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

type declaredController struct{ hits *int }

func (declaredController) GroupedBy() string                { return "" }
func (declaredController) Middleware() mw.MiddlewareHandler { return nil }
func (c declaredController) Boot(mux *Mux) {
	h := func(w http.ResponseWriter, r *http.Request) {
		*c.hits++
		w.WriteHeader(http.StatusOK)
	}
	mux.Handle("GET /v1/internal/declared/x", h)
	mux.Handle("GET /x", h)
}

func TestRejectEncodedPaths(t *testing.T) {
	ctx := context.Background()
	logger := logging.NewLogger()
	hits := 0
	srv := NewAPI(ctx, logger).Server(ctx, 0, "/", nil, declaredController{hits: &hits})
	srv = RejectEncodedPaths(logger, srv)

	tests := []struct {
		path     string
		wantCode int
		wantHit  bool
	}{
		{"/v1/internal/%64eclared/x", http.StatusBadRequest, false},
		{"/%68ealth", http.StatusBadRequest, false},
		{"/a%20b", http.StatusBadRequest, false},
		{"/a%2Fb", http.StatusBadRequest, false},
		{"/%2e%2e/x", http.StatusBadRequest, false},
		{"/v1/internal/declared/x", http.StatusOK, true},
		{"/health", http.StatusOK, false},
		{"/x?q=a%20b", http.StatusOK, true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			hits = 0
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			rec := httptest.NewRecorder()
			srv.Handler.ServeHTTP(rec, req)
			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if (hits > 0) != tt.wantHit {
				t.Fatalf("handler hits = %d, wantHit %v", hits, tt.wantHit)
			}
			if tt.wantCode == http.StatusBadRequest {
				var body map[string]any
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("body is not JSON: %v: %q", err, rec.Body.String())
				}
			}
		})
	}
}
