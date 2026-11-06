# Policy Bundle Grants Index

**Status:** Accepted  
**Date:** 2026-11-06  
**Deciders:** Platform Team  
**Technical Story:** `GET /api/v1/triggers` latency in v0.12.0-rc.3

---

## Context

The tenant bundle carries the tenant's grants as one list in `data.grants.grants`. `authz.rego` and `authz_gateway.rego` find a principal's grants by scanning that list for a matching `principal_type` and `principal_id`. One Layer 2 decision scans it about six times. The cost of a decision grows with the tenant's grant count: `BenchmarkLayer2Decision` in `internal/policyservice/bundle/authz_bench_test.go` measures 91 µs at 10 grants and 9.24 ms at 5,000 grants.

`GET /api/v1/triggers` runs one `Check` per trigger to set `can_fire`. In production the list takes 231 ms at p50 for 25 triggers and 41 ms for 5 triggers, about 9.5 ms per trigger.

## Considered Options

- **Option A (chosen):** Add a per-principal index to the bundle and read a principal's grants through it in `authz.rego`.
- **Option B (rejected):** Batch the checks of one request into one evaluation. With one required scope per trigger, a batch still scans every grant once per trigger. Callers that check one resource per request gain nothing from a batch.
- **Option C (rejected):** Cache decisions in the engine. A first decision for each principal and scope still pays the full scan. A cached allow outlives a revoked grant until the cache drops it.

## Decision

The bundles carry a per-principal grants index, and `authz.rego` and `authz_gateway.rego` read the principal's and the delegator's grants through it.

- `grants/data.json` carries `by_principal` beside `grants`: a map from principal type to a map from principal id to the positions of that principal's grants in `grants`. The policy service compiler builds it for the tenant bundle and for each tenant in the gateway bundle.
- `_principal_grants` and `_delegator_grants` in both policies collect the grants at those positions. Every rule that matched a grant by principal type and id reads one of these sets.
- A principal or delegator with no entry in the index gets an empty set. Every allow rule that reads it fails closed, as the scan did when no grant matched.

## Consequences

### Positive

- A Layer 2 decision costs the same at any tenant grant count. `BenchmarkLayer2Decision` measures 65 µs to 71 µs from 10 to 5,000 grants.
- `GET /api/v1/triggers` no longer grows by about 9.5 ms per trigger.

### Negative

- Each bundle grows by one index entry per principal.

### Neutral

- One bundle carries the rules and the index together. No bundle pairs the new rules with data that lacks the index.

## References

- `backend/internal/policyservice/bundle/compiler.go`
- `backend/internal/policyservice/bundle/authz.rego`
- `backend/internal/policyservice/bundle/authz_gateway.rego`
- `backend/internal/policyservice/bundle/authz_bench_test.go`
