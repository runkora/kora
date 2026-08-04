package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// ── parseXFCCSpiffeURI ────────────────────────────────────────────────────────

func TestParseXFCCSpiffeURI_SimpleURI(t *testing.T) {
	uri, err := parseXFCCSpiffeURI(`URI=spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("got %q, want spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot", uri)
	}
}

func TestParseXFCCSpiffeURI_MultiFieldEntry(t *testing.T) {
	// Envoy emits: By=...;Hash=...;URI=...;DNS=...
	xfcc := `By=spiffe://orbit.test/system/envoy-proxy;Hash=abc123;URI=spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot;DNS=ci-deploy-bot`
	uri, err := parseXFCCSpiffeURI(xfcc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("got %q", uri)
	}
}

func TestParseXFCCSpiffeURI_MultipleEntries(t *testing.T) {
	// Multiple comma-separated XFCC entries — first URI wins.
	xfcc := `URI=spiffe://orbit.test/tenant/team-a/agent/bot-1,URI=spiffe://orbit.test/tenant/team-b/agent/bot-2`
	uri, err := parseXFCCSpiffeURI(xfcc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uri != "spiffe://orbit.test/tenant/team-a/agent/bot-1" {
		t.Errorf("got %q", uri)
	}
}

func TestParseXFCCSpiffeURI_NoURIField(t *testing.T) {
	_, err := parseXFCCSpiffeURI(`By=spiffe://orbit.test/system/envoy-proxy;Hash=abc123`)
	if err == nil {
		t.Error("expected error for missing URI field")
	}
}

func TestParseXFCCSpiffeURI_EmptyString(t *testing.T) {
	_, err := parseXFCCSpiffeURI("")
	if err == nil {
		t.Error("expected error for empty XFCC header")
	}
}

func TestParseXFCCSpiffeURI_NonSpiffeURIField(t *testing.T) {
	// URI field present but not a SPIFFE URI — should not be returned.
	_, err := parseXFCCSpiffeURI(`URI=https://example.com/cert`)
	if err == nil {
		t.Error("expected error for non-SPIFFE URI field")
	}
}

// ── principalFromSpiffeID ─────────────────────────────────────────────────────

func TestPrincipalFromSpiffeID_SetsFields(t *testing.T) {
	p, err := principalFromSpiffeID("spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Type != PrincipalTypeAgent {
		t.Errorf("Type: got %q", p.Type)
	}
	if p.IdentitySource != IdentitySourceAttested {
		t.Errorf("IdentitySource: got %q", p.IdentitySource)
	}
	if p.Claims["spiffe_id"] != "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("Claims[spiffe_id]: got %q", p.Claims["spiffe_id"])
	}
	if p.Claims["trust_domain"] != "orbit.test" {
		t.Errorf("Claims[trust_domain]: got %q", p.Claims["trust_domain"])
	}
	if p.Claims["spiffe_path"] != "/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("Claims[spiffe_path]: got %q", p.Claims["spiffe_path"])
	}
}

func TestPrincipalFromSpiffeID_NotSpiffeURI(t *testing.T) {
	_, err := principalFromSpiffeID("https://not-spiffe/foo")
	if err == nil {
		t.Error("expected error for non-SPIFFE URI")
	}
}

func TestPrincipalFromSpiffeID_NoPathComponent(t *testing.T) {
	_, err := principalFromSpiffeID("spiffe://trust-domain-only")
	if err == nil {
		t.Error("expected error for SPIFFE URI without path")
	}
}

// ── per-run sandbox SVID ──────────────────────────────────────────────────────
//
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md introduces a
// new SVID shape for per-run sandboxes:
//
//	spiffe://orbit.test/tenant/{ns}/agent/{slug}/sandbox/{run-id}
//
// The extractor must normalize this to the agent principal for authorization
// (so existing agent grants apply unchanged across runs) while lifting the
// run_id into Claims for attribution.

func TestPrincipalFromSpiffeID_SandboxNormalizesToAgent(t *testing.T) {
	const sandboxURI = "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder/sandbox/run-9"
	const agentURI = "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder"

	p, err := principalFromSpiffeID(sandboxURI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Authorization key: the agent URI, NOT the per-run URI. This is the
	// load-bearing assertion — OPA grants on /agent/coder must authorize
	// every run of that agent.
	if p.ID != agentURI {
		t.Errorf("ID = %q, want %q (sandbox SVIDs must normalize to the agent URI)", p.ID, agentURI)
	}
	if p.Type != PrincipalTypeAgent {
		t.Errorf("Type = %q, want %q", p.Type, PrincipalTypeAgent)
	}
	if p.TenantID != "acme" {
		t.Errorf("TenantID = %q, want %q (orbit-tenant- prefix should be stripped)", p.TenantID, "acme")
	}
	if p.DisplayName != "coder" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "coder")
	}

	// Attribution: run_id must be present in claims.
	if got := p.Claims["run_id"]; got != "run-9" {
		t.Errorf("Claims[run_id] = %q, want run-9", got)
	}
	// The full SVID is preserved for audit / log correlation.
	if got := p.Claims["spiffe_id"]; got != sandboxURI {
		t.Errorf("Claims[spiffe_id] = %q, want %q", got, sandboxURI)
	}
	if got := p.Claims["spiffe_path"]; got != "/tenant/orbit-tenant-acme/agent/coder/sandbox/run-9" {
		t.Errorf("Claims[spiffe_path] = %q", got)
	}
}

// Regression: a plain agent SVID must continue to parse as it did before the
// sandbox extension. The principal ID stays the full URI (no normalization
// needed); run_id must NOT appear in claims.
func TestPrincipalFromSpiffeID_AgentPathUnchanged(t *testing.T) {
	const agentURI = "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot"
	p, err := principalFromSpiffeID(agentURI)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != agentURI {
		t.Errorf("ID = %q, want %q (non-sandbox shape should be untouched)", p.ID, agentURI)
	}
	if _, ok := p.Claims["run_id"]; ok {
		t.Errorf("Claims[run_id] must not be set for non-sandbox SVIDs; got %q", p.Claims["run_id"])
	}
}

// ── devenv-prefixed SVIDs ─────────────────────────────────────────────────────
//
// docs/adr/2026-08-04-devenv-mode-platform-support.md (F-5) prefixes
// dev-environment SVIDs with /devenv/{env-id} because environment workloads
// are attested by the host SPIRE deployment. The extractor must strip the
// prefix, lift the env-id into Claims, and otherwise parse identically to
// the non-devenv shape.

func TestPrincipalFromSpiffeID_DevenvSystemPathStrips(t *testing.T) {
	p, err := principalFromSpiffeID("spiffe://orbit.test/devenv/spike-1/system/foo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Type != PrincipalTypeSystem {
		t.Errorf("Type = %q, want %q", p.Type, PrincipalTypeSystem)
	}
	if p.DisplayName != "foo" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "foo")
	}
	if p.Claims["devenv_id"] != "spike-1" {
		t.Errorf("Claims[devenv_id] = %q, want %q", p.Claims["devenv_id"], "spike-1")
	}
	if p.Claims["spiffe_path"] != "/devenv/spike-1/system/foo" {
		t.Errorf("Claims[spiffe_path] = %q, want original path preserved", p.Claims["spiffe_path"])
	}
}

func TestPrincipalFromSpiffeID_DevenvTenantAgentPathStrips(t *testing.T) {
	const uri = "spiffe://orbit.test/devenv/spike-1/tenant/orbit-tenant-acme/agent/coder"
	p, err := principalFromSpiffeID(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Type != PrincipalTypeAgent {
		t.Errorf("Type = %q, want %q", p.Type, PrincipalTypeAgent)
	}
	if p.TenantID != "acme" {
		t.Errorf("TenantID = %q, want %q", p.TenantID, "acme")
	}
	if p.DisplayName != "coder" {
		t.Errorf("DisplayName = %q, want %q", p.DisplayName, "coder")
	}
	if p.Claims["devenv_id"] != "spike-1" {
		t.Errorf("Claims[devenv_id] = %q, want %q", p.Claims["devenv_id"], "spike-1")
	}
	if p.ID != uri {
		t.Errorf("ID = %q, want %q (non-sandbox shape keeps the full URI as principal ID)", p.ID, uri)
	}
}

func TestPrincipalFromSpiffeID_DevenvSandboxPathNormalizesToAgent(t *testing.T) {
	const uri = "spiffe://orbit.test/devenv/spike-1/tenant/orbit-tenant-acme/agent/coder/sandbox/run-42"
	p, err := principalFromSpiffeID(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const wantAgentURI = "spiffe://orbit.test/tenant/orbit-tenant-acme/agent/coder"
	if p.ID != wantAgentURI {
		t.Errorf("ID = %q, want %q (devenv prefix must not survive into the normalized agent ID)", p.ID, wantAgentURI)
	}
	if p.Claims["run_id"] != "run-42" {
		t.Errorf("Claims[run_id] = %q, want %q", p.Claims["run_id"], "run-42")
	}
	if p.Claims["devenv_id"] != "spike-1" {
		t.Errorf("Claims[devenv_id] = %q, want %q", p.Claims["devenv_id"], "spike-1")
	}
}

func TestPrincipalFromSpiffeID_DevenvNeverParsesAsBareSystemOrTenant(t *testing.T) {
	// A devenv-prefixed ID must always carry the devenv_id claim — it can
	// never silently collapse into a principal indistinguishable from a
	// bare system/tenant SVID, which is the property host authorization
	// policy safety depends on.
	for _, uri := range []string{
		"spiffe://orbit.test/devenv/spike-1/system/foo",
		"spiffe://orbit.test/devenv/spike-1/tenant/orbit-tenant-acme/agent/coder",
	} {
		p, err := principalFromSpiffeID(uri)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", uri, err)
		}
		if p.Claims["devenv_id"] == "" {
			t.Errorf("%q: Claims[devenv_id] must be set for a devenv-prefixed SVID", uri)
		}
	}
}

func TestPrincipalFromSpiffeID_MalformedDevenvPath(t *testing.T) {
	tests := []string{
		"spiffe://orbit.test/devenv/spike-1",   // no principal path after the env id
		"spiffe://orbit.test/devenv//system/x", // empty env id
		"spiffe://orbit.test/devenv",           // no env id, no principal path
	}
	for _, uri := range tests {
		if _, err := principalFromSpiffeID(uri); err == nil {
			t.Errorf("%q: expected error for malformed devenv path", uri)
		}
	}
}

func TestPrincipalFromSpiffeID_NonDevenvPathUntouched(t *testing.T) {
	// A path that merely starts with something other than "devenv" must
	// parse exactly as before — stripDevEnvPrefix must be a no-op off the
	// devenv-prefixed path.
	const uri = "spiffe://orbit.test/system/foo"
	p, err := principalFromSpiffeID(uri)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := p.Claims["devenv_id"]; ok {
		t.Errorf("Claims[devenv_id] must not be set for a non-devenv path; got %q", p.Claims["devenv_id"])
	}
}

// ── XFCCPrincipalExtractor ────────────────────────────────────────────────────

func TestXFCCPrincipalExtractor_Success(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("x-forwarded-client-cert",
		`By=spiffe://orbit.test/system/envoy-proxy;Hash=abc;URI=spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot;DNS=ci-deploy-bot`)

	x := &XFCCPrincipalExtractor{}
	p, err := x.Extract(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot" {
		t.Errorf("principal ID: got %q", p.ID)
	}
	if p.Type != PrincipalTypeAgent {
		t.Errorf("principal type: got %q", p.Type)
	}
}

func TestXFCCPrincipalExtractor_MissingHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	x := &XFCCPrincipalExtractor{}
	_, err := x.Extract(req)
	if err == nil {
		t.Error("expected error when XFCC header is absent")
	}
}

func TestXFCCPrincipalExtractor_InvalidHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("x-forwarded-client-cert", `By=spiffe://orbit.test/system/envoy-proxy;Hash=bar`)
	x := &XFCCPrincipalExtractor{}
	_, err := x.Extract(req)
	if err == nil {
		t.Error("expected error when XFCC header has no URI field")
	}
}
