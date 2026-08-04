package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	agentsConfig "github.com/clear-route/orbit/cmd/orbit-agents/config"
	"github.com/clear-route/orbit/internal/agents"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
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

// TestInitScopeRegistrationPublishesScopedRoutesAndSlugScopes asserts the
// register function orbit-agents hands to KeepRegistered sends the right
// payload: only routes carrying a required scope, and a vocabulary covering
// both the static scopes and the per-agent-type ones.
func TestInitScopeRegistrationPublishesScopedRoutesAndSlugScopes(t *testing.T) {
	got := make(chan registration, 1)
	srv := capturingPolicyService(t, got)
	defer srv.Close()
	withPolicyServiceURL(t, srv.URL)

	register := initScopeRegistration(logging.NewLogger(), stubSlugLister{slugs: []string{"triage"}}, newAgentTypeService())
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
	if len(reg.Routes) == 0 {
		t.Fatal("registered an empty route table")
	}
	for _, r := range reg.Routes {
		if r.RequiredScope == "" {
			t.Errorf("authenticated-only route was registered: %+v", r)
		}
	}

	declared := make(map[string]struct{}, len(reg.Scopes))
	for _, s := range reg.Scopes {
		declared[s.Scope] = struct{}{}
	}
	// Every route's required scope must be in the vocabulary, or the policy
	// service rejects a tenant registration outright.
	for _, r := range reg.Routes {
		if _, ok := declared[r.RequiredScope]; !ok {
			t.Errorf("route %s %s requires undeclared scope %q", r.Method, r.PathPrefix, r.RequiredScope)
		}
	}
	for _, want := range []string{"agents:runs:triage:read", "agents:runs:triage:write"} {
		if _, ok := declared[want]; !ok {
			t.Errorf("per-agent-type scope %q missing from the registration", want)
		}
	}
}

// TestInitScopeRegistrationWithoutPolicyServiceIsNoOp covers local dev, where
// no policy service is configured and there is nothing to keep registered.
func TestInitScopeRegistrationWithoutPolicyServiceIsNoOp(t *testing.T) {
	withPolicyServiceURL(t, "")
	if register := initScopeRegistration(logging.NewLogger(), stubSlugLister{}, newAgentTypeService()); register != nil {
		t.Fatal("expected no register function when no policy service is configured")
	}
}

// TestAgentRoutesFilterDropsOnlyUnscopedRoutes guards the route table itself:
// the authenticated-only writes must stay out of the policy registration, and
// every read route must stay in.
func TestAgentRoutesFilterDropsOnlyUnscopedRoutes(t *testing.T) {
	all := agentRoutes()
	scoped := routescopes.FilterScopedRoutes(all)

	if len(scoped) >= len(all) {
		t.Fatalf("expected the filter to drop the authenticated-only routes: %d of %d kept", len(scoped), len(all))
	}
	for _, r := range all {
		kept := slices.Contains(scoped, r)
		if want := r.RequiredScope != ""; kept != want {
			t.Errorf("route %+v: kept=%v, want %v", r, kept, want)
		}
	}
}
