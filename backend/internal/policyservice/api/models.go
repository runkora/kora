package api

import (
	"net/http"
	"strconv"

	"github.com/runkora/kora/backend/internal/policyservice/grants"
	"github.com/runkora/kora/backend/internal/policyservice/roles"
)

// budgetAssignmentRequest is the inline budget configuration on a grant or role.
// limit_usd is the per-window ceiling; reset_cadence is "weekly" or "monthly"
// (default monthly). pooled is accepted on roles only; per_run_limit_usd on
// grants only.
type budgetAssignmentRequest struct {
	LimitUSD       float64 `json:"limit_usd"`
	ResetCadence   string  `json:"reset_cadence"`
	Pooled         bool    `json:"pooled"`
	PerRunLimitUSD float64 `json:"per_run_limit_usd"`
}

// tenantBudgetRequest is the request body for PUT /policy/tenant-budget.
type tenantBudgetRequest struct {
	TenantID     string  `json:"tenant_id,omitempty"`
	LimitUSD     float64 `json:"limit_usd"`
	ResetCadence string  `json:"reset_cadence"`
}

// normalizeCadence validates and defaults the reset cadence.
// Returns ("", false) for unsupported cadences.
func normalizeCadence(c string) (string, bool) {
	switch c {
	case "", "monthly":
		return "monthly", true
	case "weekly":
		return "weekly", true
	default:
		return "", false
	}
}

// createGrantRequest is the request body for POST /policy/grants.
type createGrantRequest struct {
	TenantID      string                   `json:"tenant_id"`
	PrincipalType string                   `json:"principal_type"`
	PrincipalID   string                   `json:"principal_id"`
	Scopes        []string                 `json:"scopes"`
	Budget        *budgetAssignmentRequest `json:"budget,omitempty"`
	// DelegationRequired marks the grant as a delegated-agent ceiling: the
	// principal is denied all autonomous authorisation and may act only with
	// valid delegation context (delegated-agent-identity ADR, Decision 2).
	// Omitted → false (backwards compatible).
	DelegationRequired bool `json:"delegation_required"`
}

// updateGrantBudgetRequest is the request body for PATCH /policy/grants/{id}.
// Only the budget is mutable in place — scopes are immutable on grants by
// design. A null budget removes the assignment.
type updateGrantBudgetRequest struct {
	Budget *budgetAssignmentRequest `json:"budget"`
}

// updateRoleBudgetRequest is the request body for PATCH /policy/roles/{role}/budget.
// A null budget clears it.
type updateRoleBudgetRequest struct {
	TenantID string                   `json:"tenant_id"`
	Budget   *budgetAssignmentRequest `json:"budget"`
}

// upsertRoleRequest is the request body for PUT /policy/roles.
type upsertRoleRequest struct {
	TenantID string                   `json:"tenant_id"`
	Role     string                   `json:"role"`
	Scopes   []string                 `json:"scopes"`
	Budget   *budgetAssignmentRequest `json:"budget,omitempty"`
}

// grantWriteResponse is a created grant plus its coherence lint warnings.
type grantWriteResponse struct {
	*grants.Grant
	Warnings []string `json:"warnings,omitempty"`
}

// roleWriteResponse is an upserted role plus its coherence lint warnings.
type roleWriteResponse struct {
	*roles.RoleEntry
	Warnings []string `json:"warnings,omitempty"`
}

const (
	defaultPageLimit = 100
	maxPageLimit     = 1000
)

// parsePagination extracts limit/offset from query parameters with sensible defaults.
func parsePagination(r *http.Request) (limit, offset int) {
	limit = defaultPageLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxPageLimit {
		limit = maxPageLimit
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return limit, offset
}
