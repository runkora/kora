package egressgw

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// TestAuthorize_PassthroughNotRegistrable locks the design guarantee: reaching
// a public host is granted by the egress whitelist, never by declaring a
// connection for it. The passthrough adapter is held by the broker and kept out
// of the AdapterRegistry, so a Connection naming it resolves by host and then
// fails the registry lookup.
func TestAuthorize_PassthroughNotRegistrable(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme",
		Name:     "public",
		Adapter:  credentials.AdapterPassthrough,
		BaseHost: "example.com",
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	broker := NewBroker(&stubEvaluator{allow: true}, repo, NewAdapterRegistry(), passthrough.New(), logger)
	req, _ := http.NewRequest(http.MethodGet, "https://example.com/", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if !errors.Is(err, credentials.ErrUnsupportedAdapter) {
		t.Fatalf("Authorize with a declared passthrough connection = %v, want ErrUnsupportedAdapter", err)
	}
}

// TestAuthorize_UnregisteredHostUsesPassthrough locks the other half: a host
// with no connection resolves through the passthrough adapter, is not policy
// checked against a connection scope, and injects nothing.
func TestAuthorize_UnregisteredHostUsesPassthrough(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	eval := &stubEvaluator{allow: false} // any connection-scope check would deny

	broker := NewBroker(eval, &memConnRepo{items: map[string]*credentials.Connection{}}, NewAdapterRegistry(), passthrough.New(), logger)
	req, _ := http.NewRequest(http.MethodGet, "https://pypi.org/simple/", nil)
	d, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))
	if err != nil {
		t.Fatalf("Authorize(unregistered host) = %v, want allow", err)
	}
	if d.Credential.Header != "" {
		t.Errorf("passthrough injected header %q, want none", d.Credential.Header)
	}
	if d.Action != "use" {
		t.Errorf("action = %q, want \"use\" from the passthrough adapter", d.Action)
	}
	if d.Connection.BaseHost != "pypi.org" {
		t.Errorf("BaseHost = %q, want pypi.org", d.Connection.BaseHost)
	}
}
