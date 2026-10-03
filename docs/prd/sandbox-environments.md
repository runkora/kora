# Sandbox Environments - Product Requirements Document

**Status:** Draft  
**Created:** 2026-10-03  
**Last Updated:** 2026-10-03  
**Owner:** Platform team  
**Stakeholders:** Platform engineering teams, agent builders  

---

## Executive Summary

An environment is a prepackaged sandbox for agents: an image, a size, egress hosts and connectors. Platform teams build sandbox images on a base image that kora publishes and register them through GitOps. Agents attach one or more environments and pick one each time they provision a sandbox.

---

## Problem Statement

### Current State

Every sandbox runs one image that is set at install time. Each run starts with empty package and build caches. Environments today group egress hosts and connectors for reuse across agents.

### User Pain Points

*   Builds repeat the same downloads on every run. In prod, one Go test ran for 17 minutes without a result. A backend build took 24 minutes, mostly downloading modules.
*   The image lacked a code generator. One agent spent 15 minutes building it from source.
*   A general agent gets the same sandbox for every repository.
*   Platform teams cannot manage sandbox tooling through GitOps.

### Business Impact

*   Runs spend time and inference on setup work.
*   In one run, the verdict came from reading code. The agent ran out of time to reproduce the defect.

---

## Goals and Objectives

### Primary Goals

1.  Platform teams package sandboxes with the tools their agents need, through GitOps.
2.  Agents start a task with their toolchain installed.
3.  No run can change what another run receives.

### Success Metrics

None at this time.

### Non-Goals

None at this time.

---

## User Stories

### Primary User Stories

**As a** platform engineer,  
**I want to** build sandbox images on the kora base image and register them through GitOps,  
**So that** agents get our toolchains without changes to kora.

**Acceptance Criteria:**
*   [ ] kora publishes a versioned base image.
*   [ ] Each sandbox image is registered as its own resource in the platform team's infrastructure repository.
*   [ ] Each registered image reports whether it is valid and ready to use.

---

**As a** user with environment permissions,  
**I want to** create environments from registered images,  
**So that** agents get a sandbox prepared for their work.

**Acceptance Criteria:**
*   [ ] The Environments page creates, edits and deletes environments.
*   [ ] An environment without a selected image shows that it uses the kora base image.

---

**As an** agent builder,  
**I want to** attach one or more environments to an agent,  
**So that** a specialized agent gets one sandbox and a general agent can choose.

**Acceptance Criteria:**
*   [ ] New agents have the default environment attached.
*   [ ] An agent with several environments marks one as its default.
*   [ ] An agent can add egress hosts and connectors on top of its environments.

---

**As an** agent,  
**I want to** choose an environment when I provision a sandbox and switch when the task changes,  
**So that** I work with the right tools without installing them.

**Acceptance Criteria:**
*   [ ] Provisioning lists the agent's environments with their descriptions.
*   [ ] Provisioning without a choice uses the agent's default environment.
*   [ ] Switching environments keeps the files in the run's workspace.

### Secondary User Stories

**As a** kora maintainer,  
**I want to** run sandbox benchmarks for Go and Node,  
**So that** I can measure sandbox performance before and after a change.

**Acceptance Criteria:**
*   [ ] Benchmarks time sandbox start, clone, dependency install, build and test.
*   [ ] Benchmarks run against the local cluster.

---

## Proposed Solution

### High-Level Approach

An environment becomes the base an agent works from. kora publishes a base image. Platform teams build images on it in their own pipelines and register each one. An environment combines a registered image with a size, egress hosts, connectors and a description. Agents attach environments and compose further access on top.

### User Experience

#### User Flow

1.  A platform engineer builds `acme-go` on the kora base image and registers it through GitOps.
2.  A user with environment permissions creates a "Go services" environment from `acme-go`.
3.  An agent builder attaches "Go services" and "Node apps" to a general coding agent, with "Go services" as its default.
4.  A run starts. The agent provisions a sandbox in "Go services" and builds.
5.  The task turns to the frontend. The agent switches to "Node apps". Its files stay in the workspace.

### Key Components

*   **Base image:** Published by kora. It carries what every sandbox needs to work with kora, the command-line tools agents use generally, and Go and Python for scripting. It carries no Node.
*   **Sandbox image:** An image built on the base image and registered by a platform team, one resource per image.
*   **Environment:** A registered image, a size, egress hosts, connectors and a description.
*   **Default environment:** Created for every tenant. It uses the base image and is attached to new agents.
*   **Run-local build space:** Build outputs, installed dependencies and tools on the sandbox's own disk.
*   **Benchmarks:** Fixed Go and Node tasks that time each stage of sandbox work.

---

## Scope

### In Scope

*   The base image.
*   Sandbox image registration through GitOps.
*   Environments redefined as prepackaged sandboxes, on the Environments page, API and MCP tools.
*   A default environment per tenant.
*   Several environments per agent, and environment selection and switching during a run.
*   Run-local build space.
*   Go and Node benchmarks.

### Out of Scope

*   Building images inside kora.
*   Per-run setup hooks.
*   Public GitHub access without a connection credential. A separate change covers it.
*   Moving existing environments into the new model.

### Future Considerations

*   A repository file such as `.kora/sandbox.yaml` that selects the environment for that repository.
*   Building environment images inside kora.
*   Benchmarks as an integration test for sandbox performance.
*   Triggers and run starts that select the starting environment.
*   A package cache shared across runs, if the benchmarks show the need.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
*   [ ] A versioned base image with general command-line tools, Go and Python.
*   [ ] One registration resource per sandbox image.
*   [ ] Validation of each registered image against the base image, with the result on the resource.
*   [ ] Registered images kept warm on the nodes that run sandboxes.
*   [ ] Environments with a name, a description, an image, a size, egress hosts and connectors.
*   [ ] A configurable size per environment: CPU, memory and local disk.
*   [ ] Environment scopes for viewing, creating, editing and deleting environments.
*   [ ] Several environments per agent, one marked as default.
*   [ ] Agent access equal to the union of its environments plus its own egress hosts and connectors.
*   [ ] Selection limited to the agent's attached environments.
*   [ ] A record of the environment each sandbox used.
*   [ ] Go and Node benchmarks with a recorded baseline.

### Non-Functional Requirements

**Performance:**
None at this time.

**Security:**
*   Only registered images run as sandboxes.
*   Platform teams register images in their infrastructure repositories. The kora API and UI do not register images.
*   Build outputs, dependencies and tools a run installs stay on that run's sandbox.

**Reliability:**
*   An image that fails validation cannot be selected. Its status states the failure.

**Scalability:**
None at this time.

**Observability:**
*   Each sandbox start and switch records the environment and image.

---

## Dependencies

### Internal Dependencies

*   Operator: validates registered images and keeps them warm.
*   Issue #310: per-sandbox sizing.

### External Dependencies

*   A container registry for platform teams' images.

---

## Risks and Mitigations

*   **Combined access:** A general agent can reach every environment's hosts and connectors. Teams can create an agent per environment.

---

## Alternatives Considered

### Alternative 1: One Environment per Agent

**Description:** Each agent has exactly one environment.

**Why not chosen:**
*   General agents work across repositories with different stacks.

### Alternative 2: Access Limited to the Selected Environment

**Description:** A sandbox reaches only the hosts and connectors of the environment it runs in.

**Why not chosen:**
*   The team that builds an agent decides its reach by choosing its environments.

### Alternative 3: Copy the Workspace into Each Sandbox

**Description:** Each sandbox gets its own copy of the run's files on fast disk.

**Why not chosen:**
*   The agent edits files outside the sandbox. A copy separates those edits from the sandbox's builds.

### Alternative 4: Build Images Inside kora

**Description:** kora builds environment images from setup commands.

**Why not chosen:**
*   Platform teams manage images through GitOps.

### Alternative 5: Repository-Declared Tool Versions

**Description:** A version manager in the base image installs the versions a repository declares.

**Why not chosen:**
*   The base image packages the tools agents use generally. Platform teams add toolchains in their own images.

### Alternative 6: Per-Run Setup Hook

**Description:** A repository script runs at the start of every run.

**Why not chosen:**
*   Environments carry the setup.

---

## Open Questions

None at this time.

---

## Timeline and Milestones

### Phases

None at this time.

### Key Milestones

*   [ ] PRD Approved
*   [ ] ADR Created

---

## Appendix

### Related Documents

*   [Agent sandbox toolchain and connectivity environments](../adr/2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md)
*   [Sandbox provisioning and shell proxy](../adr/2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [JIT credential brokering egress gateway](../adr/2026-06-06-jit-credential-brokering-egress-gateway.md)
*   [CRD-driven inference providers](crd-inference-providers.md)

### Glossary

*   **Base image:** The image kora publishes for sandbox images to build on.
*   **Sandbox image:** An image a platform team builds on the base image and registers.
*   **Environment:** A prepackaged sandbox: image, size, egress hosts, connectors and description.
*   **Default environment:** The environment every tenant has and new agents get.
