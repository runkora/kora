package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	egc "github.com/clear-route/orbit/cmd/egress-gateway/config"
	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/egressgw"
	"github.com/clear-route/orbit/internal/platform/clients/routescopes"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// stubConnections is a fixed connection registry. Only ListByTenant is
// exercised; the broker's host lookup plays no part in scope publication.
type stubConnections struct {
	conns []*credentials.Connection
}

func (s stubConnections) GetByHost(context.Context, string, string) (*credentials.Connection, error) {
	return nil, credentials.ErrConnectionNotFound
}

func (s stubConnections) ListByTenant(context.Context, string) ([]*credentials.Connection, error) {
	return s.conns, nil
}

// registration is the wire payload the policy service receives.
type registration struct {
	Service string                         `json:"service"`
	Routes  []routescopes.RouteScope       `json:"routes"`
	Scopes  []routescopes.ScopeDeclaration `json:"scopes"`
}

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
	prev := egc.Current.Policy.PolicyServiceInternalURL
	egc.Current.Policy.PolicyServiceInternalURL = url
	t.Cleanup(func() { egc.Current.Policy.PolicyServiceInternalURL = prev })
}

func testAdapters() *egressgw.AdapterRegistry {
	registry := egressgw.NewAdapterRegistry()
	registry.Register(passthrough.New())
	return registry
}

// TestInitScopeRegistrationPublishesConnectionVocabulary asserts the register
// function the egress gateway hands to KeepRegistered publishes the read route
// and the connection:<name>:* vocabulary for the mounted registry.
func TestInitScopeRegistrationPublishesConnectionVocabulary(t *testing.T) {
	got := make(chan registration, 1)
	srv := capturingPolicyService(t, got)
	defer srv.Close()
	withPolicyServiceURL(t, srv.URL)

	conns := stubConnections{conns: []*credentials.Connection{
		{Name: "public-docs", Adapter: "passthrough", TenantID: "acme"},
	}}
	_, register := initScopeRegistration(logging.NewLogger(), conns, testAdapters())

	if err := register(t.Context()); err != nil {
		t.Fatalf("register: %v", err)
	}

	var reg registration
	select {
	case reg = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("policy service received no registration")
	}

	if reg.Service != egressgw.ServiceName {
		t.Errorf("registered as %q, want %q", reg.Service, egressgw.ServiceName)
	}
	// A registration with an empty routes list is rejected 400 by the policy
	// service, so the read route has to be present.
	if len(reg.Routes) == 0 {
		t.Error("registered an empty route table")
	}
	declared := make(map[string]struct{}, len(reg.Scopes))
	for _, s := range reg.Scopes {
		declared[s.Scope] = struct{}{}
	}
	for _, want := range []string{
		"platform:connections:read",
		"platform:connections:write",
		"connection:public-docs:read",
		"connection:public-docs:write",
	} {
		if _, ok := declared[want]; !ok {
			t.Errorf("scope %q missing from the registration: %v", want, declared)
		}
	}
}
