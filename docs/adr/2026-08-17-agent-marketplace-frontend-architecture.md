# Agent Marketplace Frontend Architecture

**Status:** Proposed; accepted amendments cover browse state (2026-10-12), backend types (2026-10-13), and server-rendered Skill routing (2026-10-16).

**Date:** 2026-08-17

**Updated:** 2026-10-29

**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)

This document describes the frontend after those amendments, the 2026-10-19 cleanup, the 2026-10-21 cursor integration, the 2026-10-27 backend API alignment, and the selected-Marketplace overview path consolidated under `/marketplace` on 2026-10-29. The original proposal and the broader [Marketplace PRD](../prd/agent-marketplace.md) retain their existing approval status. The [frontend deep dive](../architecture/marketplace.md) includes the component diagram and file map.

## Context

The Marketplace lets signed-in users browse the Skills exposed by skills-service and read their instructions and supporting files. The overview has interactive source selection, search, name sorting, page size, pagination, and list/card views. Opening a Skill must render its details on a dedicated server route, following the Agents list/detail pattern.

The frontend uses Next.js Server Components, an authenticated Backend-for-Frontend (BFF) browse route, the existing session and preferences providers, and Hades (`@orbit/hades`). It neither calls GitHub directly nor owns source registration, source credentials, or backend caching. Repository content is read-only data.

The earlier in-place detail implementation kept the selected Skill in client state and redirected detail addresses to the overview. The accepted 2026-10-16 amendment restores server-rendered detail navigation. Search, sort, page, and page size stay in memory. The selected-Marketplace amendment exposes the overview at `/marketplace/{marketplaceSlug}` so it can be opened directly and refreshed, alongside the existing Skill detail route.

## Options

1. **Server initial loads with BFF browse updates.** Server Components load overview defaults and Skill details. A client hook handles subsequent browse changes through the authenticated BFF. This retains the existing frontend pattern and keeps previous results visible during requests, with cancellation and response ordering owned by the hook.
2. **Server navigation for every browse change.** Each search, source, sort, or page change navigates to another server-rendered address. This conflicts with keeping browse settings out of navigation URLs and would replace the current browse request model.
3. **Client-only loading.** The server returns an empty shell and the browser requests all initial data. This loses the existing server-loaded first response and does not restore the requested server detail route.

For detail navigation, retaining the in-place view was considered against restoring the dedicated route. The learner selected restoration to follow Agents; the detail redirect and client selection hook are superseded.

## Decision

Retain option 1 and apply the accepted amendments below. All product JSX uses existing Hades components. Feature components remain scoped to Marketplace, with detail-only files colocated under `[marketplaceSlug]/[skillName]`.

The accepted named-parameter amendment (2026-10-16) replaces the catch-all directory with two named parameters. The 2026-10-27 backend API alignment names the second parameter `skillName`, giving `[marketplaceSlug]/[skillName]/page.tsx`. The public detail URL retains two path segments. A one-segment path now serves the selected Marketplace overview; paths with more segments than a Skill detail return 404.

The dashboard layout's `PageGutter` owns outer page spacing for both Marketplace routes. Marketplace uses that shared layout directly, following the dashboard convention that feature pages do not add their own outer padding.

### Selected Marketplace overview path (2026-10-29)

The overview for a selected Marketplace is `/marketplace/{marketplaceSlug}`. It lives in the existing `[marketplaceSlug]` directory, alongside the nested Skill detail route; no separate `/marketplaces` route is needed. The existing `/marketplace` entry loads the saved Marketplace, or the first allowed Marketplace, and replaces its address with the resolved path after rendering. Picking another Marketplace replaces the path after its browse request succeeds. A failed or superseded request retains the address of the results on screen. Replacing the history entry avoids adding browse-only history entries.

The named route loads the requested slug on the server with the existing authenticated loader. If browser Back restores a cached overview for a different Marketplace, the hook follows the Next.js pathname and reloads that selection through the authenticated browse BFF before updating the address from the results. A slug is a selection, not authorization: skills-service still checks the user's tenant and policy. Unknown selections retain the loader's existing fallback and are canonicalized to the resolved Marketplace. Search, sort, page, and page size remain in memory and reset on a fresh server load. The hook preserves unrelated query parameters and fragments when updating the path. Storybook preview paths remain outside this routing behavior.

The two overview entry points share one Marketplace-scoped server component. Skill detail URLs remain `/marketplace/{marketplaceSlug}/{skillName}`; their Skills links return to `/marketplace/{marketplaceSlug}`. Dashboard navigation and breadcrumbs recognize the selected overview route. The hook recognizes only the entry and one-segment overview paths, so it does not rewrite a Skill detail address. This amends only the Marketplace-address portion of the earlier browse-state decision; backend API paths are unchanged.

### Routes and state

| Concern                  | Current behavior                                                                                                                                                                                                                                                                                |
| ------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Overview                 | `/marketplace` loads the saved or first Marketplace; `/marketplace/{marketplaceSlug}` loads a specific Marketplace. The address reflects the resolved selection.                                                                                                                                |
| Browse controls          | `useMarketplace` owns source, search, sort, page, page size, results, loading, errors, cancellation, and retry. Only the resolved Marketplace slug appears in the path.                                                                                                                         |
| Old browse links         | The overview ignores old browse query values for its initial load. The hook removes those keys while preserving unrelated query context, fragments, and Next.js history state.                                                                                                                  |
| Detail                   | `/marketplace/{marketplaceSlug}/{skillName}` loads the requested Skill on the server. Each identifier is encoded as one path segment. The page awaits `params`; it passes the backend-provided `skillName` without splitting the full Skill slug or inferring identity from browse preferences. |
| Direct links and refresh | A valid detail link opens that Skill, including after refresh. A missing Skill shows the missing-Skill state; an address with fewer or more than two segments uses the app's 404 page.                                                                                                          |
| Return                   | The detail page's Skills link returns to `/marketplace/{marketplaceSlug}`. Other browse values reset on the server load.                                                                                                                                                                        |
| Local presentation       | The overview owns list/card selection and the toolbar owns picker-open state. These are not navigation parameters.                                                                                                                                                                              |

Browse API requests validate search, page size, page, sort, and Marketplace through the existing parser and server loader. Named overview server loads supply the path slug and default values for the other browse controls. Search is by Skill name, starts after the hook's debounce, and treats fewer than three non-space characters as no search. Sort and page-size changes reset to the first page. A page beyond the results is clamped on the server.

### Loading and responsibilities

| Boundary                                    | Responsibility                                                                                                                                                                                                                                                        |
| ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Overview pages                              | The default entry and named Marketplace route use `MarketplaceOverviewPage`, which validates the session, loads the requested or saved Marketplace, and renders `MarketplaceShell`.                                                                                   |
| Overview shell and provider                 | Mount the browse hook/context and render controls and results. No selected-Skill state lives here.                                                                                                                                                                    |
| `[marketplaceSlug]/[skillName]/page.tsx`    | Validates the session, reads both named path identifiers, calls `loadSkillPage`, and renders the requested detail or its failure state.                                                                                                                               |
| `[marketplaceSlug]/[skillName]/loading.tsx` | Defines the single-use detail loading skeleton inline while the Server Component resolves.                                                                                                                                                                            |
| Detail components                           | Render the Skill title, description, `SKILL.md`, resource files, and Source metadata. The retry component calls `router.refresh()` inside a transition and disables itself while pending.                                                                             |
| Browse BFF                                  | `GET /api/marketplace/skills` uses `withAuth`, parses API query values, and calls the shared overview loader.                                                                                                                                                         |
| Shared server loader                        | Owns source selection and browse shaping. `loadSkillPage` resolves the requested Marketplace display name from the authenticated source list and uses a private `loadSkillDetail` helper. Results distinguish success, missing data, refusals, and upstream failures. |
| Server client                               | Calls skills-service through `SKILLS_SERVICE_URL`, forwarding the session's bearer token with a ten-second timeout. The browse request also combines the caller's abort signal.                                                                                       |

The detail UI, retry, component tests, and stories live in `frontend/src/app/(dashboard)/marketplace/[marketplaceSlug]/[skillName]/_components/`. The route's `page.tsx`, `page.test.tsx`, and `loading.tsx` sit alongside that directory. Overview UI, context, hook, and shared loaders remain in their existing Marketplace directories.

The dedicated Server Component is the entry point for Skill details. The unused `GET /api/marketplace/skill` BFF and its URL builder were removed in the 2026-10-19 cleanup; `loadSkillDetail` is private to the server loader. Keeping that helper internal avoids maintaining an unused browser API alongside the server detail route.

### Backend types and identifiers

`frontend/src/shared/api.ts` mirrors skills-service with `Marketplace`, `GetMarketplaces`, `SkillSummary`, `MarketplaceDetail`, `SkillFile`, and `Skill`. `SkillSort` belongs to the frontend feature because the backend does not sort by display name.

A Marketplace's `marketplaceSlug` is its identity; its name is a label and need not be unique. The selected source and saved `marketplace.lastSelected` preference hold the slug. A Skill detail request uses the Marketplace slug and the backend-provided `skillName` (the URL-safe name segment). The full `skillSlug` remains the stable identifier for list keys and display; the frontend does not derive `skillName` from it. Existing links containing the full `skillSlug` remain valid because the backend accepts it as the detail path value. The Source panel obtains the Marketplace label from the authenticated list for that request.

The backend marketplace endpoint (`GET /v1/marketplaces/{marketplaceSlug}`) returns the selected Marketplace metadata and at most 200 Skills per cursor batch, in Skill-slug order, and may include `nextCursor`. Each summary includes `skillName`. Detail requests use `GET /v1/marketplaces/{marketplaceSlug}/{skillName}`. The server-only `fetchSkills` client follows every cursor for the selected Marketplace and search, using the same session token. The shared loader then sorts the complete matching list by name, counts it, and returns only the requested display page to the browser.

For cursor integration, passing a cursor directly to each browser page was considered. The existing backend does not supply name sorting or a total count, so that option would change the current sort and numbered-page behavior. Following cursors on the Next.js server preserves that behavior without changing the backend contract. Each Skills load has one ten-second deadline covering all batches, also honors caller cancellation, and rejects repeated cursors. A later batch failure rejects the load instead of presenting a truncated list as complete.

This choice reads and holds all matching summaries on the server for each browse request. Network work and server memory therefore grow with the matching catalogue; the browser still receives one display page. Supporting large catalogues with work proportional to one page requires a future backend contract with name sorting and counts.

### Preferences and request coordination

After an explicit source selection loads successfully, `MarketplaceProvider` updates the shared preferences document with the selected slug. It preserves unrelated preferences, skips a redundant value, and never saves defaults over a failed preferences read. `PreferencesProvider` owns the debounced `PUT /api/preferences`.

Each new browse request aborts its predecessor and receives a newer request ID. Only the latest active response can update the view. Pending and failed later requests retain previous results, and retry repeats the failed intent. If only the initial Skill-list load fails, the successfully loaded Marketplace list remains available for choosing another source.

### Identity, policy, and connections

- **Who is this agent?** The dashboard reuses its verified user session. Server Components validate the session, and BFF routes retain `withAuth`. The upstream request uses that session's platform token.
- **What can it access?** skills-service remains responsible for authorization and tenant-scoped source access. The frontend does not substitute local policy checks or use the saved source selection as authority.
- **How does it connect?** All upstream calls and credentials stay server-side. The browser navigates to the detail Server Component or calls the authenticated browse BFF. Repository bodies are displayed as plain text through Hades `CodeBlock` without executing Markdown or HTML.

### Stories and verification

Browse stories mock the browse BFF and preferences writes. Their sample Marketplace slugs are distinct from display labels, and card stories assert native detail links. Detail stories supply read-only Skill props directly; retry uses Storybook's mocked App Router.

Vitest covers loaders, sessions and failure states, exact identifier forwarding, link encoding, browse request coordination, preferences, file contents, and retry transitions. Cursor tests cover catalogues above 200 Skills, global sorting and counts, page clamping, cancellation, the shared deadline, failed later batches, and repeated cursors. Playwright exercises the actual Next.js overview/detail routes using a test-only HTTP skills-service fixture for server fetches and browser route mocks for later browse requests. No live Go backend, Postgres, Redis, or production fixture path is required.

The [accessibility and layout evidence](../../frontend/e2e/marketplace-accessibility.md) records browser checks and the remaining manual screen-reader work. Required frontend checks remain lint and TypeScript; the root quality gate is required before committing.

## Consequences

- Dedicated Skill URLs work independently of previously selected browse state and survive refresh.
- Browse controls retain their request cancellation, retained-results, retry, and preference behavior.
- Keeping server initial loads and browser browse updates requires shared validation and loading rules at both entry points.
- Skill detail loading stays behind the server page and its private helper; the authenticated browse BFF remains the browser API for Marketplace updates.
- The selected Marketplace is shareable through its overview path. Search/page/sort state is not shared through navigation URLs, and a detail return link restores the Marketplace with default browse controls.
- Catalogue sorting, counts, and display pages cover every cursor batch. Server work grows with the matching result set and is bounded by the Skills-load deadline.
- Manual screen-reader evidence and deployment wiring remain independent of the browse and detail routes.
- This frontend change adds no source-management, Skill installation, agent assignment, backend authorization, or credential-brokering behavior.

## Decision history

| Date       | Decision                                                                                                                                             | Current standing                                                                                                   |
| ---------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| 2026-08-17 | Proposed server initial rendering with BFF-driven browse updates and dedicated Skill details.                                                        | Original proposal remains Proposed; the accepted amendments define the implemented behavior above.                 |
| 2026-10-12 | Keep browse settings in memory, use server defaults with the saved source, and omit browse parameters from navigation URLs.                          | Accepted browse-state amendment.                                                                                   |
| 2026-10-12 | Open Skill details in place through a client hook and detail BFF; redirect old detail addresses to the overview.                                     | Superseded by the 2026-10-16 restoration. The hook/view and redirect were removed.                                 |
| 2026-10-13 | Align frontend types and identifiers with skills-service instead of mapping the earlier draft package types.                                         | Accepted. The real server client now calls skills-service rather than serving fixtures.                            |
| 2026-10-16 | Restore server-rendered Skill details and colocate their UI/loading/retry/stories/tests under `[...slug]`, following Agents.                         | Accepted for this amendment. Further browse URL design is a separate step.                                         |
| 2026-10-16 | Replace the catch-all directory with `[marketplaceSlug]/[skillSlug]`, retaining the two-segment detail URL and colocated detail files.               | Accepted named-parameter amendment, explicitly requested by the learner.                                           |
| 2026-10-19 | Remove the unused Skill-detail BFF and URL builder, keep the detail helper private, inline the route skeleton, and use dashboard-owned page spacing. | Implemented cleanup of the server-rendered detail architecture.                                                    |
| 2026-10-21 | Follow Skills cursors server-side before name sorting, counting, and slicing a display page.                                                         | Pagination correction with a shared deadline and cancellation; preserves the existing browser API.                 |
| 2026-10-27 | Align with the backend marketplace endpoint and use the returned `skillName` for detail navigation.                                                  | API contract alignment; retains cursor handling, full-slug link compatibility, and server-side authentication.     |
| 2026-10-28 | Put the selected overview at `/marketplaces/{marketplaceSlug}` and honor it on direct visits, refresh, and detail return.                            | User-requested amendment; other browse values remain in memory, and identity/policy enforcement stays server-side. |
| 2026-10-29 | Move the selected overview to `/marketplace/{marketplaceSlug}` inside the existing Marketplace route directory.                                      | User-requested consolidation; preserves selected-source navigation and the backend API contract.                   |

## Related decisions

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md)
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md)
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md)
- [Frontend Design System](2026-03-25-terra-frontend-design-system.md)
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md)
- [Marketplace backend](2026-08-28-marketplace-adr-backend.md)
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md)
