package orbit.budget

import rego.v1

# budget_allow is true by default (fail-open: no budget configured = allow).
default budget_allow := true

# Deny when the principal has a configured effective limit and spend meets or exceeds it.
budget_allow := false if {
	principal := data.budgets[input.principal.id]
	input.current_spend_usd >= principal.effective_limit_usd
}

# spend_limit_reached is the positive-sense inverse of budget_allow for API consumers.
spend_limit_reached := true if { not budget_allow }

spend_limit_reached := false if { budget_allow }

# has_limit is true only when the principal has an explicit budget entry in the bundle.
default has_limit := false

has_limit := true if {
	data.budgets[input.principal.id]
}

# effective_budget_usd is the pre-computed MAX across all applicable budget assignments.
# Returns 0 when the principal has no configured budget (consistent with fail-open).
default effective_budget_usd := 0

effective_budget_usd := data.budgets[input.principal.id].effective_limit_usd
