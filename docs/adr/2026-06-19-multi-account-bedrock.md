# Multi-Account AWS Bedrock Support

**Status:** Superseded by [crd-inference-providers](2026-10-01-crd-inference-providers.md)  
**Date:** 2026-06-19  
**Deciders:** kora Platform Team  
**Technical Story:** [Multi-Account Bedrock PRD](../prd/multi-account-bedrock.md) — defines the requirement for supporting multiple simultaneous AWS Bedrock environments in the inference gateway.

---

## Context

### Background

The inference gateway's Bedrock connector (`internal/inferencegw/providers/bedrock`) was previously constructed from a single `Config` struct populated by `BEDROCK_ENABLED`, `BEDROCK_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, and `AWS_SESSION_TOKEN`. This produced a single `ProviderConnector` registered under the `bedrock` `ProviderName`.

The `ProviderName` type is already a plain `string` alias. The routing engine, catalog, and usage records treat it as an opaque identifier — there is no structural reason it must be the literal string `"bedrock"`. The virtual model catalog's `VirtualModelRoute.Provider` field is already a free-form `ProviderName`, so new provider names flow through all existing infrastructure (routing, usage recording, catalog UI, policy scopes) without any schema changes.

### Problem Statement

Organisations operating multiple AWS accounts cannot use a single kora deployment to route inference traffic across more than one account. The Bedrock connector's single-instance design requires either:

- Choosing one account at deploy time and ignoring others, or
- Running multiple kora deployments (one per account), which defeats the purpose of a unified control plane.

### Goals

- Support N simultaneously active Bedrock environments, each with independent AWS credentials and regions.
- Register each environment as a distinct `ProviderConnector` with a namespaced name (`bedrock:<alias>`).
- Minimise blast radius — only the Bedrock package and `main.go` change.

### Non-Goals

- Per-tenant Bedrock account assignment.
- Dynamic credential rotation at runtime.
- Helm chart changes (follow-on).
- Cross-account model aliasing.

---

## Decision Drivers

* **Clean interface** — `BEDROCK_ACCOUNTS` is the single entry point; no fallback modes.
* **Minimal interface impact** — the `ProviderConnector` and `ConnectorFactory` interfaces should not change.
* **Self-documenting config** — account names (not indices) appear in provider names, logs, and the catalog UI.
* **Leverage existing routing primitives** — virtual model routes, failover chains, and weighted routing already handle multiple providers; we need only to register more providers.

---

## Considered Options

### Option 1: Named accounts via `BEDROCK_ACCOUNTS` + per-account prefixed env vars

Introduce `BEDROCK_ACCOUNTS=prod,staging` (comma-separated list of aliases). For each alias, read `BEDROCK_<UPPER_ALIAS>_REGION`, `BEDROCK_<UPPER_ALIAS>_ACCESS_KEY_ID`, etc. Register a connector per alias under `bedrock:<alias>`. When `BEDROCK_ACCOUNTS` is unset, Bedrock is disabled entirely.

**Pros:**
- Self-documenting: `bedrock:prod` is meaningful in usage records and the catalog UI.
- No interface changes.
- Single code path — `main.go` has no mode-switching logic.
- The `ConnectorFactory` contract (`Build(logger) → (ProviderConnector, error)`) doesn't map to "build N connectors", but a `MultiFactory` type that returns a `[]ProviderConnector` is a small, contained addition.

**Cons:**
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

- Only the `bedrock` package, `main.go`, and four handler/pricing sites are modified — no interface changes, no schema changes.
- While `ProviderName` is a plain string type, several handlers explicitly whitelist the three base names (`bedrock`, `vertexai`, `azure`) rather than treating them opaquely. A shared `IsBedrockFamily()` method on `ProviderName` handles all four sites (`isValidProvider`, `lookupPricing`, `normalisePricingModelID`, and the usage-filter validation) with minimal, targeted changes.
- Named aliases (`prod`, `staging`) produce legible provider names in every system that displays them: the catalog UI, usage records, OTel traces, and policy scope vocabulary.
- Single entry point: `BEDROCK_ACCOUNTS` is the only configuration knob. No fallback modes, no dead code paths.

---

## Consequences

### Positive

- Multi-account Bedrock routing becomes possible using existing virtual model catalog primitives (priority failover, weighted routing, lowest-cost selection across accounts).
- Provider names in the catalog are self-documenting: `bedrock:prod` unambiguously identifies the production account.
- No DB schema changes to `virtual_model_routes` or `inference_usage` column types — `provider` was already stored as a free-form string. A migration does add `CHECK` constraint extensions on `virtual_model_routes`, `inference_usage`, and `provider_model_overrides` to explicitly permit `bedrock:<alias>` values (the old constraints allowed only the three canonical names).
- The feature composes naturally with existing cross-account strategies: a virtual model route with `bedrock:prod` (priority 1) and `bedrock:dr` (priority 2) gives automatic regional failover.

### Negative

- `main.go`'s `initControllers` function iterates the `[]NamedConnector` slice from `MultiFactory`, adding a small registration loop.
- Operators migrating from `BEDROCK_ENABLED=true` must set `BEDROCK_ACCOUNTS=<alias>` and update any existing virtual model routes from `provider: bedrock` to `provider: bedrock:<alias>`. The bare `bedrock` connector is not registered in the new model — routing will fail for unupdated routes.

### Neutral

- Each Bedrock account runs its own `ListModels` sweep at startup. For large N (≥ 10 accounts) this is slower, but the sweep runs in a background goroutine and is non-blocking for inference traffic.
- Policy scope vocabulary for each `bedrock:<alias>` connector's models is registered independently — no functional difference, but the vocabulary grows in proportion to account count.

---

## Implementation

### Environment Variable Schema

```
BEDROCK_ACCOUNTS=prod,staging,dr

# Per-account variables (alias uppercased):
BEDROCK_PROD_REGION=us-east-1
BEDROCK_PROD_ACCESS_KEY_ID=...        # optional — falls back to default chain
BEDROCK_PROD_SECRET_ACCESS_KEY=...    # optional
BEDROCK_PROD_SESSION_TOKEN=...        # optional

BEDROCK_STAGING_REGION=eu-west-1
BEDROCK_STAGING_ACCESS_KEY_ID=...     # optional
BEDROCK_STAGING_SECRET_ACCESS_KEY=... # optional
BEDROCK_STAGING_SESSION_TOKEN=...     # optional
```

When `BEDROCK_ACCOUNTS` is unset, Bedrock is disabled and no connector is registered. The registered provider names are `bedrock:prod`, `bedrock:staging`, `bedrock:dr`.

### Alias Validation

Aliases are validated at startup against the pattern `^[a-z][a-z0-9_]*$` — must start with a lowercase letter and contain only `[a-z0-9_]`. Hyphens are excluded because the env var prefix `BEDROCK_<UPPER_ALIAS>_` must be a valid POSIX identifier. An alias containing `:`, `/`, `-`, spaces, uppercase characters, or a leading digit returns a startup error. This ensures provider names of the form `bedrock:<alias>` are safe to store, display, and use as OPA scope segments.

### Code Changes

1. **`internal/inferencegw/providers/bedrock/connector.go`**
   - Remove `LoadConfig`, `Factory` (single-account entry points).
   - Add `NamedConfig` struct: `{Alias string; Config Config}`.
   - `LoadMultiConfig()` reads `BEDROCK_ACCOUNTS`, validates each alias, and returns `[]NamedConfig`. Returns `nil, nil` when `BEDROCK_ACCOUNTS` is unset (Bedrock disabled). Returns an error if the env var is non-empty but yields zero valid aliases.
   - `MultiFactory.Build` iterates the slice and registers each connector under `"bedrock:" + alias`.

2. **`internal/inferencegw/connector.go`**
   - Add `NamedConnector` struct: `{Name ProviderName; Connector ProviderConnector}`.
   - Add `MultiConnectorFactory` interface: `Build(logger) ([]NamedConnector, error)`.

3. **`internal/inferencegw/models.go`**
   - Add `IsBedrockFamily() bool` method on `ProviderName`: returns true for `"bedrock"` and any `"bedrock:<alias>"` value.

4. **`internal/inferencegw/handlers_catalog.go`**
   - `isValidProvider`: add `p.IsBedrockFamily()` branch so any `bedrock:<alias>` name passes validation.
   - `lookupPricing`: replace `provider == ProviderBedrock` guard with `provider.IsBedrockFamily()`.

5. **`internal/inferencegw/pricing_source.go`**
   - `normalisePricingModelID`: replace `provider != ProviderBedrock` guard with `!provider.IsBedrockFamily()`.

6. **`internal/inferencegw/handlers_usage.go`**
   - Remove the static `providerNames` allowlist. Replace the `parseEnumParam` call for `?provider=` with an inline `isValidProvider` check, which accepts `bedrock:<alias>` values.

7. **`cmd/inference-gateway/main.go`**
   - Replace the multi-account if/else block with a single `bedrock.MultiFactory{}.Build(logger)` call. Register all returned connectors; `main.go` has no knowledge of single vs. multi-account mode.

### Migration Path

Operators migrating from `BEDROCK_ENABLED=true` must:

1. Set `BEDROCK_ACCOUNTS=<alias>` and set per-account env vars (`BEDROCK_<ALIAS>_REGION`, etc.).
2. Remove `BEDROCK_ENABLED`, `BEDROCK_REGION`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`.
3. Update all virtual model routes from `provider: bedrock` to `provider: bedrock:<alias>`. The bare `bedrock` connector is never registered — routing will fail for unupdated routes until they reference a valid `bedrock:<alias>` name.

---

## Validation

### Success Metrics

- A deployment with `BEDROCK_ACCOUNTS=prod,staging` starts successfully and logs two `provider enabled` lines.
- `GET /v1/catalog/provider-models` returns models from each account under `bedrock:prod` and `bedrock:staging`.
- A virtual model route with `provider: bedrock:prod` dispatches to the `prod` connector (verifiable via OTel trace provider attribute).
- A deployment with `BEDROCK_ACCOUNTS` unset logs no Bedrock lines and returns no Bedrock models from the catalog.

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
