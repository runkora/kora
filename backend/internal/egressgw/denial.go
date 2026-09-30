package egressgw

import "fmt"

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
	cause        error
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
func (d *Denial) Message() string {
	switch d.Reason {
	case DenialNotGranted:
		return fmt.Sprintf("kora egress policy denied %s: this agent is not granted %s on connection %s. To allow it, grant %s.",
			d.Host, d.actionPhrase(), d.Connection, d.RequiredScope)
	case DenialGrantNotApplied:
		return fmt.Sprintf("kora egress policy denied %s: this agent's grant for %s on connection %s does not apply to this call (%s).",
			d.Host, d.actionPhrase(), d.Connection, d.PolicyReason)
	case DenialUnclassified:
		return fmt.Sprintf("kora egress policy denied %s: connection %s does not map this request to a grantable action (an unsupported API shape, or a resource outside the connection), so no grant can allow it.",
			d.Host, d.Connection)
	case DenialCredentialRefused:
		return fmt.Sprintf("kora egress policy allowed %s on connection %s, but the upstream refused to issue a credential for it. To allow it, add that permission upstream (for GitHub, on the App installation).",
			d.actionPhrase(), d.Connection)
	case DenialPolicyUnavailable:
		return fmt.Sprintf("kora egress policy could not be evaluated for %s, so the call was denied. Retry shortly; report it to an admin if it persists.",
			d.Host)
	default:
		return "egress denied"
	}
}

// Details is the structured half of the denial body.
func (d *Denial) Details() map[string]any {
	details := map[string]any{"reason": string(d.Reason), "host": d.Host, "connection": d.Connection}
	for key, value := range map[string]string{"action": d.Action, "qualifier": d.Qualifier, "required_scope": d.RequiredScope, "policy_reason": d.PolicyReason} {
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
