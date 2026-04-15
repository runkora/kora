// Package policy provides in-process OPA policy evaluation with automatic
// bundle lifecycle management. Zero network hops on the evaluation hot path.
//
// Instantiate with NewEvaluator, call Start before first use.
package policy

import (
	"context"
	"time"

	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/token"
)

// Evaluator provides in-process OPA policy evaluation.
// The OPA bundle is fetched from the policy-service in the background;
// all Check and Query calls are pure in-process evaluation — no network hop.
type Evaluator interface {
	// Check returns whether the principal may perform action on resource.
	// Lazily loads the tenant bundle on first call; subsequent calls are
	// pure in-process OPA evaluation.
	Check(ctx context.Context, input CheckInput) (Result, error)

	// Query filters candidates to those the principal may access.
	// Uses the same in-process bundle; no OPA round-trip per candidate.
	Query(ctx context.Context, input QueryInput) ([]string, error)

	// Scopes returns the complete set of scopes the principal holds
	// (grants + role-derived scopes). Used by the policy gateway to project
	// permissions into the frontend session.
	Scopes(ctx context.Context, input ScopesInput) ([]string, error)

	// ScopesExplain returns the principal's scopes broken down by source
	// (grants with IDs, roles with names). Used by the admin dashboard's
	// evaluate page for permission inspection.
	ScopesExplain(ctx context.Context, input ScopesInput) (*ScopesExplainResult, error)

	// Start begins background bundle polling. Call once at service startup.
	Start(ctx context.Context)
}

// ScopesExplainResult holds the scope breakdown by source.
type ScopesExplainResult struct {
	FromGrants      []GrantDetail `json:"from_grants"`
	FromRoles       []RoleDetail  `json:"from_roles"`
	EffectiveScopes []string      `json:"effective_scopes"`
}

// GrantDetail is a single grant's contribution to the scope set.
type GrantDetail struct {
	GrantID string   `json:"grant_id"`
	Scopes  []string `json:"scopes"`
}

// RoleDetail is a single role's contribution to the scope set.
type RoleDetail struct {
	Role   string   `json:"role"`
	Scopes []string `json:"scopes"`
}

// ScopesInput is the input for collecting all scopes a principal holds.
type ScopesInput struct {
	Claims *token.PlatformClaims
}

// CheckInput is the input to a single allow/deny decision.
type CheckInput struct {
	// Claims are the validated platform JWT claims for the principal.
	Claims *token.PlatformClaims
	// Delegation is the parsed delegation token, nil if not present.
	Delegation *DelegationClaims
	// ResourceType is the resource type, e.g. "inference".
	ResourceType string
	// PathSegments qualify the resource, e.g. ["anthropic", "claude-sonnet-4-6"].
	PathSegments []string
	// Action is the operation being performed, e.g. "invoke".
	Action string
	// Attributes are optional resource metadata passed to OPA.
	Attributes map[string]string
}

// QueryInput is the input to a batch allow/deny across a resource catalog.
type QueryInput struct {
	Claims       *token.PlatformClaims
	Delegation   *DelegationClaims
	ResourceType string
	Action       string
	Candidates   []Candidate
}

// Candidate is a single resource in a Query catalog.
type Candidate struct {
	// ID is the opaque identifier returned in the allowed list.
	ID           string
	PathSegments []string
	Attributes   map[string]string
}

// Result is the outcome of a Check call.
type Result struct {
	Allow bool
	// Reason is a coarse "allow" or deny-reason string. Safe to return to
	// callers in API responses — must not contain sensitive policy internals
	// or materialized grant details.
	Reason string
}

// DelegationClaims holds parsed delegation token fields.
type DelegationClaims struct {
	// Sub is the delegating human (delegator_id in OPA input).
	Sub string
	// ActSub is the agent acting on behalf of the delegator.
	ActSub string
	// Scopes are the scopes granted for this delegation session.
	Scopes []string
}

// NewEvaluator returns an Evaluator backed by in-process OPA.
// bundleEndpoint is the base URL of the policy-service bundle API,
// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
func NewEvaluator(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) Evaluator {
	return newEngine(bundleEndpoint, pollInterval, logger)
}
