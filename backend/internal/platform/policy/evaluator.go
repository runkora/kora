// Package policy provides in-process OPA policy evaluation with automatic
// bundle lifecycle management. Zero network hops on the evaluation hot path.
//
// Instantiate with NewEvaluator, call Start before first use.
package policy

import (
	"context"
	"time"

	"github.com/runkora/kora/backend/internal/platform/logging"
	"github.com/runkora/kora/backend/internal/platform/token"
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

	// CheckBudget evaluates the orbit.budget hierarchical ceiling rules for
	// the caller. The request carries verified identity context (principal,
	// type, JWT roles, run identity) and the spends the gateway read from the
	// canonical counters; OPA resolves the applicable ceilings from bundle
	// data and denies if any is met.
	// Fails open (allow=true, err=nil) when the bundle is unavailable.
	CheckBudget(ctx context.Context, tenantID string, req BudgetCheckRequest) (BudgetResult, error)

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

	// ListBudgets returns every budget assignment in the tenant's compiled
	// bundle: per-principal grant budgets (data.budgets) and role budget
	// definitions (data.role_budgets). Role membership is not resolvable here —
	// callers attribute role budgets to principals from request-time data.
	// Fails open: returns empty maps on any bundle or evaluation error.
	ListBudgets(ctx context.Context, tenantID string) (map[string]PrincipalBudget, map[string]RoleBudget, error)

	// GetTenantBudget returns the tenant's rollup cap from the compiled bundle,
	// or nil when none is configured. Fails open: returns nil on any error.
	GetTenantBudget(ctx context.Context, tenantID string) (*TenantBudget, error)

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
	// ResetCadence is "weekly" or "monthly".
	ResetCadence string
	// LimitUSD is the per-window limit carried by this assignment (0 = none).
	LimitUSD float64
	// PerRunLimitUSD is the windowless per-run limit (grants only; 0 = none).
	PerRunLimitUSD float64
	// Pooled marks a shared-pot role budget (roles only).
	Pooled bool
}

// PrincipalBudget is a principal's compiled grant-budget entry from data.budgets.
type PrincipalBudget struct {
	Categories []BudgetCategory
	// EffectiveLimits maps window ("weekly"/"monthly") → MAX grant limit.
	EffectiveLimits map[string]float64
	// EffectivePerRunLimitUSD is MAX across grant per-run limits (0 = none).
	EffectivePerRunLimitUSD float64
}

// RoleBudget is a role's budget definition from data.role_budgets.
type RoleBudget struct {
	LimitUSD     float64
	ResetCadence string
	Pooled       bool
}

// TenantBudget is the tenant rollup cap from data.tenant_budget.
type TenantBudget struct {
	LimitUSD     float64
	ResetCadence string
}

// BudgetSpends carries the canonical counter readings for one budget check.
// Missing keys mean "counter not read" and leave the corresponding ceiling
// rules undefined in OPA (fail open).
type BudgetSpends struct {
	// Run is the current run's spend (meaningful only with RunID set).
	Run float64 `json:"run"`
	// Principal maps window → the principal's spend in that window.
	Principal map[string]float64 `json:"principal"`
	// Delegator maps window → the DELEGATOR's spend in that window (their own
	// usage plus usage attributed to them from delegated runs). Read only when
	// a verified delegation is present (delegated-agent-identity ADR, Decision 5).
	Delegator map[string]float64 `json:"delegator,omitempty"`
	// RolePools maps role name → the shared pool's spend.
	RolePools map[string]float64 `json:"role_pools"`
	// Tenant maps window → the tenant's rollup spend in that window.
	Tenant map[string]float64 `json:"tenant"`
}

// BudgetCheckRequest is the gateway-side input to CheckBudget.
type BudgetCheckRequest struct {
	PrincipalID   string
	PrincipalType string
	Roles         []string
	RunID         string
	// DelegatorID is the verified delegation's delegator — sourced exclusively
	// from policy.DelegationFromContext, never from caller-asserted data. When
	// set, the delegator's per-window and per-run ceilings apply as additional
	// independent constraints (delegated-agent-identity ADR, Decision 5).
	DelegatorID string
	Spends      BudgetSpends
}

// BudgetCeiling is one applicable ceiling reported by the budget evaluation.
type BudgetCeiling struct {
	Scope    string  `json:"scope"`  // "principal", "role_pool", "tenant", "run"
	ID       string  `json:"id"`     // principal/role/tenant/run identifier
	Window   string  `json:"window"` // "weekly", "monthly", or "run"
	LimitUSD float64 `json:"limit_usd"`
	SpendUSD float64 `json:"spend_usd"`
}

// Reached reports whether this ceiling is met or exceeded.
func (c BudgetCeiling) Reached() bool { return c.SpendUSD >= c.LimitUSD }

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
	// RunID is the request's independently-asserted run identity — the
	// /run/{run_id} path prefix extracted by middleware.StripRunPrefix (AGENT
	// principals only). When delegation is present, the engine denies a
	// delegation token bound to a different run
	// (docs/adr/2026-07-23-delegated-agent-identity.md, Decision 4).
	RunID string
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
	// JTI identifies the delegation issuance — stable across re-signed tokens,
	// and the key revocation blocklists (docs/adr/2026-07-23-delegated-agent-identity.md).
	JTI string
	// RunID binds the delegation to a single run.
	RunID string
	// DelegatorPrincipalType is the delegator's principal type (HUMAN or PAT).
	DelegatorPrincipalType string
	// TenantID is the tenant the delegation was minted in.
	TenantID string
}

// BudgetCheckInput is the OPA input shape for the orbit.budget module.
type BudgetCheckInput struct {
	Principal  budgetPrincipal   `json:"principal"`
	Delegation *budgetDelegation `json:"delegation,omitempty"`
	Spends     BudgetSpends      `json:"spends"`
}

type budgetPrincipal struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenant_id"`
	Type     string   `json:"type"`
	Roles    []string `json:"roles"`
	RunID    string   `json:"run_id"`
}

// budgetDelegation carries the verified delegation context into the budget
// evaluation; budget.rego keys the delegator ceilings on delegator_id.
type budgetDelegation struct {
	DelegatorID string `json:"delegator_id"`
}

// BudgetResult is returned by CheckBudget.
type BudgetResult struct {
	Allow    bool
	HasLimit bool
	// Ceilings is every ceiling that applied to the caller, with limits and spends.
	Ceilings []BudgetCeiling
}

// NewEvaluator returns an Evaluator backed by in-process OPA.
// bundleEndpoint is the base URL of the policy-service bundle API,
// e.g. http://orbit-policy-service.orbit-system.svc/api/v1/policy/bundles
func NewEvaluator(bundleEndpoint string, pollInterval time.Duration, logger logging.Logger) Evaluator {
	return newEngine(bundleEndpoint, pollInterval, logger)
}
