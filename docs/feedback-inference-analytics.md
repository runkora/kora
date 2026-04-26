# Feedback: ADR-0043 and PRD — Inference Analytics

**Reviewer:** Claude Code
**Date:** 2026-04-21
**Documents reviewed:**
- `docs/adr/0043-inference-analytics.md`
- `docs/prd/inference-analytics.md`

Overall these are well-structured documents. The option analysis is thorough, the security rationale is sound, and the phasing is sensible. The issues below range from a critical implementation bug to minor inconsistencies.

---

## Critical

### 1. Route registration blocks the personal path (ADR)

The ADR shows this route registration:

```go
{PathPrefix: "/v1/analytics", Method: "GET", RequiredScope: "platform:analytics:read"},
```

This registers the scope as **required** at the policy layer, which means any caller without `platform:analytics:read` — including ordinary users hitting the personal path — will receive a 403 before the handler is ever reached. The handler-level scope check described later ("if the caller lacks `platform:analytics:read`, the query is always filtered to their own `principal_id`") never fires.

The fix is to register the route as authenticated-only (no `RequiredScope`) and move the full access control logic into the handler itself: if the caller has the scope, run the tenant-wide query; otherwise, run the `principal_id`-filtered query. The ADR needs to update the code snippet and add a note explaining why `RequiredScope` is intentionally absent.

---

## Significant

### 2. PAT filter belongs on My Usage — ADR parameter table is wrong (ADR)

**Decision:** PAT filter is a My Usage feature for ordinary users, not an admin filter.

The ADR parameter table says `token_id` is "ignored if caller lacks `platform:analytics:read`", which means only admins can use it. This is the opposite of what is intended. Required fixes:

- Flip the condition to "ignored if caller **has** `platform:analytics:read`" (admins see all tenant data; narrowing by a single PAT doesn't apply)
- Add `AND token_id = $N` as a conditional clause on the personal-scope SQL path
- Update the parameter table note to "PAT filter (My Usage only; ignored if caller has `platform:analytics:read`)"

### 3. Tenant isolation not explicit on the personal path (ADR)

The SQL snippet includes `WHERE tenant_id = $1` on the shown query, but the ADR's prose description of the personal path only mentions `principal_id` filtering. It should explicitly state that `tenant_id = <authenticated tenant>` is always applied regardless of scope, so it's clear the personal path cannot leak cross-tenant data even if a `principal_id` happens to exist in another tenant's data.

### 4. `"daily"` key name doesn't work for non-daily granularities (ADR)

The response shape has a hardcoded `"daily"` key for the time-series array. If the caller requests `granularity=weekly` or `granularity=monthly`, the key name is wrong and clients would need to know which key to read based on the request parameter. Rename this to `"buckets"` or `"timeseries"` so it's granularity-agnostic, and add a note that `granularity` in the response body already tells the client how to interpret the bucket dates.

### 5. `model_id` filter missing from SQL (ADR)

The parameter table lists `model_id` as an Analytics-only filter, but the SQL snippet doesn't include the corresponding `AND model_id = $N` clause (even as a conditional comment). The implementation section should show how this parameter is conditionally applied to the query, including which bind parameter position it occupies.

### 6. Empty state decision needs to be recorded; open question should be closed (PRD)

**Decision:** My Usage is always visible. When there are no records, show the tab with empty/zero-state charts rather than explanatory copy only.

Close Open Question 1 with this decision. Update the acceptance criterion to reflect "empty charts with a zero state" rather than "explanatory copy" — the distinction matters for frontend implementation (zero-axis charts vs a placeholder panel).

---

## Minor

### 7. Cache hit rate definition inconsistency between documents

The ADR defines:

> `cache_hit_rate = SUM(cache_read_input_tokens) / (SUM(prompt_tokens) + SUM(cache_read_input_tokens))`

The PRD risk table defines it as:

> `cache_read_input_tokens / (prompt_tokens + cache_read_input_tokens)` per request, averaged across the period

These produce different results when requests have varying token counts. The ADR's definition (sum-then-divide) is the correct weighted approach. Update the PRD risk table to match.

### 8. PRD describes `principal_id` as a client-supplied parameter

The PRD's Proposed Solution section says: "The endpoint accepts an optional `principal_id` query parameter: when present (My Usage), it filters to that user's records." But both the ADR and the PRD's own security requirements state that client-supplied `principal_id` is ignored — the server always uses the authenticated identity. The PRD description of the API mechanism is misleading and should be corrected to match the actual design.

### 9. Latency chart 14d default conflicts with the shared period control (PRD)

**Decision:** All charts share a single period selector; there are no per-chart period controls.

The PRD gives the latency chart a 14-day default while all other charts default to 30 days. With a shared selector this is a contradiction — the selector has one value. Update the PRD to remove the 14-day latency default and align all charts to the same 30-day default. The period selector (7d / 30d / 90d) controls all charts simultaneously.

### 10. `percentile_cont` memory growth not mitigated

The ADR correctly identifies that `percentile_cont` loads all latency values in the window into the aggregate. For a 90-day tenant-wide query with high request volume this could be significant. The consequences section notes the risk but offers no mitigation path. Consider adding a note about approximate alternatives (`percentile_disc`, pre-bucketed histograms) as a future option if the memory footprint becomes a problem, consistent with how the read-replica option is handled elsewhere.

### 11. ADR milestone date is already past

The PRD timeline lists "ADR Created: 2026-04-22" but the ADR is dated 2026-04-21 and already exists. Update the milestone to reflect the actual creation date.

### 12. Performance SLA inconsistency between documents

The ADR's success metrics specify personal 90-day ≤ 500ms p95 and tenant-wide 90-day ≤ 1000ms p95. The PRD's non-functional requirements say both paths must respond ≤ 500ms p95. One document needs to be updated — if 500ms for tenant-wide is achievable with the right indexes, set it there; if not, the PRD needs to be relaxed for the admin path.

### 13. ADR doesn't specify the index strategy

The PRD lists the indexes needed — `(tenant_id, created_at)` and `(tenant_id, principal_id, created_at)` — as part of its performance requirements. The ADR only notes "aggregation query adds read load to the primary database" with no mention of indexes. The index requirements belong in the ADR as the authoritative implementation document.

---

## Summary

| # | Severity | Location | Issue |
|---|---|---|---|
| 1 | Critical | ADR §Access Control | Route `RequiredScope` blocks the personal path |
| 2 | Significant | Both | PRD/ADR misalignment on where PAT filter lives |
| 3 | Significant | ADR §API | Tenant isolation not stated for personal path |
| 4 | Significant | ADR §Response shape | Hardcoded `"daily"` key breaks non-daily granularities |
| 5 | Significant | ADR §SQL | `model_id` filter not reflected in SQL snippet |
| 6 | Significant | PRD §Open Questions | Open question contradicts existing acceptance criterion |
| 7 | Minor | Both | `cache_hit_rate` definition inconsistency |
| 8 | Minor | PRD §Solution | `principal_id` described as client-supplied parameter |
| 9 | Minor | PRD §Charts | Latency 14d default vs shared period selector |
| 10 | Minor | ADR §Consequences | `percentile_cont` memory risk has no mitigation path |
| 11 | Minor | PRD §Timeline | ADR milestone date is a day late |
| 12 | Minor | Both | Performance SLA differs between documents (500ms vs 1000ms for tenant) |
| 13 | Minor | ADR §Implementation | Index strategy missing from ADR |
