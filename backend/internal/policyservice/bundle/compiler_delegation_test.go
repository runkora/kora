package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"testing"

	"github.com/clear-route/orbit/internal/policyservice/grants"
	"github.com/clear-route/orbit/internal/policyservice/routes"
)

// extractBundleFile un-tars a compiled bundle and returns the named entry.
func extractBundleFile(t *testing.T, bundleData []byte, name string) []byte {
	t.Helper()
	gr, err := gzip.NewReader(bytes.NewReader(bundleData))
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Name == name {
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read tar entry %s: %v", name, err)
			}
			return data
		}
	}
	t.Fatalf("bundle entry %s not found", name)
	return nil
}

// delegationTestGrants returns one marked (delegated agent) and one unmarked grant.
func delegationTestGrants() []*grants.Grant {
	return []*grants.Grant{
		{
			ID:                 "grant-delegated",
			TenantID:           "acme",
			PrincipalType:      grants.PrincipalTypeAgent,
			PrincipalID:        "spiffe://orbit.test/tenant/acme/agent/reviewer",
			Scopes:             []string{"inference:invoke"},
			DelegationRequired: true,
		},
		{
			ID:            "grant-autonomous",
			TenantID:      "acme",
			PrincipalType: grants.PrincipalTypeAgent,
			PrincipalID:   "spiffe://orbit.test/tenant/acme/agent/coder",
			Scopes:        []string{"inference:invoke"},
		},
	}
}

// assertDelegationMarkers decodes a grants/data.json document and verifies the
// delegation_required marker survived compilation on exactly the marked grant.
func assertDelegationMarkers(t *testing.T, grantsJSON []byte) {
	t.Helper()
	var doc struct {
		Grants []struct {
			ID                 string `json:"id"`
			DelegationRequired bool   `json:"delegation_required"`
		} `json:"grants"`
	}
	if err := json.Unmarshal(grantsJSON, &doc); err != nil {
		t.Fatalf("unmarshal grants doc: %v", err)
	}
	if len(doc.Grants) != 2 {
		t.Fatalf("expected 2 grants in document, got %d", len(doc.Grants))
	}
	for _, g := range doc.Grants {
		switch g.ID {
		case "grant-delegated":
			if !g.DelegationRequired {
				t.Error("grant-delegated must carry delegation_required=true in the bundle")
			}
		case "grant-autonomous":
			if g.DelegationRequired {
				t.Error("grant-autonomous must not carry delegation_required in the bundle")
			}
		default:
			t.Errorf("unexpected grant %q in document", g.ID)
		}
	}
}

// TestCompile_EmitsDelegationRequiredMarker verifies the per-tenant bundle
// carries the fail-closed marker into grants/data.json
// (delegated-agent-identity ADR, Decision 2).
func TestCompile_EmitsDelegationRequiredMarker(t *testing.T) {
	cfg := Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform"}
	compiler := NewCompiler(cfg, routes.NewStore())

	cached, err := compiler.Compile("acme", delegationTestGrants(), nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	assertDelegationMarkers(t, extractBundleFile(t, cached.Data, "grants/data.json"))
}

// TestCompileGateway_EmitsDelegationRequiredMarker verifies the marker also
// flows into the gateway bundle's per-tenant grants document.
func TestCompileGateway_EmitsDelegationRequiredMarker(t *testing.T) {
	cfg := Config{JWKSURL: "http://127.0.0.1:1/jwks", Issuer: "orbit", Audience: "orbit-platform", FrontendHost: "orbit.example.com"}
	compiler := NewCompiler(cfg, routes.NewStore())

	cached, err := compiler.CompileGateway(
		[]string{"acme"},
		map[string][]*grants.Grant{"acme": delegationTestGrants()},
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("compile gateway: %v", err)
	}
	assertDelegationMarkers(t, extractBundleFile(t, cached.Data, "tenants/acme/grants/data.json"))
}
