# Agent Token Breakdown and Spend Card - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-23  
**Last Updated:** 2026-06-23  
**Owner:** Engineering  
**Stakeholders:** Platform Users, Ops Teams

---

## Executive Summary

The agent detail overview page currently shows a "Tokens (7d)" stat card with a single aggregated total that always displays "—" because the underlying `tokens_used` field on each `Run` is not being populated from the inference gateway. This PRD covers two improvements: replacing that single total token card with a breakdown card showing input, output, cache-read, and cache-write token counts (matching the pattern established in the model-gateway analytics view), and adding a fifth stat card showing total spend for the agent over the displayed period.

---

## Problem Statement

### Current State

The agent overview tab has four stat cards:
1. **Total Runs** — count of all runs
2. **Success Rate** — completed / total
3. **Avg Duration** — average ms of completed runs
4. **Tokens (7d)** — always shows "—"

The Tokens card is non-functional. It sums `tokensUsed` from the client-side `RunRow` list, but this field (`run.tokens_used`) is only set in the runs table when the session manager writes a final summary; in practice it is null for most runs. The actual token data is in `inference_usage`, indexed by `run_id`.

Additionally, operators and team leads have no way to see total spend for an agent without navigating to the model-gateway analytics page and filtering by principal — a multi-step process that requires knowing the agent's SPIFFE principal ID.

### User Pain Points

- Token card always shows "—", eroding trust in the stats overview.
- No cost visibility at the agent level — users cannot quickly answer "how much did this agent spend this week?"
- Lack of token category breakdown means you can't identify agents with excessive cache-miss rates or high output-to-input ratios.

### Business Impact

- Reduces utility of the agent detail page as an operational tool.
- Slows incident response when an agent is burning budget unexpectedly.

---

## Goals and Objectives

### Primary Goals

1. Surface accurate, broken-down token usage (input, output, cache-read, cache-write) on the agent overview page.
2. Add a spend card showing total USD cost for the agent over the same 7-day window.

### Success Metrics

- Token cards show non-zero values when the agent has completed runs with inference calls.
- Spend card shows a formatted USD value matching the inference gateway's attribution for that agent's principal.
- Both cards update on page load from a single new BFF call.

### Non-Goals

- Historical charts / trends on the agent detail page (covered by model-gateway analytics).
- Per-run token breakdown in the runs table (still shows aggregate per run).
- Changing the 7-day window or adding a period selector (future work).

---

## User Stories

### Primary User Stories

**As a** platform operator,  
**I want to** see token breakdown (input / output / cache read / cache write) for an agent on its overview page,  
**So that** I can identify inefficient agents and optimize prompts or caching strategies.

**Acceptance Criteria:**
- [ ] Four token sub-rows visible in the Tokens card: Input, Output, Cache read, Cache write
- [ ] Values are sourced from the inference gateway `inference_usage` table, not from `runs.tokens_used`
- [ ] Card displays "—" / zeros gracefully when the agent has no inference records

---

**As a** team lead reviewing agent costs,  
**I want to** see total spend for an agent over the last 7 days directly on its overview page,  
**So that** I don't have to navigate to analytics and filter by principal.

**Acceptance Criteria:**
- [ ] Fifth stat card titled "Spend (7d)" showing a USD-formatted total
- [ ] Value matches what the model-gateway analytics would show for that agent's principal over the same window

---

## Proposed Solution

### High-Level Approach

Add a new inference-gateway endpoint `GET /v1/usage/agent-type/{agentTypeID}` (tenant-scoped, 7-day window) that returns aggregated token breakdown and spend. The BFF proxies this as `GET /api/agents/types/[typeId]/usage`. The agent overview tab fetches this on mount and renders the data.

The existing Tokens card is replaced by a card matching the breakdown style in `analytics-kpi-cards.tsx`. A new Spend card is added as the fifth card. The grid changes from `cols={4}` to `cols={5}` (or `{ base: 2, md: 5 }` for responsive layout).

### User Experience

The four stat cards at the top of the agent Overview tab become five:

```
[ Total Runs ] [ Success Rate ] [ Avg Duration ] [ Tokens (7d) ] [ Spend (7d) ]
```

The Tokens card expands to show sub-rows:
- Total (headline)
- Input
- Output
- Cache read
- Cache write

### Key Components

- **Backend**: New `GetAgentTypeUsage` repository method + service method + handler on inference-gateway
- **BFF route**: `GET /api/agents/types/[typeId]/usage` proxied from the inference gateway
- **Frontend hook**: Fetch on mount in the agent detail context or the overview tab itself
- **Frontend component**: Updated stat cards in `agent-overview-tab.tsx`

---

## Scope

### In Scope

- New backend endpoint on inference-gateway for agent-type aggregate usage
- BFF proxy route
- Frontend card update (tokens breakdown + spend card)

### Out of Scope

- Period selector (hard-coded 7 days to match the existing Tokens card label)
- Historical trend charts
- Per-run token breakdown columns in the runs table
- Changes to the runs table schema

### Future Considerations

- Configurable period selector
- Cache hit rate display
- Cost breakdown by model (if agent calls multiple models)

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `GET /v1/usage/agent-type/{agentTypeID}` returns `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `estimated_cost_usd` summed over the last 7 days, scoped to the caller's tenant
- [ ] BFF route `/api/agents/types/[typeId]/usage` proxies to the above
- [ ] Tokens card on overview tab shows breakdown (input, output, cache read, cache write)
- [ ] New Spend card shows formatted USD value
- [ ] Both cards show graceful empty state when no data

**Should Have (P1):**
- [ ] Loading skeleton while data is in-flight
- [ ] No extra round-trip — single fetch call for both tokens and spend

### Non-Functional Requirements

**Performance:**
- Single aggregation query; expected < 50ms p95 with a composite index on `(tenant_id, principal_id, created_at)`

**Security:**
- Tenant scoping enforced in the SQL query (not just the JWT) — same pattern as existing `GetRunUsage`
- No cross-tenant data leakage possible

---

## Dependencies

### Internal Dependencies

- Inference gateway `inference_usage` table — data is already being written per run
- `run_id` is stored on `inference_usage` rows, but the agent-type-level query will use `principal_id` (the agent's SPIFFE principal) as the aggregation key
- The agent's SPIFFE principal ID is deterministic from `agent_type.slug` and `tenant_id`

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Agent principal ID not present in inference_usage (BYOC or non-gateway inference) | Medium | Low | Return zeros gracefully; show "—" |
| Query performance on large tenants | Medium | Low | 7-day window limits scan; existing index covers the pattern |

---

## Timeline and Milestones

**Phase 1: Implementation** (2026-06-23)
- PRD + ADR written
- Backend endpoint + BFF route + frontend cards
- PR opened and CI green

---

## Appendix

### Related Documents

- [ADR: 2026-06-23-agent-token-breakdown-and-spend-card](../adr/2026-06-23-agent-token-breakdown-and-spend-card.md)
- [ADR: Inference Analytics](../adr/2026-04-27-inference-analytics.md)
- [ADR: Agents BFF Integration](../adr/2026-06-04-agents-bff-integration-and-run-chat-wiring.md)
