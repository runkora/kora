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
	Check(ctx context.Context, input CheckInput) (Result, error)

	// Query filters candidates to those the principal may access.
	Query(ctx context.Context, input QueryInput) ([]string, error)

	// Scopes returns the complete set of scopes the principal holds.
	Scopes(ctx context.Context, input ScopesInput) ([]string, error)

	// ScopesExplain returns the principal's scopes broken down by source.
	ScopesExplain(ctx context.Context, input ScopesInput) (*ScopesExplainResult, error)

	// CheckBudget evaluates the orbit.budget rules for the principal.
	// currentSpendUSD is the sum of all per-category Redis counters for the
	// current period, read by the inference gateway before this call.
	// Fails open (allow=true, err=nil) when the bundle is unavailable.
	CheckBudget(ctx context.Context, tenantID, principalID string, currentSpendUSD float64) (BudgetResult, error)

	// GetBudgetCategories returns the applicable budget categories for the
	// principal from the compiled OPA bundle (data.budgets[principal_id].categories).
	// Returns nil, nil when the principal has no budget configured (no limit).
	// Fails open: returns nil, nil on any bundle or evaluation error.
	GetBudgetCategories(ctx context.Context, tenantID, principalID string) ([]BudgetCategory, error)

	// GetRoleBudgets returns the budget categories for the given OIDC role names
	// from the compiled OPA bundle (data.role_budgets). This is called at request
	// time by the gateway, which knows the caller's JWT roles but the bundle compiler
	// cannot pre-resolve (OIDC role membership is IDP-asserted, not stored in the DB).
	// Only roles that have a budget configured are returned. Fails open: returns nil, nil
	// on any bundle or evaluation error. Roles with no budget entry are silently skipped.
	GetRoleBudgets(ctx context.Context, tenantID string, roleNames []string) ([]BudgetCategory, error)

	// Start begins background bundle polling. Call once at service startup.
	Start(ctx context.Context)
}

// BudgetCategory is a single budget assignment source (role or grant).
// Mirrors the budgetCategory type in the bundle compiler.
type BudgetCategory struct {
	// Source is "role" or "grant".
	Source string
	// ID is the role name (for source="role") or grant UUID (for source="grant").
	ID string
	// ResetCadence is "monthly", "quarterly", or "yearly".
	ResetCadence string
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
	Claims       *token.PlatformClaims
	Delegation   *DelegationClaims
	ResourceType string
	PathSegments []string
	Action       string
	Attributes   map[string]string
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
	ID           string
	PathSegments []string
	Attributes   map[string]string
}

// Result is the outcome of a Check call.
type Result struct {
	Allow  bool
	Reason string
}

// DelegationClaims holds parsed delegation token fields.
type DelegationClaims struct {
	Sub    string
	ActSub string
	Scopes []string
}

// BudgetCheckInput is the OPA input shape for the orbit.budget module.
type BudgetCheckInput struct {
	Principal       budgetPrincipal `json:"principal"`
	CurrentSpendUSD float64         `json:"current_spend_usd"`
}

type budgetPrincipal struct {
	ID       string `json:"id"`
	TenantID string `json:"tenant_id"`
}

// BudgetResult is returned by CheckBudget.
type BudgetResult struct {
	Allow              bool
	HasLimit           bool
	EffectiveBudgetUSD float64
}

// NewEvaluator returns an Evaluator backed by in-process OPA.
// bundleEndpoint is the base URL of the policy-service bundle API,
// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
func NewEvaluator(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) Evaluator {
	return newEngine(bundleEndpoint, pollInterval, logger)
}
