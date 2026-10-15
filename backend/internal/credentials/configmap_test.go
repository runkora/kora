package credentials

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/clear-route/orbit/internal/platform/logging"
)

func writeRegistry(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "connections.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return path
}

func newTestRegistry(t *testing.T, body string) *ConfigMapRegistry {
	t.Helper()
	logger := logging.NewLoggerWithOutput(io.Discard)
	r, err := LoadConfigMapRegistry(context.Background(), writeRegistry(t, body), logger)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	return r
}

const sampleRegistry = `[
  {"tenant_id":"acme","name":"github","adapter":"github_app","base_host":"github.com","secret_ref":"acme.github.key","config":{"appId":"1"}},
  {"tenant_id":"acme","name":"api","adapter":"api_key","base_host":"api.acme.io","secret_ref":"acme.api.key"},
  {"tenant_id":"globex","name":"github","adapter":"github_app","base_host":"github.com","secret_ref":"globex.github.key"}
]`

func TestConfigMapRegistry_ListByHost(t *testing.T) {
	r := newTestRegistry(t, sampleRegistry)
	ctx := context.Background()

	// Exact host within tenant.
	got, err := r.ListByHost(ctx, "acme", "github.com")
	if err != nil {
		t.Fatalf("ListByHost: %v", err)
	}
	if got[0].Name != "github" || got[0].SecretRef != "acme.github.key" {
		t.Errorf("got %+v, want acme/github", got)
	}
	// ID is derived tenant|name so the github adapter's cache key is stable.
	if got[0].ID != "acme|github" {
		t.Errorf("ID = %q, want acme|github", got[0].ID)
	}

	// Host carrying a :port and mixed case still matches base_host.
	if _, err := r.ListByHost(ctx, "acme", "GitHub.com:443"); err != nil {
		t.Errorf("ListByHost with port+case: %v", err)
	}

	// Same host, different tenant resolves that tenant's connection.
	gx, err := r.ListByHost(ctx, "globex", "github.com")
	if err != nil {
		t.Fatalf("ListByHost globex: %v", err)
	}
	if gx[0].SecretRef != "globex.github.key" {
		t.Errorf("globex secret = %q, want globex.github.key", gx[0].SecretRef)
	}

	// Unknown host → not found.
	if _, err := r.ListByHost(ctx, "acme", "example.com"); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("unknown host err = %v, want ErrConnectionNotFound", err)
	}
	// Known host, wrong tenant → not found (cross-tenant isolation).
	if _, err := r.ListByHost(ctx, "nobody", "github.com"); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("wrong tenant err = %v, want ErrConnectionNotFound", err)
	}
}

func TestConfigMapRegistry_ListByHost_AdapterHostFamily(t *testing.T) {
	r := newTestRegistry(t, sampleRegistry)
	ctx := context.Background()

	// github_app serves a host family beyond its base_host, so REST/archive/raw
	// calls resolve to the same connection and get credentialed (not passthrough).
	for _, host := range []string{
		"api.github.com",                // REST API
		"codeload.github.com",           // archive downloads
		"raw.githubusercontent.com",     // *.githubusercontent.com wildcard
		"objects.githubusercontent.com", // ditto
		"API.GitHub.com:443",            // case + port normalize first
	} {
		got, err := r.ListByHost(ctx, "acme", host)
		if err != nil {
			t.Errorf("ListByHost(%q): %v", host, err)
			continue
		}
		if got[0].Name != "github" {
			t.Errorf("ListByHost(%q) = %q, want github", host, got[0].Name)
		}
	}

	// Hosts outside the family are not claimed by the github connection. The
	// credential must never reach a non-github upstream.
	for _, host := range []string{
		"githubusercontent.com", // apex, not a *.subdomain
		"api.gitlab.com",        // different service
		"github.com.evil.com",   // suffix-confusion attempt
		"notgithubusercontent.com",
	} {
		if _, err := r.ListByHost(ctx, "acme", host); !errors.Is(err, ErrConnectionNotFound) {
			t.Errorf("ListByHost(%q) err = %v, want ErrConnectionNotFound", host, err)
		}
	}
}

func TestConfigMapRegistry_ListByTenant(t *testing.T) {
	r := newTestRegistry(t, sampleRegistry)
	ctx := context.Background()

	// Empty tenant returns ALL connections (the ScopePublisher relies on this).
	all, err := r.ListByTenant(ctx, "")
	if err != nil {
		t.Fatalf("ListByTenant(\"\"): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all = %d, want 3", len(all))
	}

	// Filtered by tenant.
	acme, err := r.ListByTenant(ctx, "acme")
	if err != nil {
		t.Fatalf("ListByTenant(acme): %v", err)
	}
	if len(acme) != 2 {
		t.Errorf("acme = %d, want 2", len(acme))
	}
	for _, c := range acme {
		if c.TenantID != "acme" {
			t.Errorf("unexpected tenant %q in acme list", c.TenantID)
		}
	}
}

func TestConfigMapRegistry_EmptyAndMissing(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	ctx := context.Background()

	// Empty path → empty registry, no error.
	empty, err := LoadConfigMapRegistry(ctx, "", logger)
	if err != nil {
		t.Fatalf("empty path: %v", err)
	}
	if _, err := empty.ListByHost(ctx, "acme", "github.com"); !errors.Is(err, ErrConnectionNotFound) {
		t.Errorf("empty registry err = %v, want ErrConnectionNotFound", err)
	}

	// Missing file → empty registry, no error.
	missing, err := LoadConfigMapRegistry(ctx, filepath.Join(t.TempDir(), "nope.json"), logger)
	if err != nil {
		t.Fatalf("missing file: %v", err)
	}
	list, err := missing.ListByTenant(ctx, "")
	if err != nil {
		t.Fatalf("missing ListByTenant: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("missing list = %d, want 0", len(list))
	}

	// Empty JSON array is valid and yields no connections.
	none := newTestRegistry(t, `[]`)
	if list, _ := none.ListByTenant(ctx, ""); len(list) != 0 {
		t.Errorf("empty-array list = %d, want 0", len(list))
	}
}

func TestConfigMapRegistry_MalformedJSON(t *testing.T) {
	logger := logging.NewLoggerWithOutput(io.Discard)
	_, err := LoadConfigMapRegistry(context.Background(), writeRegistry(t, `{not json`), logger)
	if err == nil {
		t.Fatal("expected error on malformed registry JSON")
	}
}

// TestListByHost_FullyQualifiedHost locks the trailing-dot form against the
// registry. net/http accepts "github.com." and passes it through as the request
// Host; left unnormalised it matches no connection, and the broker falls back
// to passthrough, skipping the connection-scope check on a host that has one.
func TestListByHost_FullyQualifiedHost(t *testing.T) {
	reg := newTestRegistry(t, sampleRegistry)

	for _, host := range []string{"github.com", "github.com.", "GitHub.Com.", "api.github.com."} {
		conn, err := reg.ListByHost(t.Context(), "acme", host)
		if err != nil {
			t.Errorf("ListByHost(%q) = %v, want the github connection", host, err)
			continue
		}
		if conn[0].Name != "github" {
			t.Errorf("ListByHost(%q) resolved %q, want github", host, conn[0].Name)
		}
	}
}

// Same-host connections in one tenant are all returned, so the broker can choose among them.
func TestListByHost_ReturnsEverySameHostConnection(t *testing.T) {
	reg := newTestRegistry(t, `[
  {"tenant_id":"acme","name":"clear-route-github","adapter":"github_app","base_host":"github.com","secret_ref":"a.key"},
  {"tenant_id":"acme","name":"runkora-github","adapter":"github_app","base_host":"github.com","secret_ref":"b.key"}
]`)
	for _, host := range []string{"github.com", "api.github.com"} {
		got, err := reg.ListByHost(t.Context(), "acme", host)
		if err != nil {
			t.Fatalf("ListByHost(%q): %v", host, err)
		}
		if len(got) != 2 || got[0].Name != "clear-route-github" || got[1].Name != "runkora-github" {
			t.Errorf("ListByHost(%q) = %d connections, want both in load order", host, len(got))
		}
	}
}
