# Agent Token Breakdown and Spend Card

**Date:** 2026-06-23  
**Status:** Accepted  
**Deciders:** Engineering  
**PRD:** [agent-token-breakdown-and-spend-card](../prd/agent-token-breakdown-and-spend-card.md)

---

## Context

The agent detail overview page has four stat cards. The "Tokens (7d)" card currently always shows "—" because it sums `run.tokens_used` from the client-side run list, but that field is not reliably populated (it requires the session manager to write a final summary).

The actual token data is in the `inference_usage` table on the inference-gateway, indexed by `run_id` and `principal_id`. A per-run usage endpoint already exists (`GET /v1/usage/run/{runID}`), but it only returns `spend_usd` and `total_tokens`. There is no endpoint for aggregating across all runs for a given agent type.

Two new capabilities are needed:
1. Token breakdown (input / output / cache-read / cache-write) for an agent type over a 7-day window
2. Total spend for an agent type over the same window

---

## Options Considered

### Option A: Extend the existing `Run` list response to include token breakdown per run

Add `prompt_tokens`, `completion_tokens`, `cache_read_tokens`, `cache_creation_tokens` to every `Run` object by joining `inference_usage` when listing runs.

**Pros:** Single data source; breakdown per-run available for the table.  
**Cons:** N+1 join for every run list response; runs and inference-gateway are separate services — cross-service join at the agents service layer requires a new platform client call; increases response payload for every consumer of the run list even when breakdown is not needed.

**Not chosen.**

### Option B: New inference-gateway endpoint for agent-type aggregate usage (chosen)

Add `GET /v1/usage/agent-type/{agentTypeID}` to the inference-gateway that aggregates `inference_usage` rows for the agent's SPIFFE principal over the last 7 days.

The agent's SPIFFE principal ID is deterministic: `spiffe://orbit/{tenantID}/agent/{agentTypeID}`. The inference-gateway already stores `principal_id` on every `inference_usage` row written by an agent run.

**Pros:**
- Single SQL aggregation query; no cross-service join.
- Follows the existing `GetRunUsage` pattern exactly.
- BFF proxy route is trivial.
- Frontend fetches one endpoint for all new card data.

**Cons:**
- New endpoint on inference-gateway.
- SPIFFE principal ID format must be kept in sync with the identity-proxy sidecar (already managed centrally).

**Chosen.**

### Option C: Compute at the BFF from the existing run list

Sum `tokens_used` from the BFF-side run list response and call the per-run usage endpoint for each run to get breakdown.

**Pros:** No backend changes.  
**Cons:** O(N) backend calls for N runs; `tokens_used` is unreliable (same root cause as the current bug); high latency.

**Not chosen.**

---

## Decision

**Option B**: Add a new inference-gateway endpoint `GET /v1/usage/agent-type/{agentTypeID}` and proxy it through the BFF.

---

## Implementation

### 1. Backend — inference-gateway

**New model (`RunUsage` extended, new `AgentTypeUsage`):**

```go
// AgentTypeUsage is the spend and token breakdown recorded against an agent
// type's principal over the last 7 days.
type AgentTypeUsage struct {
    AgentTypeID        string  `json:"agent_type_id"`
    PromptTokens       int64   `json:"prompt_tokens"`
    CompletionTokens   int64   `json:"completion_tokens"`
    CacheReadTokens    int64   `json:"cache_read_tokens"`
    CacheCreationTokens int64  `json:"cache_creation_tokens"`
    TotalTokens        int64   `json:"total_tokens"`
    EstimatedCostUSD   float64 `json:"estimated_cost_usd"`
}
```

**Repository interface addition:**

```go
// GetAgentTypeUsage returns aggregated token counts and spend for an agent
// type's principal over the last 7 days, tenant-scoped.
GetAgentTypeUsage(ctx context.Context, tenantID, agentTypeID string) (AgentTypeUsage, error)
```

**SQL query:**

```sql
SELECT
    COALESCE(SUM(prompt_tokens), 0),
    COALESCE(SUM(completion_tokens), 0),
    COALESCE(SUM(cache_read_input_tokens), 0),
    COALESCE(SUM(cache_creation_input_tokens), 0),
    COALESCE(SUM(total_tokens), 0),
    COALESCE(SUM(estimated_cost_usd), 0)
FROM inference_usage
WHERE tenant_id = $1
  AND principal_id = $2
  AND created_at >= NOW() - INTERVAL '7 days'
```

The principal ID is derived using the same deterministic format as the identity-proxy: `spiffe://orbit/{tenantID}/agent/{agentTypeID}`.

**Handler:** `GET /v1/usage/agent-type/{agentTypeID}` — same auth pattern as `handleGetRunUsage`.

**Route registration:** Added in `controller.go` alongside the existing usage routes.

### 2. BFF — Next.js route handler

New file: `frontend/src/app/api/agents/types/[typeId]/usage/route.ts`

Proxies `GET /v1/usage/agent-type/{agentTypeID}` from the inference gateway, injecting the session token server-side. Same pattern as the existing run usage route.

### 3. Frontend — shared types

New interface in `frontend/src/shared/api.ts`:

```typescript
export interface AgentTypeUsage {
  agent_type_id: string;
  prompt_tokens: number;
  completion_tokens: number;
  cache_read_tokens: number;
  cache_creation_tokens: number;
  total_tokens: number;
  estimated_cost_usd: number;
}
```

### 4. Frontend — agent detail API

New function `fetchAgentTypeUsage(agentTypeId)` in `agent-detail-api.ts`.

New API route added to `ApiRoute.agentTypes`.

### 5. Frontend — agent overview tab

The four-card stat grid becomes five cards:

| Card | Change |
|------|--------|
| Total Runs | Unchanged |
| Success Rate | Unchanged |
| Avg Duration | Unchanged |
| Tokens (7d) | Replaced: now a breakdown card (total headline + input/output/cache-read/cache-write sub-rows) sourced from the new endpoint |
| Spend (7d) | **New** card showing `fmtUsd(estimatedCostUsd)` |

Grid: `cols={{ base: 2, md: 3, lg: 5 }}`

Data is fetched on component mount via a `useEffect`/`useState` inside `AgentOverviewTab`. The existing `totalTokens` `useMemo` (which sums `run.tokensUsed`) is removed; replaced by state populated from the new endpoint. Loading state: values show "—" until the fetch resolves.

---

## Consequences

**Positive:**
- Token card becomes functional and informative.
- Spend card closes a gap in agent-level observability.
- Pattern is consistent with the model-gateway analytics KPI cards.
- No schema migration required.

**Negative / Trade-offs:**
- New endpoint on inference-gateway adds one more route to maintain.
- The 7-day window is hard-coded (acceptable for now; can be made configurable later via query param).
- Agents not using the inference gateway (BYOC with direct Anthropic API access) will show zeros — this is a correct and expected result.

---

## Notes

- The SPIFFE principal ID format `spiffe://orbit/{tenantID}/agent/{agentTypeID}` is already established in `k8s.go:agentSpiffeID`. The inference-gateway constructs the same format in the handler.
- No new database index is needed: `inference_usage` already has an index on `(tenant_id, principal_id, created_at)` from the analytics work (ADR `2026-04-27-inference-analytics`).
