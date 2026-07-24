package orbit.budget

import rego.v1

# Hierarchical budget enforcement: every applicable ceiling is an independent
# constraint (AND); per-member assignments targeting the same (scope, window)
# resolve with MAX. Fail-open throughout: no configuration = allow; missing
# spend inputs leave the corresponding rules undefined = allow.
#
# Input shape (see docs/adr/2026-06-10-hierarchical-budget-enforcement.md and,
# for the delegation extension, docs/adr/2026-07-23-delegated-agent-identity.md
# Decision 5):
#
#   {
#     "principal": { "id", "tenant_id", "type", "roles": [...], "run_id" },
#     "delegation": { "delegator_id": "user-1" },        # verified; optional
#     "spends": {
#       "run": 1.25,
#       "principal":  { "weekly": 3.0, "monthly": 12.0 },
#       "delegator":  { "weekly": 1.0, "monthly": 4.0 },
#       "role_pools": { "engineering": 240.0 },
#       "tenant":     { "monthly": 1800.0 }
#     }
#   }
#
# Data:
#   data.budgets[pid]                 — grant budgets: effective_limits (per window),
#                                       effective_per_run_limit_usd, categories
#   data.role_budgets.role_budgets[r] — { limit_usd, reset_cadence, pooled }
#   data.tenant_budget.tenant_budget  — { limit_usd, reset_cadence } or null

# ── Per-principal window limits (grants ∪ held per-member role budgets) ──────

window_limits[w] contains l if {
	l := data.budgets[input.principal.id].effective_limits[w]
}

window_limits[w] contains l if {
	some r in input.principal.roles
	rb := data.role_budgets.role_budgets[r]
	not rb.pooled
	rb.limit_usd > 0
	w := rb.reset_cadence
	l := rb.limit_usd
}

# effective_limits maps window → MAX limit across all applicable assignments.
effective_limits[w] := max(ls) if {
	some w, ls in window_limits
}

# ── Ceilings (uniform shape consumed by the gateway and status endpoints) ────

ceilings contains c if {
	some w, l in effective_limits
	c := {
		"scope": "principal",
		"id": input.principal.id,
		"window": w,
		"limit_usd": l,
		"spend_usd": object.get(input.spends, ["principal", w], 0),
	}
}

ceilings contains c if {
	some r in input.principal.roles
	rb := data.role_budgets.role_budgets[r]
	rb.pooled
	rb.limit_usd > 0
	c := {
		"scope": "role_pool",
		"id": r,
		"window": rb.reset_cadence,
		"limit_usd": rb.limit_usd,
		"spend_usd": object.get(input.spends, ["role_pools", r], 0),
	}
}

# The tenant cap blocks AGENT and SERVICE principals only; it is not surfaced
# as a ceiling for humans (their spend accumulates but never blocks them).
ceilings contains c if {
	input.principal.type in {"AGENT", "SERVICE"}
	tb := data.tenant_budget.tenant_budget
	tb.limit_usd > 0
	c := {
		"scope": "tenant",
		"id": input.principal.tenant_id,
		"window": tb.reset_cadence,
		"limit_usd": tb.limit_usd,
		"spend_usd": object.get(input.spends, ["tenant", tb.reset_cadence], 0),
	}
}

ceilings contains c if {
	input.principal.run_id != ""
	prl := data.budgets[input.principal.id].effective_per_run_limit_usd
	prl > 0
	c := {
		"scope": "run",
		"id": input.principal.run_id,
		"window": "run",
		"limit_usd": prl,
		"spend_usd": object.get(input.spends, ["run"], 0),
	}
}

# ── Delegator ceilings (delegated-agent-identity ADR, Decision 5) ────────────
#
# When a verified delegation is present, the DELEGATOR's per-window limits and
# per-run limit apply as additional independent ceilings alongside the agent's:
# the agent's budget can never become a pool the person spends beyond their own
# allowance. input.delegation comes exclusively from the verified delegation
# context — never from anything the caller asserts. The distinct "delegator" /
# "delegator_run" scopes keep a delegator breach distinguishable from an agent
# ceiling in logs and refusals.

ceilings contains c if {
	delegator := input.delegation.delegator_id
	delegator != ""
	some w, l in data.budgets[delegator].effective_limits
	c := {
		"scope": "delegator",
		"id": delegator,
		"window": w,
		"limit_usd": l,
		"spend_usd": object.get(input.spends, ["delegator", w], 0),
	}
}

# The delegator's per-run limit caps the same run counter as the agent's.
ceilings contains c if {
	delegator := input.delegation.delegator_id
	delegator != ""
	input.principal.run_id != ""
	prl := data.budgets[delegator].effective_per_run_limit_usd
	prl > 0
	c := {
		"scope": "delegator_run",
		"id": input.principal.run_id,
		"window": "run",
		"limit_usd": prl,
		"spend_usd": object.get(input.spends, ["run"], 0),
	}
}

# ── Decision ─────────────────────────────────────────────────────────────────

breached contains c if {
	some c in ceilings
	c.spend_usd >= c.limit_usd
}

default budget_allow := true

budget_allow := false if count(breached) > 0

spend_limit_reached := true if not budget_allow

spend_limit_reached := false if budget_allow

default has_limit := false

has_limit if count(ceilings) > 0
