# Agent Token Breakdown and Spend Card - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-22  
**Last Updated:** 2026-06-22  
**Owner:** Engineering  
**Stakeholders:** Platform operators, tenant admins

---

## Executive Summary

The agent detail page shows four stat cards: Total Runs, Success Rate, Avg Duration, and Tokens. The Tokens card currently always reads zero because the `runs` table does not carry per-category token data. This PRD covers (1) fixing the Tokens card to show a real breakdown by input, output, cache-read, and cache-write tokens, and (2) adding a fifth card showing total spend for that agent over the same window.

---

## Problem Statement

### Current State

The Tokens (7d) card on the agent overview tab calls `runs.reduce(acc, r.tokensUsed)` across the already-fetched run list. The `agents.Run` struct in the backend does not include token counts, so `tokensUsed` is always null and the card always shows `—`. There is also no spend card, so operators cannot see the dollar cost for a given agent at a glance.

### User Pain Points

- Operators cannot tell how many tokens an agent consumed without navigating to the model-gateway analytics page and manually filtering by principal.
- There is no single-glance cost visibility for a specific agent type — operators must reconstruct cost from cross-feature analytics.
- The existing card silently shows `—` rather than explaining why it has no data, eroding trust in the dashboard.

### Business Impact

- Cost accountability: teams running multiple agent types cannot attribute spend without manual effort.
- Operational visibility gap: the agents page is the natural place to answer "how much did this agent cost this week?".

---

## Goals and Objectives

### Primary Goals

1. Show real token consumption broken down by input, output, cache-read, and cache-write on the agent detail page.
2. Show total estimated spend (USD) for the agent over the same 7-day window.

### Success Metrics

- Token breakdown card shows non-zero values for agents that have made inference calls.
- Spend card shows a dollar figure consistent with the model-gateway analytics page when filtered to the same agent.
- Both cards gracefully degrade to `—` / `$0.0000` when no inference data is available.

### Non-Goals

- Configurable time windows on the agent detail page (always 7 days, matching the existing card title).
- Historical charts or time-series on the agent detail page (that belongs in model-gateway analytics).
- Per-run token breakdown columns in the runs table (this is display-only aggregation).

---

## User Stories

### Primary User Stories

**As a** tenant administrator,  
**I want to** see how many input, output, cache-read, and cache-write tokens my agent used this week,  
**So that** I can understand which category is driving cost.

**Acceptance Criteria:**
- [ ] Tokens card on agent overview shows total with a breakdown row for each of: Input, Output, Cache read, Cache write.
- [ ] Values are drawn from real inference usage data, not the run list.

---

**As a** platform operator,  
**I want to** see total estimated spend for an agent over the last 7 days,  
**So that** I can spot cost outliers without leaving the agents page.

**Acceptance Criteria:**
- [ ] A fifth card labelled "Spend (7d)" displays `$x.xxxx` for the agent.
- [ ] Spend figure is consistent with the model-gateway analytics page filtered to the same agent.

---

## Proposed Solution

### High-Level Approach

Add a new inference-gateway endpoint `GET /v1/analytics/agents/{agentTypeId}` that aggregates `inference_usage` rows associated with the agent's runs by joining on `run_id → runs.id → runs.agent_type_id`. Return a lightweight KPI payload with the four token categories and estimated cost. Expose this endpoint through a Next.js BFF route and call it from the agent detail page.

### User Experience

The four existing stat cards remain unchanged. The "Tokens (7d)" card is redesigned to match the model-gateway Tokens card pattern: headline total with four breakdown rows (Input, Output, Cache read, Cache write). A new fifth card "Spend (7d)" shows the dollar amount.

#### User Flow

1. User navigates to an agent detail page.
2. Page loads: the four existing stat cards appear as before.
3. Simultaneously, the new analytics call resolves and populates the Tokens card with a breakdown and adds the Spend card.
4. If no inference data exists (new agent, no runs), the cards show `—` / `$0.0000`.

### Key Components

- **Backend**: New `GetAgentAnalytics` repository method (joins `inference_usage` with `runs`) and handler in inference-gateway.
- **Next.js API route**: `/api/analytics/agents/[agentTypeId]` — proxies the new backend endpoint via the session's access token.
- **BFF service function**: `fetchAgentAnalyticsFromGateway` in the model-gateway services layer.
- **UI**: Updated `agent-overview-tab.tsx` — expanded Tokens card with breakdown rows; new Spend card.

---

## Scope

### In Scope

- New inference-gateway endpoint for per-agent-type token + spend aggregation (7-day window, consistent with the existing analytics pattern).
- New Next.js API route to proxy the endpoint.
- Updated Tokens card with four breakdown rows.
- New Spend card as a fifth stat card.
- Unit tests for the new repository query and handler.
- TypeScript type additions in `shared/api.ts` and the model-gateway types file.

### Out of Scope

- Configurable lookback windows on the agent detail page.
- Time-series charts per agent.
- Backfilling `tokens_used` on the `runs` table.
- Changing how `runs.tokens_used` is populated (it remains null; this feature bypasses it entirely).

### Future Considerations

- Per-run token columns in the runs table data table.
- Agent-level analytics tab with full time-series charts.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `GET /v1/analytics/agents/{agentTypeId}` returns token breakdown and estimated cost for the agent over the last 7 days.
- [ ] Endpoint is tenant-isolated (only aggregates runs belonging to the verified tenant).
- [ ] Token card on agent detail page shows input/output/cache-read/cache-write breakdown.
- [ ] Spend card on agent detail page shows estimated cost in USD.
- [ ] Both cards show graceful empty state when no data is available.

**Should Have (P1):**
- [ ] Backend unit tests for the new repository query.
- [ ] Backend unit tests for the new handler.
- [ ] Frontend TypeScript types are correctly defined (no `any` casts).

**Nice to Have (P2):**
- [ ] Consider caching the analytics response for 60 seconds on the Next.js side (currently no-store is fine).

### Non-Functional Requirements

**Performance:**
- New endpoint query should complete in < 200ms p95 for typical agent run volumes (< 10 000 runs).

**Security:**
- Endpoint must enforce tenant isolation via the verified JWT `tenant_id`.
- Only principals with `platform:analytics:read` scope may call the endpoint.

**Reliability:**
- If the analytics call fails, the Tokens and Spend cards should show `—` / `$0.0000` gracefully rather than breaking the page.

---

## Dependencies

### Internal Dependencies

- Inference-gateway `inference_usage` table with `run_id` column (added in migration `20260610155505_usage_run_and_roles.sql`) — available.
- Orbit-agents `runs` table with `agent_type_id` column (initial migration `20260508135438_create_agent_types_runs_events.sql`) — available.
- Both tables are in the same Postgres instance — join is feasible.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Cross-table join adds latency | Low | Low | Add index on `runs.agent_type_id` if not already present; query is a simple aggregation |
| Agent has zero inference usage | Low | High | Handle gracefully: return zeros; UI shows `—` / `$0.0000` |
| RBAC: endpoint accessible without `platform:analytics:read` | High | Low | Register endpoint with scope enforcement middleware |

---

## Alternatives Considered

### Alternative 1: Enrich `runs` table with token columns

**Description:** Add `tokens_used`, `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `estimated_cost_usd` columns to the `runs` table and populate them at run completion.

**Why not chosen:**
- Requires a migration, a new callback from session-manager to orbit-agents, and duplication of inference data.
- Still wouldn't give a per-agent aggregate without another query.
- Cross-service coupling.

### Alternative 2: Use principal_id (SPIFFE ID) as the analytics filter

**Description:** Resolve the agent's SPIFFE ID in the frontend and pass it as `principal_id` to the existing `/v1/analytics` endpoint.

**Why not chosen:**
- The SPIFFE ID format (`spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}`) is not returned by any existing API. The frontend would need to reconstruct it, coupling it to infra naming conventions.
- Per-run attribution via `run_id → runs.agent_type_id` is authoritative; SPIFFE-based filtering would miss runs by non-SPIFFE callers (if any) and depends on naming stability.

---

## Timeline and Milestones

### Phases

**Phase 1: Backend** (Target: 2026-06-22)
- New repository query and service method
- New handler + route registration
- Unit tests

**Phase 2: Frontend** (Target: 2026-06-22)
- New API route, BFF service, updated components

### Key Milestones

- [x] PRD Created: 2026-06-22
- [x] ADR Created: 2026-06-22
- [ ] Implementation Complete: 2026-06-22
- [ ] PR Merged

---

## Appendix

### Related Documents

- [ADR: 2026-06-22-agent-token-breakdown-and-spend](../adr/2026-06-22-agent-token-breakdown-and-spend.md)
- [ADR: 2026-04-27-inference-analytics](../adr/2026-04-27-inference-analytics.md)
- [ADR: 2026-06-05-llm-cost-tracking](../adr/2026-06-05-llm-cost-tracking.md)
