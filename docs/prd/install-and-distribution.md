# Install and Distribution - Product Requirements Document

**Status:** In Review
**Created:** 2026-09-04
**Last Updated:** 2026-09-04
**Owner:** Justin Wilkin
**Stakeholders:** kora Core Team, Enterprise Platform Operators, Service Mesh and Workload Identity Owners, Security Reviewers, Evaluators

---

## Executive Summary

kora installs into any Kubernetes cluster from published, signed release artefacts using standard tooling: Helm or Argo CD. The service mesh and workload identity layers are prerequisites with a documented, verifiable contract; kora fits the mesh a platform team already runs and never installs its own. An evaluator brings a blank cluster to a running agent with one command. An enterprise platform team applies its own registry, scheduling, secret and security policy to every kora workload without forking anything. Tenants are declared and maintained automatically by a controller whose only job is tenants.

---

## Problem Statement

### Current State

A team that wants to run kora cannot install it from a release. There is no published artefact to point Helm or a GitOps tool at, no quickstart that works, and no statement of which Kubernetes, Istio or SPIRE versions kora supports. Installing kora today means handing a kora controller ownership of the mesh, workload identity and ingress, or extracting the install material from source and adapting it by hand with no documented configuration surface.

### User Pain Points

**Evaluators and small teams**
- I cannot find anything to install or a quickstart that gets me to a running agent.
- When the install fails, I cannot tell which step failed or why.
- The defaults do not work on my cluster and I do not know which settings to change.

**Enterprise platform teams**
- I cannot use the service mesh and workload identity I already run. kora assumes it brings its own.
- I cannot manage kora from my GitOps repository the way I manage everything else, so drift, review and rollback are unavailable to me.
- I cannot grant kora a permission set my security team will sign off.
- I cannot answer my security reviewer's questions: which versions are supported, whether the artefacts are signed, which images will be pulled, whether they are pinned.
- I cannot apply my cluster's registry mirror, node placement, pull secrets or secret management to kora without editing its install material by hand.

**Anyone upgrading kora**
- I have no version to upgrade to, no changelog to read, and no procedure to follow.

### Business Impact

- kora cannot be adopted as a standard Kubernetes product.
- Enterprise reviews stop at the boundary between kora and customer-owned infrastructure.
- Deployment forks accumulate configuration drift and upgrade risk.
- An open-source release without an installable distribution cannot be evaluated.

---

## Goals and Objectives

### Primary Goals

1. **Publish an installable release.** Every kora release includes signed artefacts, immutable image references, an image inventory, release notes, and compatibility information.
2. **Respect infrastructure ownership.** Production installation consumes Istio and SPIRE as prerequisites. kora does not install, upgrade, or remove them, and requires no change to them beyond the published prerequisite contract.
3. **Support GitOps directly.** Helm and Argo CD install, upgrade, roll back, and remove kora from the same declared release inputs.
4. **Provide one reference path.** Evaluation and local development use the released artefacts and the same prerequisite contract as production, with local values as the only variation.
5. **Narrow controller responsibility.** The in-cluster controller manages tenant lifecycle and attaches kora's enforcement to tenant workloads. It does not install the platform. Every permission it holds is documented with its reason.
6. **Fit the customer cluster.** Operators configure image sourcing, pull credentials, secrets, placement, resources, priority, and ingress without editing release templates.
7. **Fit any standard Istio.** kora requires only standard sidecar injection and per-pod proxy configuration from the mesh, so it can run on self-managed and managed Istio offerings. An offering is listed as supported only after release validation passes on it.
8. **Fail closed.** No tenant workload runs without kora's enforcement attached, and an unsatisfied prerequisite is reported as a failed condition before tenants are admitted.

### Success Metrics

All metrics are owned by the kora Core Team and measured in release validation unless stated.

- **Time to first run.** From a cluster that meets the tool prerequisites to a command showing a completed sample agent run, following only the quickstart: 30 minutes or less. Measured by a timed walkthrough on each release.
- **Existing mesh and identity.** A cluster with independently operated Istio and SPIRE that satisfy the prerequisite contract installs kora with no change to either system beyond that contract, and the preflight reports the contract satisfied.
- **No manual steps.** Helm and the reference install complete a documented installation, upgrade and rollback with no step outside their own workflow. Argo CD configurations render and validate against every release.
- **Verifiable release.** Every release publishes signed artefacts, digest-pinned images, a machine-readable image inventory, release notes, and a supported-version matrix.
- **Validated compatibility.** Release validation renders the artefacts against every supported Kubernetes version and installs them against every supported Istio version.
- **Restricted admission.** Every kora workload, including sidecars and the agent sandbox, admits under the Kubernetes restricted Pod Security Standard without exemptions.
- **Local parity.** Installation defects found in a customer cluster that the reference path could have caught: zero per release.

### Non-Goals

- Managing Istio, SPIRE, PostgreSQL, Redis, ingress controllers, DNS, TLS, or customer secret-management systems in production.
- Istio ambient mode for kora's namespaces. kora runs in sidecar-injected namespaces; a mesh that uses ambient elsewhere is acceptable when kora's namespaces are sidecar-injected.
- Changing how enforcement is attached inside the mesh.
- Gateway API `HTTPRoute` rendering ([#265](https://github.com/clear-route/orbit/issues/265)). Teams standardised on Gateway API attach kora's routes to an Istio Gateway they own in v1.
- Corporate egress proxy support for kora's outbound calls. Outbound destinations are documented; proxy traversal is future work.
- Database schema migration. A separate concern; this PRD covers installing, upgrading and rolling back kora's workloads.
- Multi-cluster, multi-region, or OpenShift packaging.
- A second publication channel or a standalone installer CLI.
- Exercising an Argo CD install in release validation. Future improvement; v1 validates the configuration by rendering.
- Flux support. Not in scope for v1; the reference charts are standard Helm charts and work under Flux, but no example is published or maintained.

---

## User Stories

### US-001: Evaluate kora

**As an** evaluator, **I want to** follow one supported quickstart, **so that** I can assess a working kora environment without first becoming a kora deployment expert.

**Acceptance Criteria:**
- [ ] The quickstart lists the required tools, the minimum cluster size, the credentials needed (an identity provider for sign-in and at least one model provider), the commands, expected outcomes, and recovery actions.
- [ ] The reference install installs prerequisites in the required order and installs kora from a published release artefact, and includes an identity provider suitable for evaluation.
- [ ] The completed path exposes the configured dashboard and API endpoints and verifies a completed agent run.
- [ ] The quickstart ends with a teardown that removes everything it created.

### US-002: Bring an existing mesh and identity system

**As a** platform operator, **I want to** install kora into a cluster that already runs Istio and SPIRE, **so that** the teams responsible for those systems retain their operational ownership.

**Acceptance Criteria:**
- [ ] Production installation does not install, upgrade, or remove Istio or SPIRE.
- [ ] Every release states the supported Istio and SPIRE versions and every constraint kora places on them.
- [ ] Preflight identifies an unsatisfied prerequisite before any tenant workload is admitted and reports the corrective action.
- [ ] kora requires no change to the customer's Istio control plane beyond the published prerequisite contract.
- [ ] kora follows the customer's revision-based Istio upgrades without reinstalling.

### US-003: Operate kora through GitOps

**As a** platform operator using Argo CD, **I want to** manage kora as versioned declarations in my repository, **so that** review, reconciliation, rollback, and drift handling work like the rest of my cluster.

**Acceptance Criteria:**
- [ ] The Argo CD example consumes the same versioned artefacts as Helm.
- [ ] The examples cover installation, upgrade, rollback, uninstall, and dependency ordering.
- [ ] A GitOps reconciliation completes without an out-of-band installation action.
- [ ] Uninstalling kora retains tenant declarations and tenant namespaces.
- [ ] The release lists every cluster-scoped resource it creates, the minimum permissions the installer needs, and anything kora changes on objects it does not own.

### US-004: Meet cluster policy without a fork

**As a** platform operator in a restricted cluster, **I want to** configure kora through supported release values, **so that** it passes admission and scheduling policies without locally edited manifests.

**Acceptance Criteria:**
- [ ] A private registry and pull credentials apply to every image any kora workload runs, including third-party images kora injects and workloads kora creates at run time.
- [ ] Global and component-specific settings cover placement, resources, priority, and topology requirements, including workloads created at run time.
- [ ] kora consumes credentials only from Kubernetes Secrets the customer creates by any means, and the configuration reference documents every key kora reads.
- [ ] Every kora workload, including sidecars and the agent sandbox, meets the Kubernetes restricted Pod Security Standard.
- [ ] kora installs and runs on a cluster whose network layer enforces a default-deny baseline, using only the network policies kora ships, and the release documents every network flow kora needs so a platform team can permit them in its own policies.
- [ ] kora's outbound calls work when the mesh restricts egress to registered destinations, with the required destination registrations shipped or documented.

### US-005: Review and upgrade a release

**As a** security reviewer or platform operator, **I want to** verify a release and understand its upgrade requirements, **so that** I can approve and operate it with confidence.

**Acceptance Criteria:**
- [ ] Release artefacts have a documented verification procedure that works against a customer registry mirror without reaching the publishing registry.
- [ ] Image references are immutable and every release publishes a machine-readable image inventory.
- [ ] Release notes identify breaking changes, configuration changes, and upgrade prerequisites.
- [ ] The compatibility statement names the Kubernetes, Istio, SPIRE, Helm, and reference-install tool versions supported by the release.

### US-006: Reconcile tenant lifecycle independently

**As a** platform operator, **I want to** declare tenants through the supported API or GitOps resource, **so that** tenant onboarding and ongoing reconciliation do not depend on a platform-installation controller.

**Acceptance Criteria:**
- [ ] A tenant declaration, and the connections, OAuth clients and MCP servers declared with it, converge to the declared state without manual steps.
- [ ] A tenant can be bound to a namespace the platform team already owns.
- [ ] Deleting a tenant declaration or uninstalling kora retains the tenant namespace.
- [ ] The controller's permissions are limited to tenant lifecycle, attaching enforcement to tenant workloads, its own admission configuration, and read-only prerequisite verification.
- [ ] Enforcement attachment is highly available by default, and its effect when unavailable is limited to tenant namespaces and documented.

### US-007: Develop against the shipped distribution

**As a** contributor, **I want to** deploy locally with the released artefacts and prerequisite contract, **so that** installation defects surface on my machine rather than in a customer cluster.

**Acceptance Criteria:**
- [ ] Local deployment uses the reference install with local values as the only variation.
- [ ] No local-development configuration ships inside any release artefact or image.

### US-008: Migrate an existing deployment

**As a** platform operator running kora from copied install material, **I want** a documented path onto the published release, **so that** I stop carrying a fork.

**Acceptance Criteria:**
- [ ] The release documents the migration sequence and which resources are recreated.
- [ ] Tenant declarations and tenant namespaces survive the migration.

---

## Proposed Solution

### Distribution Model

A kora release contains the artefacts, configuration reference, compatibility matrix, provenance material, release notes, and image inventory required to install the control plane. The artefacts own kora resources and the kora configuration required by the runtime. They do not own the lifecycle of customer-provided infrastructure.

Istio and SPIRE remain mandatory runtime dependencies. The release defines the versions and configuration kora requires from each. Preflight validates that contract before tenants are admitted. The reference installation provisions compatible dependencies for evaluation and local development; production operators provide and operate their own.

The tenant controller remains in the distribution because tenant state requires continuous reconciliation. It manages tenants, attaches kora's enforcement to tenant workloads at admission, and verifies prerequisites. It does not install charts, create platform infrastructure, or reconcile mesh and identity dependencies.

### Constraints on Customer-Owned Systems

kora places these requirements on systems it does not own, and the prerequisite contract states each precisely per release:

- Istio and SPIRE share one trust domain, which kora adopts as its own.
- Istio runs in sidecar mode for kora's namespaces with the Istio CNI enabled.
- SPIRE issues workload identities to kora-governed workloads only through kora's registrations.
- The SPIFFE CSI driver and SPIRE controller manager are present.

These are the only changes a platform team may need to make to an existing mesh or identity system. kora's registrations apply to kora's namespaces and kora's gateway only.

### Install Paths

The reference install is a Helmfile that installs compatible prerequisites and the released kora artefacts in dependency order. It is the evaluator quickstart and the local-development deployment path.

The production path starts with a cluster that already meets the prerequisite contract. The operator stores the release version, values, and tenant declarations in Git and reconciles them with Helm or Argo CD. The same artefacts, values schema, release metadata, and verification procedure apply to both paths.

Supported ingress topologies: an Istio Gateway kora creates, an Istio Gateway the customer owns, and a Kubernetes Ingress in front of either.

Upgrade means moving an installed kora to a newer release through the same tool that installed it, with the same declared inputs, and being able to roll back to the previous release.

### Security Boundaries

Installation moves where enforcement is delivered. Workload identity, policy evaluation before execution, and brokered credentials remain mandatory in every supported topology.

---

## Scope

### In Scope

- Published, versioned, signed artefacts and release metadata.
- Helm installation and upgrade guidance plus an Argo CD example.
- A release-specific Istio and SPIRE prerequisite contract, preflight validation, and compatibility matrix.
- A reference installation for evaluation and local development that uses the released artefacts.
- A documented configuration surface for image sources, pull credentials, secrets, placement, resources, priority, and the supported ingress topologies.
- Tenant-only reconciliation, enforcement attachment at admission, and defined lifecycle behaviour for tenant resources and namespaces.
- Release verification, upgrade, rollback, uninstall, and troubleshooting guidance.
- Release validation against every declared Kubernetes and Istio version.
- A documented list of every network flow kora's workloads need, inbound and outbound, and the destination registrations for registry-only mesh egress.
- A migration path for deployments running from copied install material.
- A single registry-authentication step in the quickstart while release artefacts remain private.

### Out of Scope

- A general-purpose Kubernetes platform operator.
- Lifecycle management of customer-owned mesh, identity, database, cache, ingress, or secret-management systems.
- New identity, policy, or credential-brokering capabilities unrelated to distribution.
- Deployment variants listed in the non-goals above.

### Future Considerations

- Public artefact distribution after the repository opens. The private-preview registry authentication step is removed at that point; nothing else changes.
- Naming managed Istio offerings as supported after each passes release validation.
- An Argo CD install exercised in release validation.
- Flux support, if a customer need for it appears.
- Corporate egress proxy support.
- Ambient mesh and Gateway API support after the enforcement model for those environments receives its own approved ADR.

---

## Alternatives Considered

### Keep the controller-driven install

The in-cluster controller continues to install the mesh, identity system and kora from one platform definition. Rejected: enterprises will not delegate ownership of their mesh or identity system, GitOps tooling cannot see what the controller installs, and the controller needs near cluster-admin permission permanently.

### No controller

Tenants configured through settings or scripts. Rejected: keeping a tenant correct over time is continuous work, and scripts reintroduce the manual steps this change removes.

### Controller installs prerequisites on request for evaluators

Rejected: two install paths maintained by one team, with the secondary path decaying. The reference install gives evaluators the same convenience without the privilege.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] Every release installs, upgrades and rolls back through Helm from published artefacts.
- [ ] A documented Argo CD configuration consumes the same release inputs.
- [ ] Production installation never manages the lifecycle of Istio or SPIRE and requires no change to them beyond the prerequisite contract.
- [ ] Preflight validates the prerequisite contract and returns actionable failures before tenant workloads are admitted.
- [ ] The configuration reference identifies defaults, required fields, supported overrides, and every Secret key kora reads.
- [ ] Registry, pull, scheduling, resource, and secret settings apply to every kora workload, including workloads created at run time and images kora injects.
- [ ] The three ingress topologies are supported with documented DNS and TLS requirements.
- [ ] The controller's permissions are limited to tenant lifecycle, enforcement attachment, its own admission configuration, and read-only prerequisite verification, each documented with its reason.
- [ ] The quickstart, local deployment, and GitOps examples use the published artefacts rather than duplicate manifests.
- [ ] Every release publishes provenance information, immutable image references, an image inventory, release notes, and compatibility information.
- [ ] Every kora workload meets the Kubernetes restricted Pod Security Standard, and tenant namespaces enforce it by default.
- [ ] kora runs under a default-deny network baseline with its shipped policies; every required flow is documented; disabling kora's policies is an explicit setting whose lost isolation is stated.
- [ ] kora's outbound calls work under registry-only mesh egress, with destination registrations shipped or documented.

**Should Have (P1):**
- [ ] A post-install preflight command for troubleshooting and drift detection.
- [ ] A single command that collects the state needed to diagnose an install or preflight failure.
- [ ] Release automation that generates release notes and image inventories.

### Non-Functional Requirements

**Security**
- Workload identity, policy enforcement, and credential brokering remain mandatory in every supported deployment topology.
- Release provenance and image immutability are independently verifiable, including from a customer registry mirror.
- Failure to attach kora's enforcement to a tenant workload rejects that workload.

**Compatibility**
- Every release declares its supported Kubernetes, Istio, SPIRE, Helm, and reference-install tool versions.
- Release validation renders against every declared Kubernetes version and installs against every declared Istio version, on a network layer that enforces policy.
- Each release states its support window, security patch cadence, and supported upgrade paths.

**Operability**
- Install, preflight, upgrade, rollback, and uninstall failures expose a meaningful condition, event, or command result.
- The release documents the ownership boundary for every resource it creates or changes.
- Every workload exposes metrics and traces to the customer's collectors through documented values; logs are structured.

**Reliability**
- An unsatisfied prerequisite prevents tenant workloads from being admitted and is reported as a failed condition. kora's control-plane services remain available for diagnosis.
- Enforcement attachment runs highly available by default. When it is unavailable, only tenant workload creation is affected, and the release states what an operator sees.

---

## Dependencies

### Internal Dependencies

- kora install artefacts and custom resource definitions.
- Tenant lifecycle reconciler, admission webhook, and tenant resource definitions.
- Release automation for publication, signing, compatibility testing, image inventory generation, and release notes.

### External Dependencies

- A supported Kubernetes version.
- A compatible Istio sidecar deployment with the Istio CNI.
- SPIRE configured to issue the SVIDs and expose the workload API required by kora.
- An OIDC identity provider for human sign-in; the reference installation includes one for evaluation.
- Credentials for at least one supported model provider.
- Operator-provided PostgreSQL, Redis, DNS, TLS, registry access, and secrets.
- A package registry, a signing key, and the reference-install tool.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
| --- | --- | --- | --- |
| A supported Istio release changes behaviour required by kora enforcement. | High | Medium | Exercise every supported version in release validation and withdraw unsupported versions. |
| Meeting the restricted profile forces changes to the agent sandbox. | High | Medium | Validate against Pod Security Admission before release and resolve the constraint in the sandbox rather than exempt the workload. |
| Reference and production paths diverge. | High | Medium | Make the reference path consume released artefacts and values; exercise it in release validation. |
| Enforcement attachment becomes a single point of failure for tenant workloads. | High | Low | Two replicas and a disruption budget by default; effect limited to tenant namespaces; documented operator view. |
| Removing platform lifecycle from the controller leaves an unclear upgrade path. | Medium | Medium | Publish ownership boundaries, compatibility, ordering, rollback, and migration guidance with every release. |
| Private-preview artefacts slow evaluation. | Low | High | Document one registry-authentication step outside the long-term installation contract. |

---

## Delivery

Single release.

---

## Appendix

### Related Documents

- [kora architecture](../../ARCHITECTURE.md)
- [Deployment guide](../guides/deployment.md)
- [Helm Chart Distribution and GitOps Repository Structure](../adr/2026-03-31-helm-chart-distribution-and-gitops-repository-structure.md)
- [kora Kubernetes Operator](../adr/2026-03-31-orbit-kubernetes-operator.md)
- [SPIFFE and SPIRE](../spiffe.md)

### Glossary

- **Prerequisite contract:** The release-specific configuration and version requirements kora places on systems it does not own.
- **Preflight:** Validation that reports whether a cluster meets the prerequisite contract before tenant workloads are admitted.
- **Reference installation:** The supported composition used for evaluation and local development. It uses the same kora release artefacts as production.
- **Release artefact:** A versioned chart or accompanying provenance, compatibility, and inventory material published for a kora release.
- **Enforcement attachment:** Adding kora's identity and policy sidecars to a tenant workload when it is created.
