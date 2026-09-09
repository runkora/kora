package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/clear-route/orbit/internal/credentials"
	"github.com/clear-route/orbit/internal/credentials/adapters/passthrough"
	"github.com/clear-route/orbit/internal/platform/logging"
)

// badConfigAdapter refuses every connection, standing in for an adapter whose
// configuration does not satisfy it.
type badConfigAdapter struct{}

func (badConfigAdapter) Type() credentials.AdapterType            { return credentials.AdapterGitHubApp }
func (badConfigAdapter) Actions() []credentials.ActionDeclaration { return nil }

func (badConfigAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	return nil, credentials.ErrInvalidConfig
}

// TestAuthorize_UnreadableConnectionDenies locks that a connection Orbit cannot
// read denies the call, and reports it as a configuration fault rather than a
// policy one. Calling it forbidden sends whoever reads the audit log hunting a
// grant that was never the problem.
func TestAuthorize_UnreadableConnectionDenies(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	repo := &memConnRepo{items: map[string]*credentials.Connection{}}
	if err := repo.Create(context.Background(), &credentials.Connection{
		TenantID: "acme", Name: "github", Adapter: credentials.AdapterGitHubApp,
		BaseHost: "github.com", Config: json.RawMessage(`{"app_id":"1"}`),
	}); err != nil {
		t.Fatalf("seed connection: %v", err)
	}

	registry := NewAdapterRegistry()
	registry.Register(badConfigAdapter{})
	broker := NewBroker(&stubEvaluator{allow: true}, repo, registry, passthrough.New(), logger)

	req, _ := http.NewRequest(http.MethodGet, "https://github.com/orbital/orbit", nil)
	_, err := broker.Authorize(context.Background(),
		claimsFor("acme", "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))

	if !errors.Is(err, credentials.ErrInvalidConfig) {
		t.Fatalf("err = %v, want wrapped ErrInvalidConfig", err)
	}
	if errors.Is(err, ErrForbidden) {
		t.Error("an unreadable connection must not be reported as a policy denial")
	}
}
