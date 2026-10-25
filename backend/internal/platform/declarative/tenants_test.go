package declarative

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const registryYAML = `version: v1
tenants:
  - id: acme
    displayName: Acme
    state: active
    oidc: {issuerURL: https://idp, clientID: c, scopes: [openid]}
    apiAuthoring: Disabled
    budget: {limitUSD: 1000, resetCadence: monthly}
  - id: beta
    state: active
    apiAuthoring: Enabled
  - id: refused
    state: active
    apiAuthoring: Enabled
  - id: gone
    state: suspended
`

func writeRegistry(t *testing.T, path, body string, mod time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	writeRegistry(t, path, registryYAML, time.Unix(1, 0))
	r := NewRegistry(path)

	ids := r.TenantIDs()
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"acme", "beta", "refused"}) {
		t.Errorf("TenantIDs() = %v, want the active tenants", ids)
	}
	for id, want := range map[string]bool{"acme": false, "beta": true, "refused": true, "unknown": true} {
		if got := r.APIAuthoringEnabled(id); got != want {
			t.Errorf("APIAuthoringEnabled(%q) = %v, want %v", id, got, want)
		}
	}
	if b, ok := r.TenantBudget("acme"); !ok || b.LimitUSD != 1000 || b.ResetCadence != "monthly" {
		t.Errorf("TenantBudget(acme) = %+v, %v", b, ok)
	}
	if _, ok := r.TenantBudget("beta"); ok {
		t.Error("beta declares no budget")
	}
	if err := r.Err(); err != nil {
		t.Errorf("Err() = %v", err)
	}
}

func TestRegistryReloadsAndKeepsLastGood(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	writeRegistry(t, path, registryYAML, time.Unix(1, 0))
	r := NewRegistry(path)
	if !r.APIAuthoringEnabled("beta") {
		t.Fatal("beta starts enabled")
	}

	writeRegistry(t, path, "tenants:\n  - {id: beta, state: active, apiAuthoring: Disabled}\n", time.Unix(2, 0))
	if r.APIAuthoringEnabled("beta") {
		t.Error("a changed file is re-read")
	}
	if slices.Contains(r.TenantIDs(), "acme") {
		t.Error("a tenant removed from the file is gone")
	}

	writeRegistry(t, path, "tenants: [", time.Unix(3, 0))
	if r.Err() == nil {
		t.Error("Err() reports a parse failure")
	}
	if r.APIAuthoringEnabled("beta") {
		t.Error("a parse failure keeps the last good file")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if r.Err() == nil || r.APIAuthoringEnabled("beta") || !slices.Contains(r.TenantIDs(), "beta") {
		t.Error("a removed file keeps the last good contents and reports an error")
	}
}

func TestRegistryWithoutFile(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing.yaml")} {
		r := NewRegistry(path)
		if len(r.TenantIDs()) != 0 {
			t.Errorf("%q: no tenants expected", path)
		}
		if !r.APIAuthoringEnabled("acme") || r.Err() != nil {
			t.Errorf("%q: defaults expected", path)
		}
	}
}

func TestRegistryUnreadableFromStartRefusesAPIWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tenants.yaml")
	writeRegistry(t, path, "tenants: [", time.Unix(1, 0))
	r := NewRegistry(path)
	if r.APIAuthoringEnabled("acme") {
		t.Error("an unreadable registry with no good copy must refuse API writes")
	}
	if r.Err() == nil {
		t.Error("Err() reports the parse failure on every call")
	}
	writeRegistry(t, path, registryYAML, time.Unix(2, 0))
	if !r.APIAuthoringEnabled("beta") || r.Err() != nil {
		t.Error("a fixed file is read again")
	}
}
