package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	igc "github.com/clear-route/orbit/cmd/inference-gateway/config"
	"github.com/clear-route/orbit/internal/inferencegw"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// registration is the wire payload the policy service receives.
type registration struct {
	Service string                         `json:"service"`
	Routes  []routescopes.RouteScope       `json:"routes"`
	Scopes  []routescopes.ScopeDeclaration `json:"scopes"`
}

// testToken stands in for the identity client's self token source.
func testToken(context.Context) (string, error) { return "self-token", nil }

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

func withPolicyServiceURL(t *testing.T, url string) {
	t.Helper()
	prev := igc.Current.Policy.PolicyServiceURL
	igc.Current.Policy.PolicyServiceURL = url
	t.Cleanup(func() { igc.Current.Policy.PolicyServiceURL = prev })
}

// TestInitScopeRegistrationPublishesScopedRoutes asserts the register function
// the inference gateway hands to KeepRegistered publishes under the right
// service name, drops the unscoped routes, and declares its static vocabulary.
//
// The routing engine is built with no virtual catalog, so CatalogScopes returns
// nil and only the static declarations are published. The per-model
// inference:<family>:use and inference:<provider>:<model>:use scopes are not
// covered here: populating a catalog needs the repository-backed CatalogStore,
// and the snapshot type is internal to the inferencegw package. Those scopes
// are exercised by the router's own tests.
func TestInitScopeRegistrationPublishesScopedRoutes(t *testing.T) {
	got := make(chan registration, 1)
	srv := capturingPolicyService(t, got)
	defer srv.Close()
	withPolicyServiceURL(t, srv.URL)

	router := inferencegw.NewRoutingEngine(nil)
	register := initScopeRegistration(logging.NewLogger(), testToken, router)

	if err := register(t.Context()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var reg registration
	select {
	case reg = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("policy service received no registration")
	}

	if reg.Service != "inference-gateway" {
		t.Errorf("service = %q, want inference-gateway", reg.Service)
	}

	// Only scoped routes are registered: an entry with an empty RequiredScope is
	// authenticated-but-unscoped and is filtered out before publication, so
	// registering one would make the policy service enforce a scope of "".
	if want := len(routescopes.FilterScopedRoutes(inferenceRoutes())); len(reg.Routes) != want {
		t.Errorf("published %d routes, want %d", len(reg.Routes), want)
	}
	for _, r := range reg.Routes {
		if r.RequiredScope == "" {
			t.Errorf("published unscoped route %s %s", r.Method, r.PathPrefix)
		}
	}

	declared := make(map[string]bool, len(reg.Scopes))
	for _, s := range reg.Scopes {
		declared[s.Scope] = true
	}
	for _, want := range []string{"inference:invoke", "inference:use", "inference:list", "platform:analytics:read"} {
		if !declared[want] {
			t.Errorf("scope %q missing from the published vocabulary", want)
		}
	}
}
