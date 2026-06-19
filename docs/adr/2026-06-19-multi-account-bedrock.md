# Multi-Account AWS Bedrock Support

**Status:** Accepted  
**Date:** 2026-06-19  
**Deciders:** Orbit Platform Team  
**Technical Story:** [Multi-Account Bedrock PRD](../prd/multi-account-bedrock.md) — defines the requirement for supporting multiple simultaneous AWS Bedrock environments in the inference gateway.

---

## Context

### Background

The inference gateway's Bedrock connector (`internal/inferencegw/providers/bedrock`) is constructed from a single `Config` struct populated by a fixed set of environment variables: `BEDROCK_ENABLED`, `BEDROCK_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN`. This produces a single `ProviderConnector` registered under the `bedrock` `ProviderName`.

The `ProviderName` type is already a plain `string` alias. The routing engine, catalog, and usage records treat it as an opaque identifier — there is no structural reason it must be the literal string `"bedrock"`. The virtual model catalog's `VirtualModelRoute.Provider` field is already a free-form `ProviderName`, so new provider names flow through all existing infrastructure (routing, usage recording, catalog UI, policy scopes) without any schema changes.

### Problem Statement

Organisations operating multiple AWS accounts cannot use a single Orbit deployment to route inference traffic across more than one account. The Bedrock connector's single-instance design requires either:

- Choosing one account at deploy time and ignoring others, or
- Running multiple Orbit deployments (one per account), which defeats the purpose of a unified control plane.

### Goals

- Support N simultaneously active Bedrock environments, each with independent AWS credentials and regions.
- Register each environment as a distinct `ProviderConnector` with a namespaced name (`bedrock:<alias>`).
- Preserve full backwards compatibility with the existing single-account path (`BEDROCK_ENABLED=true`).
- Minimise blast radius — only the Bedrock package and `main.go` change.

### Non-Goals

- Per-tenant Bedrock account assignment.
- Dynamic credential rotation at runtime.
- Helm chart changes (follow-on).
- Cross-account model aliasing.

---

## Decision Drivers

* **Zero breaking change** — existing `BEDROCK_ENABLED` deployments must work without any config change.
* **Minimal interface impact** — the `ProviderConnector` and `ConnectorFactory` interfaces should not change.
* **Self-documenting config** — account names (not indices) appear in provider names, logs, and the catalog UI.
* **Leverage existing routing primitives** — virtual model routes, failover chains, and weighted routing already handle multiple providers; we need only to register more providers.

---

## Considered Options

### Option 1: Named accounts via `BEDROCK_ACCOUNTS` + per-account prefixed env vars

Introduce `BEDROCK_ACCOUNTS=prod,staging` (comma-separated list of aliases). For each alias, read `BEDROCK_<UPPER_ALIAS>_REGION`, `BEDROCK_<UPPER_ALIAS>_ACCESS_KEY_ID`, etc. Register a connector per alias under `bedrock:<alias>`. When `BEDROCK_ACCOUNTS` is unset, fall back to the existing `BEDROCK_ENABLED` path unchanged.

**Pros:**
- Self-documenting: `bedrock:prod` is meaningful in usage records and the catalog UI.
- No interface changes.
- The existing `BEDROCK_ENABLED` path is untouched — zero migration risk.
- The `ConnectorFactory` contract (`Build(logger) → (ProviderConnector, error)`) doesn't map to "build N connectors", but a `MultiFactory` type that returns a `[]ProviderConnector` is a small, contained addition.

**Cons:**
- Adds a second factory abstraction (`MultiFactory` alongside `ConnectorFactory`). `main.go` must handle both cases.
- Alias validation (reject spaces, slashes, colons) must be added to avoid malformed provider names.

### Option 2: Indexed env vars (`BEDROCK_1_REGION`, `BEDROCK_2_REGION`, …)

Use integer indices rather than named aliases.

**Pros:**
- Simple to parse (iterate until index not found).

**Cons:**
- Produces opaque provider names (`bedrock:1`, `bedrock:2`) that carry no semantic meaning in the catalog, usage records, or OTel traces.
- Operators cannot tell which index corresponds to which environment without consulting external documentation.
- Index reuse (removing index 2 out of 3) creates confusing gaps.

### Option 3: Single connector with per-request credential switching via model metadata

Extend the model ID format or add a request header that selects the AWS account at call time. The single `bedrock` connector dispatches to the right `aws.Config` based on this signal.

**Pros:**
- Single registered provider name — simpler catalog representation.

**Cons:**
- Requires invasive changes to `ProviderConnector`, `ChatCompletion`, `AnthropicMessages`, and all other interface methods to thread the account hint through.
- The routing engine's model-dispatch logic would need to decode the account from the model ID, conflating two concerns.
- Makes the connector stateful in a way that is harder to test and reason about.

### Option 4: Separate connector packages per account (e.g. `bedrock-prod`, `bedrock-staging`)

Treat each account as a fully distinct provider package with its own `ProviderName` constant.

**Pros:**
- No shared code — maximum isolation.

**Cons:**
- Requires N copies of the connector package for N accounts; code duplication at scale.
- Operators cannot add a new account without recompiling the binary.
- The number of accounts is a runtime configuration concern, not a compile-time one.

---

## Decision

**Chosen Option: Option 1 — Named accounts via `BEDROCK_ACCOUNTS` + per-account prefixed env vars.**

### Rationale

Option 1 achieves all goals with the smallest change surface:

- Only the `bedrock` package and `main.go` are modified — no interface changes, no schema changes, no handler changes.
- The `ProviderName` type already supports arbitrary strings. The routing engine, catalog, usage records, and all handlers treat provider names opaquely.
- Named aliases (`prod`, `staging`) produce legible provider names in every system that displays them: the catalog UI, usage records, OTel traces, and policy scope vocabulary.
- The existing `BEDROCK_ENABLED` path is left completely intact. Operators who do not set `BEDROCK_ACCOUNTS` see no change at all.

---

## Consequences

### Positive

- Multi-account Bedrock routing becomes possible using existing virtual model catalog primitives (priority failover, weighted routing, lowest-cost selection across accounts).
- Provider names in the catalog are self-documenting: `bedrock:prod` unambiguously identifies the production account.
- No DB migrations — `ProviderName` is stored as a free-form string in `virtual_model_routes.provider` and `inference_usage.provider`.
- The feature composes naturally with existing cross-account strategies: a virtual model route with `bedrock:prod` (priority 1) and `bedrock:dr` (priority 2) gives automatic regional failover.

### Negative

- `main.go`'s `initControllers` function must distinguish between the single-factory and multi-factory cases, adding a small amount of conditional logic.
- Operators configuring multi-account mode must understand that `BEDROCK_ENABLED` is ignored when `BEDROCK_ACCOUNTS` is set.

### Neutral

- Each Bedrock account runs its own `ListModels` sweep at startup. For large N (≥ 10 accounts) this is slower, but the sweep runs in a background goroutine and is non-blocking for inference traffic.
- Policy scope vocabulary for each `bedrock:<alias>` connector's models is registered independently — no functional difference, but the vocabulary grows in proportion to account count.

---

## Implementation

### Environment Variable Schema

**Single-account mode (unchanged):**
```
BEDROCK_ENABLED=true
BEDROCK_REGION=us-east-1
AWS_ACCESS_KEY_ID=...        # optional — falls back to default chain
AWS_SECRET_ACCESS_KEY=...    # optional
AWS_SESSION_TOKEN=...        # optional
```

**Multi-account mode:**
```
BEDROCK_ACCOUNTS=prod,staging,dr

# Per-account variables (alias uppercased):
BEDROCK_PROD_REGION=us-east-1
BEDROCK_PROD_ACCESS_KEY_ID=...        # optional
BEDROCK_PROD_SECRET_ACCESS_KEY=...    # optional
BEDROCK_PROD_SESSION_TOKEN=...        # optional

BEDROCK_STAGING_REGION=eu-west-1
BEDROCK_STAGING_ACCESS_KEY_ID=...     # optional
BEDROCK_STAGING_SECRET_ACCESS_KEY=... # optional
BEDROCK_STAGING_SESSION_TOKEN=...     # optional
```

When `BEDROCK_ACCOUNTS` is set, `BEDROCK_ENABLED` is not consulted. The registered provider names are `bedrock:prod`, `bedrock:staging`, `bedrock:dr`.

### Alias Validation

Aliases are validated at startup against the pattern `^[a-z0-9_-]+$`. An alias containing `:`, `/`, spaces, or uppercase characters returns a startup error. This ensures provider names of the form `bedrock:<alias>` are safe to store, display, and use as OPA scope segments.

### Code Changes

1. **`internal/inferencegw/providers/bedrock/connector.go`**
   - Add `NamedConfig` struct: `{Name string; Config Config}`.
   - Add `LoadMultiConfig() ([]NamedConfig, error)`: reads `BEDROCK_ACCOUNTS`, iterates, reads per-account env vars.
   - Add `MultiFactory` struct that implements a new `MultiConnectorFactory` interface (returns `[]inferencegw.NamedConnector`).
   - Preserve `Factory` (single-account) and `LoadConfig` unchanged.

2. **`internal/inferencegw/connector.go`**
   - Add `NamedConnector` struct: `{Name ProviderName; Connector ProviderConnector}`.
   - Add `MultiConnectorFactory` interface: `Build(logger) ([]NamedConnector, error)`.

3. **`internal/inferencegw/models.go`**
   - No changes.

4. **`cmd/inference-gateway/main.go`**
   - In `initControllers`: attempt `bedrock.MultiFactory{}.Build(logger)` first. If it returns connectors, register them. Otherwise fall back to the existing single-factory loop unchanged.

5. **`.env.example`**
   - Document the `BEDROCK_ACCOUNTS` variable and per-account naming convention.

### Migration Path

None required. Existing deployments with `BEDROCK_ENABLED=true` are not affected. Operators adopting multi-account mode set `BEDROCK_ACCOUNTS` and remove `BEDROCK_ENABLED`.

---

## Validation

### Success Metrics

- A deployment with `BEDROCK_ACCOUNTS=prod,staging` starts successfully and logs two `provider enabled` lines.
- `GET /v1/catalog/provider-models` returns models from each account under `bedrock:prod` and `bedrock:staging`.
- A virtual model route with `provider: bedrock:prod` dispatches to the `prod` connector (verifiable via OTel trace provider attribute).
- Existing single-account deployment with only `BEDROCK_ENABLED=true` is unaffected.

---

## Related Decisions

- [inference-gateway-architecture](2026-04-01-inference-gateway-architecture.md) — Connector abstraction and factory pattern this ADR extends.
- [provider-model-catalog-and-alias-routing](2026-04-01-provider-model-catalog-and-alias-routing.md) — ProviderName flows through catalog; this ADR adds namespaced variants.

---

## References

- [Multi-Account Bedrock PRD](../prd/multi-account-bedrock.md)
- [AWS Bedrock multi-region inference profiles](https://docs.aws.amazon.com/bedrock/latest/userguide/inference-profiles.html)

---

**Last Updated:** 2026-06-19
