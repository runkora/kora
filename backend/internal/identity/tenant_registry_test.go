package identity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

const apiAuthoringRegistry = `version: "v1"
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
  - id: suspended
    state: suspended
    apiAuthoring: Disabled
    oidc: {clientID: c}
`

func writeTenantRegistry(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return path
}

func TestLoadTenantRegistry_APIAuthoring(t *testing.T) {
	reg, err := loadTenantRegistry(writeTenantRegistry(t, apiAuthoringRegistry), nil)
	if err != nil {
		t.Fatalf("loadTenantRegistry: %v", err)
	}

	want := map[string]bool{"locked": true}
	if len(reg.APIAuthoringDisabled) != len(want) || !reg.APIAuthoringDisabled["locked"] {
		t.Fatalf("APIAuthoringDisabled = %v, want %v", reg.APIAuthoringDisabled, want)
	}
	if len(reg.OIDC) != 3 {
		t.Fatalf("OIDC tenants = %d, want 3 active tenants", len(reg.OIDC))
	}
}

func TestLoadTenantRegistry_EmptyPath(t *testing.T) {
	reg, err := loadTenantRegistry("", nil)
	if err != nil {
		t.Fatalf("loadTenantRegistry: %v", err)
	}
	if len(reg.OIDC) != 0 || len(reg.APIAuthoringDisabled) != 0 {
		t.Fatalf("want an empty registry, got %+v", reg)
	}
}

func TestCreateSession_ReportsAPIAuthoring(t *testing.T) {
	svc := newWatcherTestService(t)
	svc.cfg.Registry.TenantsPath = writeTenantRegistry(t, apiAuthoringRegistry)
	svc.reloadTenantOIDCProviders(context.Background())

	tests := []struct {
		tenant string
		want   bool
	}{
		{tenant: "locked", want: false},
		{tenant: "open", want: true},
		{tenant: "unset", want: true},
		{tenant: "not-in-registry", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.tenant, func(t *testing.T) {
			resp, err := svc.createSession(context.Background(), &Principal{
				ID:       "user-1",
				Type:     PrincipalTypeHuman,
				TenantID: tc.tenant,
			})
			if err != nil {
				t.Fatalf("createSession: %v", err)
			}
			if resp.APIAuthoring != tc.want {
				t.Fatalf("APIAuthoring = %v, want %v", resp.APIAuthoring, tc.want)
			}
		})
	}
}

func TestReloadTenantRegistry_KeepsAPIAuthoringOnParseError(t *testing.T) {
	svc := newWatcherTestService(t)
	path := writeTenantRegistry(t, apiAuthoringRegistry)
	svc.cfg.Registry.TenantsPath = path
	svc.reloadTenantOIDCProviders(context.Background())

	if err := os.WriteFile(path, []byte("tenants: ["), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	svc.reloadTenantOIDCProviders(context.Background())

	if svc.apiAuthoringEnabled("locked") {
		t.Fatal("a failed reload must keep the last parsed authoring state")
	}
}
