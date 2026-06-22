package policy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

func staticToken(_ context.Context) (string, error) { return "test-token", nil }

func TestRegisterRoutes_PostsBodyAndAuth(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody registerRoutesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, staticToken, logging.NewLoggerWithOutput(io.Discard))
	routes := []routescopes.RouteScope{{PathPrefix: "/api/v1/triggers", Method: "GET", RequiredScope: "triggers:read"}}
	scopes := []routescopes.ScopeDeclaration{{Scope: "triggers:read", ResourceType: "triggers", Action: "read"}}

	if err := c.RegisterRoutes(context.Background(), "identity", routes, scopes); err != nil {
		t.Fatalf("RegisterRoutes: %v", err)
	}
	if gotPath != "/api/v1/policy/routes/register" {
		t.Errorf("path = %q, want /api/v1/policy/routes/register", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("auth = %q, want Bearer test-token", gotAuth)
	}
	if gotBody.Service != "identity" || len(gotBody.Scopes) != 1 || gotBody.Scopes[0].Scope != "triggers:read" {
		t.Errorf("body = %+v, want service=identity with triggers:read scope", gotBody)
	}
}

func TestRegisterRoutes_Non204IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, staticToken, logging.NewLoggerWithOutput(io.Discard))
	if err := c.RegisterRoutes(context.Background(), "identity", nil, nil); err == nil {
		t.Fatal("expected an error on a non-204 response")
	}
}
