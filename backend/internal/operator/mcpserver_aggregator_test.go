package operator

import (
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// mcpSrv builds an MCPServer CR for tests.
func mcpSrv(name, namespace string, spec MCPServerSpec) MCPServer {
	return MCPServer{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       spec,
	}
}

func TestValidateMCPServerSpec(t *testing.T) {
	cases := []struct {
		name      string
		spec      MCPServerSpec
		namespace string
		wantErr   error
	}{
		{
			name:      "valid tenant server",
			spec:      MCPServerSpec{Endpoint: "http://gh.acme.svc:8080/mcp"},
			namespace: "kora-tenant-acme",
			wantErr:   nil,
		},
		{
			name:      "valid global forward server",
			spec:      MCPServerSpec{Endpoint: "http://kora-mcp.svc/mcp", CallerIdentity: CallerIdentityForward},
			namespace: systemNamespace,
			wantErr:   nil,
		},
		{
			name:      "missing endpoint",
			spec:      MCPServerSpec{},
			namespace: systemNamespace,
			wantErr:   ErrMCPServerNoEndpoint,
		},
		{
			name:      "userinfo rejected",
			spec:      MCPServerSpec{Endpoint: "https://user:s3cret@mcp.example.com/mcp"},
			namespace: "kora-tenant-acme",
			wantErr:   ErrMCPServerBadEndpoint,
		},
		{
			name:      "query string rejected",
			spec:      MCPServerSpec{Endpoint: "https://mcp.example.com/mcp?key=s3cret"},
			namespace: "kora-tenant-acme",
			wantErr:   ErrMCPServerBadEndpoint,
		},
		{
			name:      "empty query rejected",
			spec:      MCPServerSpec{Endpoint: "https://mcp.example.com/mcp?"},
			namespace: "kora-tenant-acme",
			wantErr:   ErrMCPServerBadEndpoint,
		},
		{
			name:      "unparseable endpoint rejected",
			spec:      MCPServerSpec{Endpoint: "https://s3cret@[::1/mcp"},
			namespace: "kora-tenant-acme",
			wantErr:   ErrMCPServerBadEndpoint,
		},
		{
			name:      "bad transport",
			spec:      MCPServerSpec{Endpoint: "http://x/mcp", Transport: "stdio"},
			namespace: systemNamespace,
			wantErr:   ErrMCPServerBadTransport,
		},
		{
			// The security-critical rule: a tenant CR cannot set forward.
			name:      "forward rejected in tenant namespace",
			spec:      MCPServerSpec{Endpoint: "http://x/mcp", CallerIdentity: CallerIdentityForward},
			namespace: "kora-tenant-acme",
			wantErr:   ErrMCPServerForwardNotGlobal,
		},
		{
			name:      "none allowed in tenant namespace",
			spec:      MCPServerSpec{Endpoint: "http://x/mcp", CallerIdentity: CallerIdentityNone},
			namespace: "kora-tenant-acme",
			wantErr:   nil,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateMCPServerSpec(c.spec, "github", c.namespace)
			if !errors.Is(err, c.wantErr) {
				t.Errorf("validateMCPServerSpec = %v, want %v", err, c.wantErr)
			}
			// The error lands in CR status and operator logs.
			if err != nil && strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error %q repeats a credential from the endpoint", err)
			}
		})
	}
}

func TestEffectiveDefaults(t *testing.T) {
	if got := effectiveTransport(""); got != transportStreamableHTTP {
		t.Errorf("effectiveTransport(\"\") = %q, want %q", got, transportStreamableHTTP)
	}
	if got := effectiveTransport("streamable-http"); got != "streamable-http" {
		t.Errorf("effectiveTransport passthrough = %q", got)
	}
	if got := effectiveCallerIdentity(""); got != CallerIdentityNone {
		t.Errorf("effectiveCallerIdentity(\"\") = %q, want %q", got, CallerIdentityNone)
	}
	if got := effectiveCallerIdentity(CallerIdentityForward); got != CallerIdentityForward {
		t.Errorf("effectiveCallerIdentity passthrough = %q", got)
	}
}

func TestResolveTenantID(t *testing.T) {
	nsToTenant := map[string]string{"kora-tenant-acme": "acme", "custom-ns": "beta"}

	if id, ok := resolveTenantID(systemNamespace, nsToTenant); !ok || id != "" {
		t.Errorf("kora-system → (%q,%v), want (\"\",true)", id, ok)
	}
	if id, ok := resolveTenantID("kora-tenant-acme", nsToTenant); !ok || id != "acme" {
		t.Errorf("tenant ns → (%q,%v), want (acme,true)", id, ok)
	}
	if id, ok := resolveTenantID("custom-ns", nsToTenant); !ok || id != "beta" {
		t.Errorf("custom ns → (%q,%v), want (beta,true)", id, ok)
	}
	if _, ok := resolveTenantID("kora-tenant-ghost", nsToTenant); ok {
		t.Error("namespace with no active tenant should not resolve")
	}
}

func TestBuildMCPRegistry(t *testing.T) {
	nsToTenant := map[string]string{"kora-tenant-acme": "acme"}

	servers := []MCPServer{
		// Global server with forward — valid, tenant_id "".
		mcpSrv("orbit", systemNamespace, MCPServerSpec{
			Endpoint:                    "http://kora-mcp.kora-system.svc:8095/mcp",
			ReintrospectIntervalSeconds: 900,
			CallerIdentity:              CallerIdentityForward,
		}),
		// Tenant server — valid, tenant_id "acme", defaults applied.
		mcpSrv("github", "kora-tenant-acme", MCPServerSpec{
			Endpoint: "http://github-mcp.kora-tenant-acme.svc:8080/mcp",
		}),
		// Tenant server setting forward — excluded (forward is global-only).
		mcpSrv("evil", "kora-tenant-acme", MCPServerSpec{
			Endpoint:       "http://evil.acme.svc/mcp",
			CallerIdentity: CallerIdentityForward,
		}),
		// Server in a namespace with no active tenant — excluded.
		mcpSrv("orphan", "kora-tenant-ghost", MCPServerSpec{
			Endpoint: "http://ghost/mcp",
		}),
		// Invalid spec (no endpoint) — excluded.
		mcpSrv("broken", systemNamespace, MCPServerSpec{}),
	}

	entries, skipped := buildMCPRegistry(servers, nsToTenant)

	// Two valid servers, sorted by (tenant_id, name): "" < "acme" so orbit first.
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(entries), entries)
	}
	if entries[0].Name != "orbit" || entries[0].TenantID != "" {
		t.Errorf("entry[0] = %+v, want global orbit", entries[0])
	}
	if entries[0].CallerIdentity != CallerIdentityForward {
		t.Errorf("global server caller_identity = %q, want forward", entries[0].CallerIdentity)
	}
	if entries[1].Name != "github" || entries[1].TenantID != "acme" {
		t.Errorf("entry[1] = %+v, want tenant github", entries[1])
	}
	// Defaults applied to the tenant server.
	if entries[1].Transport != transportStreamableHTTP || entries[1].CallerIdentity != CallerIdentityNone {
		t.Errorf("tenant defaults wrong: transport=%q caller=%q", entries[1].Transport, entries[1].CallerIdentity)
	}

	// Three excluded: evil (forward), orphan (no tenant), broken (no endpoint).
	if len(skipped) != 3 {
		t.Fatalf("got %d skipped, want 3: %+v", len(skipped), skipped)
	}
	skippedNames := map[string]bool{}
	for _, s := range skipped {
		skippedNames[s.Name] = true
	}
	for _, want := range []string{"evil", "orphan", "broken"} {
		if !skippedNames[want] {
			t.Errorf("expected %q to be excluded; skipped=%+v", want, skipped)
		}
	}
}

func TestBuildMCPRegistry_Empty(t *testing.T) {
	entries, skipped := buildMCPRegistry(nil, map[string]string{})
	if len(entries) != 0 || len(skipped) != 0 {
		t.Errorf("empty input → entries=%v skipped=%v, want empty", entries, skipped)
	}
}
