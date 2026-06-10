package api

import (
	"net/http"
	"strconv"
)

// budgetAssignmentRequest is the inline budget configuration on a grant or role.
type budgetAssignmentRequest struct {
	MonthlyLimitUSD float64 `json:"monthly_limit_usd"`
	ResetCadence    string  `json:"reset_cadence"`
}

// createGrantRequest is the request body for POST /policy/grants.
type createGrantRequest struct {
	TenantID      string                   `json:"tenant_id"`
	PrincipalType string                   `json:"principal_type"`
	PrincipalID   string                   `json:"principal_id"`
	Scopes        []string                 `json:"scopes"`
	Budget        *budgetAssignmentRequest `json:"budget,omitempty"`
}

// updateGrantBudgetRequest is the request body for PATCH /policy/grants/{id}.
// Only the budget is mutable in place — scopes are immutable on grants by
// design. A null budget removes the assignment.
type updateGrantBudgetRequest struct {
	Budget *budgetAssignmentRequest `json:"budget"`
}

// upsertRoleRequest is the request body for PUT /policy/roles.
type upsertRoleRequest struct {
	TenantID string                   `json:"tenant_id"`
	Role     string                   `json:"role"`
	Scopes   []string                 `json:"scopes"`
	Budget   *budgetAssignmentRequest `json:"budget,omitempty"`
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
