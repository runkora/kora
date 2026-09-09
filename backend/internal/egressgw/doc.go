// Package egressgw is the per-tenant egress credential broker. It receives
// outbound requests from a sandbox (carrying the agent's platform token), looks
// up the connection by the upstream Host the request is destined for, asks the
// adapter to classify the call into a policy action, authorizes the egress
// against policy, asks the matching credentials.CredentialAdapter for a
// just-in-time credential, swaps the platform token for the minted upstream
// credential, and forwards the request to the real third-party service. The
// agent never holds the upstream credential.
//
// Sandboxed tools reach the gateway as an HTTPS proxy. It terminates TLS
// against a per-tenant CA the sandbox trusts, then re-originates to the real
// upstream. CONNECT to :443, and to any port a connection is registered on, is
// intercepted this way; every other port is relayed as raw TCP.
// See docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
package egressgw

import "errors"

// resourceType is the policy resource type for connection authorization. The
// gateway evaluates resourceType=connection / pathSegments=[connection-name] /
// action=<adapter-classified-action>.
const resourceType = "connection"

// Sentinel errors. Callers may test for these with errors.Is to branch on
// failure category without string matching.
var (
	// ErrUnauthenticated is returned when the inbound request has no platform
	// token, an invalid one, or one whose claims don't yield a usable principal.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden is returned when policy denies the connection use for this
	// principal. No credential is minted; nothing reaches the upstream.
	ErrForbidden = errors.New("connection use denied by policy")

	// ErrTenantMismatch is returned when the caller's tenant does not match the
	// connection's tenant. Cross-tenant egress does not exist.
	ErrTenantMismatch = errors.New("connection tenant mismatch")

	// ErrMissingHost is returned when the inbound request's Host is empty. The
	// gateway has no way to resolve the destination connection without it.
	ErrMissingHost = errors.New("inbound request missing host")
)
