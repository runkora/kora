# Decision Models and the System One API — Product Requirements Document

**Status:** In Review  
**Created:** 2026-10-10  
**Last Updated:** 2026-10-10  
**Owner:** kora Core Team  
**Stakeholders:** kora Core Team, Agent Developers, Platform Operators, Finance / Budget Owners

---

## Executive Summary

A decision model takes a `state` and typed questions and returns probabilities, not text. TypeSafe's Jev is the first; its System One API (`POST /v1/systemone`) is the de facto wire format, and other hosted and self-hosted servers speak it. This PRD adds decision models to the inference gateway: a `systemone` API type on `compatible` InferenceProviders, a `POST /v1/systemone` route, and a `decision` model capability. Calls are attributed, budgeted and policy-checked like every other inference call. It is the first step towards a classifier primitive, which follows in a separate PRD.

---

## Problem Statement

### Current State

The gateway serves chat completions, messages, embeddings and Responses. A `compatible` InferenceProvider can declare only `chatCompletions`, `embeddings`, `responses` and `messages`. The model capabilities are chat, embedding, image, video, rerank and audio. A decision model has no place in any of them.

Decision models are cheap and fast. A call is priced from the catalog like other inference (for Jev, input tokens only, because its output price is zero) and returns calibrated probabilities that code can threshold, which suits routing, triage, gating and scoring work that does not need generated text. Endpoints known to serve the format today are hosted (TypeSafe, OpenRouter, Liquid d1, Upstage Solar Decide) and self-hosted (`laya-serve`, `kev.serve`).

### User Pain Points

- Agent developers who want a yes/no, a choice or a score call the decision provider directly with a raw API key, outside kora.
- Platform operators cannot see those calls in the inference logs, cannot attribute their cost to a tenant or budget, and cannot limit which principals may make them.
- A team that self-hosts a decision model has no way to register it with the gateway.

### Business Impact

- Spend on decision models is invisible to budgets, so a tenant budget does not bound total inference spend.
- Provider API keys are distributed to workloads, which widens credential exposure.
- The classifier primitive on the roadmap has nothing to build on.

---

## Goals and Objectives

### Primary Goals

1. A `compatible` provider can declare `systemone`, pass CRD validation and become Ready.
2. A client using a TypeSafe SDK works against kora by changing only the base URL.
3. Decision calls are logged, priced from the catalog, attributed to the tenant, and stopped by an exhausted budget.
4. Decision models are a first-class capability in the virtual model catalog.

### Success Metrics

- 100% of decision calls through the gateway write a usage row.
- `noul`, `choice` and `score` answers come back from the upstream unchanged.
- A request to a virtual model with no `systemone` route is refused with an error that names the API.
- No provider credential reaches the caller.

### Non-Goals

- A classifier primitive: tenant-defined classifiers, thresholds, UI, attachments to triggers or workflows.
- Translating OpenAI's `/v1/decisions` or OpenRouter's `/api/alpha/decisions`. Their schemas differ and one is unconfirmed.
- Shipping or hosting a decision model.
- Egress or tool-policy use of classifiers.
- Serving decision calls over a chat or messages model, or the reverse.

---

## User Stories

### Primary User Stories

**As an** agent developer,  
**I want to** call `POST /v1/systemone` on kora with the TypeSafe SDK,  
**So that** I get probabilities for typed questions without holding a provider key.

**Acceptance Criteria:**
- [ ] Changing the SDK base URL to kora is the only change.
- [ ] `noul`, `choice` and `score` answers return as the upstream sent them.
- [ ] A request to a model that has no `systemone` route fails with an error naming `systemone`.

---

**As a** platform operator,  
**I want to** register a hosted or self-hosted decision endpoint as a provider and put its models in a virtual model,  
**So that** callers use a stable model name and I control access, failover and price.

**Acceptance Criteria:**
- [ ] An InferenceProvider with `apis: [systemone]` becomes Ready.
- [ ] A virtual model with the `decision` capability routes `/v1/systemone` calls through its routes.
- [ ] Callers can filter the model list on `decision`.

---

**As a** budget owner,  
**I want** decision calls to count against the tenant's budget,  
**So that** total inference spend stays bounded.

**Acceptance Criteria:**
- [ ] Each call writes a usage row priced from the catalog.
- [ ] An exhausted budget blocks the call.

### Secondary User Stories

- As an operator running a self-hosted decision server, I connect it as a `compatible` provider with a base URL and no key.
- As an operator of a model with no published price, I set its price through the override API.

---

## Proposed Solution

### High-Level Approach

Add the System One wire format as one more API a `compatible` provider can declare, and one more route on the gateway that passes the request through to providers that declare it. Add `decision` to the model capabilities so the catalog can hold and filter decision models. Reuse the gateway's existing identity, scope, policy, budget, retry and usage machinery; none of it changes.

### User Experience

#### User Flow

1. An operator applies an InferenceProvider with `type: compatible` and `apis: [systemone]`.
2. The gateway lists its models. An admin puts them in a virtual model.
3. A caller sends `POST /v1/systemone` with `state`, `model` and `questions`, authenticated as for any other gateway call.
4. The gateway checks policy and budget, routes to a provider that declares `systemone`, and returns the upstream response.
5. The call appears in the inference logs with its input tokens and cost.

### Key Components

- **API type:** `systemone` on the `compatible` InferenceProvider (Go types and CRD enum).
- **Route:** `POST /v1/systemone`, registered in the route table.
- **Capability:** `decision`.
- **Usage and cost:** a usage row per call, priced from the catalog.
- **Docs:** provider configuration guide and capability taxonomy.

---

## Scope

### In Scope

- The `systemone` API type, CRD enum and validation.
- The `POST /v1/systemone` route, pass-through, with a refusal that names the API when no route declares it.
- The `decision` capability and virtual model routing by it.
- Usage rows, catalog pricing and budget enforcement.
- Scope registration for the route.
- Updates to the provider configuration guide and the capability taxonomy.

### Out of Scope

- The classifier primitive and everything built on it.
- OpenAI `/v1/decisions` translation.
- Shipping or hosting a decision model.
- Egress or tool-policy use of classifiers.

### Future Considerations

- The classifier primitive, in a separate PRD.
- Translating OpenAI `/v1/decisions` once its schema is confirmed.
- Per-implementation limits (Laya caps choices at 100 options) surfaced in the catalog.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] `systemone` is accepted by the Go types and the CRD `apis` enum.
- [ ] `POST /v1/systemone` passes the request through and returns the upstream response unchanged.
- [ ] A request with no `systemone` route is refused with an error naming the API.
- [ ] `CapabilityDecision` exists and a `decision` virtual model routes through its routes.
- [ ] Each call writes a usage row priced from the catalog (input at the input price, output at the output price); an exhausted budget blocks the call.
- [ ] The route is registered in the route table with a required scope.

**Should Have (P1):**
- [ ] Upstream 429 and 529 responses are retried and failed over like other inference.
- [ ] A self-hosted server with no model list can be registered with `spec.models`.

**Nice to Have (P2):**
- [ ] Documented price setup for models LiteLLM does not price for `compatible` providers.

### Non-Functional Requirements

**Performance:**
- The gateway adds no more latency to a decision call than to an embeddings call.

**Security:**
- The caller's token is never forwarded; the provider key is injected by the gateway.
- The route requires the same authentication and scope enforcement as the other inference routes.

**Reliability:**
- Failover across routes follows the existing retry policy.

**Scalability:**
- No new state. The route is stateless.

**Observability:**
- Each call emits a span and a usage row, including failed calls.

---

## Dependencies

### Internal Dependencies

- [Embeddings and Responses API Endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md): the pattern this follows.
- [CRD-Driven Inference Providers](../adr/2026-10-01-crd-inference-providers.md): the `compatible` provider type.
- [Model Capability Taxonomy](../adr/2026-04-01-model-capability-taxonomy.md): the capability layer.
- [LLM Cost Tracking](../adr/2026-06-05-llm-cost-tracking.md): pricing and budgets.

### External Dependencies

- The System One wire format ([TypeSafe API reference](https://docs.typesafe.ai/api)). It has no formal specification or standards body, and implementations differ in their limits.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| The wire format drifts, or implementations differ | Medium | Medium | Pass the body through without interpreting questions, so new fields and per-implementation limits need no gateway change |
| Decision models are unpriced, so budgets undercount | Medium | High | Price through the override API; the usage row flags an unknown price |
| A client expects TypeSafe's exact error body | Low | Medium | Keep the status codes; document the error body difference |
| Name collision with OpenAI `/v1/decisions` | Medium | Low | Name the API and route after the wire format (`systemone`), not "decisions" |

---

## Alternatives Considered

### Alternative 1: Translate decision calls onto chat completions

**Description:** Serve `/v1/systemone` over any chat model by prompting for JSON.

**Why not chosen:**
- It returns generated text parsed into numbers, not calibrated probabilities, so the contract callers rely on is false.
- It bills output tokens that the model does not charge for.

### Alternative 2: Wait for a standard

**Description:** Defer until a standards body or OpenAI's schema settles.

**Why not chosen:**
- The System One format already has several hosted and self-hosted implementations, and the classifier primitive needs a base now.

---

## Open Questions

Resolved in the ADR [systemone-decision-models](../adr/2026-10-10-systemone-decision-models.md):

1. **Pricing source:** does the LiteLLM registry price decision models?
   - **Status:** Resolved in the ADR
   - **Owner:** kora Core Team
2. **Scope:** `inference:invoke` or a new scope?
   - **Status:** Resolved in the ADR
   - **Owner:** kora Core Team
3. **Validation:** does the gateway validate the body?
   - **Status:** Resolved in the ADR
   - **Owner:** kora Core Team
4. **429 and 529:** how are they handled?
   - **Status:** Resolved in the ADR
   - **Owner:** kora Core Team
5. **Bridging:** may a decision model route over chat?
   - **Status:** Resolved in the ADR
   - **Owner:** kora Core Team

---

## Timeline and Milestones

### Phases

**Phase 1: Docs** (Target: 2026-10-10)
- PRD and ADR reviewed and merged.

**Phase 2: Implementation** (Target: follows Phase 1)
- API type, route, capability, usage and cost, scope registration, docs.

### Key Milestones

- [ ] PRD Approved
- [x] ADR Created: 2026-10-10
- [ ] Implementation Started
- [ ] Launch

---

## Appendix

### Related Documents

- [ADR: Decision Models and the System One API](../adr/2026-10-10-systemone-decision-models.md)
- [Issue #473](https://github.com/clear-route/orbit/issues/473)
- [Embeddings & Responses API PRD](embeddings-and-responses-api.md)

### References

- [TypeSafe API reference](https://docs.typesafe.ai/api)

### Glossary

- **Decision model:** a model that takes a `state` and typed questions and returns probabilities, not text.
- **System One:** TypeSafe's name for the model class and the wire format served at `/v1/systemone`.
- **Question types:** `noul` (yes or no), `choice` (up to 255 options) and `score` (2 to 10 ordered levels).

---

**Approval Signatures:**

- [ ] Product Owner: _________________ Date: _______
- [ ] Technical Lead: _________________ Date: _______
- [ ] Security Review: _________________ Date: _______

