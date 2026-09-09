# Agent Marketplace Frontend Architecture

<!-- If there are any mentions of plugins, ignore it, Plugins are out of the initial scope -->

**Status:** Proposed  
**Date:** 2026-08-17  
**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)  
**Technical Story:** Agent Marketplace: Skills PRD

## Context

Orbit needs a read-only Marketplace under Control Plane where approved human users can find and review skills. The marketplace must support browsing, search, filters, package details, and a source-status view. It must not install, run, assign, update, or remove packages.

The frontend must follow Orbit's existing security and application patterns. It must keep Orbit tokens outside browser JavaScript, use backend authorisation for every read, and treat all repository content as unsafe data.


### Background

Orbit's frontend is a Next.js App Router application. React Server Components provide the initial page data. Later interactive updates to the Marketplace overview use same-origin Backend-for-Frontend routes. The BFF reads the sealed user session and calls authorised Orbit services without exposing the access token to the browser.

All product JSX uses Terra (`@orbit/terra`). Shared frontend components follow Orbit's three-tier component model. Marketplace must use these existing patterns instead of adding another state library, design system, or direct source-provider integration.

The Marketplace backend owns source access. The frontend does not call GitHub or another Marketplace source directly, it calls the back-end service in this case the Marketplace Service. The request boundary for later browse interactions is:

```text
Browser -> Next.js BFF -> Marketplace Service -> Egress Gateway -> Approved source
```

For the first Marketplace overview server render, the React Server Component calls the same server-only Marketplace client used by the BFF. It does not call its own BFF URL.

When a user selects a Skill, the browser navigates to the dedicated `/marketplace/[id]` route. Its React Server Component calls the server-only Marketplace client directly and receives all information needed to render the read-only detail page in the first response:

```text
Browser -> Next.js /marketplace/[id] route -> React Server Component -> server-only Marketplace client -> Marketplace Service
```

The detail page does not call a BFF route and has no file-preview interaction. The frontend does not fetch Skill content directly from GitHub or another source provider. Backend cache, hydration, and refresh behaviour remain backend decisions.

### Problem Statement

Choose a frontend architecture that:

1. Shows package results when the Marketplace page first opens, without requiring another browser request.
2. Allow users to search, filter, sort, choose how many results to display, and move through result pages.
3. Have the current filters, search, sorting, requested result limit, and cursor selected in the page URL so copied and pasted links preserve the selected view.
4. When the Marketplace Overview opens, validate any browse parameters in the URL and use defaults for values that are absent. No cursor loads the first result page for the validated query. An optional current cursor loads the selected result page.
5. The Front-end must not expose Orbit tokens or Marketplace source credentials to the browser.
6. Display Skill details as read-only information on a dedicated route.
7. Prevent obsolete search requests from replacing newer results.
8. Handle packages with the same name or slug across different sources safely.
9. Follows Orbit's existing frontend structure and Terra design system.

### Goals

- Add Marketplace page under the Control Plane dashboard.
- Show the result page selected by validated URL parameters on the first render. No cursor means the first result page for the validated query.
- Support search, filters, sorting, and pagination without loading the complete Marketplace into browser memory.
- Keep search, filters, sorting, number of packages per page, and current pagination position in the URL parameters.
- Open a dedicated detail page when a user selects a Skill, and load its full details in the page's first server render.
- Show package metadata and category when the backend provides them in the detail page.
- Use package identifiers supplied by the backend.
- Support desktop, tablet and mobile screens .

### Non-Goals

- Choosing the Marketplace backend's source adapter, cache, storage, refresh, hydration, metadata-extraction, or search-ranking implementation.
- Installing, running, assigning, updating, rolling back, or removing packages.
- Adding packages to agents, AgentTypes, environments, or runs.
- Previewing or downloading repository files from the Skill detail page.
- Creating a new frontend framework.
- Managing Marketplace sources from the dashboard.

## Decision Drivers

- Orbit access and refresh tokens must remain server-side.
- Every Marketplace response must enforce the active user, tenant, and permissions.
- The content from the repository must be loaded as read-only.
- Search, filters, sorting, and pagination must not require the browser to load the complete Marketplace.
- The client must control its requested result limit.
- Browse state must support refresh, back and forward navigation, and direct links.
- Multiple sources may contain packages with the same name or slug.
- The implementation must use the existing BFF, RSC, Terra, component placement, and testing patterns.
- Marketplace failures must not affect unrelated Orbit pages.
- Usable authorised Marketplace data must remain visible during loading and request errors, but a forbidden response must clear it immediately.

## Considered Options

### Option 1: RSC first render with BFF updates and backend cursor pagination

**Description:** When Marketplace first opens, the React Server Component validates the browse values in the URL, including the optional current cursor, and uses the server-only Marketplace client to request the selected result page from the Marketplace Service. No cursor means the first result page for the validated query. Later search, filter, sorting, requested-limit, and pagination requests go through Orbit’s Next.js BFF routes. Selecting a Skill navigates to the dedicated `/marketplace/[id]` route. Its React Server Component calls the server-only Marketplace client directly and renders all read-only Skill details in the first response. The detail page makes no follow-up BFF data request and has no file-preview interaction. When the user selects Previous or Next, the frontend requests the result page identified by the corresponding backend-provided cursor. After a successful response, it updates the results and writes that cursor as the current cursor in the URL. The backend enforces a maximum limit, applies search, filtering, sorting, and pagination, and returns only the requested result page with optional previous and next cursors.

**Pros:**

- Shows package results as soon as Marketplace first opens, without another browser request.
- Updates the package results without reloading or navigating away from the page.
- Keeps search, filter, sorting, requested-limit, and cursor choices in the URL, so links can be shared and the browser’s Back and Forward buttons work.
- Prevents the browser from loading the complete Marketplace.
- Lets the user choose how many packages on a page.
- Follows the frontend pattern already used by other Orbit pages.

**Cons:**

- The Marketplace overview's initial load and later browse interactions use two request paths: the initial request comes from the Next.js server, while later requests come from the browser through the BFF.
- Search, filter, sorting, requested-limit, and cursor values come from the URL and can be changed by users. The front-end and BFF must use validation rules, and the Marketplace back-end must validate the request before processing it.
- The frontend must handle invalid or expired cursors when source data changes.
- Cancellation must propagate from the browser through the BFF and server-only client to the Marketplace Service.

### Option 2: RSC navigation for every interaction

**Description:** When the user searches, changes a filter, sorting option, or requested result limit, or moves through result pages, Next.js updates the URL and requests the new results on the server. The React Server Component runs again and returns the updated Marketplace page. The browser does not send a separate BFF request to update only the package results. This is a Next.js route navigation, not a complete browser refresh like pressing F5.

**Pros:**

- The frontend needs little React state because the search, filters, sorting, requested limit, and cursor are read from the URL.
- The URL records the current Marketplace view, so it can be refreshed, bookmarked, or shared without losing the selected options.
- All data reads happen during server rendering because of the server components.

**Cons:**

- Server-rendered routes, the browser must wait for the server response before showing the new route content. For dynamic routes, this can make the application appear unresponsive
- Rapid changes can start repeated route renders for results the user no longer needs.

### Option 3: Client-only Marketplace application

**Description:** When Marketplace first opens, Next.js sends a page layout without any package results. After the browser downloads and starts the page’s JavaScript, a Client Component requests the first result page through a Next.js BFF route. The Client Component manages the package results, search, filters, sorting, requested limit, cursor pagination, loading states, errors, and overlapping requests. Later interactions use the same BFF request flow.

**Pros:**

- Uses the same BFF request flow for the initial results and all later interactions.
- Keeps data-loading logic in one client-side hook or service.
- Makes it straightforward to update only the package results without navigating or re- rendering the route.
- Makes it easier to keep existing results visible while loading new results or handling a request error.

**Cons:**

- When Marketplace first opens, the user sees the page layout without package results. The results appear only after the Client Component starts and completes a BFF request.
- Client Components must manage the package results, search options, filters, pagination, loading states, errors, and overlapping requests in React state.

## Decision

**Chosen Option:** Option 1. RSC first render with BFF updates and backend cursor pagination.

Marketplace will follow Orbit's existing server-first frontend pattern. A React Server Component will load one result page for the initial render. Later search, filter, sorting, requested-limit, and pagination changes will use authenticated Next.js BFF requests and update only the results area. The Marketplace Service will apply each query, enforce a maximum result limit, and return only the requested result page with optional previous and next cursors.

Selecting a Skill will navigate to `/marketplace/[id]`. The detail route's React Server Component will call the server-only Marketplace client directly, so all Skill details are included in the first response. The detail page will not use a BFF route or provide file-preview interactions.

### Browse State and Pagination

- An initial request without valid browse parameters uses an empty search, no filters, the default sort, the default requested limit, and no cursor.
- A direct, refreshed, bookmarked, or shared URL uses its validated search, filters, sorting, requested limit, and optional current cursor during the first React Server Component render.
- The Marketplace Service performs search, filtering, sorting, and cursor pagination. The frontend never fetches the complete Marketplace and divides it locally.
- The user chooses a requested result limit from validated frontend options. The backend-enforced cap remains authoritative.
- The backend returns `previousCursor` when an earlier result page exists and `nextCursor` when a later result page exists. The frontend does not infer either value from the number of returned items.
- The URL contains at most one `cursor` parameter. When present, it is the cursor used to request the currently displayed result page. The response cursors are not added to the URL until the user selects one of them and the request succeeds.
- Selecting Previous or Next requests the result page through the BFF without a React Server Component navigation. After a successful response, the frontend updates the results and adds the selected cursor to the URL as a browser history entry with `window.history.pushState`. If the request ends in `error`, the existing results and URL remain unchanged. A forbidden response follows the availability and error rules and clears retained Marketplace data.
- Previous remains visible but is disabled when `previousCursor` is absent. Next remains visible but is disabled when `nextCursor` is absent.
- Changing search, filters, sorting, or requested limit requests the first matching result page without a cursor. After a successful response, the frontend updates the results and replaces the current browser history entry with the new query URL by using `window.history.replaceState`. If the request ends in `error`, the existing results and URL remain unchanged. A forbidden response clears retained Marketplace data.
- Browser Back and Forward navigation handles the browser's `popstate` event, revalidates the restored URL, and reloads its result page through the BFF. For a general `error` other than `invalid_cursor`, the frontend restores the last successfully displayed URL with `window.history.replaceState` when earlier results remain visible. A forbidden response clears retained Marketplace data instead of restoring the last results. A direct navigation or hard refresh uses the React Server Component path.
- The backend returns the machine-readable `invalid_cursor` code for an invalid, query-mismatched, or expired cursor. This code is handled before the general `popstate` failure rule. For a later BFF request, the frontend keeps the other browse values and retries once without a cursor. If the retry succeeds, it commits the first matching result page, replaces the invalid URL, and shows a safe notice. If the retry returns `forbidden`, the frontend clears retained Marketplace data and shows the blocking forbidden state. If the retry ends in `error`, it restores the last successful results and URL and shows the general request error.
- For an initial React Server Component request, the server-only Marketplace client retries once without the invalid cursor and passes an invalid-cursor flag to the Client Component shell. After hydration, the shell removes the cursor from the URL with `window.history.replaceState` so it cannot cause a retry loop. A successful retry shows the first matching result page and safe notice. A forbidden retry shows the blocking forbidden state with no retained Marketplace data. A retry ending in `error` shows the general unavailable or error state.

### Search and Cancellation

- An empty search shows all authorised packages.
- Any non-empty search is valid. The frontend requires a minimum number of 3 characters.
- Search changes are debounced by 300 milliseconds.
- When browse values change, the Client Component aborts the previous request with `AbortController` before starting the next request.
- The BFF route and server-only Marketplace client propagate the cancellation signal to the Marketplace Service.
- A request-generation guard prevents a late response from replacing results from a newer request.
- Search matching and ranking are backend concerns.

### Availability and Error Behaviour

The frontend models three separate areas:

- **Request state:** `loading`, `success`, `error`, or `forbidden`. This describes the latest frontend request.
- **Marketplace availability:** `not_configured`, `pending`, `current`, `stale`, `degraded`, or `unavailable`. This describes whether the Marketplace Service has usable data.
- **Result state:** `populated`, `configured_but_empty`, or `no_matches`. This describes the result of a successful browse request.

Marketplace availability has these meanings:

- `not_configured` means that no Marketplace sources are configured and no usable data exists.
- `pending` means that sources are configured, but the first usable Marketplace snapshot is not ready. It does not describe an in-flight frontend refresh.
- `current` means that a complete and current snapshot is available.
- `stale` means that a complete but older snapshot is still usable.
- `degraded` means that a usable snapshot exists, but some source data may be missing or a source has failed.
- `unavailable` means that sources are configured, but no usable Marketplace snapshot exists.

`current`, `stale`, and `degraded` are usable availability states. `not_configured`, `pending`, and `unavailable` are not usable availability states. When usable data is both old and incomplete, `degraded` takes priority over `stale`. The degraded warning may also show the data's freshness.

The frontend applies these rendering rules in order:

1. `forbidden` always shows a blocking forbidden page and clears retained results, response cursors, Skill details, and source-status data. Retained data is never restored after a forbidden response.
2. The existing `invalid_cursor` recovery runs before the general request-error behaviour.
3. If a previous authorised response remains usable, the frontend keeps its results visible while a later request is `loading` or ends in `error`. Retained data must belong to the same authenticated tenant and permission context.
4. A loading refresh shows a non-blocking progress indicator above the existing results. A refresh that ends in `error` shows a non-blocking warning above them.
5. `stale` and `degraded` show warnings above usable results and do not hide those results.
6. A successful response with `not_configured`, `pending`, or `unavailable` clears an older retained response. Retention applies only while a later request is `loading` or has ended in `error`.
7. A blocking loading, not-configured, pending, unavailable, or general error page is shown only when no usable Marketplace response exists.
8. For a successful usable response, `populated` shows Skill cards, `configured_but_empty` shows that the configured Marketplace has no Skills available to the user, and `no_matches` shows that the active search or filters found no Skills.

A request cancelled because a newer browse request replaced it does not enter `error` or show a warning. The newer request controls request state.

A successful response with no items and no active search or filters is `configured_but_empty`. A successful response with no items and an active search or filter is `no_matches`.

Request and availability priority also apply to the dedicated Skill detail route and the read-only Sources view. Result state applies only to the Marketplace overview. The detail route renders usable `current`, `stale`, or `degraded` details in its first server response, with a warning for `stale` or `degraded`. It uses a blocking state for `forbidden` or when no usable detail response exists.

### Skill Identity and Detail Navigation

- The frontend uses stable, source-qualified, URL-safe opaque Skill identifiers supplied by the backend. Names and slugs are display data and are not used as route identifiers.
- The dedicated detail route is `/marketplace/[id]`, where `[id]` is the backend-provided opaque Skill identifier. The frontend does not create source-collision or repository-path rules.
- The detail route's React Server Component calls the server-only Marketplace client directly. It does not call its own BFF URL.
- The Marketplace Service returns all provider-neutral, read-only Skill details required for the first render, regardless of whether the information came from a Marketplace manifest, `SKILL.md` frontmatter, or another supported source format.
- The detail page has no file-preview interaction and makes no later BFF data request.
- Detail reads go to the Marketplace Service rather than directly to GitHub or another source provider.

### Rationale

This approach was chosen because it shows results on the first render and prevents large Marketplaces from being loaded into browser memory. A client-selected result limit with a backend cap provides user choice without allowing unbounded responses. Debounce, cancellation propagation, and stale-response protection prevent obsolete searches from consuming work or overwriting newer results. Separating request, availability, and result state prevents a request error from replacing authorised usable data with a blocking error. A dedicated server-rendered Skill route supports direct links, refreshes, bookmarks, and browser navigation without an additional browser data request. Reusing Orbit's existing Next.js, BFF, authentication, and Terra patterns also reduces implementation and maintenance risk.

Option 2 ties every browse change to a route render. Option 3 delays the first package results until the browser starts the Client Component and completes another request.

The decision preserves Orbit's three architecture questions:

- **Who is making the request?** The RSC and BFF use the authenticated Orbit session. The Marketplace Service remains responsible for tenant and permission enforcement.
- **What can it access?** The read-only surface displays only authorised Marketplace data. Viewing a package grants no access to tools, models, connections, files, networks, agents, or workloads.
- **How does it connect?** The browser calls only Next.js. The Marketplace Service reaches approved sources through the Egress Gateway, and source credentials never enter browser JavaScript.

## Consequences

### Positive

- Package results are included in the initial response sent to the browser, so users do not wait for an additional client-side request.
- The browser receives only the current result page or selected package details, which limits browser memory use.
- Each Skill has a dedicated detail URL that supports direct navigation, refreshes, bookmarks, sharing, and browser Back and Forward navigation.
- Skill details are included in the detail route's initial server response, so the page does not need a later BFF request.
- Later search, filter, sorting, and pagination requests update only the results area.
- URL parameters allow filtered Marketplace views to survive refreshes and be shared.
- Users can choose a requested result limit while the backend prevents excessively large responses.
- Obsolete requests are cancelled, and late responses cannot overwrite current results.
- Usable results remain visible during refreshes and request errors, with clear warnings when data is stale or degraded.
- A forbidden response cannot leave previously authorised Marketplace data visible.
- Stable, source-qualified identifiers prevent name and slug collisions across sources.
- Orbit tokens and Marketplace source credentials remain outside browser code.
- The implementation reuses Orbit's existing RSC, BFF, authentication, Terra, and testing patterns.

### Negative

- The browse experience uses two request paths: the Server Component for the first render and BFF routes for later interactions. The dedicated detail page uses the server-render path. These paths must be implemented and tested.
- Every search, filter, sorting, requested-limit, or pagination change requires a request to the Marketplace backend, so the user experience depends on backend latency.
- Client Components must manage the latest request state, Marketplace availability, result state, last successful authorised response, current cursor, returned previous and next cursors, pagination controls, browser-history navigation, cancellation, and stale-response protection.
- URL parameters, BFF requests, and backend query rules must use the same names, allowed values, and defaults.
- Cursors may expire when source data changes, requiring a safe reset path.

### Neutral

- The frontend uses a provider-neutral Marketplace API. GitHub may be the first source provider, but GitHub-specific behaviour remains behind the backend API.
- The Marketplace Service owns availability. The frontend owns request state, result-state derivation, retained-response handling, and presentation.
- Backend source access, per-source credentials, caching, storage, refresh, metadata extraction, search ranking, and reconciliation remain governed by the backend Marketplace ADR.
- A View source link may navigate the user to an external provider, but Marketplace data requests still pass through Orbit's backend.

## Implementation

Implementation will follow Orbit's existing frontend structure:

- Add authenticated routes for the Marketplace overview, `/marketplace/[id]` Skill details, and the read-only Sources view.
- Add one typed, server-only Marketplace client for authorised requests to the Marketplace Service.
- Use React Server Components to load the selected result page and Skill details for their first render. Use same-origin BFF routes for later browser interactions on the Marketplace overview.
- Keep browse values in validated URL parameters. Marketplace-specific client code will manage later result updates and apply the pagination, cancellation, browser navigation, recovery, and error rules defined in this decision.
- Use backend-provided opaque Skill identifiers for detail routes and render Skill details as read-only information.
- Build the interface with Terra and follow Orbit's existing access-control, component-placement, and frontend testing patterns.
- Test the two overview request paths, the Skill detail request path, pagination and URL restoration, availability and error behaviour, authorisation, cancellation, accessibility, and responsive layouts.

The Marketplace API must provide provider-neutral result pages, previous and next cursors, stable Skill identifiers, read-only Skill details, source state, Marketplace availability, and safe machine-readable errors. The Marketplace Service performs search, filtering, sorting, and pagination, while the frontend controls presentation and browser request state.

The server-only client and BFF must preserve the active Orbit identity and permission context. Orbit tokens, source credentials, and raw upstream errors must not be exposed to browser JavaScript. Exact backend source configuration, credential isolation, cache, storage, refresh, metadata extraction, search ranking, cursor encoding, and collision behaviour are defined by the backend ADR.

### Migration Path

Marketplace is a new read-only dashboard area, so no existing frontend data migration is required. Navigation remains hidden until the required scope and Marketplace API are available. The feature can be released behind the normal deployment and access-control process.


## Validation

The decision is correct if users can browse and review Marketplace packages through Orbit without exposing credentials, executing repository content, or loading the complete Marketplace into the browser.

Validation must cover the default first render without a cursor, direct React Server Component loads with a cursor, BFF refreshes, URL state, client-selected result limits, Previous and Next movements, disabled pagination boundaries, and committing a new result page with its `pushState` or `replaceState` URL only after success. It must cover `popstate` Back and Forward reloads, `invalid_cursor` precedence over general failure handling, restoration of the last successful URL after a general `popstate` error or cursor-free retry error other than `forbidden`, and cursor reset after a query change. Invalid or expired cursor tests must cover successful and failed recovery on both request paths, including post-hydration URL clean-up after an initial request. Validation must also cover one-character and other short searches, debounce, end-to-end cancellation, stale-response protection, permission failures, source failures, stale data, source collisions, direct, refreshed, bookmarked, shared, Back, and Forward navigation to `/marketplace/[id]`, opaque-ID routing, details in the initial server response, the absence of detail BFF and file-preview requests, responsive behaviour, keyboard use, and screen-reader output.

State validation must cover loading and request errors both with and without a previous usable response. It must cover `stale` and `degraded` warnings above populated and empty result states. It must cover `degraded` taking priority when usable data is also stale. It must cover blocking not-configured, pending, unavailable, and initial-error states. It must cover a successful non-usable response clearing an older response. It must also cover the difference between `configured_but_empty` and `no_matches`. Detail-page validation must cover current, stale, and degraded details in the initial server response, blocking unavailable and forbidden responses, and the absence of a detail BFF request. Forbidden tests must cover initial RSC loading, later BFF updates, pagination, `popstate`, and the invalid-cursor retry. Each forbidden case must clear retained results, response cursors, Skill details, and source-status data, and retained data must never cross an authenticated tenant or permission-context change.

### Monitoring

Distributed traces must cover:

- initial RSC render -> server-only Marketplace client -> Marketplace Service.
- browser browse request -> BFF -> server-only Marketplace client -> Marketplace Service.
- Skill detail route -> RSC -> server-only Marketplace client -> Marketplace Service.
- source-status reads.
- cache-hit and cache-miss outcomes reported by the Marketplace Service.
- cancellation from the browser through the BFF and Marketplace Service.

Operators must be able to distinguish a package error, source error, Marketplace-wide error, authentication failure, authorisation failure, and upstream source failure.


## Related Decisions

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) - Next.js App Router and frontend structure.
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) - sealed server-side session and token boundary.
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) - scope-aware navigation and presentation.
- [Terra Frontend Design System](2026-03-25-terra-frontend-design-system.md) - required UI foundation.
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) - three-tier component placement.
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) - frontend testing approach.
