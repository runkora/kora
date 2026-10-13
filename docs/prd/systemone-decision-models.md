# Decision Models and the System One API - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-10  
**Last Updated:** 2026-10-10  
**Owner:** kora Core Team  
**Stakeholders:** kora Core Team, Agent Developers, Platform Operators, Finance / Budget Owners

---

## Executive Summary

kora will serve decision models through TypeSafe's System One API, the de facto format for hosted and self-hosted decision servers. Decision calls will be attributed, budgeted and policy-checked like every other inference call. The feature is the first step towards a classifier primitive with its own PRD.

---

## Problem Statement

### Current State

kora serves chat, messages, embeddings and Responses calls. A decision model has no place in its provider configuration, its model capabilities or its endpoints. Decision models are cheap and fast, and they return calibrated probabilities that code can threshold. They suit routing, triage, gating and scoring work that needs no generated text.

### User Pain Points

*   Agent developers who want a yes or no, a choice or a score call the decision provider directly with a raw API key, outside kora.
*   Platform operators cannot see those calls in the inference logs, attribute their cost to a tenant or budget, or limit which principals make them.
*   A team that self-hosts a decision model has no way to register it with kora.

### Business Impact

*   Spend on decision models is invisible to budgets. A tenant budget does not bound total inference spend.
*   Distributing provider API keys to workloads widens credential exposure.
*   The classifier primitive on the roadmap has nothing to build on.

---

## Goals and Objectives

### Primary Goals

1.  An operator can register a System One endpoint as a provider, and the provider becomes Ready.
2.  A client using a TypeSafe SDK works against kora by changing only the base URL.
3.  Decision calls are logged, priced from the catalog, attributed to the tenant, and stopped by an exhausted budget.
4.  Decision models are a first-class capability in the virtual model catalog.

### Success Metrics

*   100% of decision calls through kora appear in the inference logs.
*   No provider credential reaches the caller.

### Non-Goals

*   A classifier primitive: tenant-defined classifiers, thresholds, UI, and attachments to triggers or workflows.
*   Support for OpenAI's or OpenRouter's separate decisions APIs. Their formats differ, and one is unconfirmed.
*   Shipping or hosting a decision model.
*   Egress or tool-policy use of classifiers.
*   Answering decision calls with a chat model, or chat calls with a decision model.

---

## User Stories

### Primary User Stories

**As an** agent developer,  
**I want to** call kora's System One endpoint with the TypeSafe SDK,  
**So that** I get probabilities for typed questions without holding a provider key.

**Acceptance Criteria:**
- [ ] Changing the SDK base URL to kora is the only change.
- [x] `noul`, `choice` and `score` answers return as the upstream sent them.
- [x] A request to a model that has no `systemone` route fails with an error that cites `systemone`.

---

**As a** platform operator,  
**I want to** register a hosted or self-hosted decision endpoint as a provider and put its models in a virtual model,  
**So that** callers use a stable model name and I control access, failover and price.

**Acceptance Criteria:**
- [ ] A provider that declares the System One API becomes Ready.
- [x] A virtual model with the `decision` capability routes decision calls through its routes.
- [ ] Callers can filter the model list on `decision`.

---

**As a** budget owner,  
**I want** decision calls to count against the tenant's budget,  
**So that** total inference spend stays bounded.

**Acceptance Criteria:**
- [x] Each call is logged with a cost priced from the catalog.
- [x] An exhausted budget blocks the call.

### Secondary User Stories

*   As an operator running a self-hosted decision server, I connect it with a base URL and no key.
*   As an operator of a model with no published price, I set its price myself.

---

## Proposed Solution

### High-Level Approach

kora passes each decision call through to a provider that declares System One and returns that provider's answer. Identity, scope, policy, budgets, failover and usage logging work as they do for other inference.

### User Experience

#### User Flow

1.  An operator registers a provider that declares the System One API.
2.  kora lists the provider's models, and an admin puts them in a virtual model.
3.  A caller sends a System One request with a state, a model and questions, authenticated as for any other kora call.
4.  kora checks policy and budget, routes to a provider that serves System One, and returns the provider's answer.
5.  The call appears in the inference logs with its input tokens and cost.

### Key Components

*   **Provider API:** System One as an API a provider can declare.
*   **Endpoint:** The System One decision endpoint on kora.
*   **Capability:** `decision`.
*   **Usage and cost:** A logged, priced entry for every call.

---

## Scope

### In Scope

*   The requirements below.
*   Updates to the provider configuration guide and the model capability taxonomy.

### Out of Scope

*   The non-goals above.

### Future Considerations

*   A separate PRD for the classifier primitive.
*   Support for OpenAI's decisions API once its format is confirmed.
*   Per-implementation limits (e.g., Laya caps choices at 100 options) shown in the catalog.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [x] Providers can declare the System One API, and kora validates the declaration.
- [x] The System One endpoint passes each request through and returns the provider's answer unchanged.
- [x] A request with no System One route is refused with an error that cites the API.
- [x] Decision is a model capability in the catalog.
- [x] Each call is logged and priced from the catalog at the model's input and output prices, and an exhausted budget blocks the call.
- [x] The endpoint requires a scope.

**Should Have (P1):**
- [x] Rate-limited and overloaded provider responses are retried and failed over like other inference.
- [x] A self-hosted server with no model list can be registered with an explicit model list.

**Nice to Have (P2):**
- [x] Documented price setup for decision models that kora's pricing source does not price.

### Non-Functional Requirements

**Performance:**
*   kora adds no more latency to a decision call than to an embeddings call.

**Security:**
*   kora injects the provider key on each call and never forwards the caller's token.
*   The endpoint requires the same authentication and scope enforcement as the other inference endpoints.

**Reliability:**
*   Failover across routes follows the existing retry policy.

**Scalability:**
*   The feature adds no stored state.

**Observability:**
*   Every call, including a failed call, is traced and appears in the inference logs.

---

## Dependencies

### Internal Dependencies

*   [Embeddings and Responses API Endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md): the pattern followed.
*   [CRD-Driven Inference Providers](../adr/2026-10-01-crd-inference-providers.md): the `compatible` provider type.
*   [Model Capability Taxonomy](../adr/2026-04-01-model-capability-taxonomy.md): the capability layer.
*   [LLM Cost Tracking](../adr/2026-06-05-llm-cost-tracking.md): pricing and budgets.

### External Dependencies

*   The System One wire format. It has no formal specification or standards body, and implementations differ in their limits.

---

## Risks and Mitigations

*   **Format drift or differing implementations (Impact: Medium, Probability: Medium):** kora forwards questions without checking them. New fields and per-implementation limits need no kora change.
*   **Unpriced decision models undercount budgets (Impact: Medium, Probability: High):** Operators set a price for each decision model, and the inference logs flag a call with an unknown price.
*   **A client expects TypeSafe's exact error body (Impact: Low, Probability: Medium):** kora keeps TypeSafe's status codes, and the docs record the difference in the error body.
*   **Confusion with OpenAI's decisions API (Impact: Medium, Probability: Low):** The API and endpoint carry the System One name.

---

## Alternatives Considered

### Alternative 1: Answer decision calls with chat models

**Description:** Serve System One calls with any chat model by prompting for JSON.

**Why not chosen:**
*   See Option 3 in the [ADR](../adr/2026-10-10-systemone-decision-models.md).

### Alternative 2: Wait for a standard

**Description:** Defer until a standards body or OpenAI's format settles.

**Why not chosen:**
*   The System One format already has several hosted and self-hosted implementations, and the classifier primitive needs a base now.

---

## Open Questions

Each question below is resolved in the ADR [systemone-decision-models](../adr/2026-10-10-systemone-decision-models.md).

1.  **Pricing source:** Does the LiteLLM registry price decision models?
2.  **Scope:** Does the endpoint use `inference:invoke` or a new scope?
3.  **Validation:** Does kora validate the request body?
4.  **429 and 529:** How are they handled?
5.  **Bridging:** May a decision model route over chat?

---

## Timeline and Milestones

### Phases

**Phase 1: Docs** (Target: 2026-10-10)
*   PRD and ADR reviewed and merged.

**Phase 2: Implementation** (Target: follows Phase 1)
*   Provider API, endpoint, capability, usage and cost, scope registration, docs.

### Key Milestones

- [ ] PRD Approved
- [x] ADR Created: 2026-10-10
- [x] Implementation Started
- [ ] Launch

---

## Appendix

### Related Documents

*   [ADR: Decision Models and the System One API](../adr/2026-10-10-systemone-decision-models.md)
*   [Issue #473](https://github.com/clear-route/orbit/issues/473)
*   [Embeddings & Responses API PRD](embeddings-and-responses-api.md)

### References

*   [TypeSafe API reference](https://docs.typesafe.ai/api)

### Glossary

*   **Decision model:** A model that takes a state and typed questions and returns probabilities.
*   **System One:** TypeSafe's name for the model class and for the wire format it serves.
*   **Question types:** `noul` (yes or no), `choice` (up to 255 options) and `score` (2 to 10 ordered levels).

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______
