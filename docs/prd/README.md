# Product Requirements Documents

PRDs capture the problem, user needs, and solution approach for a feature **before** the technical design (which lives in one or more [ADRs](../adr/README.md)). See the [contributing workflow](../../CONTRIBUTING.md) for how PRDs and ADRs fit together. New PRDs start from [`template.md`](template.md).

| PRD | Status | Primary ADR(s) |
|-----|--------|----------------|
| [Agent Control Plane](agent-control-plane.md) | Implemented | [orbit-agents-service-architecture](../adr/2026-05-11-orbit-agents-service-architecture.md), [run-and-session-lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md), [agent-registry-and-definition-model](../adr/2026-05-11-agent-registry-and-definition-model.md) |
| [Inference Gateway](inference-gateway.md) | Implemented | [inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md), [provider-model-catalog-and-alias-routing](../adr/2026-04-01-provider-model-catalog-and-alias-routing.md), [anthropic-zero-translation-path](../adr/2026-04-01-anthropic-zero-translation-path.md) |
| [Policy Gateway](policy-gateway.md) | Implemented | [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md), [dynamic-route-scope-registration](../adr/2026-04-14-dynamic-route-scope-registration.md) |
| [Personal Access Tokens](personal-access-tokens.md) | Implemented | [personal-access-tokens](../adr/2026-04-02-personal-access-tokens.md), [pat-scope-assignment-via-explicit-grants](../adr/2026-04-19-pat-scope-assignment-via-explicit-grants.md) |
| [Browser Authentication Flow](browser-auth-flow.md) | Implemented | [browser-auth-flow-and-bff-session-architecture](../adr/2026-04-01-browser-auth-flow-and-bff-session-architecture.md), [home-realm-discovery-and-email-based-tenant-routing](../adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) |
| [Embeddings & Responses API](embeddings-and-responses-api.md) | Implemented | [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) |
| [Inference Logs](inference-logs.md) | Implemented | [inference-usage-logs](../adr/2026-04-17-inference-usage-logs.md) |
| [Inference Log Details](inference-log-details.md) | Implemented | [inference-log-details](../adr/2026-06-05-inference-log-details.md) |
| [Inference Analytics](inference-analytics.md) | Implemented | [inference-analytics](../adr/2026-04-27-inference-analytics.md) |
| [Terra Design System](design-system.md) | Implemented | [terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md) |
| [Terra Design System Hardening](terra-design-system-hardening.md) | Implemented | [terra-design-system-hardening](../adr/2026-05-04-terra-design-system-hardening.md) |
| [Frontend Component Consolidation](frontend-component-consolidation.md) | Implemented | [frontend-component-consolidation](../adr/2026-05-04-frontend-component-consolidation.md) |
| [Semantic HTML Consolidation](semantic-html-consolidation.md) | Implemented | [semantic-html-consolidation](../adr/2026-05-04-semantic-html-consolidation.md) |
| [Frontend Test Strategy](frontend-test-strategy.md) | Implemented | [frontend-test-strategy-and-toolchain](../adr/2026-05-08-frontend-test-strategy-and-toolchain.md) |

> The original **Model Gateway** PRD was retired (superseded by the Inference Gateway) and moved to [`../archive/`](../archive/).
