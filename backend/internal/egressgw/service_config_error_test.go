package egressgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/runkora/kora/backend/internal/credentials"
	"github.com/runkora/kora/backend/internal/credentials/adapters/passthrough"
	"github.com/runkora/kora/backend/internal/platform/logging"
)

// badConfigAdapter refuses every connection, standing in for an adapter whose
// configuration does not satisfy it.
type badConfigAdapter struct{}

func (badConfigAdapter) Type() credentials.AdapterType            { return credentials.AdapterGitHubApp }
func (badConfigAdapter) Actions() []credentials.ActionDeclaration { return nil }

func (badConfigAdapter) Open(credentials.Connection) (credentials.Connector, error) {
	return nil, credentials.ErrInvalidConfig
}

// TestAuthorize_UnreadableConnectionDenies locks that a connection kora cannot
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
		claimsFor("acme", "spiffe://orbit.test/tenant/kora-tenant-acme/agent/coder", "run-1"),
		FromHTTP(req))

	if !errors.Is(err, credentials.ErrInvalidConfig) {
		t.Fatalf("err = %v, want wrapped ErrInvalidConfig", err)
	}
	if errors.Is(err, ErrForbidden) {
		t.Error("an unreadable connection must not be reported as a policy denial")
	}
}

// TestHandleBrokerError_ConfigFaultOutranksForbidden locks the classification
// order. An operator's malformed connection is not a policy decision, so even
// wrapped in ErrForbidden it must surface as a server fault: reported as a
// denial it sends whoever reads it hunting a grant that was never the problem.
func TestHandleBrokerError_ConfigFaultOutranksForbidden(t *testing.T) {
	p := &ProxyHandler{brand: testBrand, logger: logging.NewLoggerWithOutput(io.Discard)}

	for name, err := range map[string]error{
		"bare":               credentials.ErrInvalidConfig,
		"wrapped in context": fmt.Errorf("open connector: %w", credentials.ErrInvalidConfig),
		"wrapped in forbidden": fmt.Errorf("%w: %w",
			ErrForbidden, credentials.ErrInvalidConfig),
	} {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, "https://github.com/", nil)

			p.handleBrokerError(rec, req, err)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
		})
	}
}
