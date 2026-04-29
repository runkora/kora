package identity

import (
	"fmt"
	"net/http"
	"strings"
)

// PrincipalExtractor extracts a verified Principal from an inbound HTTP request.
//
// In production (Istio + SPIRE) the Envoy sidecar terminates mTLS and injects
// the peer's verified SPIFFE URI into the x-forwarded-client-cert (XFCC) header
// before forwarding the request to the identity service. The identity service
// trusts that header because:
//  1. The Envoy sidecar performs the mTLS handshake and cryptographic SVID
//     validation — no re-validation is needed in application code.
//  2. Istio / Cilium (ADR-0015) ensures traffic cannot reach the service without
//     passing through the sidecar, so the header cannot be forged by a caller.
//
// Agents make plain HTTP calls — they never handle SVID material directly
// (ADR-0010, ADR-0011).
//
// In local development there is no Envoy sidecar, but there is also no threat
// model — callers simply set the x-forwarded-client-cert header directly (e.g.
// via curl or test code). The security guarantee lives in the mesh topology, not
// in application code.
type PrincipalExtractor interface {
	Extract(r *http.Request) (*Principal, error)
}

// XFCCPrincipalExtractor reads the Envoy-injected x-forwarded-client-cert
// (XFCC) header and parses the SPIFFE URI out of it. This is the only
// implementation — it is used in production, local development, and tests alike.
//
// Example header written by Envoy for an Orbit agent workload (ADR-0010):
//
//	x-forwarded-client-cert: By=spiffe://orbit.test/system/envoy-proxy;Hash=…;URI=spiffe://orbit.test/tenant/platform-team/agent/ci-deploy-bot;DNS=ci-deploy-bot
//
// Only the URI field is consumed here; all other fields are ignored.
type XFCCPrincipalExtractor struct{}

// Extract parses the SPIFFE URI from the XFCC header and returns a Principal.
// Returns an error if the header is absent or contains no URI field.
func (x *XFCCPrincipalExtractor) Extract(r *http.Request) (*Principal, error) {
	xfcc := r.Header.Get("x-forwarded-client-cert")
	if xfcc == "" {
		return nil, fmt.Errorf("x-forwarded-client-cert header is absent: request did not arrive via mTLS sidecar")
	}

	spiffeID, err := parseXFCCSpiffeURI(xfcc)
	if err != nil {
		return nil, fmt.Errorf("parse xfcc header: %w", err)
	}

	principal, err := principalFromSpiffeID(spiffeID)
	if err != nil {
		return nil, fmt.Errorf("build principal from spiffe id %q: %w", spiffeID, err)
	}

	return principal, nil
}

// parseXFCCSpiffeURI extracts the URI=spiffe://… value from an XFCC header.
// The XFCC header may contain multiple comma-separated entries; the first entry
// with a URI= field wins.
func parseXFCCSpiffeURI(xfcc string) (string, error) {
	// Each XFCC entry is semicolon-delimited; multiple entries are
	// comma-separated. We only need the first SPIFFE URI found.
	for entry := range strings.SplitSeq(xfcc, ",") {
		for field := range strings.SplitSeq(strings.TrimSpace(entry), ";") {
			field = strings.TrimSpace(field)
			if after, ok := strings.CutPrefix(field, "URI="); ok {
				uri := strings.TrimSpace(after)
				if strings.HasPrefix(uri, "spiffe://") {
					return uri, nil
				}
			}
		}
	}
	return "", fmt.Errorf("no SPIFFE URI field found in XFCC header")
}

// principalFromSpiffeID constructs a Principal from a SPIFFE URI.
//
// Supported SPIFFE ID formats per ADR-0010:
//
//	Agent:   spiffe://orbit.{instance-id}/tenant/{tenant-id}/agent/{agent-id}
//	Service: spiffe://orbit.{instance-id}/tenant/{tenant-id}/service/{service-id}
//	System:  spiffe://orbit.{instance-id}/system/{component-name}
//
// Istio mTLS currently requires the standard SPIFFE path shape:
//
//	Istio:   spiffe://orbit.{instance-id}/ns/{namespace}/sa/{service-account}
//
// Known Orbit system service accounts are normalized back to their canonical
// /system/{component-name} principal while preserving the transport identity in
// Claims["transport_spiffe_id"].
//
// The canonical Orbit URI is stored as the principal ID and in
// Claims["spiffe_id"] so that policy rules can match on it directly (ADR-0010).
func principalFromSpiffeID(spiffeID string) (*Principal, error) {
	if !strings.HasPrefix(spiffeID, "spiffe://") {
		return nil, fmt.Errorf("not a SPIFFE URI: %q", spiffeID)
	}

	// Strip scheme for path parsing: {trust-domain}/{path}
	withoutScheme := strings.TrimPrefix(spiffeID, "spiffe://")
	slashIdx := strings.Index(withoutScheme, "/")
	if slashIdx < 0 {
		return nil, fmt.Errorf("malformed SPIFFE URI: no path component in %q", spiffeID)
	}

	trustDomain := withoutScheme[:slashIdx]
	path := withoutScheme[slashIdx:] // e.g. /tenant/platform-team/agent/ci-deploy-bot

	// Parse tenant and principal type/ID from ADR-0010 path structure:
	// /tenant/{tenant-id}/agent/{agent-id}
	// /tenant/{tenant-id}/service/{service-id}
	// /system/{component-name}
	var tenantID, displayName string
	var principalType PrincipalType

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	canonicalID := spiffeID

	// Parse principal type and identifiers based on path structure
	switch {
	case len(segments) >= 2 && segments[0] == "system":
		// System principals: /system/{component-name}
		principalType = PrincipalTypeSystem
		displayName = segments[1]
		// System principals have no tenant
	case len(segments) >= 4 && segments[0] == "tenant":
		// Tenant-scoped principals: /tenant/{id}/agent|service/{name}
		tenantID = segments[1]
		displayName = segments[3]

		switch segments[2] {
		case "agent":
			principalType = PrincipalTypeAgent
		case "service":
			principalType = PrincipalTypeService
		default:
			return nil, fmt.Errorf("unknown principal type %q in SPIFFE path %q", segments[2], path)
		}
	case len(segments) == 4 && segments[0] == "ns" && segments[2] == "sa":
		component, ok := systemComponentFromIstioIdentity(segments[1], segments[3])
		if !ok {
			return nil, fmt.Errorf("unsupported Istio SPIFFE path %q: only known Orbit system service accounts can be normalized", path)
		}
		principalType = PrincipalTypeSystem
		displayName = component
		canonicalID = "spiffe://" + trustDomain + "/system/" + component
	default:
		return nil, fmt.Errorf("malformed SPIFFE path: expected /tenant/{id}/agent|service/{name} or /system/{name}, got %q", path)
	}

	claims := map[string]string{
		"spiffe_id":    canonicalID,
		"trust_domain": trustDomain,
		"spiffe_path":  strings.TrimPrefix(canonicalID, "spiffe://"+trustDomain),
	}
	if canonicalID != spiffeID {
		claims["transport_spiffe_id"] = spiffeID
		claims["transport_spiffe_path"] = path
	}

	return &Principal{
		// Use the canonical SPIFFE URI as the stable principal ID so that
		// policy rules can match on it directly.
		ID:             canonicalID,
		Type:           principalType,
		TenantID:       tenantID,
		DisplayName:    displayName,
		IdentitySource: IdentitySourceAttested,
		Claims:         claims,
	}, nil
}

func systemComponentFromIstioIdentity(namespace, serviceAccount string) (string, bool) {
	if namespace != "orbit-system" {
		return "", false
	}

	switch serviceAccount {
	case "orbit-frontend":
		return "frontend", true
	case "orbit-identity":
		return "identity", true
	case "orbit-inference-gateway":
		return "inference-gateway", true
	case "orbit-policy-gateway":
		return "policy-gateway", true
	case "orbit-policy-service":
		return "policy-service", true
	default:
		return "", false
	}
}
