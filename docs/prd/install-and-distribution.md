# Install and Distribution - Product Requirements Document

**Status:** In Review
**Created:** 2026-09-04
**Last Updated:** 2026-09-04
**Owner:** Justin Wilkin
**Stakeholders:** Orbit Core Team, Enterprise Platform Operators, Evaluators

---

## Executive Summary

Orbit will ship as a signed, versioned Kubernetes distribution. Platform teams will install it with Helm, Argo CD, or Flux; retain ownership of Istio, SPIRE, ingress, databases, and secrets; and configure every Orbit workload through a documented values surface. Evaluators will use the same release artefacts through a reference installation that brings up the required dependencies and a working Orbit environment.

---

## Problem Statement

### Current State

Orbit's installation model makes the Orbit operator responsible for platform infrastructure as well as Orbit itself. It reconciles Istio, SPIRE, gateways, and application services from one platform resource. That model is unsuitable for the production clusters Orbit targets: a platform team already owns the mesh, workload identity system, ingress, registry, security policy, and upgrade cadence.

Orbit has no supported release contract. A release must publish its chart, supported versions, image inventory, provenance, configuration reference, upgrade procedure, and GitOps examples.

### User Pain Points

- Evaluators need a supported path from an empty cluster to a completed agent run.
- Platform operators need to install Orbit without delegating ownership of cluster infrastructure to an Orbit controller.
- Security reviewers need to verify exactly which artefacts and images a release contains.
- GitOps users need installation, upgrade, rollback, and uninstall to work through declared release inputs.
- Contributors need local deployment to exercise the same artefacts and dependency contract used outside the repository.

### Business Impact

- Orbit cannot be adopted as a standard Kubernetes product.
- Enterprise reviews stop at the boundary between Orbit and customer-owned infrastructure.
- Deployment forks accumulate configuration drift and upgrade risk.
- An open-source release cannot offer a credible evaluation experience without an installable distribution.

---

## Goals and Objectives

### Primary Goals

1. **Publish an installable release.** Every Orbit release includes signed Helm artefacts, immutable image references, an image inventory, release notes, and compatibility information.
2. **Respect infrastructure ownership.** Production installation consumes Istio and SPIRE as dependencies. Orbit does not install, upgrade, or remove customer-owned mesh or workload identity systems.
3. **Support GitOps directly.** Helm, Argo CD, and Flux install, upgrade, roll back, and remove Orbit from the same declared release inputs.
4. **Provide one reference path.** Evaluation and local development use the released charts and the same prerequisite contract as production, with local values as the only variation.
5. **Narrow controller responsibility.** The in-cluster controller reconciles tenant lifecycle only. It does not install the platform or hold platform-installation permissions.
6. **Fit the customer cluster.** Operators configure image sourcing, pull credentials, secrets, placement, resources, priority, and supported ingress without editing release templates.
7. **Fit any supported mesh.** Orbit runs on a compatible Istio installation, including managed offerings, without Orbit-specific mesh configuration.
8. **Fail closed.** A missing prerequisite or failed enforcement injection prevents Orbit from reporting a governed runtime as ready.

### Success Metrics

- A new evaluator completes the quickstart and a sample agent run in 30 minutes or less.
- A cluster with independently operated, compatible Istio and SPIRE installs Orbit without a lifecycle change to either system.
- Helm, Argo CD, and Flux complete a documented installation and upgrade with no imperative recovery step outside their reconciliation workflow.
- Every release publishes verifiable artefacts, digest-pinned images, a machine-readable image inventory, release notes, and a supported-version matrix.
- Release validation renders and installs the charts against every Kubernetes version the release supports.
- Every Orbit workload, including sidecars and the agent sandbox, admits under the restricted Pod Security Standard without exemptions.
- Local deployment uses the released chart structure and prerequisite contract.

### Non-Goals

- Managing Istio, SPIRE, PostgreSQL, Redis, ingress, DNS, TLS, or customer secret-management systems in production.
- Supporting Istio ambient mode. Orbit's runtime and enforcement model uses sidecars.
- Replacing the current Envoy-filter-based enforcement path.
- Adding Gateway API `HTTPRoute` support. This remains tracked in issue #265.
- Delivering managed database, cache, multi-cluster, multi-region, or OpenShift packaging.
- Database migration mechanics. Addressed separately.
- Publishing through a second registry or providing a standalone installer CLI in v1.

---

## User Stories

### US-001: Evaluate Orbit

**As an** evaluator, **I want to** follow one supported quickstart, **so that** I can assess a working Orbit environment without first becoming an Orbit deployment expert.

**Acceptance Criteria:**
- [ ] The quickstart lists the required tools, dependency versions, commands, expected outcomes, and recovery actions.
- [ ] The reference path installs dependencies in the required order and installs Orbit from a published release artefact.
- [ ] The completed path exposes the configured dashboard and API endpoints and verifies a completed agent run.

### US-002: Bring an existing mesh and identity system

**As a** platform operator, **I want to** install Orbit into a cluster that already runs Istio and SPIRE, **so that** the teams responsible for those systems retain their operational ownership.

**Acceptance Criteria:**
- [ ] Production installation does not install, upgrade, or remove Istio or SPIRE.
- [ ] Every release states the supported Istio and SPIRE versions and the exact runtime contract Orbit requires.
- [ ] Preflight identifies an unsatisfied dependency before Orbit workloads become ready and reports the corrective action.
- [ ] Orbit runs without Orbit-specific mesh configuration. The customer's Istio needs no custom injection templates or other Orbit-authored control-plane settings.

### US-003: Operate Orbit through GitOps

**As a** platform operator using Argo CD or Flux, **I want to** manage Orbit as versioned declarations in my repository, **so that** review, reconciliation, rollback, and drift handling work like the rest of my cluster.

**Acceptance Criteria:**
- [ ] Argo CD and Flux examples consume the same versioned artefacts as Helm.
- [ ] The examples cover installation, upgrade, rollback, uninstall, and dependency ordering.
- [ ] A GitOps reconciliation completes without an out-of-band imperative installation action.
- [ ] Uninstall behaviour, including retention of tenant declarations, is explicit and tested.

### US-004: Meet cluster policy without a fork

**As a** platform operator in a restricted cluster, **I want to** configure Orbit through supported release values, **so that** it passes admission and scheduling policies without locally edited manifests.

**Acceptance Criteria:**
- [ ] A private registry and pull credentials apply to all Orbit-provided images, including sidecars and runtime images.
- [ ] Global and component-specific settings cover placement, resources, priority, and topology requirements.
- [ ] Secrets remain customer-supplied and integrate through supported secret references.
- [ ] Every Orbit workload, including sidecars and the agent sandbox, meets the Kubernetes restricted Pod Security Standard.

### US-005: Review and upgrade a release

**As a** security reviewer or platform operator, **I want to** verify a release and understand its upgrade requirements, **so that** I can approve and operate it with confidence.

**Acceptance Criteria:**
- [ ] Release artefacts have a documented verification procedure.
- [ ] Image references are immutable and every release publishes a machine-readable image inventory.
- [ ] Release notes identify breaking changes, configuration changes, and migration prerequisites.
- [ ] The compatibility statement names the Kubernetes, Istio, and SPIRE versions supported by the release.

### US-006: Reconcile tenant lifecycle independently

**As a** platform operator, **I want to** declare tenants through the supported API or GitOps resource, **so that** tenant onboarding and ongoing reconciliation do not depend on a platform-installation controller.

**Acceptance Criteria:**
- [ ] The tenant controller reconciles tenant resources and their declared connections, OAuth clients, and MCP servers.
- [ ] The controller holds only the permissions required by the tenant lifecycle it owns.
- [ ] Removing the platform installation does not silently delete tenant declarations or tenant namespaces.

### US-007: Develop against the shipped distribution

**As a** contributor, **I want to** deploy locally with the released charts and prerequisite contract, **so that** installation defects surface on my machine rather than in a customer cluster.

**Acceptance Criteria:**
- [ ] Local deployment uses the reference installation with local values as the only variation.
- [ ] No local-development configuration ships inside any release artefact or image.

---

## Proposed Solution

### Distribution Model

An Orbit release contains the charts, CRDs, configuration reference, compatibility matrix, provenance material, release notes, and image inventory required to install the control plane. The charts own Orbit resources and the Orbit configuration required by the runtime. They do not own the lifecycle of customer-provided infrastructure.

Istio and SPIRE remain mandatory runtime dependencies. The release defines the versions and configuration Orbit requires from each. Preflight validates that contract before Orbit is ready. The reference installation provisions compatible dependencies for evaluation and local development; production operators provide and operate their own dependencies.

The tenant controller remains in the distribution because tenant state requires continuous reconciliation. Its scope ends at tenants. The controller does not install charts, create platform infrastructure, or reconcile mesh and identity dependencies.

### Install Paths

The reference path installs compatible dependencies and the released Orbit charts in dependency order. It is the evaluator quickstart and the local-development deployment path.

The production path starts with a cluster that already meets the prerequisite contract. The operator stores the release version, values, and tenant declarations in Git and reconciles them with Helm, Argo CD, or Flux. The same charts, values schema, release metadata, and verification procedure apply to both paths.

### Security Boundaries

Installation changes where Orbit's enforcement is delivered, not what it enforces: workload identity, policy evaluation before execution, and brokered credentials remain mandatory in every supported topology, and no install path bypasses them.

---

## Scope

### In Scope

- Published, versioned, signed Helm artefacts and release metadata.
- Helm installation and upgrade guidance plus working Argo CD and Flux examples.
- A release-specific Istio and SPIRE prerequisite contract, preflight validation, and compatibility matrix.
- Operation on any compatible Istio installation, including managed offerings, without Orbit-specific mesh configuration.
- A reference installation for evaluation and local development that uses the released charts.
- A documented configuration surface for image sources, pull credentials, secrets, placement, resources, priority, and supported ingress.
- Tenant-only reconciliation and defined lifecycle behaviour for tenant CRDs and namespaces.
- Release verification, upgrade, rollback, uninstall, and troubleshooting guidance.
- Release validation against every declared Kubernetes version.
- A single registry-authentication step in the quickstart while release artefacts remain private.

### Out of Scope

- A general-purpose Kubernetes platform operator.
- Lifecycle management of customer-owned mesh, identity, database, cache, ingress, or secret-management systems.
- New identity, policy, or credential-brokering capabilities unrelated to distribution.
- Deployment variants listed in the non-goals above.

### Future Considerations

- Public chart distribution after the repository opens. Private-preview registry authentication must remain an additive access step, not a separate installation model.
- Certification for managed Istio offerings after each offering passes the prerequisite contract.
- Ambient-mesh and Gateway API support after the enforcement model for those environments receives its own approved ADR.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Every release installs and upgrades through Helm from published artefacts.
- [ ] Documented Argo CD and Flux configurations consume the same release inputs.
- [ ] Production installation never manages the lifecycle of Istio or SPIRE.
- [ ] Preflight validates the prerequisite contract and returns actionable failures before workloads report ready.
- [ ] The values reference identifies defaults, required fields, supported overrides, and secret references.
- [ ] Registry, pull, scheduling, resource, and secret settings apply to every applicable Orbit workload, including workloads created at run time.
- [ ] The tenant controller has no platform-installation responsibility and no permissions outside its tenant lifecycle.
- [ ] The quickstart, local deployment, and GitOps examples use the published charts rather than duplicate raw manifests.
- [ ] Every release publishes provenance information, immutable image references, an image inventory, release notes, and compatibility information.

**Should Have (P1):**
- [ ] Supported customer-owned ingress with documented DNS and TLS requirements.
- [ ] A post-install preflight command for troubleshooting and drift detection.
- [ ] Release automation that generates release notes and image inventories.

### Non-Functional Requirements

**Security**
- Workload identity, policy enforcement, and credential brokering remain mandatory in every supported deployment topology.
- Release provenance and image immutability are independently verifiable.
- Every workload meets the Kubernetes restricted Pod Security Standard. Release validation enforces it.

**Compatibility**
- Every release declares its supported Kubernetes, Istio, and SPIRE versions.
- Automated release validation renders, installs, and smoke-tests every declared compatibility combination.

**Operability**
- Install, preflight, upgrade, rollback, and uninstall failures expose a meaningful condition, event, or command result.
- The release documents the ownership boundary for every resource it creates or changes.

**Reliability**
- Failed prerequisite validation and failed enforcement injection fail closed. Orbit never reports a partially governed agent runtime as ready.

---

## Dependencies

### Internal Dependencies

- Orbit Helm charts and CRDs.
- Tenant lifecycle reconciler and tenant resource definitions.
- Release automation for publication, signing, compatibility testing, image inventory generation, and release notes.

### External Dependencies

- A supported Kubernetes version.
- A compatible Istio sidecar deployment.
- SPIRE configured to issue the SVIDs and expose the workload API required by Orbit.
- Operator-provided PostgreSQL, Redis, DNS, TLS, registry access, and secrets.
- A chart registry and signing service.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
| --- | --- | --- | --- |
| A supported Istio release changes behaviour required by Orbit enforcement. | High | Medium | Exercise every supported version in release validation and withdraw unsupported versions. |
| The agent sandbox cannot meet the restricted profile as built. | High | Medium | Validate against Pod Security Admission early and resolve the constraint in the sandbox rather than exempt the workload. |
| Reference and production paths diverge. | High | Medium | Make the reference path consume released charts and values; exercise it in release validation. |
| Removing platform lifecycle from the operator leaves an unclear upgrade path. | Medium | Medium | Publish ownership boundaries, compatibility, ordering, rollback, and migration guidance with every release. |
| Private-preview artefacts slow evaluation. | Low | High | Document one registry-authentication step outside the long-term installation contract. |

---

## Delivery

This PRD defines the product boundary and acceptance criteria. The ADR that follows approval will record the chart topology, release pipeline, preflight design, controller transition, and migration plan before implementation begins.

---

## Appendix

### Related Documents

- [Orbit architecture](../../ARCHITECTURE.md)
- [Deployment guide](../guides/deployment.md)
- [Helm Chart Distribution and GitOps Repository Structure](../adr/2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md)
- [Orbit Kubernetes Operator](../adr/2026-03-31-orbit-kubernetes-operator.md)
- [SPIFFE and SPIRE](../spiffe.md)

### Glossary

- **Prerequisite contract:** The release-specific configuration and version requirements Orbit places on systems it does not own.
- **Preflight:** Validation that reports whether a cluster meets the prerequisite contract before Orbit reports ready.
- **Reference installation:** The supported composition used for evaluation and local development. It uses the same Orbit release artefacts as production.
- **Release artefact:** A versioned chart or accompanying provenance, compatibility, and inventory material published for an Orbit release.
