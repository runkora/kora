package inferencegw

import "context"

// ─── PolicyEvaluator ─────────────────────────────────────────────────────────

// InferencePolicyRequest carries the context the policy engine needs to decide
// whether a given principal may call a given model.
type InferencePolicyRequest struct {
	TenantID      string
	PrincipalID   string
	PrincipalType string
	Provider      ProviderName
	ModelID       string
	// RawToken is the raw platform JWT for the principal, used by the
	// PolicyGatewayEvaluator to call the policy gateway check API.
	RawToken string
}

// PolicyDecision is the outcome of a policy evaluation.
type PolicyDecision struct {
	// Allowed is true when the request may proceed.
	Allowed bool
	// Reason is a human-readable explanation (logged on denial, never sent to caller).
	Reason string
}

// PolicyEvaluator evaluates whether an inference request is permitted.
// The production implementation will delegate to the OPA policy engine
// (ADR-0011). Until that integration exists, AllowAllPolicyEvaluator is used.
type PolicyEvaluator interface {
	EvaluateInferenceRequest(ctx context.Context, req *InferencePolicyRequest) (*PolicyDecision, error)
}

// ─── AllowAllPolicyEvaluator ─────────────────────────────────────────────────

// AllowAllPolicyEvaluator is the stub implementation used until OPA is
// integrated (ADR-0011). It permits every request unconditionally.
//
// This must be replaced before any production deployment — it exists only to
// allow the service to boot and function during the MVP development phase.
type AllowAllPolicyEvaluator struct{}

// EvaluateInferenceRequest always returns Allowed=true.
func (a *AllowAllPolicyEvaluator) EvaluateInferenceRequest(
	_ context.Context,
	_ *InferencePolicyRequest,
) (*PolicyDecision, error) {
	return &PolicyDecision{
		Allowed: true,
		Reason:  "policy engine not yet integrated — all requests permitted",
	}, nil
}
