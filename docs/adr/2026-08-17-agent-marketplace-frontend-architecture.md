# Agent Marketplace Frontend Architecture

**Status:** Proposed  
**Date:** 2026-08-18  
**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)  
**Technical Story:** Agent Marketplace: Skills PRD

---

## Context

Orbit needs a read-only Marketplace under Control Plane where approved human users can select a marketplace, browse and search its Skills, and review Skill details. The frontend must not install, run, assign, update, or remove Skills.

This ADR decides how the frontend loads data, divides responsibilities between server and client code, manages browse state, and presents results and errors.

### Background

Orbit's frontend uses the Next.js App Router, React Server Components, and same-origin Backend-for-Frontend routes. The BFF reads the sealed user session and calls authorised Orbit services.

All product JSX uses Terra (`@orbit/terra`), Terra is a design system. Shared frontend components follow Orbit's three-tier component model. The Marketplace must use these existing patterns instead of adding another state library, design system, or direct source-provider integration.

The frontend consumes `skills-service` through a server-only Marketplace client. It does not call GitHub or access backend storage directly. All repository content is treated as unsafe, read-only data.

All three options consume the same backend API. Offset/limit pagination is an integration constraint, not a separate frontend architecture choice.

### Problem Statement

Choose a frontend architecture that:

1. Includes Marketplace names in the initial server render. If the page first loads with a valid Marketplace already specified in the URL, the server also loads that Marketplace’s Skill results; if the user selects a Marketplace from the dropdown after the page has loaded, the frontend requests its Skills through the BFF.
2. Supports marketplace selection, name search, a user-selected result limit, and pagination without loading the complete dataset into the browser.
3. Restores the selected view from the URL during refresh, browser Back and Forward navigation, and direct navigation.
4. Keeps Orbit tokens and Marketplace source credentials outside the client-side.
5. Loads read-only Skill details on a dedicated route without an additional browser data request.
6. Ensures that only the latest active browse request can update the displayed results.


### Goals

* Add a Marketplace overview route under Control Plane and a dedicated Skill detail for when a skill is selected from the overview to view.
* Use server rendering for initial data and BFF requests for later browse updates.
* Keep the URL, displayed results, and pagination position consistent.
* Render Skill content and supplied resource files read-only.
* Reuse Terra and Orbit's authentication, component-placement, and testing patterns.
* Support desktop, tablet, mobile.

### Non-Goals

* Choosing or restating backend source access, credentials, caching, storage, refresh, or search implementation.
* Installing, running, assigning, updating, rolling back, or removing packages.
* Adding packages to agents, AgentTypes, environments, or runs.
* Adding separate repository-file preview or download interactions. Rendering content already supplied by the detail response remains in scope.
* Creating a new frontend framework or state library.
* Managing Marketplace sources, displaying source health, or adding sorting and filters absent from the backend contract.

---

## Decision Drivers

* Orbit access and refresh tokens must remain server-side.
* The Marketplace frontend must reuse Orbit's existing authentication, session-scope, navigation-gating, and page-guard mechanisms.
* Repository content must remain read-only and must not execute in the browser.
* The user must be able to choose a requested result limit within the backend contract.
* The page must restore the selected marketplace, search text, number of results per page, and pagination position from the URL when users refresh the page, use the browser's Back or Forward buttons, or open a direct link.
* Skills with the same display name in different marketplaces must remain distinguishable during navigation.
* Marketplace failures must not affect unrelated Orbit pages.


---

## Considered Options

### Option 1: RSC initial rendering with BFF-driven browse updates

**Description:** The overview Server Component loads the Marketplace names for the initial page render. If a valid Marketplace is already selected in the URL, it also loads that Marketplace's Skill results. If no Marketplace is selected, no Skills are loaded and the page prompts the user to choose one from the Marketplace dropdown. After the page has loaded, Marketplace selection, search, result-limit changes, and pagination use the BFF to request updated Skill results without re-rendering the route. Skill details use a dedicated server-rendered route.


**Pros:**


* The Marketplace selector is populated during the initial server render without requiring an extra browser request.
* If a Marketplace is already selected in the URL, its Skill results are included in the initial server render.
* After the page has loaded, Marketplace selection, search, and pagination can update only the Skill results area instead of re-rendering the route.
* Reuses Orbit's existing React Server Component and BFF architecture.

**Cons:**

* The frontend must support two data-loading paths: server rendering for the initial page load and BFF requests for later user interactions.
* The Client Component must manage updated Skill results, loading states, pagination, and URL changes after the page has loaded.
* The frontend must prevent older requests from replacing newer results when users quickly change Marketplace, search, or pagination.


### Option 2: RSC navigation for every interaction

**Description:** The initial Marketplace page is rendered by the Server Component. When the user selects a Marketplace, searches, changes the result limit, or moves between pages, the frontend updates the URL through Next.js navigation. Each navigation causes the Server Component to load the required Skill results and return updated page content. No separate BFF request is used for browse updates.

**Pros:**

* Uses the same server-rendered data-loading path for both the initial page load and later browse interactions.
* Marketplace selection, search, result limit, and pagination are stored in the URL, and Next.js navigation reloads the page state from those values.
* Requires less client-side request and result-state management than Option 1.
* Direct links, refreshes, and browser Back and Forward navigation restore the Marketplace view from the values stored in the URL.

**Cons:**

* Every time the user changes the Marketplace, search, or page, Next.js has to navigate to a new URL and run the Server Component again before new results are shown.
* Quick changes to search or filters can start multiple server renders, including renders for states.
* Each browse change causes the Marketplace route to render again, instead of updating only the Skill results section.


### Option 3: Client-only Marketplace application

**Description:** The initial server response renders only the Marketplace page structure. After the page loads in the browser, a Client Component requests the Marketplace names through the BFF. The same Client Component then uses the BFF again whenever the user selects a Marketplace, searches, changes the result limit, or changes page, and updates the displayed Skill results with each response.

**Pros:**

* The Client Component sends a request to the BFF to load Marketplace data, both when the page first loads and during later user interactions such as selecting a Marketplace, searching, or changing page.
* Keeps Marketplace loading and interaction state in one Client Component.
* Marketplace selection, search, and pagination can update the Skill results area without re-rendering the route.


**Cons:**

* Marketplace names are loaded only after the page has opened in the browser and the Client Component has sent its first request to the BFF.
* Because no Marketplace data is included in the initial server render, the selector may show a loading state before the Marketplace names appear.
* The Client Component must manage the first Marketplace data request as well as later Marketplace selection, search, pagination, loading, and error states.
* If a user opens a direct link with a Marketplace already selected, the Skill results are still loaded only after the browser starts and sends a request to the BFF.

---

## Decision

The overview Server Component loads marketplace names and, when the URL selects an available marketplace, it's selected Skill page. The interactive overview receives that initial data and owns later marketplace-selection, search, requested-limit, and pagination changes. When the page becomes interactive in the browser, the Client Component must reuse the Marketplace data already loaded by the server instead of requesting the same data again.

The initial overview and Skill detail routes use:

```text
React Server Component -> server-only Marketplace client -> skills-service
```

Later overview interactions use:

```text
Browser -> Next.js BFF -> server-only Marketplace client -> skills-service
```

Server Components do not call their own BFF URLs. The Skill detail route loads its content in the initial server response and does not issue a follow-up BFF data request.

**Chosen Option:** Option 1 - RSC initial rendering with BFF-driven browse updates.

### Rationale

Option 1 loads Marketplace names in the initial server response. If the URL selects a valid Marketplace, the server also loads its requested page of Skills. The interactive component starts with this data and does not request it again when the page loads.

After that, changes to the Marketplace, search text, page size, or page number request Skill results through the BFF. Only the results area updates. Option 2 renders the route again for every change. Option 3 waits for browser JavaScript before loading any Marketplace data.

The decision follows Orbit’s three architecture questions.

  - Who is making the request? The Server Component and BFF use the current user’s Orbit session. skills-service authenticates the user’s token on every request.

  - What can it access? skills-service enforces the required Marketplace read permission and tenant boundaries. Viewing a Skill does not grant access to other Orbit resources.

  - How does it connect? The browser communicateswith Next.js. Server Components load initial and detail data. Later browse requests passthrough BFF routes. Only server-side code calls skills-service with the user’s Orbit token.

---

## Consequences

### Positive

* Initial Marketplace data and direct-linked Skill details are server-rendered.
* Later browse changes update the results area instead of requiring a route render.
* One owner coordinates interactive state and prevents inconsistent controls, results, and URLs.
* Authorised results remain usable during transient failures without being mislabelled.
* Orbit's security, Terra, and testing patterns are reused.

### Negative

* The interactive overview must coordinate debounce, cancellation, late responses, and Back and Forward navigation.
* Each browse change depends on a request to skills-service.

### Neutral

* skills-service remains responsible for access checks, search, and pagination.


---

## Implementation

| Area | Planned behaviour |
| --- | --- |
| Overview (`/marketplace`) | Server loads Marketplace names and, when selected in the URL, one page of Skills. Client reuses initial data. |
| Browse requests | Selection, search, result limit, and pagination call the same-origin BFF. It validates input and calls skills-service through a server-only client using the Orbit session. |
| Overview state | Cancel the previous request when a new one starts. Apply results and update the URL only for the latest successful request. Back and Forward reload the view from the URL. |
| Skill detail | Server loads the Skill and it's details. The page renders them read-only, without another browser request. |
| Navigation | Marketplace is visible to users with `marketplace:read`. Skill links include the source organisation, repository, and Skill name. |

### Migration Path

Marketplace is a new read-only dashboard area, so no existing frontend data migration is required. Navigation remains hidden until `marketplace:read` and the Marketplace API are available. Use Orbit's normal deployment and access-control process, and resolve affected integration and frontend decisions before declaring the corresponding behaviour complete.

### Timeline

Not specified.



## Related Decisions

* [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) - Next.js App Router and frontend structure.
* [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) - sealed server-side session and token boundary.
* [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) - scope-aware navigation and presentation.
* [Terra Frontend Design System](2026-03-25-terra-frontend-design-system.md) - required UI foundation.
* [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) - three-tier component placement.
* [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) - frontend testing approach.
* **Agent Marketplace — Cache-Aside, Tenant-Scoped Redis Keys, and Resource-Aware Skills** - authoritative backend API contract, Skill data model, tenant resolution, pagination, and status-code behaviour.

---

## References

Agent Marketplace: Skills PRD is identified in the Technical Story. A supporting link has not been supplied.


**Last Updated:** 2026-09-17
