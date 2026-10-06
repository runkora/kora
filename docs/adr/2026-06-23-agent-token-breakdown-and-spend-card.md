# Agent Token Breakdown and Spend Card

**Date:** 2026-06-23  
**Status:** Accepted (Amended 2026-10-06)  
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
- Superseded by Amendment 2: The 7-day window is hard-coded (acceptable for now; can be made configurable later via query param).
- Agents not using the inference gateway (BYOC with direct Anthropic API access) will show zeros — this is a correct and expected result.

---

## Notes

- The SPIFFE principal ID format `spiffe://orbit/{tenantID}/agent/{agentTypeID}` is already established in `k8s.go:agentSpiffeID`. The inference-gateway constructs the same format in the handler.
- No new database index is needed: `inference_usage` already has an index on `(tenant_id, principal_id, created_at)` from the analytics work (ADR `2026-04-27-inference-analytics`).

---

## Amendment 1 (2026-09-29): Aggregates key on an agent slug stored at write time

Touches **Who is this agent?**: the aggregates key on the agent identity SPIRE attests, recorded once when usage is written. Access is unchanged: both endpoints keep the `agents:types:read` route scope and the tenant filter from the verified token.

### Context

The principal format stated above is wrong. Agent pods receive `spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}` from the `orbit-tenant-agent` ClusterSPIFFEID (`charts/orbit/templates/platform/clusterspiffeids.yaml`), where the trust domain is per install and the namespace is the tenant namespace (`orbit-tenant-{tenant}`). Identity keeps that URI as the principal ID and normalises per-run sandbox SVIDs (`.../agent/{slug}/sandbox/{run-id}`) to it, so every `inference_usage` row an agent writes carries it as `principal_id` with `principal_type = 'AGENT'`. The last path segment is the agent type slug, not its UUID. The gateway built `spiffe://orbit/{tenantID}/agent/{agentTypeID}` instead, so no row ever matched and the agents list and agent overview showed "—".

The run list's `tokens_used` field has no source: orbit-agents never had such a column, so the "Tokens" column in the run tables and the runs-based fallback on the agents list were always empty.

### Options

- **A. Rebuild the full principal in the gateway** from a configured trust domain and the `orbit-tenant-` namespace convention. Exact match on the existing index, but duplicates two facts the gateway does not own and breaks silently if either changes.
- **B. Store the slug at write time.** The gateway records usage with the verified principal, so it takes the agent segment of the SPIFFE path once and stores it in a new `inference_usage.agent_slug` column. Aggregates filter on that column through an index. Within one tenant, a slug is unique (`agent_types (tenant_id, slug)`) and is the identity SPIRE attests.
- **C. Match the `/agent/{slug}` suffix of `principal_id` at query time.** No schema change, but the suffix comparison cannot use an index, and every read parses identity downstream of where it was verified.
- **D. Attribute by run**: ask orbit-agents for the agent's run IDs and sum `inference_usage` by `run_id`. Correct, but adds a cross-service call and an unbounded `IN` list.

### Decision

Option B.

- `inference_usage` gains a nullable `agent_slug` column and the index `idx_inference_usage_agent_slug (tenant_id, agent_slug, created_at DESC) WHERE agent_slug IS NOT NULL`. The migration is additive.
- When the gateway writes a usage row for an `AGENT` principal whose ID has the form `spiffe://{trust-domain}/tenant/{namespace}/agent/{slug}[/sandbox/{run-id}]`, it stores `{slug}`. Every other principal stores NULL.
- The same migration backfills existing `AGENT` rows from the exact `/agent/{slug}` path segment. SERVICE, HUMAN and PAT rows stay NULL.
- Superseded by Amendments 2 and 3 in part (scope, window, response): `GET /v1/usage/agent-type/{slug}` sums rows where `tenant_id` is the caller's tenant and `agent_slug = {slug}` over the last 7 days. The handler rejects with 400 a slug that is not a valid Kubernetes label value, the format the `spiffe.io/agent` pod label that SPIRE reads already requires.
- Superseded by Amendment 2 (removed): `GET /v1/usage/agent-types` groups the same rows by `agent_slug`.
- Responses carry `agent_slug` in place of `agent_type_id`. The BFF route moves to `GET /api/agents/types/usage/{slug}`, and the frontend looks usage up by slug. Superseded by Amendment 3 (BFF route).
- The run tables drop the "Tokens" column and the agents list drops the runs-based token fallback. Per-run tokens stay on the run page, which reads `GET /v1/usage/run/{runID}`.

### Consequences

- The aggregates show real numbers for every agent, whatever the trust domain or tenant namespace, and both queries are served by the new index.
- Identity is parsed once, at write time, from the verified principal; reads never interpret `principal_id`.
- Usage follows the slug. An agent type deleted and recreated with the same slug within the 7-day window reports the combined usage, as both held the same SPIFFE identity.
- Delegated runs count toward the agent's aggregate, so anyone with `agents:usage:read` sees the agent's combined spend across all delegators (scope changed by Amendment 2). Per-person figures stay on the personal usage views.
- Rows written by a gateway binary older than the migration carry no slug until they are backfilled; the migration backfills everything present when it runs.
- The index is built without `CONCURRENTLY`, so on a large `inference_usage` the migration blocks writes for the length of the backfill and index build. That is acceptable at current production volume; a larger install needs a concurrent build in a no-transaction migration.
- Per-run token counts are no longer shown in run lists. Showing them again needs a batch per-run usage read from the gateway.

---

## Amendment 2 (2026-10-03): Agent usage scope, windows and budgets

Touches **What can it access?**: a new scope, `agents:usage:read`, decides who may read agent usage.

### Context

Amendment 1 left the usage reads on `agents:types:read` with a fixed 7-day window. The Agents page needs a selectable window, a model split, spend over time, per-agent budgets and run outcomes.

### Decision

- Agent usage is gated by the route scope `agents:usage:read`. `agents:usage:{slug}:read` covers one agent and does not open the routes. Both scopes are siblings of `agents:types` and `agents:runs` in the orbit-agents vocabulary. No role is evaluated.
- `GET /v1/usage/agent-insights` and `GET /v1/usage/agent-type/{slug}` return aggregates per agent: totals, spend by model, spend over time, the previous window's total and budgets. The responses carry no per-person data.
- The predicate is the verified tenant, `agent_slug IS NOT NULL` and the window, with the slug when one is asked for. Delegated rows and the rows of deleted agents count for every caller.
- The window is 24h, 7d, 30d or 90d, ends at the server's now, and defaults to 7d. Both services parse it through `internal/platform/usagewindow`.
- Budget rows come from the policy bundle's agent principals: one row per agent with a window limit, delegated agents included. Spend follows the same predicate. The tenant cap is not part of the page.
- `GET /api/v1/run-outcomes` in orbit-agents counts completed and failed runs by `completed_at` under the run list visibility rule. It requires `agents:runs:read` and uses the index `idx_runs_tenant_completed`.
- The rollups run in `pgx.QueryExecModeExec`. The generic plan of a prepared statement reads most of the table through an index and took 2 s at 90 days and 150 agents, against 0.3 s with the actual window.
- The trend query sums one `FILTER` aggregate per bucket in a single scan. A plain `GROUP BY` on the bucket measured 1.35 to 3.5 times slower. The `FILTER` clauses use generated `$N` placeholders and no value is formatted into the SQL.
- `GET /v1/usage/agent-types` is removed.

### Consequences

- A platform admin holds `agents:usage:read` through the derived admin scopes.
- A holder of `agents:usage:read` sees every delegator's spend on a delegated agent.
- Budget spend is counted by agent slug and can differ slightly from the per-principal spend the limit check counts.

---

## Amendment 3 (2026-10-06): Agent detail Overview reads

Touches **What can it access?**: no new scope.

### Context

The agent detail Overview needs one agent's run counts, run durations, tokens, spend and budget for a selectable window.

### Decision

- `GET /v1/usage/agent-type/{slug}?period=24h|7d|30d|90d` returns the `GET /v1/usage/agent-insights` shape for one slug. The route scope stays `agents:usage:read`. The flat `AgentTypeUsage` response gives way to the insights shape, where `AgentTypeUsage` is the element of `usage`.
- `GET /api/v1/run-stats/{slug}?period=24h|7d|30d|90d` in orbit-agents requires `agents:runs:read` and the caller's `agents:runs:{slug}:read`. A caller without `agents:delegated:read` sees autonomous runs and their own delegated runs.
- It returns `total`, `triggered`, `completed`, `failed` and `durations`. `total` counts runs created in the window and `triggered` those with a trigger attribution. `completed` and `failed` count runs finished in the window by `completed_at`. `durations` carries `median_ms`, `p95_ms`, `p99_ms`, `longest_ms`, `capped`, ten `buckets` and `overflow`, all computed in SQL.
- From 20 finished runs the buckets span 0 to p99, runs above p99 count in `overflow` and `capped` is true. Below 20 they span 0 to the longest run, `overflow` is 0 and `capped` is false. The client shows the p95 marker only when `capped` is true.
- The BFF composes both reads at `GET /api/agents/types/overview/{slug}?period=` under the caller's token. A part the caller may not read, a 403 or 404, is left out. A part that errors or times out is named in `failed` and its card reads Could not load.

### Consequences

- A caller with `agents:runs:read` and without `agents:usage:read` sees the run tiles and no spend or token cards.
