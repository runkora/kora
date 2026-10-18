package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentsConfig "github.com/runkora/kora/backend/cmd/orbit-agents/config"
	"github.com/runkora/kora/backend/internal/agents"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
	mw "github.com/runkora/kora/backend/internal/platform/middleware"
)

// stubSlugLister is the whole repository surface scope registration uses.
type stubSlugLister struct {
	slugs []string
	err   error
}

func (s stubSlugLister) ListAllAgentTypeSlugs(context.Context) ([]string, error) {
	return s.slugs, s.err
}

// registration is the wire payload the policy service receives.
type registration struct {
	Service string                         `json:"service"`
	Routes  []routescopes.RouteScope       `json:"routes"`
	Scopes  []routescopes.ScopeDeclaration `json:"scopes"`
}

// testToken stands in for the identity client's self token source.
func testToken(context.Context) (string, error) { return "self-token", nil }

// capturingPolicyService stands in for the policy service's
// POST /api/v1/policy/routes/register endpoint.
func capturingPolicyService(t *testing.T, got chan<- registration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reg registration
		if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
			t.Errorf("decode registration: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		select {
		case got <- reg:
		default:
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

// withPolicyServiceURL points the package config at srv for the duration of the
// test.
func withPolicyServiceURL(t *testing.T, url string) {
	t.Helper()
	prev := agentsConfig.Current.Platform.PolicyServiceURL
	agentsConfig.Current.Platform.PolicyServiceURL = url
	t.Cleanup(func() { agentsConfig.Current.Platform.PolicyServiceURL = prev })
}

func newAgentTypeService() *agents.AgentTypeService {
	// Only SetAgentTypeHook is exercised, so the collaborators can stay nil.
	return agents.NewAgentTypeService(nil, nil, nil, nil, agents.AgentTypeServiceConfig{}, logging.NewLogger())
}

// TestInitScopeRegistrationPublishesRoutesAndSlugScopes asserts the register
// function orbit-agents hands to KeepRegistered sends the right payload: the
// whole route table, authenticated-only routes included, and a vocabulary
// covering both the static scopes and the per-agent-type ones.
func TestInitScopeRegistrationPublishesRoutesAndSlugScopes(t *testing.T) {
	got := make(chan registration, 1)
	srv := capturingPolicyService(t, got)
	defer srv.Close()
	withPolicyServiceURL(t, srv.URL)

	register := initScopeRegistration(logging.NewLogger(), testToken, stubSlugLister{slugs: []string{"triage"}}, newAgentTypeService())
	if register == nil {
		t.Fatal("expected a register function when a policy service URL is configured")
	}
	if err := register(t.Context()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var reg registration
	select {
	case reg = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("policy service received no registration")
	}

	if reg.Service != "orbit-agents" {
		t.Errorf("registered as %q, want orbit-agents", reg.Service)
	}
	if want := len(agentRoutes()); len(reg.Routes) != want {
		t.Fatalf("registered %d routes, want %d", len(reg.Routes), want)
	}

	declared := make(map[string]struct{}, len(reg.Scopes))
	for _, s := range reg.Scopes {
		declared[s.Scope] = struct{}{}
	}
	// Every route's required scope must be in the vocabulary, or the policy
	// service rejects a tenant registration outright. Authenticated-only routes
	// carry no scope.
	for _, r := range reg.Routes {
		if r.RequiredScope == "" {
			continue
		}
		if _, ok := declared[r.RequiredScope]; !ok {
			t.Errorf("route %s %s requires undeclared scope %q", r.Method, r.PathPrefix, r.RequiredScope)
		}
	}
	for _, want := range []string{"agents:types:triage:read", "agents:runs:triage:read", "agents:runs:triage:write", "agents:usage:triage:read"} {
		if _, ok := declared[want]; !ok {
			t.Errorf("per-agent-type scope %q missing from the registration", want)
		}
	}
	for _, want := range []string{"agents:usage:read", "agents:delegated:read", "agents:runs:delegated:write"} {
		if _, ok := declared[want]; !ok {
			t.Errorf("scope %q missing from the registration", want)
		}
	}
}

// TestAgentRoutesPerAgentReadsAuthenticatedOnly guards F3: a parent scope at the
// route layer would refuse every per-agent holder before the service checks run.
func TestAgentRoutesPerAgentReadsAuthenticatedOnly(t *testing.T) {
	tests := []struct{ method, path string }{
		{"GET", "/api/v1/agent-types"},
		{"GET", "/api/v1/agent-types/abc"},
		{"GET", "/api/v1/agent-types/abc/effective-access"},
		{"GET", "/api/v1/runs"},
		{"GET", "/api/v1/runs/stats"},
		{"GET", "/api/v1/runs/abc"},
		{"GET", "/api/v1/runs/abc/events"},
		{"GET", "/api/v1/run-outcomes"},
		{"GET", "/api/v1/run-stats/coder"},
		{"POST", "/api/v1/runs"},
		{"POST", "/api/v1/runs/abc/messages"},
		{"POST", "/api/v1/runs/abc/interrupt"},
		{"DELETE", "/api/v1/runs/abc"},
	}
	for _, tt := range tests {
		got, ok := mw.ResolveRequiredScope(agentRoutes(), tt.method, tt.path)
		if !ok || got != "" {
			t.Errorf("%s %s: scope %q (matched %v), want authenticated-only", tt.method, tt.path, got, ok)
		}
	}
}
