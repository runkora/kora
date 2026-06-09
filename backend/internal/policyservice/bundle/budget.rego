package orbit.budget

import rego.v1

# budget_allow is true by default (fail-open when no quota is configured).
default budget_allow := true

# Deny when the principal has an individual quota and spend meets or exceeds it.
budget_allow := false if {
	limit := data.quotas.principal_quotas[input.principal.id]
	input.current_spend_usd >= limit
}

# Deny when the principal has no individual quota but belongs to a group that is over budget.
budget_allow := false if {
	not data.quotas.principal_quotas[input.principal.id]
	group_id := data.quotas.memberships[input.principal.id]
	limit := data.quotas.roles[group_id].monthly_budget_usd
	input.current_spend_usd >= limit
}

# effective_budget_usd is the active limit for this principal (individual takes priority).
effective_budget_usd := limit if {
	limit := data.quotas.principal_quotas[input.principal.id]
}

effective_budget_usd := limit if {
	not data.quotas.principal_quotas[input.principal.id]
	group_id := data.quotas.memberships[input.principal.id]
	limit := data.quotas.roles[group_id].monthly_budget_usd
}

# spend_limit_reached mirrors budget_allow as a positive-sense boolean for API consumers.
spend_limit_reached := true if {budget_allow == false}

spend_limit_reached := false if {budget_allow == true}
