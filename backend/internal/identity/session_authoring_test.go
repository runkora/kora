package identity

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/runkora/kora/backend/internal/platform/declarative"
)

func TestCreateSession_ReportsAPIAuthoring(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	registry := `version: "v1"
tenants:
  - id: locked
    state: active
    apiAuthoring: Disabled
    oidc: {clientID: c}
  - id: open
    state: active
    apiAuthoring: Enabled
    oidc: {clientID: c}
  - id: unset
    state: active
    oidc: {clientID: c}
`
	if err := os.WriteFile(path, []byte(registry), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	svc := newWatcherTestService(t)
	svc.authoring = declarative.NewRegistry(path)

	for tenant, want := range map[string]bool{"locked": false, "open": true, "unset": true, "not-in-registry": true} {
		t.Run(tenant, func(t *testing.T) {
			resp, err := svc.createSession(context.Background(), &Principal{ID: "user-1", Type: PrincipalTypeHuman, TenantID: tenant})
			if err != nil {
				t.Fatalf("createSession: %v", err)
			}
			if resp.APIAuthoring != want {
				t.Fatalf("APIAuthoring = %v, want %v", resp.APIAuthoring, want)
			}
		})
	}
}
