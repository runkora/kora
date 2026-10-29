# Set budgets

In runkora, budgets cap what people and agents spend on models. The inference gateway checks every budget that applies before a call reaches the model, and records the call's cost against the caller. Budgets are set in US dollars per week or per month on **Model Gateway** → **Budgets**. Setting them needs `platform:budgets:write`, held by the built-in `kora-finops` role.

## Where budgets apply

| Budget | Applies to | Set on |
| --- | --- | --- |
| Tenant cap | All spend in the tenant, by every person and agent | The **Tenant Cap** card on the Budgets page |
| Role budget | Each member of an OIDC role, or one shared pool for all members | The **Role Budgets** tab, or when creating a role |
| Grant budget | One person, with an optional limit on each run they start | The **Grant Budgets** tab, or when creating a grant |
| Agent budget | One agent, with an optional limit on each of its runs | The agent's guardrails, when you [create an agent](create-an-agent.md) or on its configuration page |

Every budget that applies is checked on its own. A call is blocked when any one of them is reached. When a person has a grant budget and a role budget for the same period, the higher limit is the one that counts. The tenant cap, a shared pool and a run limit are always checked separately. Role budgets apply to people only. The built-in roles cannot carry a budget.

A [delegated agent](../concepts/identity.md#delegated-agents) spends the budget of the person who started the run. Its calls count against that person's grant budgets as well as the agent's own.

## Set a budget

Choose **Set cap** on the Tenant Cap card, or the edit button on a role or grant. Turn on the spend limit, enter an amount and choose **Weekly** or **Monthly**. A role budget can be a **Shared pool**, one limit for all members together. A grant budget can also set a **Per-run limit** for each run started on that person's behalf.

Budget changes reach the inference gateway within about 30 seconds.

## When a budget runs out

| What | Happens |
| --- | --- |
| A model call | Refused with `403 BUDGET_EXCEEDED`, naming the budget that was reached |
| An agent run | Stops with the state `budget_exhausted`, and its sandbox is deleted |
| A weekly budget | Resets on Monday at 00:00 UTC |
| A monthly budget | Resets on the 1st at 00:00 UTC |
| A run limit | Never resets. Start a new run, or raise the limit. |

Raising a limit lifts the block once the change reaches the gateway.

## See spend

- **Budgets** → **Principals** shows each person and agent's spend this month against their effective limit, and marks those near their limit at 80% or blocked.
- **My Usage** shows your own spend against every budget that applies to you.
- The **Agents** page shows spend by agent and by model, and which agents are closest to their budgets.

You can also manage budgets through the API under `/api/v1/policy`, or with the `budgets_tenant_*` and `policy_grants_set_budget` tools in the kora MCP server.
