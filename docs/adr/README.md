# Architecture Decision Records

kora records every significant technical decision as an ADR. This index is the catalogue.

**Naming.** ADR files are named `YYYY-MM-DD-kebab-slug.md` — the decision date plus a short slug. There is **no sequential number**; the slug is the citation handle (e.g. *inference-gateway-architecture*). Files sort chronologically. The original sequential numbers are preserved in the *Former #* column below for traceability with older commits and discussions. To add one, copy [`template.md`](template.md) and follow the [contributing workflow](../../CONTRIBUTING.md).

**Status values:** `Proposed` (under discussion) · `Accepted` (ratified; may or may not be fully implemented) · `Deprecated` / `Superseded`.

## By subsystem

### Foundations & Platform

- [Monorepo Technology Stack](2026-02-16-monorepo-technology-stack.md) — `Accepted` · 2026-02-16
- [Language and Framework Selection (Go & Next.js)](2026-02-16-language-and-framework-selection.md) — `Accepted` · 2026-02-16
- [Repository Pattern and Code Organization](2026-02-17-repository-pattern-and-code-organization.md) — `Accepted` · 2026-02-17
- [OTEL-Compatible Dependency-Injected Logging](2026-02-17-dependency-injected-logging.md) — `Accepted` · 2026-02-17
- [Controller-Based HTTP Routing with Stdlib ServeMux](2026-02-17-controller-based-http-routing.md) — `Accepted` · 2026-02-17
- [YAML File-Based Configuration with Typed Generic Loader](2026-02-18-yaml-file-based-configuration.md) — `Accepted (Amended 2026-10-06)` · 2026-02-18
- [Database Infrastructure Layer](2026-03-03-database-infrastructure-layer.md) — `Accepted` · 2026-03-03
- [Multi-Service Backend Deployment Structure](2026-03-19-multi-service-backend-deployment-structure.md) — `Accepted` · 2026-03-19
- [OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md) — `Accepted` · 2026-03-21
- [Prometheus Metrics Endpoints for Backend Services](2026-10-06-prometheus-metrics-endpoints.md) — `Accepted` · 2026-10-06
- [Local Development Environment](2026-03-10-local-development-environment.md) — `Superseded` · 2026-03-10
- [Kubernetes-Only Local Development](2026-07-10-kubernetes-only-local-development.md) — `Accepted` · 2026-07-10
- [CI Pipeline and Local Build Parallelisation](2026-07-21-ci-pipeline-and-local-build-parallelisation.md) — `Proposed` · 2026-07-21
- [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) — `Accepted` · 2026-10-06

### Identity, Auth & Tokens

- [Unified Identity Model](2026-03-05-unified-identity-model.md) — `Accepted` · 2026-03-05
- [SPIFFE/SPIRE Workload Attestation](2026-03-05-spiffe-spire-workload-attestation.md) — `Accepted` · 2026-03-05
- [OIDC Provider Abstraction and Local Identity Provider](2026-03-16-oidc-provider-abstraction-and-local-identity-provider.md) — `Accepted` · 2026-03-16
- [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) — `Accepted` · 2026-03-17
- [Tenant Registration and OIDC Provider Binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) — `Accepted` · 2026-03-31
- [OIDC Client Secret Projection for Confidential Tenants](2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md) — `Accepted` · 2026-03-31
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) — `Accepted` · 2026-04-01
- [Home Realm Discovery and Email-Based Tenant Routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) — `Accepted` · 2026-04-01
- [Personal Access Tokens for Inference](2026-04-02-personal-access-tokens.md) — `Accepted` · 2026-04-02
- [User Profiles Table for Principal Attribution](2026-04-19-user-profiles-table-for-principal-attribution.md) — `Accepted` · 2026-04-19
- [User App Preferences](2026-05-08-user-app-preferences.md) — `Proposed` · 2026-05-08
- [Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) — `Accepted` · 2026-03-21
- [OAuth2 Web-Redirect Clients: GitOps Pre-Registration via an OAuthClient CRD](2026-07-28-oauth2-web-redirect-clients.md) — `Accepted` · 2026-07-28

### Policy & Authorization

- [Universal Gateway as Policy Enforcement Point](2026-03-05-universal-gateway-policy-enforcement.md) — `Accepted` · 2026-03-05
- [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) — `Accepted (revised 2026-04-13)` · 2026-04-14
- [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) — `Accepted (Amended 2026-10-01)` · 2026-04-14
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) — `Accepted` · 2026-04-14
- [Policy Administration Dashboard](2026-04-15-policy-administration-dashboard.md) — `Accepted` · 2026-04-15
- [PAT Scope Assignment via Explicit Grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) — `Accepted` · 2026-04-19
- [Gateway-Level OPA Enforcement for External Traffic](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md) — `Accepted` · 2026-04-27
- [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) — `Accepted` · 2026-06-04

### Inference Gateway

- [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md) — `Accepted` · 2026-04-01
- [Provider Model Catalog and Alias Routing](2026-04-01-provider-model-catalog-and-alias-routing.md) — `Accepted (Amended 2025-07-22)` · 2026-04-01
- [Anthropic Zero-Translation Path](2026-04-01-anthropic-zero-translation-path.md) — `Accepted (Amended 2026-10-07)` · 2026-04-01
- [Model Capability Taxonomy](2026-04-01-model-capability-taxonomy.md) — `Accepted (Amended 2026-10-10)` · 2026-04-01
- [Virtual Model Catalog and Platform Routing](2026-04-17-virtual-model-catalog-and-platform-routing.md) — `Accepted` · 2026-04-17
- [Inference Usage Logs](2026-04-17-inference-usage-logs.md) — `Accepted` · 2026-04-17
- [Inference Analytics: My Usage and Analytics Tabs](2026-04-27-inference-analytics.md) — `Accepted (Amended 2026-07-20)` · 2026-04-27
- [Embeddings and Responses API Endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md) — `Accepted` · 2026-05-03
- [Inference Log Detail Modal and Failure Details Capture](2026-06-05-inference-log-details.md) — `Accepted` · 2026-06-05
- [LLM Cost Tracking: Pricing Pipeline, Budget Enforcement, and Quota Roles](2026-06-05-llm-cost-tracking.md) — `Accepted (Decision 5 superseded 2026-06-10)` · 2026-06-05
- [Budget Model Redesign: Inline JSONB on Roles/Grants, MAX Resolution, Per-Category Spend](2026-06-10-budget-model-redesign.md) — `Proposed` · 2026-06-10
- [Budget Enforcement Corrections: Canonical Spend Counter, Request-Time Role Enforcement, All Principal Types, and the Budgets Management Surface](2026-06-10-budget-enforcement-corrections-and-budgets-page.md) — `Proposed` · 2026-06-10
- [Hierarchical Budget Enforcement: Windows, Pools, Tenant Caps, and Per-Run Limits](2026-06-10-hierarchical-budget-enforcement.md) — `Proposed` · 2026-06-10
- [Role-Budget Attribution for Non-Interactive Credentials](2026-06-15-role-budget-attribution-for-non-interactive-credentials.md) — `Accepted` · 2026-06-15
- [Model Cost Visibility: Pricing on the Models Page and Virtual Models Catalog](2026-06-22-model-cost-visibility.md) — `Accepted` · 2026-06-22
- [Multi-Account AWS Bedrock Support](2026-06-19-multi-account-bedrock.md) — `Superseded by [crd-inference-providers](2026-10-01-crd-inference-providers.md)` · 2026-06-19
- [Provider Error Visibility in the Inference Gateway](2026-06-19-provider-error-visibility.md) — `Accepted` · 2026-06-19
- [Non-Token Model Pricing: Full Cost Dimension Pipeline](2026-07-24-non-token-model-pricing.md) — `Accepted` · 2026-07-24
- [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md) — `Accepted (Amended 2026-10-02)` · 2026-10-01
- [Decision Models and the System One API](2026-10-10-systemone-decision-models.md) — `Accepted` · 2026-10-10

### Agents Control Plane & Execution

- [Agent Execution Flow Patterns & Delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) — `Accepted` · 2026-03-05
- [MCP Server Registration, Deployment, and Governance](2026-03-05-mcp-server-registration-deployment-and-governance.md) — `Accepted` · 2026-03-05
- [Scope-Gated Tool Exposure via MCP](2026-03-05-scope-gated-tool-exposure-via-mcp.md) — `Accepted` · 2026-03-05
- [Credential-Aware Outbound Gateway](2026-03-05-credential-aware-outbound-gateway.md) — `Superseded` · 2026-03-05
- [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) — `Proposed (Amended 2026-10-07: Amendment 5 Accepted)` · 2026-06-06
- [Kubernetes Connection — `kubernetes` Adapter and Per-Connection Gateway CA Trust](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md) — `Proposed` · 2026-07-06
- [Connection Scope Qualifiers — Grant-Time Resource Scoping for Connectors](2026-07-22-connection-scope-qualifiers.md) — `Accepted` · 2026-07-22
- [GitHub Connection — Category-Scoped Actions, Repository Qualifiers, and Down-Scoped Token Minting](2026-07-22-github-connection-category-scoped-actions.md) — `Accepted` · 2026-07-22
- [GitHub GraphQL Authorised by the Granular Actions of Selected Fields](2026-09-29-github-graphql-field-classification.md) — `Accepted` · 2026-09-29
- [Same-Host Connection Selection in the Egress Gateway](2026-10-15-same-host-connection-selection.md) — `Accepted` · 2026-10-15
- [Agent Sandbox Toolchain and Connectivity Environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md) — `Superseded by [sandbox-environments](2026-10-03-sandbox-environments.md)` · 2026-06-14
- [Sandbox Environments](2026-10-03-sandbox-environments.md) — `Accepted (Amended 2026-10-08)` · 2026-10-03
- [Bundled Sandbox Images](2026-10-07-bundled-sandbox-images.md) — `Accepted` · 2026-10-07
- [Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md) — `Accepted` · 2026-10-08
- [Agent CLI Framework & Transform Layer](2026-05-11-agent-cli-framework-and-transform-layer.md) — `Accepted` · 2026-05-11
- [Codex Agent Runner](2026-06-04-codex-agent-runner.md) — `Accepted (Amended 2026-09-29)` · 2026-06-04
- [OpenCode Agent Runner](2026-06-04-opencode-agent-runner.md) — `Accepted (Amended 2026-09-30)` · 2026-06-04
- [Agent Registry & Definition Model](2026-05-11-agent-registry-and-definition-model.md) — `Accepted (Amended 2026-09-29)` · 2026-05-11
- [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) — `Accepted (Amended 2026-10-12)` · 2026-05-11
- [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md) — `Accepted` · 2026-05-11
- [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md) — `Accepted (Amended 2026-10-12)` · 2026-05-11
- [Run Scheduling](2026-10-12-run-scheduling.md) — `Accepted` · 2026-10-12
- [Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md) — `Accepted` · 2026-05-11
- [Agent Workspace Model](2026-05-11-agent-workspace-model.md) — `Accepted` · 2026-05-11
- [BYOC Agent Discovery & Observability](2026-05-11-byoc-agent-discovery-and-observability.md) — `Accepted` · 2026-05-11
- [Agent Identity & Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) — `Accepted (Amended 2026-09-29)` · 2026-05-11
- [Agent Portal & Chat UI](2026-05-11-agent-portal-and-chat-ui.md) — `Accepted` · 2026-05-11
- [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) — `Accepted` · 2026-05-11
- [Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md) — `Accepted` · 2026-09-18
- [Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md) — `Accepted` · 2026-09-18
- [Run Activity Events](2026-09-29-run-activity-events.md) — `Accepted` · 2026-09-29
- [orbit-agents Route & Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md) — `Accepted` · 2026-05-11
- [Agents BFF Integration and Run Chat Wiring](2026-06-04-agents-bff-integration-and-run-chat-wiring.md) — `Accepted` · 2026-06-04
- [Agent Run Filesystem and Process Isolation](2026-06-04-per-session-home-isolation.md) — `Accepted (Amended 2026-09-29, 2026-09-30, 2026-10-08)` · 2026-06-04
- [Service-Layer Authorization for orbit-agents](2026-06-04-orbit-agents-authorization-model.md) — `Accepted` · 2026-06-04

- [K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md) — `Superseded by [manifest-patches-configmap](2026-09-15-manifest-patches-configmap.md)` · 2026-06-12
- [Config-Driven Kustomize Manifest Patches for orbit-agents](2026-09-15-manifest-patches-configmap.md) — `Accepted` · 2026-09-15
- [First-Class Customisation Points for Provisioned Agent Workloads](2026-09-16-first-class-workload-placement.md) — `Accepted` · 2026-09-16
- [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) — `Accepted (Decision item 3 superseded in part 2026-09-29)` · 2026-06-17
- [Run Task Summarization](2026-06-19-run-task-summarization.md) — `Accepted` · 2026-06-19
- [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md) — `Accepted (field map, empty event_type rule and declared event types superseded 2026-09-29)` · 2026-06-20
- [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md) — `Accepted` · 2026-06-20
- [Agent Token Breakdown and Spend Card](2026-06-23-agent-token-breakdown-and-spend-card.md) — `Accepted (Amended 2026-10-06)` · 2026-06-23
- [Agent Builder](2026-07-17-agent-builder.md) — `Accepted` · 2026-07-17
- [Agent Detail Configuration and the Shared Builder Motif](2026-07-17-agent-detail-configuration.md) — `Accepted` · 2026-07-17
- [Motif Component Promotion into Hades](2026-07-17-motif-component-promotion.md) — `Accepted` · 2026-07-17
- [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md) — `Accepted (curated fields and sub-decision 3 superseded in part 2026-09-29)` · 2026-07-21
- [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md) — `Accepted` · 2026-07-23
- [Hades Wizard Component](2026-08-03-terra-wizard-component.md) — `Proposed` · 2026-08-03
- [Owner References for Agents and Sandboxes](2026-09-06-owner-references-for-agents-and-sandboxes.md) — `Proposed` · 2026-09-06
- [Authenticated Sandbox Exec and Runner Isolation Parity](2026-09-18-sandbox-exec-trust-boundary.md) — `Proposed` · 2026-09-18
- [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) — `Accepted` · 2026-09-28
- [Agent Run Guardrails](2026-09-29-agent-run-guardrails.md) — `Accepted (Amended 2026-09-30)` · 2026-09-29
- [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md) — `Accepted` · 2026-10-13
- [Trigger Routing](2026-10-19-trigger-routing.md) — `Accepted` · 2026-10-19

### MCP / Tool Gateway

Build order: **Registration** is the foundation; the **Gateway** depends on it, and on **OAuth** (which can be built in parallel with the Gateway).

- [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) — `Accepted` · 2026-06-29
- [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) — `Accepted` · 2026-06-29
- [MCP OAuth Authentication Model](2026-06-29-mcp-oauth-authentication.md) — `Accepted` · 2026-06-29
- [kora MCP — Platform Management MCP Server](2026-06-30-orbit-mcp.md) — `Accepted` · 2026-06-30
- [Tenant MCP Servers as a Recognised Workload Class](2026-09-17-mcp-workload-class.md) — `Proposed` · 2026-09-17

### Service Mesh, Operator & Deployment

- [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) — `Accepted` · 2026-03-05
- [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) — `Accepted` · 2026-03-31
- [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) — `Superseded` · 2026-03-31
- [Frontend Cluster Deployment and Ingress Routing](2026-04-06-frontend-cluster-deployment-and-ingress-routing.md) — `Accepted` · 2026-04-06
- [North-South Ingress and External Service Exposure](2026-07-04-north-south-ingress-and-external-service-exposure.md) — `Accepted` · 2026-07-04
- [Install and Distribution Model: Published Charts and a Tenant-Only Operator](2026-09-04-install-and-distribution-model.md) — `Proposed` · 2026-09-04
- [Enforcement Sidecar Injection, Preflight and Restricted Pod Security](2026-09-04-enforcement-sidecar-injection-and-preflight.md) — `Accepted (Amended 2026-10-02, 2026-10-12)` · 2026-09-04
- [Database Migrations](2026-09-10-database-migrations.md) — `Proposed` · 2026-09-10
- [Agent Pod Egress Lockdown](2026-09-30-agent-pod-egress-lockdown.md) — `Accepted` · 2026-09-30

### Frontend & Design System

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) — `Accepted` · 2026-03-17
- [Hades Frontend Design System](2026-03-25-terra-frontend-design-system.md) — `Accepted` · 2026-03-25
- [Hades Versioning and Theme Surface Manifest](2026-10-11-hades-versioning-and-theme-surface-manifest.md) — `Accepted` · 2026-10-11
- [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) — `Accepted` · 2026-10-11
- [White-labelling Theme](2026-10-12-white-labelling-theme.md) — `Accepted` · 2026-10-12
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) — `Accepted` · 2026-05-04
- [Hades Design System Hardening](2026-05-04-terra-design-system-hardening.md) — `Accepted` · 2026-05-04
- [Semantic HTML Consolidation — Unconditional Ban on header, article, nav, aside, footer](2026-05-04-semantic-html-consolidation.md) — `Accepted` · 2026-05-04
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) — `Accepted` · 2026-05-08
- [Nav Cleanup and Live Dashboard](2026-06-19-nav-cleanup-and-live-dashboard.md) — `Accepted` · 2026-06-19
- [Mobile & Tablet Responsive Design](2026-06-19-mobile-responsive-design.md) — `Accepted` · 2026-06-19
- [Profile Page Architecture](2026-05-13-profile-page-architecture.md) — `Proposed` · 2026-05-13

## All ADRs (chronological)

| Date | Decision | Status | Former # |
|------|----------|--------|----------|
| 2026-02-16 | [Language and Framework Selection (Go & Next.js)](2026-02-16-language-and-framework-selection.md) | Accepted | 0002 |
| 2026-02-16 | [Monorepo Technology Stack](2026-02-16-monorepo-technology-stack.md) | Accepted | 0001 |
| 2026-02-17 | [Controller-Based HTTP Routing with Stdlib ServeMux](2026-02-17-controller-based-http-routing.md) | Accepted | 0005 |
| 2026-02-17 | [OTEL-Compatible Dependency-Injected Logging](2026-02-17-dependency-injected-logging.md) | Accepted | 0004 |
| 2026-02-17 | [Repository Pattern and Code Organization](2026-02-17-repository-pattern-and-code-organization.md) | Accepted | 0003 |
| 2026-02-18 | [YAML File-Based Configuration with Typed Generic Loader](2026-02-18-yaml-file-based-configuration.md) | Accepted (Amended 2026-10-06) | 0006 |
| 2026-03-03 | [Database Infrastructure Layer](2026-03-03-database-infrastructure-layer.md) | Accepted | 0007 |
| 2026-03-05 | [Agent Execution Flow Patterns & Delegation](2026-03-05-agent-execution-flow-patterns-and-delegation.md) | Accepted | 0009 |
| 2026-03-05 | [Credential-Aware Outbound Gateway](2026-03-05-credential-aware-outbound-gateway.md) | Superseded | 0012 |
| 2026-03-05 | [MCP Server Registration, Deployment, and Governance](2026-03-05-mcp-server-registration-deployment-and-governance.md) | Accepted | 0013 |
| 2026-03-05 | [Scope-Gated Tool Exposure via MCP](2026-03-05-scope-gated-tool-exposure-via-mcp.md) | Accepted | 0014 |
| 2026-03-05 | [Service Mesh and Network Enforcement Infrastructure](2026-03-05-service-mesh-and-network-enforcement-infrastructure.md) | Accepted | 0015 |
| 2026-03-05 | [SPIFFE/SPIRE Workload Attestation](2026-03-05-spiffe-spire-workload-attestation.md) | Accepted | 0010 |
| 2026-03-05 | [Unified Identity Model](2026-03-05-unified-identity-model.md) | Accepted | 0008 |
| 2026-03-05 | [Universal Gateway as Policy Enforcement Point](2026-03-05-universal-gateway-policy-enforcement.md) | Accepted | 0011 |
| 2026-03-10 | [Local Development Environment](2026-03-10-local-development-environment.md) | Superseded | 0016 |
| 2026-03-16 | [OIDC Provider Abstraction and Local Identity Provider](2026-03-16-oidc-provider-abstraction-and-local-identity-provider.md) | Accepted | 0017 |
| 2026-03-17 | [Dynamic SPIFFE Identity Grants and Tenant Isolation](2026-03-17-dynamic-spiffe-identity-grants-and-tenant-isolation.md) | Accepted | 0018 |
| 2026-03-17 | [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) | Accepted | 0019 |
| 2026-03-19 | [Multi-Service Backend Deployment Structure](2026-03-19-multi-service-backend-deployment-structure.md) | Accepted | 0020 |
| 2026-03-21 | [Envoy ext_proc Sidecar for Transparent Platform Token Injection](2026-03-21-envoy-ext-proc-sidecar-token-injection.md) | Accepted | 0021 |
| 2026-03-21 | [OpenTelemetry Observability Discipline](2026-03-21-opentelemetry-observability-discipline.md) | Accepted | 0022 |
| 2026-03-25 | [Hades Frontend Design System](2026-03-25-terra-frontend-design-system.md) | Accepted | 0021 |
| 2026-03-31 | [Helm Chart Distribution and GitOps Repository Structure](2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md) | Accepted | 0027 |
| 2026-03-31 | [OIDC Client Secret Projection for Confidential Tenants](2026-03-31-oidc-client-secret-projection-for-confidential-tenants.md) | Accepted | 0029 |
| 2026-03-31 | [kora Kubernetes Operator](2026-03-31-orbit-kubernetes-operator.md) | Superseded | 0028 |
| 2026-03-31 | [Tenant Registration and OIDC Provider Binding](2026-03-31-tenant-registration-and-oidc-provider-binding.md) | Accepted | 0026 |
| 2026-04-01 | [Anthropic Zero-Translation Path](2026-04-01-anthropic-zero-translation-path.md) | Accepted (Amended 2026-10-07) | 0025 |
| 2026-04-01 | [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) | Accepted | 0030 |
| 2026-04-01 | [Home Realm Discovery and Email-Based Tenant Routing](2026-04-01-home-realm-discovery-and-email-based-tenant-routing.md) | Accepted | 0031 |
| 2026-04-01 | [Inference Gateway Architecture](2026-04-01-inference-gateway-architecture.md) | Accepted | 0023 |
| 2026-04-01 | [Model Capability Taxonomy](2026-04-01-model-capability-taxonomy.md) | Accepted (Amended 2026-10-10) | 0032 |
| 2026-04-01 | [Provider Model Catalog and Alias Routing](2026-04-01-provider-model-catalog-and-alias-routing.md) | Accepted (Amended 2025-07-22) | 0024 |
| 2026-04-02 | [Personal Access Tokens for Inference](2026-04-02-personal-access-tokens.md) | Accepted | 0033 |
| 2026-04-06 | [Frontend Cluster Deployment and Ingress Routing](2026-04-06-frontend-cluster-deployment-and-ingress-routing.md) | Accepted | 0034 |
| 2026-04-14 | [Dynamic Route-Scope Registration](2026-04-14-dynamic-route-scope-registration.md) | Accepted (Amended 2026-10-01) | 0036 |
| 2026-04-14 | [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) | Accepted | 0037 |
| 2026-04-14 | [Policy Gateway and Authorisation Model](2026-04-14-policy-gateway-and-authorisation-model.md) | Accepted (revised 2026-04-13) | 0035 |
| 2026-04-15 | [Policy Administration Dashboard](2026-04-15-policy-administration-dashboard.md) | Accepted | 0038 |
| 2026-04-17 | [Inference Usage Logs](2026-04-17-inference-usage-logs.md) | Accepted | 0040 |
| 2026-04-17 | [Virtual Model Catalog and Platform Routing](2026-04-17-virtual-model-catalog-and-platform-routing.md) | Accepted | 0039 |
| 2026-04-19 | [PAT Scope Assignment via Explicit Grants](2026-04-19-pat-scope-assignment-via-explicit-grants.md) | Accepted | 0041 |
| 2026-04-19 | [User Profiles Table for Principal Attribution](2026-04-19-user-profiles-table-for-principal-attribution.md) | Accepted | 0042 |
| 2026-04-27 | [Gateway-Level OPA Enforcement for External Traffic](2026-04-27-gateway-level-opa-enforcement-for-external-traffic.md) | Accepted | 0044 |
| 2026-04-27 | [Inference Analytics: My Usage and Analytics Tabs](2026-04-27-inference-analytics.md) | Accepted (Amended 2026-07-20) | 0043 |
| 2026-05-03 | [Embeddings and Responses API Endpoints](2026-05-03-embeddings-and-responses-api-endpoints.md) | Accepted | 0045 |
| 2026-05-04 | [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) | Accepted | 0046 |
| 2026-05-04 | [Semantic HTML Consolidation — Unconditional Ban on header, article, nav, aside, footer](2026-05-04-semantic-html-consolidation.md) | Accepted | 0048 |
| 2026-05-04 | [Hades Design System Hardening](2026-05-04-terra-design-system-hardening.md) | Accepted | 0047 |
| 2026-05-08 | [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) | Accepted | 0049 |
| 2026-05-11 | [Agent CLI Framework & Transform Layer](2026-05-11-agent-cli-framework-and-transform-layer.md) | Accepted | 0049 |
| 2026-05-11 | [Agent Event Streaming Protocol](2026-05-11-agent-event-streaming-protocol.md) | Accepted | 0054 |
| 2026-05-11 | [Agent Execution Infrastructure](2026-05-11-agent-execution-infrastructure.md) | Accepted (Amended 2026-10-12) | 0051 |
| 2026-05-11 | [Agent Identity & Policy Provisioning](2026-05-11-agent-identity-and-policy-provisioning.md) | Accepted (Amended 2026-09-29) | 0057 |
| 2026-05-11 | [Agent Portal & Chat UI](2026-05-11-agent-portal-and-chat-ui.md) | Accepted | 0058 |
| 2026-05-11 | [Agent Registry & Definition Model](2026-05-11-agent-registry-and-definition-model.md) | Accepted (Amended 2026-09-29) | 0050 |
| 2026-05-11 | [Agent Workspace Model](2026-05-11-agent-workspace-model.md) | Accepted | 0055 |
| 2026-05-11 | [BYOC Agent Discovery & Observability](2026-05-11-byoc-agent-discovery-and-observability.md) | Accepted | 0056 |
| 2026-05-11 | [orbit-agents Route & Scope Registration](2026-05-11-orbit-agents-route-scope-registration.md) | Accepted | 0060 |
| 2026-05-11 | [orbit-agents Service Architecture](2026-05-11-orbit-agents-service-architecture.md) | Accepted | 0052 |
| 2026-05-11 | [Run & Session Lifecycle](2026-05-11-run-and-session-lifecycle.md) | Accepted (Amended 2026-10-12) | 0053 |
| 2026-05-11 | [Sandbox Provisioning and Shell Proxy](2026-05-11-sandbox-provisioning-and-shell-proxy.md) | Accepted | 0059 |
| 2026-05-13 | [Profile Page Architecture](2026-05-13-profile-page-architecture.md) | Proposed | — |
| 2026-06-04 | [Agents BFF Integration and Run Chat Wiring](2026-06-04-agents-bff-integration-and-run-chat-wiring.md) | Accepted | 0061 |
| 2026-06-04 | [Service-Layer Authorization for orbit-agents](2026-06-04-orbit-agents-authorization-model.md) | Accepted | 0063 |
| 2026-06-04 | [Agent Run Filesystem and Process Isolation](2026-06-04-per-session-home-isolation.md) | Accepted (Amended 2026-09-29, 2026-09-30, 2026-10-08) | 0062 |
| 2026-06-04 | [Platform-wide Scope Enforcement Middleware](2026-06-04-platform-scope-enforcement-middleware.md) | Accepted | 0064 |
| 2026-06-04 | [Codex Agent Runner](2026-06-04-codex-agent-runner.md) | Accepted (Amended 2026-09-29) | — |
| 2026-06-04 | [OpenCode Agent Runner](2026-06-04-opencode-agent-runner.md) | Accepted (Amended 2026-09-30) | — |
| 2026-06-05 | [Inference Log Detail Modal and Failure Details Capture](2026-06-05-inference-log-details.md) | Accepted | 0050 |
| 2026-06-05 | [LLM Cost Tracking: Pricing Pipeline, Budget Enforcement, and Quota Roles](2026-06-05-llm-cost-tracking.md) | Accepted (Decision 5 superseded 2026-06-10) | — |
| 2026-06-06 | [JIT Credential Brokering via a Per-Tenant Egress Gateway](2026-06-06-jit-credential-brokering-egress-gateway.md) | Proposed (Amended 2026-10-07: Amendment 5 Accepted) | 0065 |
| 2026-06-10 | [Budget Model Redesign: Inline JSONB on Roles/Grants, MAX Resolution, Per-Category Spend](2026-06-10-budget-model-redesign.md) | Proposed | — |
| 2026-06-10 | [Budget Enforcement Corrections: Canonical Spend Counter, Request-Time Role Enforcement, All Principal Types, and the Budgets Management Surface](2026-06-10-budget-enforcement-corrections-and-budgets-page.md) | Proposed | — |
| 2026-06-10 | [Hierarchical Budget Enforcement: Windows, Pools, Tenant Caps, and Per-Run Limits](2026-06-10-hierarchical-budget-enforcement.md) | Proposed | — |
| 2026-06-14 | [Agent Sandbox Toolchain and Connectivity Environments](2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md) | Superseded by [sandbox-environments](2026-10-03-sandbox-environments.md) | — |
| 2026-06-12 | [K8s Manifest Field Overrides via Environment Variables](2026-06-12-k8s-manifest-overrides-via-env-vars.md) | Superseded by [manifest-patches-configmap](2026-09-15-manifest-patches-configmap.md) | — |
| 2026-06-15 | [Role-Budget Attribution for Non-Interactive Credentials](2026-06-15-role-budget-attribution-for-non-interactive-credentials.md) | Accepted | — |
| 2026-06-17 | [Agent Triggers and Webhook Ingestion](2026-06-17-agent-triggers-and-webhook-ingestion.md) | Accepted (Decision item 3 superseded in part 2026-09-29) | — |
| 2026-06-19 | [Provider Error Visibility in the Inference Gateway](2026-06-19-provider-error-visibility.md) | Accepted | — |
| 2026-06-19 | [Nav Cleanup and Live Dashboard](2026-06-19-nav-cleanup-and-live-dashboard.md) | Accepted | — |
| 2026-06-19 | [Mobile & Tablet Responsive Design](2026-06-19-mobile-responsive-design.md) | Accepted | — |
| 2026-06-19 | [Run Task Summarization](2026-06-19-run-task-summarization.md) | Accepted | — |
| 2026-06-20 | [Trigger Deliveries: Client-Side Status Filter and Inline Payload Excerpt](2026-06-20-trigger-deliveries-filter.md) | Accepted | — |
| 2026-06-20 | [Extended Trigger Types and an Operable Delivery Pipeline](2026-06-20-extended-trigger-types.md) | Accepted (field map, empty event_type rule and declared event types superseded 2026-09-29) | — |
| 2026-06-22 | [Model Cost Visibility: Pricing on the Models Page and Virtual Models Catalog](2026-06-22-model-cost-visibility.md) | Accepted | — |
| 2026-06-23 | [Agent Token Breakdown and Spend Card](2026-06-23-agent-token-breakdown-and-spend-card.md) | Accepted (Amended 2026-10-06) | — |
| 2026-07-04 | [North-South Ingress and External Service Exposure](2026-07-04-north-south-ingress-and-external-service-exposure.md) | Accepted | — |
| 2026-07-06 | [Kubernetes Connection — `kubernetes` Adapter and Per-Connection Gateway CA Trust](2026-07-06-kubernetes-connection-bearer-adapter-and-gateway-ca-trust.md) | Proposed | — |
| 2026-07-17 | [Agent Builder](2026-07-17-agent-builder.md) | Accepted | — |
| 2026-07-17 | [Agent Detail Configuration and the Shared Builder Motif](2026-07-17-agent-detail-configuration.md) | Accepted | — |
| 2026-07-17 | [Motif Component Promotion into Hades](2026-07-17-motif-component-promotion.md) | Accepted | — |
| 2026-07-22 | [Connection Scope Qualifiers — Grant-Time Resource Scoping for Connectors](2026-07-22-connection-scope-qualifiers.md) | Accepted | — |
| 2026-07-22 | [GitHub Connection — Category-Scoped Actions, Repository Qualifiers, and Down-Scoped Token Minting](2026-07-22-github-connection-category-scoped-actions.md) | Accepted | — |
| 2026-07-23 | [Delegated Agent Identity](2026-07-23-delegated-agent-identity.md) | Accepted | — |
| 2026-07-28 | [OAuth2 Web-Redirect Clients: GitOps Pre-Registration via an OAuthClient CRD](2026-07-28-oauth2-web-redirect-clients.md) | Accepted | — |
| 2026-08-03 | [Hades Wizard Component](2026-08-03-terra-wizard-component.md) | Proposed | — |
| 2026-09-06 | [Owner References for Agents and Sandboxes](2026-09-06-owner-references-for-agents-and-sandboxes.md) | Proposed | — |
| 2026-06-29 | [MCP Server Registration & Introspection](2026-06-29-mcp-server-registration-and-introspection.md) | Accepted | — |
| 2026-07-24 | [Non-Token Model Pricing: Full Cost Dimension Pipeline](2026-07-24-non-token-model-pricing.md) | Accepted | — |
| 2026-06-29 | [MCP / Tool Gateway](2026-06-29-mcp-tool-gateway.md) | Accepted | — |
| 2026-06-29 | [MCP OAuth Authentication Model](2026-06-29-mcp-oauth-authentication.md) | Accepted | — |
| 2026-06-30 | [kora MCP — Platform Management MCP Server](2026-06-30-orbit-mcp.md) | Accepted | — |
| 2026-07-04 | [North-South Ingress and External Service Exposure](2026-07-04-north-south-ingress-and-external-service-exposure.md) | Accepted | — |
| 2026-07-10 | [Kubernetes-Only Local Development](2026-07-10-kubernetes-only-local-development.md) | Accepted | — |
| 2026-06-19 | [Multi-Account AWS Bedrock Support](2026-06-19-multi-account-bedrock.md) | Superseded by [crd-inference-providers](2026-10-01-crd-inference-providers.md) | — |
| 2026-07-21 | [Alertmanager Webhook Adapter](2026-07-21-alertmanager-webhook-adapter.md) | Accepted (curated fields and sub-decision 3 superseded in part 2026-09-29) | — |
| 2026-07-21 | [CI Pipeline and Local Build Parallelisation](2026-07-21-ci-pipeline-and-local-build-parallelisation.md) | Proposed | — |
| 2026-09-04 | [Install and Distribution Model: Published Charts and a Tenant-Only Operator](2026-09-04-install-and-distribution-model.md) | Proposed | — |
| 2026-09-04 | [Enforcement Sidecar Injection, Preflight and Restricted Pod Security](2026-09-04-enforcement-sidecar-injection-and-preflight.md) | Accepted (Amended 2026-10-02, 2026-10-12) | — |
| 2026-09-10 | [Database Migrations](2026-09-10-database-migrations.md) | Proposed | — |
| 2026-09-15 | [Config-Driven Kustomize Manifest Patches for orbit-agents](2026-09-15-manifest-patches-configmap.md) | Accepted | — |
| 2026-09-16 | [First-Class Customisation Points for Provisioned Agent Workloads](2026-09-16-first-class-workload-placement.md) | Accepted | — |
| 2026-09-17 | [Tenant MCP Servers as a Recognised Workload Class](2026-09-17-mcp-workload-class.md) | Proposed | — |
| 2026-09-18 | [Authenticated Sandbox Exec and Runner Isolation Parity](2026-09-18-sandbox-exec-trust-boundary.md) | Proposed | — |
| 2026-09-18 | [Trace Propagation into the Claude Code Agent Runtime](2026-09-18-agent-runtime-trace-propagation.md) | Accepted | — |
| 2026-09-18 | [Telemetry Collector as a Declared Platform Destination](2026-09-18-telemetry-collector-platform-destination.md) | Accepted | — |
| 2026-09-28 | [Trigger Payload Templating](2026-09-28-trigger-payload-templating.md) | Accepted | — |
| 2026-09-29 | [GitHub GraphQL Authorised by the Granular Actions of Selected Fields](2026-09-29-github-graphql-field-classification.md) | Accepted | — |
| 2026-09-29 | [Run Activity Events](2026-09-29-run-activity-events.md) | Accepted | — |
| 2026-09-29 | [Agent Run Guardrails](2026-09-29-agent-run-guardrails.md) | Accepted (Amended 2026-09-30) | — |
| 2026-09-30 | [Agent Pod Egress Lockdown](2026-09-30-agent-pod-egress-lockdown.md) | Accepted | — |
| 2026-10-01 | [CRD-Driven Inference Providers](2026-10-01-crd-inference-providers.md) | Accepted (Amended 2026-10-02) | — |
| 2026-10-03 | [Sandbox Environments](2026-10-03-sandbox-environments.md) | Accepted (Amended 2026-10-08) | — |
| 2026-10-06 | [Rendered Service Config Files](2026-10-06-rendered-service-config-files.md) | Accepted | — |
| 2026-10-07 | [Bundled Sandbox Images](2026-10-07-bundled-sandbox-images.md) | Accepted | — |
| 2026-10-08 | [Sandbox Workspace Mount](2026-10-08-sandbox-workspace-mount.md) | Accepted | — |
| 2026-10-10 | [Decision Models and the System One API](2026-10-10-systemone-decision-models.md) | Accepted | — |
| 2026-10-11 | [Hades Versioning and Theme Surface Manifest](2026-10-11-hades-versioning-and-theme-surface-manifest.md) | Accepted | — |
| 2026-10-11 | [Hades Delivery to External Consumers](2026-10-11-hades-delivery-to-external-consumers.md) | Accepted | — |
| 2026-10-12 | [Run Scheduling](2026-10-12-run-scheduling.md) | Accepted | — |
| 2026-10-12 | [White-labelling Theme](2026-10-12-white-labelling-theme.md) | Accepted | — |
| 2026-10-13 | [Trigger Fire Coalescing](2026-10-13-trigger-fire-coalescing.md) | Accepted | — |
| 2026-10-15 | [Same-Host Connection Selection in the Egress Gateway](2026-10-15-same-host-connection-selection.md) | Accepted | — |
| 2026-10-19 | [Trigger Routing](2026-10-19-trigger-routing.md) | Accepted | — |
| — | [User App Preferences](2026-05-08-user-app-preferences.md) | Proposed | 0061 |
