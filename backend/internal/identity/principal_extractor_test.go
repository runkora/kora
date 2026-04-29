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

func TestPrincipalFromSpiffeID_NormalizesIstioSystemIdentity(t *testing.T) {
	transportID := "spiffe://orbit.test/ns/orbit-system/sa/orbit-inference-gateway"
	p, err := principalFromSpiffeID(transportID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "spiffe://orbit.test/system/inference-gateway" {
		t.Errorf("ID: got %q", p.ID)
	}
	if p.Type != PrincipalTypeSystem {
		t.Errorf("Type: got %q", p.Type)
	}
	if p.DisplayName != "inference-gateway" {
		t.Errorf("DisplayName: got %q", p.DisplayName)
	}
	if p.Claims["spiffe_id"] != "spiffe://orbit.test/system/inference-gateway" {
		t.Errorf("Claims[spiffe_id]: got %q", p.Claims["spiffe_id"])
	}
	if p.Claims["spiffe_path"] != "/system/inference-gateway" {
		t.Errorf("Claims[spiffe_path]: got %q", p.Claims["spiffe_path"])
	}
	if p.Claims["transport_spiffe_id"] != transportID {
		t.Errorf("Claims[transport_spiffe_id]: got %q", p.Claims["transport_spiffe_id"])
	}
	if p.Claims["transport_spiffe_path"] != "/ns/orbit-system/sa/orbit-inference-gateway" {
		t.Errorf("Claims[transport_spiffe_path]: got %q", p.Claims["transport_spiffe_path"])
	}
}

func TestPrincipalFromSpiffeID_RejectsUnknownIstioIdentity(t *testing.T) {
	_, err := principalFromSpiffeID("spiffe://orbit.test/ns/orbit-tenant-example/sa/orbit-example")
	if err == nil {
		t.Error("expected error for unsupported Istio SPIFFE identity")
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

func TestXFCCPrincipalExtractor_NormalizesIstioSystemIdentity(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("x-forwarded-client-cert",
		`By=spiffe://orbit.test/system/envoy-proxy;Hash=abc;URI=spiffe://orbit.test/ns/orbit-system/sa/orbit-policy-gateway;DNS=orbit-policy-gateway`)

	x := &XFCCPrincipalExtractor{}
	p, err := x.Extract(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.ID != "spiffe://orbit.test/system/policy-gateway" {
		t.Errorf("principal ID: got %q", p.ID)
	}
	if p.Type != PrincipalTypeSystem {
		t.Errorf("principal type: got %q", p.Type)
	}
	if p.Claims["transport_spiffe_id"] != "spiffe://orbit.test/ns/orbit-system/sa/orbit-policy-gateway" {
		t.Errorf("transport identity: got %q", p.Claims["transport_spiffe_id"])
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
