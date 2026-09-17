package bundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestTenantRegistry_ActiveTenantIDs(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tenants.yaml")
	body := "tenants:\n  - id: acme\n    state: active\n  - id: dormant\n    state: suspended\n  - id: unstated\n  - id: \"\"\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	ids, err := (&TenantRegistry{path: path}).ActiveTenantIDs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "acme" {
		t.Fatalf("ids = %v, want [acme]", ids)
	}
}

func TestTenantRegistry_UnsetAndMissing(t *testing.T) {
	t.Parallel()

	if ids, err := (&TenantRegistry{}).ActiveTenantIDs(context.Background()); err != nil || ids != nil {
		t.Fatalf("unset path: ids=%v err=%v", ids, err)
	}
	if ids, err := (&TenantRegistry{path: filepath.Join(t.TempDir(), "absent.yaml")}).ActiveTenantIDs(context.Background()); err != nil || ids != nil {
		t.Fatalf("absent file: ids=%v err=%v, want none", ids, err)
	}
}
