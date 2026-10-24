package declarative

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

const registryYAML = `version: v1
tenants:
  - id: acme
    displayName: Acme
    state: active
    oidc: {issuerURL: https://idp, clientID: c, scopes: [openid]}
    namespace: kora-tenant-acme
    apiAuthoring: Disabled
    budget: {limitUSD: 1000, resetCadence: monthly}
  - id: beta
    state: active
    namespace: custom
    apiAuthoring: Enabled
  - id: refused
    state: active
    apiAuthoring: Enabled
  - id: gone
    state: suspended
    namespace: kora-tenant-gone
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

	for ns, want := range map[string]string{"kora-tenant-acme": "acme", "custom": "beta", "kora-tenant-gone": "", "": "", "other": ""} {
		got, ok := r.TenantForNamespace(ns)
		if got != want || ok != (want != "") {
			t.Errorf("TenantForNamespace(%q) = %q, %v; want %q", ns, got, ok, want)
		}
	}
	if ns, ok := r.NamespaceForTenant("beta"); !ok || ns != "custom" {
		t.Errorf("NamespaceForTenant(beta) = %q, %v", ns, ok)
	}
	if _, ok := r.NamespaceForTenant("refused"); ok {
		t.Error("a tenant without a namespace has none")
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

	writeRegistry(t, path, "tenants:\n  - {id: beta, state: active, namespace: custom, apiAuthoring: Disabled}\n", time.Unix(2, 0))
	if r.APIAuthoringEnabled("beta") {
		t.Error("a changed file is re-read")
	}
	if _, ok := r.TenantForNamespace("kora-tenant-acme"); ok {
		t.Error("a tenant removed from the file is gone")
	}

	writeRegistry(t, path, "tenants: [", time.Unix(3, 0))
	if r.Err() == nil {
		t.Error("Err() reports a parse failure")
	}
	if id, ok := r.TenantForNamespace("custom"); !ok || id != "beta" {
		t.Errorf("a parse failure keeps the last good file, got %q, %v", id, ok)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.TenantForNamespace("custom"); ok {
		t.Error("a removed file lists no tenants")
	}
}

func TestRegistryWithoutFile(t *testing.T) {
	for _, path := range []string{"", filepath.Join(t.TempDir(), "missing.yaml")} {
		r := NewRegistry(path)
		if _, ok := r.TenantForNamespace("kora-tenant-acme"); ok {
			t.Errorf("%q: no tenants expected", path)
		}
		if !r.APIAuthoringEnabled("acme") || r.Err() != nil {
			t.Errorf("%q: defaults expected", path)
		}
	}
}

var _ Tenants = (*Registry)(nil)

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
