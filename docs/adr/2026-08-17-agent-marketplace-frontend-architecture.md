# Agent Marketplace Frontend Architecture

**Status:** Proposed; accepted amendments cover browse state (2026-10-12), backend types (2026-10-13), and server-rendered Skill routing (2026-10-16).

**Date:** 2026-08-17

**Updated:** 2026-10-16

**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)

This document describes the implemented frontend after those amendments. The original proposal and the broader [Marketplace PRD](../prd/agent-marketplace.md) retain their existing approval status. The [frontend deep dive](../architecture/marketplace.md) includes the component diagram and file map.

## Context

The Marketplace lets signed-in users browse the Skills exposed by skills-service and read their instructions and supporting files. The overview has interactive source selection, search, name sorting, page size, pagination, and list/card views. Opening a Skill must render its details on a dedicated server route, following the Agents list/detail pattern.

The frontend uses Next.js Server Components, authenticated Backend-for-Frontend (BFF) routes, the existing session and preferences providers, and Hades (`@orbit/hades`). It neither calls GitHub directly nor owns source registration, source credentials, or backend caching. Repository content is read-only data.

The earlier in-place detail implementation kept the selected Skill in client state and redirected detail addresses to the overview. The accepted 2026-10-16 amendment restores server-rendered detail navigation. Browse settings remain a separate concern: the current implementation keeps them in memory and off navigation URLs.

## Options

1. **Server initial loads with BFF browse updates.** Server Components load overview defaults and Skill details. A client hook handles subsequent browse changes through the authenticated BFF. This retains the existing frontend pattern and keeps previous results visible during requests, with cancellation and response ordering owned by the hook.
2. **Server navigation for every browse change.** Each search, source, sort, or page change navigates to another server-rendered address. This conflicts with keeping browse settings out of navigation URLs and would replace the current browse request model.
3. **Client-only loading.** The server returns an empty shell and the browser requests all initial data. This loses the existing server-loaded first response and does not restore the requested server detail route.

For detail navigation, retaining the in-place view was considered against restoring the dedicated route. The learner selected restoration to follow Agents; the detail redirect and client selection hook are superseded.

## Decision

Retain option 1 and apply the accepted amendments below. All product JSX uses existing Hades components. Feature components remain scoped to Marketplace, with detail-only files colocated under `[marketplaceSlug]/[skillSlug]`.

The accepted named-parameter amendment (2026-10-16) replaces the catch-all directory with `[marketplaceSlug]/[skillSlug]/page.tsx`. The public detail URL stays the same. Next.js supplies two named string parameters and rejects addresses with fewer or more segments through its normal 404 handling.

### Routes and state

| Concern                  | Current behavior                                                                                                                                                                                                                                  |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Overview                 | `/marketplace` loads the default browse values with the saved Marketplace, or the first available source in skills-service order.                                                                                                                 |
| Browse controls          | `useMarketplace` owns source, search, sort, page, page size, results, loading, error, cancellation, and retry. It sends browse values on API requests without adding them to the address or creating pagination history entries.                  |
| Old browse links         | The overview ignores old browse query values for its initial load. The hook removes those keys while preserving unrelated query context, fragments, and Next.js history state.                                                                    |
| Detail                   | `/marketplace/{marketplaceSlug}/{skillSlug}` loads the requested Skill on the server. Each identifier is encoded as one path segment. The page awaits `params`; it does not split an opaque Skill slug or infer identity from browse preferences. |
| Direct links and refresh | A valid detail link opens that Skill, including after refresh. A missing Skill shows the missing-Skill state; an address with fewer or more than two segments uses the app's 404 page.                                                            |
| Return                   | The detail page's Skills link returns to `/marketplace`. No extra search/page persistence or browse-state restoration mechanism is introduced.                                                                                                    |
| Local presentation       | The overview owns list/card selection and the toolbar owns picker-open state. These are not navigation parameters.                                                                                                                                |

Browse API requests validate search, page size, page, sort, and Marketplace through the existing parser and server loader. Search is by Skill name, starts after the hook's debounce, and treats fewer than three non-space characters as no search. Sort and page-size changes reset to the first page. A page beyond the results is clamped on the server.

### Loading and responsibilities

| Boundary                                    | Responsibility                                                                                                                                                                                                                                         |
| ------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Overview `page.tsx`                         | Validates the session, calls `loadMarketplaceOverview`, and passes initial data/error to `MarketplaceShell`.                                                                                                                                           |
| Overview shell and provider                 | Mount the browse hook/context and render controls and results. No selected-Skill state lives here.                                                                                                                                                     |
| `[marketplaceSlug]/[skillSlug]/page.tsx`    | Validates the session, reads both named path identifiers, calls `loadSkillPage`, and renders the requested detail or its failure state.                                                                                                                |
| `[marketplaceSlug]/[skillSlug]/loading.tsx` | Shows the detail loading skeleton while the Server Component resolves.                                                                                                                                                                                 |
| Detail components                           | Render the Skill title, description, `SKILL.md`, resource files, and Source metadata. The retry component calls `router.refresh()` inside a transition and disables itself while pending.                                                              |
| Browse BFF                                  | `GET /api/marketplace/skills` uses `withAuth`, parses API query values, and calls the shared overview loader.                                                                                                                                          |
| Detail BFF                                  | The existing authenticated `GET /api/marketplace/skill?marketplace=&skill=` remains available through `loadSkillDetail`. Dashboard selection no longer calls this API from a browser detail hook.                                                      |
| Shared server loader                        | Owns source selection and browse shaping. `loadSkillPage` resolves the requested Marketplace display name from the authenticated source list and shares `loadSkillDetail`. Results distinguish success, missing data, refusals, and upstream failures. |
| Server client                               | Calls skills-service through `SKILLS_SERVICE_URL`, forwarding the session's bearer token with a ten-second timeout. The browse request also combines the caller's abort signal.                                                                        |

The detail UI, skeleton, retry, tests, and stories live in `frontend/src/app/(dashboard)/marketplace/[marketplaceSlug]/[skillSlug]/_components/`. Overview UI, context, hook, and shared loaders remain in their existing Marketplace directories.

### Backend types and identifiers

`frontend/src/shared/api.ts` mirrors skills-service with `Marketplace`, `GetMarketplaces`, `SkillSummary`, `ListSkillsResponse`, `SkillFile`, and `Skill`. `SkillSort` belongs to the frontend feature because the backend does not sort by display name.

A Marketplace's `marketplaceSlug` is its identity; its name is a label and need not be unique. The selected source and saved `marketplace.lastSelected` preference hold the slug. A Skill is addressed with both its Marketplace slug and opaque Skill slug. The Source panel obtains the Marketplace label from the authenticated list for that request.

The backend list API returns at most 200 Skills per cursor batch, in Skill-slug order, and may include `nextCursor`. The current frontend requests only the first batch and sorts, slices, and counts that batch. It does not yet follow cursors, so page numbers and total do not cover a larger catalogue. Cursor integration remains a separate decision; this routing change does not resolve it.

### Preferences and request coordination

After an explicit source selection loads successfully, `MarketplaceProvider` updates the shared preferences document with the selected slug. It preserves unrelated preferences, skips a redundant value, and never saves defaults over a failed preferences read. `PreferencesProvider` owns the debounced `PUT /api/preferences`.

Each new browse request aborts its predecessor and receives a newer request ID. Only the latest active response can update the view. Pending and failed later requests retain previous results, and retry repeats the failed intent. If only the initial Skill-list load fails, the successfully loaded Marketplace list remains available for choosing another source.

### Identity, policy, and connections

- **Who is this agent?** The dashboard reuses its verified user session. Server Components validate the session, and BFF routes retain `withAuth`. The upstream request uses that session's platform token.
- **What can it access?** skills-service remains responsible for authorization and tenant-scoped source access. The frontend does not substitute local policy checks or use the saved source selection as authority.
- **How does it connect?** All upstream calls and credentials stay server-side. The browser navigates to the detail Server Component or calls the authenticated browse BFF. Repository bodies are displayed as plain text through Hades `CodeBlock` without executing Markdown or HTML.

### Stories and verification

Browse stories mock the browse BFF and preferences writes. Their sample Marketplace slugs are distinct from display labels, and card stories assert native detail links. Detail stories supply read-only Skill props directly; retry uses Storybook's mocked App Router.

Vitest covers loaders, sessions and failure states, exact identifier forwarding, link encoding, browse request coordination, preferences, file contents, and retry transitions. Playwright exercises the actual Next.js overview/detail routes using a test-only HTTP skills-service fixture for server fetches and browser route mocks for later browse requests. No live Go backend, Postgres, Redis, or production fixture path is required.

The [accessibility and layout evidence](../../frontend/e2e/marketplace-accessibility.md) records browser checks and the remaining manual screen-reader work. Required frontend checks remain lint and TypeScript; the root quality gate is required before committing.

## Consequences

- Dedicated Skill URLs work independently of previously selected browse state and survive refresh.
- Browse controls retain their request cancellation, retained-results, retry, and preference behavior.
- Keeping server initial loads and browser browse updates requires shared validation and loading rules at both entry points.
- Search/page/sort state is not shared through navigation URLs, and restoring an arbitrary prior browse view is not guaranteed by the detail return link.
- Large-catalogue cursor handling, manual screen-reader evidence, and deployment wiring remain independent of the restored detail route.
- This frontend change adds no source-management, Skill installation, agent assignment, backend authorization, or credential-brokering behavior.

## Decision history

| Date       | Decision                                                                                                                               | Current standing                                                                                   |
| ---------- | -------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| 2026-08-17 | Proposed server initial rendering with BFF-driven browse updates and dedicated Skill details.                                          | Original proposal remains Proposed; the accepted amendments define the implemented behavior above. |
| 2026-10-12 | Keep browse settings in memory, use server defaults with the saved source, and omit browse parameters from navigation URLs.            | Accepted browse-state amendment.                                                                   |
| 2026-10-12 | Open Skill details in place through a client hook and detail BFF; redirect old detail addresses to the overview.                       | Superseded by the 2026-10-16 restoration. The hook/view and redirect were removed.                 |
| 2026-10-13 | Align frontend types and identifiers with skills-service instead of mapping the earlier draft package types.                           | Accepted. The real server client now calls skills-service rather than serving fixtures.            |
| 2026-10-16 | Restore server-rendered Skill details and colocate their UI/loading/retry/stories/tests under `[...slug]`, following Agents.           | Accepted for this amendment. Further browse URL design is a separate step.                         |
| 2026-10-16 | Replace the catch-all directory with `[marketplaceSlug]/[skillSlug]`, retaining the two-segment detail URL and colocated detail files. | Accepted named-parameter amendment, explicitly requested by the learner.                           |

## Related decisions

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md)
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md)
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md)
- [Frontend Design System](2026-03-25-terra-frontend-design-system.md)
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md)
- [Marketplace backend](2026-08-28-marketplace-adr-backend.md)
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md)
