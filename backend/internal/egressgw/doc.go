// Package egressgw is the per-tenant egress credential broker. It receives
// outbound requests from a sandbox (carrying the agent's platform token), looks
// up the connection by the upstream Host the request is destined for, asks the
// adapter to classify the call into a policy action, authorizes the egress
// against policy, asks the matching credentials.CredentialAdapter for a
// just-in-time credential, swaps the platform token for the minted upstream
// credential, and forwards the request to the real third-party service. The
// agent never holds the upstream credential.
//
// In Phase 2a the gateway acts as a normal HTTPS client toward the upstream;
// transparent TLS interception against a per-tenant CA lands in Phase 2b. The
// resolution model is the same in both phases — connection lookup is by Host,
// action classification is by the adapter — so 2b only changes the transport,
// not the broker logic. See the ADR:
// docs/adr/2026-06-06-jit-credential-brokering-egress-gateway.md.
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

	// ErrUpstream is returned when the gateway successfully minted a credential
	// but the upstream forward leg failed (transport error or non-2xx that the
	// gateway treats as fatal).
	ErrUpstream = errors.New("upstream call failed")

	// ErrMissingHost is returned when the inbound request's Host is empty —
	// the gateway has no way to resolve the destination connection.
	ErrMissingHost = errors.New("inbound request missing host")
)
