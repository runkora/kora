# Agent Marketplace Frontend Architecture

<!-- Filename: 2026-08-17-agent-marketplace-frontend-architecture.md -->

**Status:** Proposed  
**Date:** 2026-08-17  
**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)  
**Technical Story:** [Agent Marketplace: Skills and Plugins PRD](https://github.com/clear-route/orbit/blob/main/docs/prd/agent-marketplace.md)

---

## Context

Orbit needs a read-only Marketplace under Control Plane where approved human users can find and review skills and plugins. The first release must support browsing, search, filters, package details, safe text previews, and a read-only source-status view. It must not install, run, assign, update, or remove packages.

The frontend must follow Orbit's existing security and application patterns. It must keep Orbit tokens outside browser JavaScript, use backend authorisation for every read, and treat all repository content as unsafe data.

### Background

Orbit's frontend is a Next.js App Router application. React Server Components provide the initial page data. Browser interactions use same-origin Backend-for-Frontend routes. The BFF reads the sealed user session and calls authorised Orbit services without exposing the access token to the browser.

All product JSX uses Terra (`@orbit/terra`). Shared frontend components follow Orbit's three-tier component model. Marketplace must use these existing patterns instead of adding another state library, design system, or direct source-provider integration.

The Marketplace backend owns source access. The frontend does not call GitHub or another Marketplace source directly. The agreed request boundary is:

```text
Browser -> Next.js BFF -> Marketplace Service -> Egress Gateway -> Approved source
```

For the first server render, the React Server Component calls the same server-only Marketplace client used by the BFF. It does not call its own BFF URL.

### Problem Statement

Choose a frontend architecture that:

1. provides a useful first render without a second browser request;
2. supports responsive search, filtering, sorting, and pagination;
3. keeps browse state in shareable URLs;
4. sends only the requested page of package summaries to the browser;
5. keeps authentication material and source credentials outside browser JavaScript;
6. renders package content without executing it; and
7. follows Orbit's existing frontend structure and Terra design system.

### Goals

- Add Marketplace pages under the authenticated Control Plane dashboard.
- Use a server-rendered first page and BFF requests for later interactions.
- Make search, filters, sorting, and pagination backend operations.
- Keep filter, sorting, and page state in validated URL parameters.
- Load package details and supported text previews only when needed.
- Clearly separate publisher claims, Orbit checks, package needs, and source health.
- Support desktop, tablet, mobile, keyboard, and screen-reader use.

### Non-Goals

- Choosing the Marketplace backend service, database, cache, refresh process, or source-provider adapter.
- Installing, running, assigning, updating, rolling back, or removing packages.
- Adding packages to agents, AgentTypes, environments, or runs.
- Rendering repository HTML, Markdown, MDX, SVG, images, scripts, hooks, or services.
- Creating a new frontend framework, global state library, or design system.
- Managing Marketplace sources from the dashboard in the first release.
- Defining final visual designs or final product wording.

---

## Decision Drivers

* Orbit access and refresh tokens must remain server-side.
* Every Marketplace response must respect the active user, tenant, and permissions.
* Repository content is untrusted and must remain inert.
* The first page must contain useful content before client hydration.
* Search, filters, sorting, and pagination must not require the browser to load the complete Marketplace.
* Browse state must support refresh, back and forward navigation, and direct links.
* The implementation must use the existing BFF, RSC, Terra, component placement, and testing patterns.
* Marketplace failures must not affect unrelated Orbit pages.

---

## Considered Options

### Option 1: RSC first render with BFF client refresh and server paging

**Description:** The React Server Component requests the initial page through a server-only Marketplace client. Later search, filter, sort, page, detail, and preview requests go through same-origin BFF routes. The Marketplace Service applies browse queries and returns only the requested page. URL parameters represent public browse state.

**Pros:**

- Provides useful content on the first render.
- Keeps Orbit tokens on the server.
- Supports responsive interactions without full-page navigation.
- Keeps deep links and browser navigation working.
- Prevents the browser from loading the complete Marketplace.
- Reuses established Orbit frontend patterns.

**Cons:**

- Requires both server and browser data paths.
- Query validation must stay consistent across URL parsing, the BFF, and the backend.
- Client requests must cancel or ignore older responses when filters change quickly.

### Option 2: RSC navigation for every interaction

**Description:** Every search, filter, sort, and page change updates the URL and causes a complete React Server Component navigation. No Marketplace client data hook or BFF refresh route is used.

**Pros:**

- Uses very little client-side state.
- URL state and rendered state cannot diverge.
- All data reads happen during server rendering.

**Cons:**

- Each interaction causes a route transition and loading state.
- Search input feels slow because every change waits for a complete navigation.
- Retaining current results during a temporary refresh failure is difficult.

### Option 3: Client-only Marketplace application

**Description:** The route renders an empty shell. A client component loads all Marketplace data through BFF routes after hydration and owns the page state.

**Pros:**

- Uses one browser-side data path after hydration.
- Makes highly interactive transitions simple.

**Cons:**

- The first useful content waits for hydration and another network request.
- More state and lifecycle logic moves into the browser.
- It does not follow Orbit's established server-first page pattern.
- Deep-link, loading, and error handling require more client-only code.

---

## Decision

Use **Option 1: RSC first render with BFF client refresh and server paging**.

**Chosen Option:** Option 1 - RSC first render with BFF client refresh and server paging

The Marketplace frontend will use these rules:

- The browser calls only same-origin Next.js routes. It never calls GitHub, another source provider, or the Marketplace Service directly.
- The first React Server Component render calls a server-only Marketplace client and receives one authorised page.
- Later browser interactions call BFF routes protected by Orbit's existing session and authorisation wrapper.
- The Marketplace Service performs search, filtering, sorting, and page slicing and returns only the requested page.
- Search, type, Marketplace source, category, framework support, sort, and page choices are represented by validated URL parameters.
- Package details and supported text previews are requested only when needed.
- Package content is rendered as escaped, read-only text. The frontend does not render repository HTML or execute package code.
- Publisher claims and Orbit check results use separate fields and separate visual sections.
- Required and optional package needs are shown separately and clearly state that they grant no access.
- The Sources page is read-only and visible only to users with the required projected scope. Backend authorisation remains authoritative.
- All product JSX uses Terra components. Marketplace-specific components begin at feature scope unless the normal component-placement rules require promotion.

### Rationale

Option 1 gives Orbit a fast first render and responsive later interactions while keeping the security boundary clear. The browser communicates only with Next.js, and Next.js communicates with the authorised Marketplace API.

Backend paging satisfies the PRD requirement that large Marketplaces must not require the browser to load all items. URL state makes searches and filtered pages shareable and keeps browser navigation predictable. The approach also reuses Orbit's existing frontend patterns, which reduces implementation and maintenance risk.

Option 2 is simpler but would make repeated Marketplace controls feel slow. Option 3 would delay useful content and add unnecessary browser state.

---

## Consequences

### Positive

- Users receive useful Marketplace content on the first render.
- Orbit tokens and source credentials remain outside browser JavaScript.
- The browser holds only the current page or selected detail data.
- Search, filtering, sorting, and pagination remain shareable through URLs.
- The frontend stays independent from GitHub and other source-provider formats.
- Unsafe package content remains read-only and cannot add Orbit controls or behaviour.
- Existing Terra, BFF, RSC, permissions, and testing patterns are reused.

### Negative

- The feature needs server-rendered and browser-refresh data paths.
- Each browse interaction requires a backend request.
- URL, BFF, and backend query validation must remain aligned.
- The UI depends on the Marketplace API meeting its latency target.
- Detailed error and degraded-state handling adds frontend test cases.

### Neutral

- Backend source fetching, caching, storage, and refresh decisions remain outside this ADR.
- Package installation and runtime use require separate future decisions.
- GitHub may be the first backend provider, but the frontend uses a provider-neutral API model.
- The browser may display source links, but it never receives source credentials.

---

## Implementation

Implementation will follow Orbit's existing frontend golden path:

- Add authenticated Marketplace routes for browse, package detail, text previews, and Sources.
- Add a server-only Marketplace client for authorised upstream reads.
- Add same-origin BFF route handlers for browser-initiated reads.
- Add a feature hook and Context where interactive refresh is required.
- Parse and validate URL query values before sending them upstream.
- Keep the current results visible while a later request is pending or fails.
- Use opaque package and file identifiers supplied by the backend.
- Use Terra layout, text, form, status, alert, pagination, and code components.
- Render supported previews as text and provide a separately validated View source link.
- Show clear empty, not configured, pending, stale, degraded, unavailable, forbidden, and error states.
- Add unit, component, route-handler, accessibility, responsive, and end-to-end tests.
- Run `cd frontend && pnpm lint && pnpm tsc --noEmit` before completion.

The frontend requires a typed Marketplace API that returns paged package summaries, package details, text previews, source status, safe errors, and availability state. Exact backend source and storage behaviour is defined by the backend ADR.

### Migration Path

Marketplace is a new read-only dashboard area, so no existing frontend data migration is required. Navigation remains hidden until the required scope and Marketplace API are available. The feature can be released behind the normal deployment and access-control process.

### Timeline

- Product and architecture approval: TBD.
- UX and accessibility review: TBD.
- Frontend implementation: TBD.
- Integration, security, performance, and tenant-isolation testing: TBD.
- Controlled release and general availability: TBD.

---

## Validation

The decision is correct if users can browse and review Marketplace packages through Orbit without exposing credentials, executing repository content, or loading the complete Marketplace into the browser.

Validation must cover initial server rendering, BFF refreshes, URL state, permission failures, source failures, stale data, safe previews, responsive behaviour, keyboard use, and screen-reader output.

### Success Metrics

- Initial browse, package detail, preview, and Sources reads meet the agreed 300 ms API target under the agreed load.
- Large result sets do not require the browser to load all Marketplace records at once.
- Search, filtering, sorting, and pagination return the correct backend-selected page.
- A user cannot retrieve Marketplace data outside their tenant or permissions.
- Repository content cannot create HTML, scripts, controls, requests, or executable behaviour.
- One failed source does not prevent available source data from being shown.
- Users can find a package, identify its source, and explain its package needs without leaving Orbit.
- Frontend lint, type checking, unit tests, accessibility tests, and end-to-end tests pass.

### Monitoring

Monitor Marketplace request count, latency, status, safe error codes, and page type at the BFF and Marketplace Service boundaries. Operators must be able to distinguish a package error, source error, Marketplace-wide error, authentication failure, authorisation failure, and upstream source failure.

Telemetry must not contain Orbit tokens, source credentials, repository file content, search text, private repository locations, or raw upstream errors.

---

## Related Decisions

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) - Next.js App Router and frontend structure.
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) - sealed server-side session and token boundary.
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) - scope-aware navigation and presentation.
- [Terra Frontend Design System](2026-03-25-terra-frontend-design-system.md) - required UI foundation.
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) - three-tier component placement.
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) - frontend testing approach.
- **Agent Marketplace - GitHub App Direct Fetch** - backend Marketplace architecture decision.

---

## References

- [Agent Marketplace: Skills and Plugins PRD](https://github.com/clear-route/orbit/blob/main/docs/prd/agent-marketplace.md)
- [Orbit ADR template](template.md)
- [Frontend data-driven page golden path](../../frontend/docs/golden-paths/frontend-state-pattern.md)
- [Frontend component placement reference](../../frontend/docs/reference/component-placement.md)
- [Terra design system reference](../../frontend/docs/reference/terra-design-system.md)

---

## Notes

- This ADR is written in B2-level English.
- The ADR remains Proposed while the linked Marketplace PRD is Draft.
- Backend service boundaries, source access, storage, caching, and refresh stay in the Marketplace backend ADR.

**Last Updated:** 2026-08-18
