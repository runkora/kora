# Product Requirements Documents

PRDs capture the problem, user needs, and solution approach for a feature **before** the technical design (which lives in one or more [ADRs](../adr/README.md)). See the [contributing workflow](../../CONTRIBUTING.md) for how PRDs and ADRs fit together. New PRDs start from [`template.md`](template.md).

| PRD | Status | Primary ADR(s) |
|-----|--------|----------------|
| [Agent Control Plane](agent-control-plane.md) | Implemented | [orbit-agents-service-architecture](../adr/2026-05-11-orbit-agents-service-architecture.md), [run-and-session-lifecycle](../adr/2026-05-11-run-and-session-lifecycle.md), [agent-registry-and-definition-model](../adr/2026-05-11-agent-registry-and-definition-model.md) |
| [Agent Builder](agent-builder.md) | Implemented | [agent-builder](../adr/2026-07-17-agent-builder.md) |
| [Delegated Agent Identity](delegated-agent-identity.md) | Draft | TBD |
| [Codex and OpenCode Agent Runners](codex-opencode-agent-runners.md) | Draft | [codex-agent-runner](../adr/2026-06-04-codex-agent-runner.md), [opencode-agent-runner](../adr/2026-06-04-opencode-agent-runner.md) |
| [Run Task Summarization](run-task-summarization.md) | Approved | [run-task-summarization](../adr/2026-06-19-run-task-summarization.md) |
| [Agent Triggers](agent-webhook-triggers.md) | Draft | [agent-triggers-and-webhook-ingestion](../adr/2026-06-17-agent-triggers-and-webhook-ingestion.md) |
| [Trigger Deliveries Filter and Event Detail](trigger-deliveries-filter.md) | Approved | [trigger-deliveries-filter](../adr/2026-06-20-trigger-deliveries-filter.md) |
| [Agent Token Breakdown and Spend Card](agent-token-breakdown-and-spend-card.md) | Approved | [agent-token-breakdown-and-spend-card](../adr/2026-06-23-agent-token-breakdown-and-spend-card.md) |
| [Credential Broker & Outbound Connections](credential-broker-and-outbound-connections.md) | Draft | [jit-credential-brokering-egress-gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md) |
| [Kubernetes Connection](kubernetes-connection.md) | Draft | [jit-credential-brokering-egress-gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md) |
| [Inference Gateway](inference-gateway.md) | Implemented | [inference-gateway-architecture](../adr/2026-04-01-inference-gateway-architecture.md), [provider-model-catalog-and-alias-routing](../adr/2026-04-01-provider-model-catalog-and-alias-routing.md), [anthropic-zero-translation-path](../adr/2026-04-01-anthropic-zero-translation-path.md) |
| [Policy Gateway](policy-gateway.md) | Implemented | [policy-gateway-and-authorisation-model](../adr/2026-04-14-policy-gateway-and-authorisation-model.md), [dynamic-route-scope-registration](../adr/2026-04-14-dynamic-route-scope-registration.md) |
| [Personal Access Tokens](personal-access-tokens.md) | Implemented | [personal-access-tokens](../adr/2026-04-02-personal-access-tokens.md), [pat-scope-assignment-via-explicit-grants](../adr/2026-04-19-pat-scope-assignment-via-explicit-grants.md) |
| [Browser Authentication Flow](browser-auth-flow.md) | Implemented | [browser-auth-flow-and-bff-session-architecture](../adr/2026-04-01-browser-auth-flow-and-bff-session-architecture.md), [home-realm-discovery-and-email-based-tenant-routing](../adr/2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) |
| [Embeddings & Responses API](embeddings-and-responses-api.md) | Implemented | [embeddings-and-responses-api-endpoints](../adr/2026-05-03-embeddings-and-responses-api-endpoints.md) |
| [Inference Logs](inference-logs.md) | Implemented | [inference-usage-logs](../adr/2026-04-17-inference-usage-logs.md) |
| [Inference Log Details](inference-log-details.md) | Implemented | [inference-log-details](../adr/2026-06-05-inference-log-details.md) |
| [Inference Analytics](inference-analytics.md) | Implemented | [inference-analytics](../adr/2026-04-27-inference-analytics.md) |
| [LLM Cost Tracking](llm-cost-tracking.md) | Approved | [llm-cost-tracking](../adr/2026-06-05-llm-cost-tracking.md), [budget-model-redesign](../adr/2026-06-10-budget-model-redesign.md) |
| [Budgets Page](budgets-page.md) | Draft | [budget-enforcement-corrections-and-budgets-page](../adr/2026-06-10-budget-enforcement-corrections-and-budgets-page.md) |
| [Hierarchical Budgets](hierarchical-budgets.md) | Draft | [hierarchical-budget-enforcement](../adr/2026-06-10-hierarchical-budget-enforcement.md) |
| [Model Cost Visibility](model-cost-visibility.md) | Approved | [model-cost-visibility](../adr/2026-06-22-model-cost-visibility.md) |
| [MCP / Tool Gateway](mcp-tool-gateway.md) | Approved | [mcp-server-registration-and-introspection](../adr/2026-06-29-mcp-server-registration-and-introspection.md), [mcp-tool-gateway](../adr/2026-06-29-mcp-tool-gateway.md), [mcp-oauth-authentication](../adr/2026-06-29-mcp-oauth-authentication.md) |
| [Orbit MCP](orbit-mcp.md) | Approved | [orbit-mcp](../adr/2026-06-30-orbit-mcp.md) |
| [Terra Design System](design-system.md) | Implemented | [terra-frontend-design-system](../adr/2026-03-25-terra-frontend-design-system.md) |
| [Terra Design System Hardening](terra-design-system-hardening.md) | Implemented | [terra-design-system-hardening](../adr/2026-05-04-terra-design-system-hardening.md) |
| [Frontend Component Consolidation](frontend-component-consolidation.md) | Implemented | [frontend-component-consolidation](../adr/2026-05-04-frontend-component-consolidation.md) |
| [Semantic HTML Consolidation](semantic-html-consolidation.md) | Implemented | [semantic-html-consolidation](../adr/2026-05-04-semantic-html-consolidation.md) |
| [Frontend Test Strategy](frontend-test-strategy.md) | Implemented | [frontend-test-strategy-and-toolchain](../adr/2026-05-08-frontend-test-strategy-and-toolchain.md) |
| [Nav Cleanup and Live Dashboard](nav-cleanup-and-live-dashboard.md) | Approved | [nav-cleanup-and-live-dashboard](../adr/2026-06-19-nav-cleanup-and-live-dashboard.md) |
| [Mobile & Tablet Responsive Design](mobile-responsive-design.md) | Approved | [mobile-responsive-design](../adr/2026-06-19-mobile-responsive-design.md) |
| [Profile Page](profile-page.md) | Draft | [profile-page-architecture](../adr/2026-05-13-profile-page-architecture.md) |
| [User App Preferences](user-app-preferences.md) | Approved | [user-app-preferences](../adr/2026-05-08-user-app-preferences.md) |

> The original **Model Gateway** PRD was retired (superseded by the Inference Gateway) and moved to [`../archive/`](../archive/).
