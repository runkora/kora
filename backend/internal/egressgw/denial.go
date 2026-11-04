package egressgw

import (
	"fmt"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
)

// DenialReason names why the broker refused an egress call. It is part of the
// denial body, so callers can branch on it without parsing the message.
type DenialReason string

// Denial reasons, one per broker deny path.
const (
	DenialNotGranted        DenialReason = "not_granted"
	DenialGrantNotApplied   DenialReason = "grant_not_applied"
	DenialUnclassified      DenialReason = "unclassified"
	DenialCredentialRefused DenialReason = "credential_refused"
	DenialPolicyUnavailable DenialReason = "policy_unavailable"
	// DenialConnectionUnresolved is a request on a host several connections
	// serve that selects none of them.
	DenialConnectionUnresolved DenialReason = "connection_unresolved"
)

// Denial is a refused egress call described in terms the agent can act on: the
// host, the classified action, and what would allow it. It unwraps to
// ErrForbidden and its cause, so errors.Is callers are unaffected.
type Denial struct {
	Reason     DenialReason
	Host       string
	Connection string
	Action     string
	Qualifier  string
	// RequiredScope is the scope a grant must cover, set only on a not-granted
	// denial since that is the one a grant can fix.
	RequiredScope string
	// PolicyReason is the policy decision label when a held grant did not apply,
	// such as deny:delegation_required. It is our own bundle's label, never a secret.
	PolicyReason string
	// Candidates are the connections serving the host, set only on an
	// unresolved-connection denial.
	Candidates []string
	// Unresolved states which part of the request could not be tied to an
	// owner, set only on an unresolved-connection denial.
	Unresolved string
	// ambiguous marks an unresolved request the caller holds several
	// candidates for. The anonymous read fallback does not serve it.
	ambiguous bool
	// adapter is the candidates' adapter, which decides whether the anonymous
	// read fallback applies.
	adapter credentials.AdapterType
	// decision is the raw policy label behind a policy denial, for the log.
	decision string
	cause    error
}

// Error reports the denial reason; Message carries the caller-facing text.
func (d *Denial) Error() string {
	if d.cause == nil {
		return ErrForbidden.Error() + ": " + string(d.Reason)
	}
	return ErrForbidden.Error() + ": " + string(d.Reason) + ": " + d.cause.Error()
}

// Unwrap exposes ErrForbidden and the underlying cause to errors.Is and errors.As.
func (d *Denial) Unwrap() []error {
	if d.cause == nil {
		return []error{ErrForbidden}
	}
	return []error{ErrForbidden, d.cause}
}

// Message is the caller-facing explanation. It names only the caller's own
// tenant's connection and scope vocabulary, never grants or credentials.
// product is the configured product name the policy is attributed to.
func (d *Denial) Message(product string) string {
	switch d.Reason {
	case DenialNotGranted:
		return fmt.Sprintf("%s egress policy denied %s: this agent is not granted %s on connection %s. To allow it, grant %s.",
			product, d.Host, d.actionPhrase(), d.Connection, d.RequiredScope)
	case DenialGrantNotApplied:
		return fmt.Sprintf("%s egress policy denied %s: this agent's grant for %s on connection %s does not apply to this call (%s).",
			product, d.Host, d.actionPhrase(), d.Connection, d.PolicyReason)
	case DenialUnclassified:
		return fmt.Sprintf("%s egress policy denied %s: connection %s does not map this request to a grantable action (an unsupported API shape, or a resource outside the connection), so no grant can allow it.",
			product, d.Host, d.Connection)
	case DenialCredentialRefused:
		return fmt.Sprintf("%s egress policy allowed %s on connection %s, but the upstream refused to issue a credential for it. To allow it, add that permission upstream (for GitHub, on the App installation).",
			product, d.actionPhrase(), d.Connection)
	case DenialConnectionUnresolved:
		return fmt.Sprintf("%s egress policy denied %s: connections %s all serve this host, and the request selects none of them (%s). A connection is selected by the owner in the request path, the owners a GraphQL document or search query names, or the owners of a GraphQL document's node ids. A request that names none needs the agent to hold exactly one of these connections.",
			product, d.Host, strings.Join(d.Candidates, ", "), d.Unresolved)
	case DenialPolicyUnavailable:
		return fmt.Sprintf("%s egress policy could not be evaluated for %s, so the call was denied. Retry shortly; report it to an admin if it persists.",
			product, d.Host)
	default:
		return "egress denied"
	}
}

// Details is the structured half of the denial body.
func (d *Denial) Details() map[string]any {
	details := map[string]any{"reason": string(d.Reason), "host": d.Host, "connection": d.Connection}
	if len(d.Candidates) > 0 {
		details["candidates"] = d.Candidates
	}
	for key, value := range map[string]string{"unresolved": d.Unresolved, "action": d.Action, "qualifier": d.Qualifier, "required_scope": d.RequiredScope, "policy_reason": d.PolicyReason} {
		if value != "" {
			details[key] = value
		}
	}
	return details
}

func (d *Denial) actionPhrase() string {
	if d.Qualifier == "" {
		return d.Action
	}
	return d.Action + " for " + d.Qualifier
}
