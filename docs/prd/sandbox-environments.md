# Sandbox Environments - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-03  
**Last Updated:** 2026-10-03  
**Owner:** Platform team  
**Stakeholders:** Platform engineering teams, agent builders  

---

## Executive Summary

Sandbox environments will let platform teams prepackage the sandboxes their agents run in, and agents will choose the right sandbox for each task.

---

## Problem Statement

### Current State

Every sandbox runs the one image set at install time, and each run starts with empty package and build caches. An environment today groups egress hosts and connectors for reuse across agents.

### User Pain Points

*   Builds download the same packages on every run. In prod, one Go test ran for 17 minutes without a result, and a backend build spent most of its 24 minutes downloading modules.
*   The image lacked a code generator, and one agent spent 15 minutes building it from source.
*   A general agent gets the same sandbox for every repository.
*   Platform teams cannot manage sandbox tooling through GitOps.

### Business Impact

*   Runs spend time and inference on setup work.
*   In one run, the agent ran out of time to reproduce a defect and gave a verdict from reading the code.

---

## Goals and Objectives

### Primary Goals

1.  Platform teams package sandboxes with the tools their agents need through GitOps.
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
**I want to** build sandbox images on the kora base image and register them per tenant through GitOps,  
**So that** agents get our toolchains without changes to kora.

**Acceptance Criteria:**
*   [ ] kora publishes a versioned base image with general command-line tools, Go and Python.
*   [ ] A platform team registers each sandbox image as its own resource in its infrastructure repository.
*   [ ] Each registered image reports whether it is valid for use as a sandbox.
*   [ ] A platform team turns warming on or off for each image.

---

**As a** user with environment permissions,  
**I want to** create environments from registered images,  
**So that** agents get a sandbox prepared for their work.

**Acceptance Criteria:**
*   [ ] The Environments page creates, edits and deletes environments.
*   [ ] An environment without a selected image shows that it uses the kora base image.
*   [ ] An environment sets its own CPU, memory and local disk within platform limits.

---

**As an** agent builder,  
**I want to** attach one or more environments to an agent,  
**So that** a specialized agent gets one sandbox and a general agent can choose.

**Acceptance Criteria:**
*   [ ] An agent with several environments marks one as its default.
*   [ ] An agent with no environments runs on the kora base image.
*   [ ] An agent can add egress hosts and connectors on top of its environments.

---

**As an** agent,  
**I want to** list my environments, provision a sandbox in one of them, and replace it with another when the task changes,  
**So that** I work with the right tools without installing them.

**Acceptance Criteria:**
*   [ ] A tool lists the agent's environments with their descriptions and the default.
*   [ ] Provisioning without a choice uses the agent's default environment.
*   [ ] A separate tool replaces the sandbox with one in another environment and keeps the files in the run's workspace.

### Secondary User Stories

None at this time.

---

## Proposed Solution

### High-Level Approach

kora will publish a base image, and platform teams will build images on it in their own pipelines. Users with environment permissions will turn registered images into environments that agents attach.

### User Experience

#### User Flow

1.  A platform engineer builds `acme-go` on the kora base image and registers it in a tenant through GitOps.
2.  A user with environment permissions creates a "Go services" environment from `acme-go`.
3.  An agent builder attaches "Go services" and "Node apps" to a general coding agent with "Go services" as its default.
4.  A run starts, and the agent provisions a sandbox in "Go services" and builds.
5.  The task turns to the frontend, and the agent replaces its sandbox with one in "Node apps". Its files stay in the workspace.

### Key Components

*   **Base image:** The image kora publishes with the command-line tools agents use generally, Go and Python. It does not include Node.
*   **Sandbox image:** An image that a platform team builds on the base image and registers in a tenant.
*   **Environment:** A registered image, a size, egress hosts, connectors and a description.
*   **Run-local build space:** The sandbox's own disk for build outputs, installed dependencies and tools.

---

## Scope

### In Scope

*   The base image, sandbox image registration, environments, the agent environment tools and run-local build space.

### Out of Scope

*   Per-run setup hooks.
*   Public GitHub access without a connection credential. A separate change covers that access.
*   Moving existing environments into the new model.

### Future Considerations

*   A repository file such as `.kora/sandbox.yaml` that selects the environment for that repository.
*   Building environment images inside kora.
*   Triggers and run starts that select the starting environment.
*   A package cache shared across runs if measurements show the need.
*   Sandbox performance integration tests in a production-like environment.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
*   [ ] Validation of each registered image against the base image.
*   [ ] Warming on the sandbox nodes for each image that has warming turned on.
*   [ ] Environment scopes for viewing, creating, editing and deleting environments.
*   [ ] Agent access equal to the union of its environments plus its own egress hosts and connectors.
*   [ ] Provisioning limited to the agent's attached environments.
*   [ ] A record on the run of the environment and image each sandbox used.

### Non-Functional Requirements

**Performance:**
None at this time.

**Security:**
*   Only registered images run as sandboxes.
*   Platform teams register images in their infrastructure repositories, and the kora API and UI do not register images.
*   Build outputs, dependencies and tools that a run installs stay on that run's sandbox.

**Reliability:**
*   An image that fails validation cannot be selected, and its status states the failure.

**Scalability:**
None at this time.

**Observability:**
None at this time.

---

## Dependencies

### Internal Dependencies

*   The operator validates and warms registered images.
*   Issue #310 covers per-sandbox sizing.

### External Dependencies

*   Platform teams publish their images to a container registry.

---

## Risks and Mitigations

*   **Combined access:** A general agent can reach the hosts and connectors of every attached environment. Teams can create one agent per environment for narrower access.

---

## Alternatives Considered

### Alternative 1: One Environment per Agent

**Description:** Each agent would have exactly one environment.

**Why not chosen:** General agents work across repositories with different stacks.

### Alternative 2: Access Limited to the Selected Environment

**Description:** A sandbox would reach only the hosts and connectors of the environment it runs in.

**Why not chosen:** The team that builds an agent decides its reach when it chooses the agent's environments.

### Alternative 3: Copy the Workspace into Each Sandbox

**Description:** Each sandbox would get its own copy of the run's files on fast disk.

**Why not chosen:** The agent edits files outside the sandbox, and a copy would separate those edits from the sandbox's builds.

### Alternative 4: Build Images Inside kora

**Description:** kora would build environment images from setup commands.

**Why not chosen:** Platform teams manage images through GitOps.

### Alternative 5: Repository-Declared Tool Versions

**Description:** A version manager in the base image would install the versions a repository declares.

**Why not chosen:** The base image packages the tools agents use generally, and platform teams add toolchains in their own images.

### Alternative 6: Per-Run Setup Hook

**Description:** A repository script would run at the start of every run.

**Why not chosen:** Environments carry the setup.

---

## Open Questions

None at this time.

---

## Timeline and Milestones

### Phases

None at this time.

### Key Milestones

*   [x] PRD Approved
*   [x] ADR Created

---

## Appendix

### Related Documents

*   [Sandbox environments ADR](../adr/2026-10-03-sandbox-environments.md)
*   [Agent sandbox toolchain and connectivity environments](../adr/2026-06-14-agent-sandbox-toolchain-and-connectivity-environments.md)
*   [Sandbox provisioning and shell proxy](../adr/2026-05-11-sandbox-provisioning-and-shell-proxy.md)
*   [CRD-driven inference providers](crd-inference-providers.md)

### Glossary

None at this time.
