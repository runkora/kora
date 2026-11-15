package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/logging"
)

// A kora MCP call under /run/{run_id} reaches the route without the prefix.
func TestServerMiddleware_StripsTheRunPrefix(t *testing.T) {
	var path string
	h := serverMiddleware(logging.NewLogger())(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/run/7d0f0d7e-2a8e-4f7c-9d55-0a4b8e0c1f00/v1/events/types", nil))
	if path != "/v1/events/types" {
		t.Errorf("path = %q, want /v1/events/types", path)
	}
}
