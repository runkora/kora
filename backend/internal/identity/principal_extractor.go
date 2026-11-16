package identity

import (
	"net/http"

	"github.com/runkora/kora/backend/internal/platform/token"
)

// PrincipalExtractor extracts a verified Principal from an inbound HTTP request.
//
// In production (Istio + SPIRE) the Envoy sidecar terminates mTLS and injects
// the peer's verified SPIFFE URI into the x-forwarded-client-cert (XFCC) header
// before forwarding the request to the identity service. The identity service
// trusts that header because:
//  1. The Envoy sidecar performs the mTLS handshake and cryptographic SVID
//     validation — no re-validation is needed in application code.
//  2. Istio / Cilium (docs/adr/2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) ensures traffic cannot reach the service without
//     passing through the sidecar, so the header cannot be forged by a caller.
//
// Agents make plain HTTP calls — they never handle SVID material directly
// (docs/adr/2026-03-05-spiffe-spire-workload-attestation.md, docs/adr/2026-03-05-universal-gateway-policy-enforcement.md).
//
// In local development there is no Envoy sidecar, but there is also no threat
// model — callers simply set the x-forwarded-client-cert header directly (e.g.
// via curl or test code). The security guarantee lives in the mesh topology, not
// in application code.
type PrincipalExtractor interface {
	Extract(r *http.Request) (*Principal, error)
}

// XFCCPrincipalExtractor is implemented in internal/platform/token so services
// that must not import this package can use it.
type XFCCPrincipalExtractor = token.XFCCPrincipalExtractor

var (
	parseXFCCSpiffeURI    = token.ParseXFCCSpiffeURI
	principalFromSpiffeID = token.PrincipalFromSpiffeID
)
