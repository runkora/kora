package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	oc "github.com/runkora/kora/backend/cmd/operator/config"
	"github.com/runkora/kora/backend/internal/platform/declarative"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// TestDeclaredClientsCarryTheOperatorToken checks each service gets its own
// base URL and every call carries the shared token.
func TestDeclaredClientsCarryTheOperatorToken(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = r.Header.Get("Authorization")
		mu.Unlock()
		_, _ = w.Write([]byte(`{"items":[]}`))
	}))
	defer srv.Close()
	cfg := oc.Declared{
		AgentsURL:           srv.URL,
		TriggersURL:         srv.URL + "/api/v1",
		PolicyServiceURL:    srv.URL + "/api/v1",
		InferenceGatewayURL: srv.URL + "/v1",
	}
	token := func(context.Context) (string, error) { return "operator-token", nil }
	clients := declaredClients(cfg, token, logging.NewLoggerWithOutput(io.Discard))

	ctx := context.Background()
	for _, call := range []struct {
		kind string
		list func() error
	}{
		{declarative.KindAgents, func() error { _, err := clients.Agents.List(ctx, declarative.KindAgents, "acme"); return err }},
		{declarative.KindTriggers, func() error { _, err := clients.Triggers.List(ctx, declarative.KindTriggers, "acme"); return err }},
		{declarative.KindPolicyRoles, func() error {
			_, err := clients.PolicyService.List(ctx, declarative.KindPolicyRoles, "acme")
			return err
		}},
		{declarative.KindVirtualModels, func() error {
			_, err := clients.InferenceGateway.List(ctx, declarative.KindVirtualModels, declarative.PlatformTenant)
			return err
		}},
	} {
		if err := call.list(); err != nil {
			t.Fatalf("%s: %v", call.kind, err)
		}
	}
	want := map[string]string{
		"/internal/declared/agents/acme":                "Bearer operator-token",
		"/api/v1/internal/declared/triggers/acme":       "Bearer operator-token",
		"/api/v1/internal/declared/policyroles/acme":    "Bearer operator-token",
		"/v1/internal/declared/virtualmodels/_platform": "Bearer operator-token",
	}
	for path, auth := range want {
		if seen[path] != auth {
			t.Errorf("%s: Authorization %q, want %q (seen %v)", path, seen[path], auth, seen)
		}
	}
}
