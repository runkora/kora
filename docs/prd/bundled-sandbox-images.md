# Bundled Sandbox Images - Product Requirements Document

**Status:** Approved  
**Created:** 2026-10-07  
**Last Updated:** 2026-10-07  
**Owner:** Platform team  
**Stakeholders:** Platform engineering teams, agent builders, kora maintainers  

---

## Executive Summary

kora will ship ready-made sandbox images for Go, Python and Node with each release and make them available in every tenant. kora will also publish an image with its own development tools for the agents that work on kora.

---

## Problem Statement

### Current State

Every tenant receives one sandbox image from kora: the kora base image with general command-line tools, Go and Python. Any other toolchain needs a platform team to build an image on the base image in its own pipeline and register it in each tenant.

### User Pain Points

*   A platform team that wants a Node sandbox, or Go and Python linters and code generators, has to set up an image build before any agent can use them.
*   Every organization builds the same language images on the same base.
*   Agents that work on kora install kora's development tools at the start of each run.

### Business Impact

None at this time.

---

## Goals and Objectives

### Primary Goals

1.  A new kora installation offers Go, Python and Node sandbox images in every tenant without an image pipeline of its own.
2.  Platform teams decide which bundled images their tenants receive and which ones stay warm.
3.  The kora development image carries the development tools at the versions kora's repository pins and the Node LTS release of the Node image.

### Success Metrics

None at this time.

### Non-Goals

*   Removing Go or Python from the kora base image.

---

## User Stories

### Primary User Stories

**As a** platform engineer,  
**I want to** receive Go, Python and Node sandbox images with kora and turn each one on or off,  
**So that** agents get common toolchains without an image pipeline of our own.

**Acceptance Criteria:**
*   [ ] Each kora release publishes the Go, Python and Node images at the release's version.
*   [ ] Every tenant receives each bundled image that is turned on.
*   [ ] Turning a bundled image off removes it from every tenant.
*   [ ] Every bundled image is turned on by default.
*   [ ] Warming is set per bundled image and is off by default.
*   [ ] A platform team can point each bundled image at its own copy of the image.
*   [ ] A tenant can still register its own images alongside the kora base image and the bundled images.
*   [ ] A tenant image that uses the name of a bundled image stays in place, and that tenant does not receive the bundled image.

---

**As a** user with environment permissions,  
**I want to** create an environment from a bundled image,  
**So that** an agent gets a Go, Python or Node toolchain.

**Acceptance Criteria:**
*   [ ] The bundled images appear with their descriptions among the images an environment can use.

---

**As a** kora maintainer,  
**I want to** run kora's own agents on an image with kora's development tools at the versions kora pins,  
**So that** those agents build and test kora without installing tools in each run.

**Acceptance Criteria:**
*   [ ] Each kora release publishes the kora development image.
*   [ ] kora's own deployment registers the kora development image like any other tenant image.

### Secondary User Stories

None at this time.

---

## Proposed Solution

### High-Level Approach

kora will build three language images on the kora base image and release them with every version of kora. The kora installation will register each one that is turned on in every tenant, in the same way it registers the kora base image today. The base image keeps Go and Python, and each language image adds the extra tools for its language.

### User Experience

#### User Flow

1.  A platform team installs kora with the bundled images turned on.
2.  Every tenant lists the kora base image, the Go image, the Python image and the Node image.
3.  A user with environment permissions creates a "Node apps" environment from the Node image.
4.  An agent attached to "Node apps" provisions a sandbox with Node and pnpm installed.

### Key Components

*   **Go image:** The base image plus Go linting and code generation tools.
*   **Python image:** The base image plus Python linting and test tools.
*   **Node image:** The base image plus Node and pnpm.
*   **kora development image:** The base image plus kora's pinned development tools.

---

## Scope

### In Scope

*   The Go, Python and Node images, their release with kora, and their registration in every tenant.
*   Per-image settings for turning each bundled image on or off and for warming.
*   The kora development image.

### Out of Scope

*   Registering the kora development image through the kora installation.

### Future Considerations

None at this time.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
*   [ ] Each bundled image carries the same version as the kora release that publishes it.
*   [ ] Each bundled image passes the existing validation for sandbox images.
*   [ ] The bundled images are available for every processor architecture the kora base image supports.

### Non-Functional Requirements

**Performance:**
None at this time.

**Security:**
*   The bundled images and the kora development image are signed with the other images of the release.

**Reliability:**
*   An environment whose bundled image is turned off cannot start a new sandbox, and the agent receives a message that the image is not registered.

**Scalability:**
None at this time.

**Observability:**
None at this time.

---

## Dependencies

### Internal Dependencies

*   [Sandbox environments](sandbox-environments.md): the kora base image, sandbox image registration and validation, and warming.

### External Dependencies

None at this time.

---

## Risks and Mitigations

None at this time.

---

## Alternatives Considered

### Alternative 1: Documented Examples Only

**Description:** kora would document example images that platform teams build themselves.

**Why not chosen:** Every organization would rebuild the same images.

### Alternative 2: Language Runtimes Out of the Base Image

**Description:** Go and Python would move from the base image into the language images.

**Why not chosen:** Agents would lose the common scripting languages in the default environment.

### Alternative 3: kora Development Image in the Installation

**Description:** The kora installation would register the kora development image in every tenant.

**Why not chosen:** The image serves kora development and does not serve tenants.

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

*   [Bundled sandbox images ADR](../adr/2026-10-07-bundled-sandbox-images.md)
*   [Sandbox environments PRD](sandbox-environments.md)
*   [Sandbox environments ADR](../adr/2026-10-03-sandbox-environments.md)

### Glossary

None at this time.
