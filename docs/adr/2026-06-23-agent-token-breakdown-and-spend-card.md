# Agent Token Breakdown and Spend Card

**Date:** 2026-06-23  
**Status:** Accepted; Amendment 1 Proposed  
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

---

## Amendment 1 (2026-09-29): Aggregates key on the agent slug in the attested SPIFFE path

Touches **Who is this agent?**: the aggregates now read the agent identity exactly as SPIRE attests it. Access is unchanged: both endpoints keep the `agents:types:read` route scope and the tenant filter from the verified token.

### Context

The principal format stated above is wrong. Agent pods receive `spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}` from the `orbit-tenant-agent` ClusterSPIFFEID (`charts/orbit/templates/platform/clusterspiffeids.yaml`), where the trust domain is per install and the namespace is the tenant namespace (`orbit-tenant-{tenant}`). Identity keeps that URI as the principal ID and normalises per-run sandbox SVIDs (`.../agent/{slug}/sandbox/{run-id}`) to it, so every `inference_usage` row an agent writes carries it as `principal_id` with `principal_type = 'AGENT'`. The last path segment is the agent type slug, not its UUID. The gateway built `spiffe://orbit/{tenantID}/agent/{agentTypeID}` instead, so no row ever matched and the agents list and agent overview showed "—". On the local cluster, 0 of 491 agent rows in the last 7 days matched.

The run list's `tokens_used` field has no source: orbit-agents never had such a column, so the "Tokens" column in the run tables and the runs-based fallback on the agents list were always empty.

### Options

- **A. Rebuild the full principal in the gateway** from a configured trust domain and the `orbit-tenant-` namespace convention. Exact match on the existing index, but duplicates two facts the gateway does not own and breaks silently if either changes.
- **B. Key by slug, matching the `/agent/{slug}` suffix of agent principals within the tenant.** The trust domain and namespace are not reconstructed. Within one tenant, a slug is unique (`agent_types (tenant_id, slug)`) and is the identity SPIRE attests.
- **C. Attribute by run**: ask orbit-agents for the agent's run IDs and sum `inference_usage` by `run_id`. Correct, but adds a cross-service call and an unbounded `IN` list.

### Decision

Option B.

- `GET /v1/usage/agent-type/{slug}` sums rows where `tenant_id` is the caller's tenant, `principal_type = 'AGENT'`, and the last `len('/agent/{slug}')` characters of `principal_id` equal `/agent/{slug}`. The comparison is exact, so no input character acts as a wildcard, and `probe` never counts `token-probe` rows. The handler rejects with 400 a slug that is not a valid Kubernetes label value, the format the `spiffe.io/agent` pod label that SPIRE reads already requires.
- `GET /v1/usage/agent-types` groups the same rows by the slug taken from the last `/agent/{slug}` path segment.
- Responses carry `agent_slug` in place of `agent_type_id`. The BFF route moves to `GET /api/agents/types/usage/{slug}`, and the frontend looks usage up by slug.
- The run tables drop the "Tokens" column and the agents list drops the runs-based token fallback. Per-run tokens stay on the run page, which reads `GET /v1/usage/run/{runID}`.

### Consequences

- The aggregates show real numbers for every agent, whatever the trust domain or tenant namespace.
- Usage follows the slug. An agent type deleted and recreated with the same slug within the 7-day window reports the combined usage, as both held the same SPIFFE identity.
- The suffix match cannot use the `principal_id` index; the query scans the tenant's rows for the last 7 days through `(tenant_id, created_at)`.
- Per-run token counts are no longer shown in run lists. Showing them again needs a batch per-run usage read from the gateway.
