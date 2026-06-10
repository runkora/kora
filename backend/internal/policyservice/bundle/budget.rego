package orbit.budget

import rego.v1

# Budget limits apply from two sources, resolved with MAX semantics
# (a budget assignment can only raise the effective limit, never lower it):
#
#   - Grant budgets: pre-computed at bundle compile time into
#     data.budgets[principal_id].effective_limit_usd (MAX over the
#     principal's grant budget categories).
#   - Role budgets: data.role_budgets.role_budgets[role].monthly_limit_usd,
#     resolved here against input.principal.roles — the caller's IDP-asserted
#     OIDC roles from the verified JWT, which the bundle compiler cannot
#     pre-resolve per principal.

# limits is the set of all budget limits applicable to the caller.
limits contains l if {
	l := data.budgets[input.principal.id].effective_limit_usd
}

limits contains l if {
	some r in input.principal.roles
	l := data.role_budgets.role_budgets[r].monthly_limit_usd
}

# has_limit is true when at least one budget source applies to the caller.
default has_limit := false

has_limit if count(limits) > 0

# effective_budget_usd is MAX across all applicable budget assignments.
# Returns 0 when the caller has no configured budget (consistent with fail-open).
default effective_budget_usd := 0

effective_budget_usd := max(limits) if count(limits) > 0

# budget_allow is true by default (fail-open: no budget configured = allow).
default budget_allow := true

budget_allow := false if {
	has_limit
	input.current_spend_usd >= effective_budget_usd
}

# spend_limit_reached is the positive-sense inverse of budget_allow for API consumers.
spend_limit_reached := true if not budget_allow

spend_limit_reached := false if budget_allow
