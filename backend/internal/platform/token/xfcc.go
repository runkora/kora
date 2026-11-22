package token

import (
	"fmt"
	"net/http"
	"strings"
)

// XFCCPrincipalExtractor reads the Envoy-injected x-forwarded-client-cert
// (XFCC) header and parses the SPIFFE URI out of it. This is the only
// implementation — it is used in production, local development, and tests alike.
//
// Example header written by Envoy for a kora agent workload (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md):
//
//	x-forwarded-client-cert: By=spiffe://runkora.test/system/envoy-proxy;Hash=…;URI=spiffe://runkora.test/tenant/platform-team/agent/ci-deploy-bot;DNS=ci-deploy-bot
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

	spiffeID, err := ParseXFCCSpiffeURI(xfcc)
	if err != nil {
		return nil, fmt.Errorf("parse xfcc header: %w", err)
	}

	principal, err := PrincipalFromSpiffeID(spiffeID)
	if err != nil {
		return nil, fmt.Errorf("build principal from spiffe id %q: %w", spiffeID, err)
	}

	return principal, nil
}

// ParseXFCCSpiffeURI extracts the URI=spiffe://… value from an XFCC header.
// Only the last element is read. Istio's inbound sidecar appends the verified
// peer to any XFCC the client sent (APPEND_FORWARD), so earlier elements are
// caller-supplied and untrusted.
func ParseXFCCSpiffeURI(xfcc string) (string, error) {
	elems := splitOutsideQuotes(xfcc, ',')
	last := elems[len(elems)-1]
	for _, field := range splitOutsideQuotes(last, ';') {
		field = strings.TrimSpace(field)
		if after, ok := strings.CutPrefix(field, "URI="); ok {
			uri := strings.Trim(strings.TrimSpace(after), `"`)
			if strings.HasPrefix(uri, "spiffe://") {
				return uri, nil
			}
		}
	}
	return "", fmt.Errorf("no SPIFFE URI field found in XFCC header")
}

// splitOutsideQuotes splits s on sep, ignoring separators inside double
// quotes. Envoy quotes a value such as Subject that contains commas.
func splitOutsideQuotes(s string, sep rune) []string {
	var out []string
	var inQuote bool
	start := 0
	for i, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == sep && !inQuote:
			out = append(out, s[start:i])
			start = i + len(string(r))
		}
	}
	return append(out, s[start:])
}

// PoolNamespace returns the tenant namespace of a pool pod SPIFFE ID,
// spiffe://{trust-domain}/tenant/{namespace}/pool/{pod}. A pool pod has not
// yet been bound to an agent (docs/adr/2026-11-22-agent-cold-tier.md). It is
// not a principal: PrincipalFromSpiffeID refuses it, and only the policy
// bundle download accepts it.
func PoolNamespace(spiffeID string) (string, bool) {
	withoutScheme, ok := strings.CutPrefix(spiffeID, "spiffe://")
	if !ok {
		return "", false
	}
	_, path, ok := strings.Cut(withoutScheme, "/")
	if !ok {
		return "", false
	}
	segments := strings.Split(path, "/")
	if len(segments) != 4 || segments[0] != "tenant" || segments[2] != "pool" || segments[1] == "" || segments[3] == "" {
		return "", false
	}
	return segments[1], true
}

// PrincipalFromSpiffeID constructs a Principal from a SPIFFE URI.
//
// Supported SPIFFE ID formats per docs/adr/2026-03-05-spiffe-spire-workload-attestation.md
// and docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md:
//
//	Agent:        spiffe://{trust-domain}/tenant/{tenant-id}/agent/{agent-slug}
//	Per-run sandbox:
//	              spiffe://{trust-domain}/tenant/{tenant-id}/agent/{agent-slug}/sandbox/{run-id}
//	Service:      spiffe://{trust-domain}/tenant/{tenant-id}/service/{service-id}
//	System:       spiffe://{trust-domain}/system/{component-name}
//
// Per-run sandbox SVIDs are normalized to the agent principal for
// authorization (so an agent's existing grants apply to all of its runs)
// while the run_id is lifted into Claims for attribution and audit. This is
// the "authorize as agent, attribute as run" model from the credential-broker
// ADR — it lets any human with the agent's scope drive any run, with every
// outbound call auditably tied to the specific run that made it.
//
// The full URI is stored in Claims["spiffe_id"] so policy rules and audit can
// always recover it; the principal ID is the agent URI (sandbox segment
// stripped) so OPA grant matching uses one identity per agent across runs.
func PrincipalFromSpiffeID(spiffeID string) (*Principal, error) {
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

	// Parse tenant and principal type/ID from the path structure:
	// /tenant/{tenant-id}/agent/{agent-slug}
	// /tenant/{tenant-id}/agent/{agent-slug}/sandbox/{run-id}
	// /tenant/{tenant-id}/service/{service-id}
	// /system/{component-name}
	var tenantID, displayName, runID, namespace string
	var agentURI string // the URI used as the principal ID — strips /sandbox/{run-id} when present
	var principalType PrincipalType

	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")

	// Parse principal type and identifiers based on path structure
	switch {
	case len(segments) >= 2 && segments[0] == "system":
		// System principals: /system/{component-name}
		principalType = PrincipalTypeSystem
		displayName = segments[1]
		// System principals have no tenant
	case len(segments) >= 6 && segments[0] == "tenant" && segments[2] == "agent" && segments[4] == "sandbox":
		// Per-run sandbox: /tenant/{id}/agent/{slug}/sandbox/{run-id}
		// Authorize as the agent (strip sandbox suffix); attribute as the run.
		principalType = PrincipalTypeAgent
		tenantID = strings.TrimPrefix(segments[1], "kora-tenant-")
		namespace = segments[1]
		displayName = segments[3]
		runID = segments[5]
		agentURI = "spiffe://" + trustDomain + "/tenant/" + segments[1] + "/agent/" + segments[3]
	case len(segments) >= 4 && segments[0] == "tenant":
		// Tenant-scoped principals: /tenant/{id}/agent|service/{name}
		// K8s namespaces are prefixed with "kora-tenant-" (e.g. "kora-tenant-example").
		// Strip the prefix so the tenantID matches the logical tenant (e.g. "example").
		tenantID = strings.TrimPrefix(segments[1], "kora-tenant-")
		namespace = segments[1]
		displayName = segments[3]

		switch segments[2] {
		case "agent":
			principalType = PrincipalTypeAgent
		case "service":
			principalType = PrincipalTypeService
		default:
			return nil, fmt.Errorf("unknown principal type %q in SPIFFE path %q", segments[2], path)
		}
	default:
		return nil, fmt.Errorf("malformed SPIFFE path: expected /tenant/{id}/agent|service/{name} or /system/{name}, got %q", path)
	}

	// Use the agent URI as the stable principal ID for sandbox SVIDs so OPA
	// grants on the agent apply to every run; for all other shapes use the
	// full SVID (which already is the agent/service URI).
	principalID := spiffeID
	if agentURI != "" {
		principalID = agentURI
	}

	claims := map[string]string{
		"spiffe_id":    spiffeID,
		"trust_domain": trustDomain,
		"spiffe_path":  path,
	}
	if runID != "" {
		claims["run_id"] = runID
	}

	return &Principal{
		ID:             principalID,
		Type:           principalType,
		TenantID:       tenantID,
		Namespace:      namespace,
		DisplayName:    displayName,
		IdentitySource: IdentitySourceAttested,
		Claims:         claims,
	}, nil
}
