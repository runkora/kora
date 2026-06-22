# Agent Token Breakdown and Spend Card

**Status:** Accepted  
**Date:** 2026-06-22  
**Deciders:** Engineering  
**Technical Story:** PRD: [agent-token-breakdown-and-spend](../prd/agent-token-breakdown-and-spend.md)

---

## Context

The agent detail page has a "Tokens (7d)" stat card that currently always shows `—`. The `agents.Run` struct does not carry per-category token data; the frontend derives its `tokensUsed` value from `run.tokens_used` which is never populated by the backend.

Separately, there is no spend card on the agent detail page, despite the inference gateway already tracking `estimated_cost_usd` on every `inference_usage` row with a `run_id` linking back to the originating agent run.

The `inference_usage` table (inference-gateway schema) and the `runs` table (orbit-agents schema) share the same Postgres instance and can be joined on `inference_usage.run_id = runs.id`.

### Goals

- Surface real per-category token data (input, output, cache-read, cache-write) on the agent overview.
- Surface total estimated spend for the agent over the last 7 days.

### Non-Goals

- Configurable time windows on the agent detail page.
- Backfilling `tokens_used` on the `runs` table.
- Full analytics charts per agent.

---

## Decision Drivers

* **Accuracy**: token breakdown must come from the authoritative source (`inference_usage`), not a derived/stale run field.
* **Minimal blast radius**: no schema migrations needed; the join between existing tables is sufficient.
* **Consistency**: the response shape should mirror the existing `AnalyticsKPIs` struct so the frontend can use the same rendering pattern as the model-gateway analytics cards.
* **Security**: tenant isolation must be enforced at the query level; the endpoint must require `platform:analytics:read`.

---

## Considered Options

### Option 1: Add a new `GET /v1/analytics/agents/{agentTypeId}` endpoint in inference-gateway

**Description:** Add a `GetAgentTypeAnalytics` method to `PostgresInferenceUsageRepository` that runs a single aggregation query:

```sql
SELECT
    COALESCE(SUM(prompt_tokens), 0),
    COALESCE(SUM(completion_tokens), 0),
    COALESCE(SUM(cache_read_input_tokens), 0),
    COALESCE(SUM(cache_creation_input_tokens), 0),
    COALESCE(SUM(total_tokens), 0),
    COALESCE(SUM(estimated_cost_usd), 0)
FROM inference_usage iu
JOIN runs r ON r.id = iu.run_id
WHERE iu.tenant_id = $1
  AND r.agent_type_id = $2
  AND iu.created_at >= $3   -- 7-day cutoff
```

Register a new handler `handleAnalyticsAgent` at `GET /v1/analytics/agents/{agentTypeId}`. Require `platform:analytics:read` scope. Return a minimal `AgentAnalyticsKPIs` struct.

**Pros:**
- Single round-trip; lightweight query.
- Respects existing pattern (analytics endpoints in inference-gateway).
- No schema changes.
- Tenant-isolated by both `iu.tenant_id = $1` AND the `runs.agent_type_id` filter.
- Endpoint can reuse the existing OTel/logging/error infra.

**Cons:**
- Small cross-service DB coupling (inference-gateway querying the `runs` table which is owned by orbit-agents). Acceptable because they share one Postgres instance by design.

### Option 2: Enrich the `runs` table with token columns

**Description:** Add `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens`, `estimated_cost_usd` to the `runs` table and populate them at run completion via a session-manager callback.

**Pros:**
- Keeps all run data co-located.
- Enables per-run token columns in the runs table data table without an extra call.

**Cons:**
- Requires a new Goose migration.
- Requires a new session-manager callback to orbit-agents at run completion.
- Duplication of data already in `inference_usage`.
- Per-agent aggregate still requires a second query over run rows.
- Significant scope for what is essentially a display feature.

### Option 3: Use existing analytics endpoint with principal_id (SPIFFE) filter

**Description:** Resolve the agent's SPIFFE ID and pass it as `principal_id` to `GET /v1/analytics`.

**Pros:**
- No new backend endpoint needed.

**Cons:**
- SPIFFE ID is not exposed via any public API; the frontend would need to reconstruct it from internal naming conventions (`spiffe://{td}/tenant/{ns}/agent/{slug}`), coupling UI to infra.
- Trust-domain and namespace values are deployment-specific.
- Non-sandbox traffic (e.g., direct PAT calls) would not be attributed even if such a scenario exists.
- Fragile to identity changes.

---

## Decision

**Chosen Option:** Option 1 — new `GET /v1/analytics/agents/{agentTypeId}` endpoint.

### Rationale

The join query is the only authoritative path that handles the existing data model without migration. The cross-service table reference is acceptable given the single-database architecture. The response shape mirrors `AnalyticsKPIs` (minus latency/request-count fields not relevant here), keeping the frontend rendering straightforward.

---

## Consequences

### Positive

- Tokens card shows real data immediately for all agents that have ever made inference calls.
- Spend card provides cost visibility at a glance without navigating to model-gateway analytics.
- No migrations required.
- Implementation is self-contained in one new handler + one new repository method + minimal frontend additions.

### Negative

- The inference-gateway repository now references the `runs` table (orbit-agents schema). If the `runs` table is ever moved to a separate database, this query breaks. This is an intentional, documented trade-off.

### Neutral

- The endpoint uses a 7-day fixed window. The agent detail page already titles the card "Tokens (7d)", so no UX inconsistency is introduced.

---

## Implementation

### Backend (`backend/internal/inferencegw/`)

1. **New type** `AgentAnalyticsKPIs` in `models.go` — lightweight struct with the five token-category fields and `estimated_cost_usd`.
2. **New repository method** `GetAgentTypeAnalytics(ctx, tenantID, agentTypeID string, days int) (AgentAnalyticsKPIs, error)` in `repository.go` — single aggregation query joining `inference_usage` with `runs`.
3. **New repository interface method** added to `InferenceUsageRepository`.
4. **New handler** `handleAnalyticsAgent` in `handlers_analytics.go` — extracts `agentTypeId` path param, calls service, returns JSON.
5. **New service method** `AnalyticsAgent(ctx, agentTypeID string, days int)` in `service.go` — thin delegation to repository.
6. **Route registration** in `controller.go`: `GET /v1/analytics/agents/{agentTypeId}` with `platform:analytics:read` scope.

### Frontend (`frontend/src/`)

1. **New type** `AgentAnalyticsKPIs` in `shared/api.ts` and re-exported from `app/(dashboard)/model-gateway/_types/index.ts`.
2. **New BFF service function** `fetchAgentAnalyticsFromGateway` in `app/(dashboard)/model-gateway/_services/analytics-server.ts`.
3. **New Next.js API route** `app/api/analytics/agents/[agentTypeId]/route.ts` — session-gated proxy.
4. **Updated `agent-overview-tab.tsx`**:
   - Fetch analytics on mount via `useEffect` + `fetch`.
   - Replace the simple Tokens card with a breakdown card (total + 4 rows: Input, Output, Cache read, Cache write) matching the `analytics-kpi-cards.tsx` `TokensCard` pattern.
   - Add a fifth card "Spend (7d)" showing `$x.xxxx`.
   - Grid changes from `cols={4}` to `cols={{ base: 2, md: 3, lg: 5 }}` to accommodate 5 cards.

---

## Validation

### Success Metrics

- Tokens card shows non-zero breakdown for agents with inference calls.
- Spend card matches the figure from model-gateway analytics when filtered to the same agent and period.
- Zero regressions on the existing four cards.

---

## Related Decisions

- [2026-04-27-inference-analytics](2026-04-27-inference-analytics.md) — analytics aggregation pattern this follows.
- [2026-06-05-llm-cost-tracking](2026-06-05-llm-cost-tracking.md) — cost attribution model in inference-gateway.
- [2026-06-10-hierarchical-budget-enforcement](2026-06-10-hierarchical-budget-enforcement.md) — per-run budget and `run_id` column motivation.

---

**Last Updated:** 2026-06-22
