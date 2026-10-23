package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	egc "github.com/runkora/kora/backend/cmd/egress-gateway/config"
	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/githubapp"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/egressgw"
	"github.com/runkora/kora/backend/internal/platform/clients/routescopes"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// mountedRegistry writes a connections.json the way the operator renders it and
// loads the gateway's real registry over it, so the test publishes from the
// same type production does.
func mountedRegistry(t *testing.T, entries string) *credentials.ConfigMapRegistry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "connections.json")
	if err := os.WriteFile(path, []byte(entries), 0o600); err != nil {
		t.Fatalf("write connections.json: %v", err)
	}
	reg, err := credentials.LoadConfigMapRegistry(t.Context(), path, logging.NewLoggerWithOutput(io.Discard))
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return reg
}

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
	prev := egc.Current.Policy.PolicyServiceInternalURL
	egc.Current.Policy.PolicyServiceInternalURL = url
	t.Cleanup(func() { egc.Current.Policy.PolicyServiceInternalURL = prev })
}

// testAdapters registers the adapters main.go registers. Passthrough is
// deliberately absent: AdapterRegistry.Register refuses it.
func testAdapters(t *testing.T) *egressgw.AdapterRegistry {
	t.Helper()
	registry := egressgw.NewAdapterRegistry()
	registry.Register(githubapp.New(nil, nil, logging.NewLoggerWithOutput(io.Discard)))
	return registry
}

// TestAdapterRegistryRefusesPassthrough locks the wiring guarantee: reaching a
// host that needs no credential is granted by the egress whitelist, so no
// Connection may name the passthrough adapter, however main.go is edited.
func TestAdapterRegistryRefusesPassthrough(t *testing.T) {
	registry := egressgw.NewAdapterRegistry()
	registry.Register(passthrough.New())
	if _, ok := registry.Lookup(credentials.AdapterPassthrough); ok {
		t.Fatal("AdapterRegistry accepted the passthrough adapter")
	}
}

// TestInitScopeRegistrationPublishesConnectionVocabulary asserts the register
// function the egress gateway hands to KeepRegistered publishes the read route
// and the connection:<name>:* vocabulary for the mounted registry.
func TestInitScopeRegistrationPublishesConnectionVocabulary(t *testing.T) {
	got := make(chan registration, 1)
	srv := capturingPolicyService(t, got)
	defer srv.Close()
	withPolicyServiceURL(t, srv.URL)

	conns := mountedRegistry(t, `[{"tenant_id":"acme","name":"github","adapter":"github_app","base_host":"github.com","secret_ref":"k8s:kora-conn-acme-github"}]`)
	register := initScopeRegistration(logging.NewLoggerWithOutput(io.Discard), testToken, conns, testAdapters(t))

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
		"connection:github:read",
		"connection:github:write",
		"connection:github:contents:write",
	} {
		if _, ok := declared[want]; !ok {
			t.Errorf("scope %q missing from the registration: %v", want, declared)
		}
	}
}
