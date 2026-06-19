# Multi-Account AWS Bedrock Support - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-19  
**Last Updated:** 2026-06-19  
**Owner:** Platform Team  
**Stakeholders:** Platform Team, Tenant Administrators, Infrastructure Team

---

## Executive Summary

Orbit's inference gateway currently supports a single AWS Bedrock environment, configured via a fixed set of environment variables (`BEDROCK_REGION`, `AWS_ACCESS_KEY_ID`, etc.). This prevents organisations that operate multiple AWS accounts — for environment isolation, cost centre separation, regional compliance, or cross-account model access — from routing inference traffic across more than one Bedrock account. This PRD defines the requirements for supporting multiple, simultaneously-active Bedrock environments that are each registered as distinct provider entries in the Orbit model catalog.

---

## Problem Statement

### Current State

The Bedrock connector is constructed from a single Config struct populated by a fixed set of environment variables. The result is a single `ProviderConnector` registered under the `bedrock` provider name. Every virtual model route that uses Bedrock must share the same AWS account and region.

### User Pain Points

- **Multi-account AWS organisations** cannot use Orbit across more than one AWS account. A platform team managing a `prod` account (us-east-1) and a `dev` account (eu-west-1) must pick one or run two separate Orbit clusters.
- **Regional compliance** requirements may mandate that certain tenants' traffic stays within a specific AWS region. A single Bedrock connector cannot enforce this — the region is fixed at deploy time.
- **Cost centre / chargeback isolation** is not possible when all traffic flows through one account. Different business units that pay separate AWS invoices need separate Bedrock accounts reflected in the catalog.
- **Model availability differences** between accounts (e.g. different inference profile quota approvals, Claude model access) cannot be expressed in the catalog today.

### Business Impact

- Blocks expansion into enterprise customers with strict AWS account-per-environment policies.
- Forces costly architectural workarounds (separate Orbit clusters per account, manual catalog management).
- Prevents the platform team from expressing realistic multi-account routing strategies (lowest-cost across accounts, failover between regions) through the existing virtual model catalog.

---

## Goals and Objectives

### Primary Goals

1. Allow operators to configure one or more named Bedrock environments, each backed by independent AWS credentials and/or a different region.
2. Each named Bedrock environment is registered as a distinct provider in the inference gateway's model catalog (e.g. `bedrock:prod`, `bedrock:staging`).
3. Platform teams configure virtual model routes that target specific Bedrock environments, enabling cross-account routing strategies (failover, weighted, lowest-cost) using existing catalog primitives.
4. The existing single-account configuration (`BEDROCK_ENABLED=true`) continues to work unchanged — this feature is fully backwards compatible.

### Success Metrics

- Zero changes required to existing single-account deployments.
- A deployment with N Bedrock accounts exposes N separate provider entries in `GET /v1/catalog/provider-models`.
- Virtual model routes reference a specific Bedrock environment by name; the routing engine dispatches to the correct connector.
- All existing connector tests pass unchanged.

### Non-Goals

- Cross-account model aliasing (mapping one model ID to an equivalent in another account) — handled by the existing virtual model catalog routing.
- Dynamic credential rotation or refreshing at runtime — credentials are injected at pod startup via existing mechanisms (IRSA, static keys, STS tokens).
- Per-tenant Bedrock account assignment — tenants share whichever provider entries are enabled in the catalog.
- A UI for managing Bedrock account configuration — configuration is environment-variable-driven.

---

## User Stories

### Primary User Stories

**As a** platform engineer deploying Orbit in a multi-account AWS organisation,  
**I want to** configure multiple Bedrock environments with independent credentials and regions,  
**So that** I can expose models from each account in the Orbit catalog and route traffic appropriately.

**Acceptance Criteria:**
- [ ] Setting `BEDROCK_ACCOUNTS=prod,staging` with per-account env vars causes two distinct providers to be registered in the catalog.
- [ ] `GET /v1/catalog/provider-models` returns models from each account under their respective provider names (`bedrock:prod`, `bedrock:staging`).
- [ ] Virtual model routes can reference `bedrock:prod` or `bedrock:staging` as their provider field.

---

**As a** platform engineer with an existing single-account Bedrock deployment,  
**I want to** upgrade Orbit without any configuration changes,  
**So that** the new multi-account feature does not break my existing setup.

**Acceptance Criteria:**
- [ ] `BEDROCK_ENABLED=true` with existing env vars continues to register a single `bedrock` provider (no `:` suffix), exactly as before.
- [ ] Existing virtual model routes with `provider: bedrock` continue to route correctly.
- [ ] No configuration migration is required.

---

**As a** platform engineer,  
**I want to** define a virtual model that fails over from a primary Bedrock account to a secondary one,  
**So that** inference traffic is resilient to regional AWS outages or quota exhaustion.

**Acceptance Criteria:**
- [ ] A virtual model can define routes with `provider: bedrock:prod` (priority 1) and `provider: bedrock:dr` (priority 2).
- [ ] When the primary account returns a retryable error, the routing engine advances to the secondary account.

---

## Proposed Solution

### High-Level Approach

Introduce a `BEDROCK_ACCOUNTS` environment variable that accepts a comma-separated list of named account aliases (e.g. `prod,staging,dr`). For each alias, a per-account Config is loaded from prefixed environment variables (`BEDROCK_PROD_REGION`, `BEDROCK_STAGING_REGION`, etc.). Each account produces an independent `Connector` registered under a namespaced `ProviderName` of the form `bedrock:<alias>`.

The `BEDROCK_ENABLED=true` path remains the default single-account path, registering a connector under the bare `bedrock` name. The two paths are mutually exclusive: if `BEDROCK_ACCOUNTS` is set, the old `BEDROCK_ENABLED` path is not consulted.

Because `ProviderName` is already a `string` type in the routing engine, no changes are required to the router, catalog, usage records, or handlers — the namespaced names flow through the existing infrastructure identically to any other provider.

### Key Components

- **`bedrock.LoadMultiConfig()`** — reads `BEDROCK_ACCOUNTS`, iterates aliases, and returns `[]NamedConfig{Name, Config}`.
- **`bedrock.MultiFactory`** — a `ConnectorFactory` that produces multiple connectors (one per account). Replaces the single `Factory` in `main.go` when multi-account mode is active.
- **Updated `.env.example`** — documents the new per-account variable naming convention.
- **ADR** — records the design decision and its trade-offs.

---

## Scope

### In Scope

- `BEDROCK_ACCOUNTS` environment variable and per-account variable convention.
- `MultiFactory` that registers N connectors with provider names `bedrock:<alias>`.
- Backwards compatibility: existing `BEDROCK_ENABLED` path untouched.
- Updated `.env.example` documentation.
- PRD and ADR.

### Out of Scope

- UI changes — the existing catalog management UI already displays provider names generically.
- Changes to the virtual model catalog schema — `VirtualModelRoute.Provider` is already a free-form `ProviderName` string.
- Helm chart changes — documented as follow-on work.
- Per-account pricing overrides beyond what the existing pricing catalog supports.

### Future Considerations

- Helm chart values for multi-account Bedrock.
- Account health checks in `GET /v1/catalog/provider-models`.
- Dynamic credential injection via IRSA without restart.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `BEDROCK_ACCOUNTS=a,b` causes providers `bedrock:a` and `bedrock:b` to be registered.
- [ ] Each account reads credentials from `BEDROCK_<UPPER_ALIAS>_REGION`, `BEDROCK_<UPPER_ALIAS>_ACCESS_KEY_ID`, `BEDROCK_<UPPER_ALIAS>_SECRET_ACCESS_KEY`, `BEDROCK_<UPPER_ALIAS>_SESSION_TOKEN`.
- [ ] Missing `BEDROCK_<ALIAS>_REGION` returns a startup error naming the offending alias.
- [ ] Existing `BEDROCK_ENABLED=true` single-account path unchanged.
- [ ] `GET /v1/catalog/provider-models` lists models under correct namespaced provider names.
- [ ] Virtual model routes with `provider: bedrock:prod` dispatch to the `prod` connector.

**Should Have (P1):**
- [ ] `GET /v1/models` lists models from all enabled Bedrock accounts (merged into a single list by the existing catalog).
- [ ] Startup log lines indicate which Bedrock accounts were successfully registered.

**Nice to Have (P2):**
- [ ] Per-account `BEDROCK_<ALIAS>_ENABLED=false` opt-out without removing from `BEDROCK_ACCOUNTS`.

### Non-Functional Requirements

**Security:**
- Each account's credentials are isolated — no credential sharing or cross-account leakage is possible by design (each `Connector` holds its own `aws.Config`).

**Backwards Compatibility:**
- The `BEDROCK_ENABLED` path is preserved exactly. No environment variable renames or removals.

**Observability:**
- Startup log already emits `provider enabled` lines per provider. No additional instrumentation needed — the namespaced provider name flows through OTel traces automatically.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Operator misconfigures alias names (e.g. spaces, slashes) | Malformed provider names in DB/logs | Low | Validate alias is `[a-z0-9_-]+` at startup; return error on invalid characters |
| Existing virtual model routes break after upgrade | Routing failures for existing traffic | Low | `BEDROCK_ENABLED` path unchanged; existing `bedrock` provider name untouched |
| N×ListModels calls at startup slow warm-up | Slower startup for large N | Low | Already parallelised via background goroutine in `WarmupProviderModels`; N is expected to be ≤ 5 |

---

## Alternatives Considered

### Alternative 1: Single connector with per-request account selection via metadata

Have one `bedrock` connector that accepts an account name in the model ID or request metadata and selects credentials at call time.

**Why not chosen:** Requires invasive changes to the `ProviderConnector` interface and service layer. Cannot reuse the existing model catalog or routing engine without significant rework. Couples account selection to request routing in a non-obvious way.

### Alternative 2: Multiple Orbit inference-gateway deployments (one per account)

Deploy a separate gateway instance per AWS account and configure the agent to target the right one.

**Why not chosen:** Operational overhead multiplies with account count. No cross-account virtual model routing. Defeats the purpose of a unified control plane.

### Alternative 3: Indexed env vars (`BEDROCK_1_REGION`, `BEDROCK_2_REGION`, …)

Use numeric indices instead of named aliases.

**Why not chosen:** Produces opaque provider names (`bedrock:1`, `bedrock:2`) that carry no semantic meaning in the catalog UI and usage records. Named aliases like `prod`, `staging` are self-documenting.

---

## Timeline and Milestones

### Phases

**Phase 1: Core implementation** (Target: 2026-06-19)
- PRD, ADR, implementation, PR

**Phase 2: Helm chart support** (Target: TBD)
- Add `bedrock.accounts` section to inference-gateway Helm chart values

---

## Appendix

### Related Documents

- [ADR: Multi-Account Bedrock](../adr/2026-06-19-multi-account-bedrock.md)
- [ADR: Inference Gateway Architecture](../adr/2026-04-01-inference-gateway-architecture.md)
- [ADR: Provider Model Catalog and Alias Routing](../adr/2026-04-01-provider-model-catalog-and-alias-routing.md)

### Glossary

- **Account alias**: A short, operator-chosen name for a Bedrock environment (e.g. `prod`, `staging`). Used as the suffix in the provider name `bedrock:<alias>`.
- **Provider name**: The `ProviderName` string that identifies a connector within the routing engine and appears in catalog, usage records, and virtual model route definitions.
- **Single-account mode**: The existing `BEDROCK_ENABLED=true` code path that registers one connector under the bare `bedrock` name.
- **Multi-account mode**: The new `BEDROCK_ACCOUNTS=…` code path that registers N connectors under `bedrock:<alias>` names.
