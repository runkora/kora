# Agent Marketplace Frontend Architecture

**Status:** Proposed  
**Date:** 2026-08-17  
**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)  
**Technical Story:** [Agent Marketplace PRD](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md) ([PR #225](https://github.com/clear-route/orbit/pull/225))

---

## Context

> **RSC** (React Server Component): a React component that runs only on the server.
>
> **BFF** (Backend-for-Frontend): a Next.js route that the browser calls, and that calls the Orbit services for it.
>
> **Slug:** the text identifier that the backend gives each Skill.

Orbit needs a read-only Marketplace under Control Plane where signed-in users can select a Marketplace, browse and search its Skills, and review Skill details.

This ADR decides how the frontend loads data, divides responsibilities between server and client code, manages browse state, and presents results and errors.

### Background

Orbit's frontend uses the Next.js App Router, React Server Components, and same-origin Backend-for-Frontend routes. The BFF reads the user session and calls authorized Orbit services.

All product JSX uses Terra (`@orbit/terra`), the Orbit design system. Shared frontend components follow Orbit's three-tier component model. The Marketplace must use these existing patterns instead of adding another state library, design system, or direct source-provider integration.

The frontend consumes `skills-service`, the back-end service, through a server-only Marketplace client. It does not call GitHub or access backend storage directly. The frontend treats all repository content as unsafe, read-only data.

All three options consume the same backend API. The backend defines the offset/limit pagination, and the frontend uses it as it is.

### Problem Statement

Choose a frontend architecture that:

1. Shows the Marketplace names and one page of Skills in the first response. The page uses the Marketplace in the URL. When the URL contains no Marketplace, the page uses the Marketplace that the user selected last. On a first visit, or for an unknown name, the page uses the first Marketplace in name order.
2. Keeps the controls and the current results on screen while new results load after a browse change.
3. Supports Marketplace selection, name search, a sort order by name, a page size that the user selects, and pagination. The browser never loads the complete dataset from the back-end.
4. Restores the selected view from the URL during refresh, browser Back and Forward navigation, and direct navigation.
5. Loads read-only Skill details on a dedicated route without an additional browser data request.
6. Ensures that only the latest active browse request can update the displayed results.


### Goals

* Add a Marketplace overview route under Control Plane, and a dedicated Skill detail route that opens when the user selects a Skill in the overview.
* Show Skills in the first response, and keep the controls and the current results on screen during later browse changes.
* Keep the URL, displayed results, and pagination position consistent.
* Render Skill content and supplied resource files read-only.
* Reuse Terra and Orbit's authentication, component-placement, and testing patterns.
* Support desktop, tablet, mobile.

### Non-Goals

* This ADR does not choose or restate the backend implementation of source access, credentials, caching, storage, refresh, or search.
* The frontend does not install, run, assign, update, roll back, or remove Skills.
* The frontend does not add Skills to agents, AgentTypes, environments, or runs.
* The frontend does not add separate preview or download interactions for repository files. Content that the detail response supplies stays in scope, and the page renders it.
* The Marketplace does not add a new frontend framework or state library.
* The frontend does not manage Marketplace sources. It adds no filters that the [PRD](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md) does not ask for. The sort order ([PRD FR-18](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)) is not in the backend contract yet (see Open Integration Questions).

---

## Decision Drivers

* Orbit access and refresh tokens must remain server-side.
* Every signed-in Orbit user must be able to open the Marketplace. The frontend must reuse Orbit's existing authentication and session mechanisms.
* Repository content must remain read-only and must not execute in the browser.
* The user must be able to choose a page size that the backend contract allows.
* The page must restore the selected Marketplace, search text, sort order, page size, and page number from the URL. It must do so after a refresh, after a click on the browser Back or Forward button, and when a user opens a direct link ([PRD FR-17](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)).
* A save of the Marketplace must never replace the user's other stored preferences.
* Skills with the same display name in different Marketplaces must remain distinguishable during navigation.
* Marketplace failures must not affect unrelated Orbit pages.


---

## Considered Options

### Option 1: RSC initial rendering with BFF-driven browse updates

**Description:** The overview Server Component loads the Marketplace names, sorted by name, for the initial page render. It also loads one result page of Skills for the Marketplace, search text, page, and page size in the URL. If the URL does not contain a valid Marketplace name, the Server Component uses the first Marketplace in the sorted list.

After the page loads, a client hook sends a BFF request for each Marketplace selection, search, page-size change, and page change. Each BFF response contains the Marketplace list and the new Skill results. The BFF uses the same validation rules as the Server Component. After each successful response, the hook writes the new browse values to the URL with the browser History API. Next.js does not send a server request for these URL changes, so the Server Component does not run again.

Skill details use a dedicated server-rendered route.


**Pros:**


* The initial server render fills the Marketplace selector, so the browser sends no extra request.
* The initial server render includes one page of Skills, for the Marketplace in the URL or for the first Marketplace.
* After the page loads, Marketplace selection, search, and page changes update the results without a new server render of the route.
* Reuses Orbit's existing React Server Component and BFF architecture.

**Cons:**

* The frontend must support two data-loading paths: server rendering for the initial page load and BFF requests for later user interactions.
* The Client Component must manage updated Skill results, loading states, pagination, and URL changes after the page loads.
* The frontend must prevent older requests from replacing newer results when users quickly change Marketplace, search, or pagination.
* The URL changes without a Next.js navigation, so the Next.js router state stays at the first server render. Back or Forward from another route can show that render again.


### Option 2: RSC navigation for every interaction

**Description:** The Server Component renders the initial Marketplace page. When the user selects a Marketplace, searches, changes the page size, or moves between pages, the frontend updates the URL through Next.js navigation. Each navigation causes the Server Component to load the required Skill results and return updated page content. Browse updates use no separate BFF request.

**Pros:**

* Uses the same server-rendered data-loading path for both the initial page load and later browse interactions.
* Needs no BFF route for browse changes.
* Requires less client-side request and result-state management than Option 1.
* Each URL gets its own server render, so Back or Forward from another route shows the correct browse state.

**Cons:**

* Each browse change is a Next.js navigation, so the Server Component runs again before the new results appear. According to the frontend state golden path, each such navigation also shows the loading skeleton.
* Quick changes to the search text or the Marketplace can start server renders for states that the user no longer needs.


### Option 3: Client-only Marketplace application

**Description:** The initial server response renders only the Marketplace page structure. After the page loads in the browser, a Client Component requests the Marketplace names through the BFF. The same Client Component then sends a BFF request for each Marketplace selection, search, page-size change, and page change. It updates the displayed Skill results with each response.

**Pros:**

* Uses one data path, the BFF, for the first load and for all later browse changes.
* Keeps Marketplace loading and interaction state in one Client Component.
* Marketplace selection, search, and pagination can update the Skill results area without re-rendering the route.


**Cons:**

* The browser loads the Marketplace names only after the page opens and the Client Component sends its first BFF request.
* The initial server render contains no Marketplace data, so the selector may show a loading state before the Marketplace names appear.
* The Client Component must manage the first Marketplace data request as well as later Marketplace selection, search, pagination, loading, and error states.
* For a direct link with a Marketplace in the URL, the Skill results still wait for the first BFF request from the browser.

---

## Decision

The overview Server Component loads the Marketplace names and one page of Skills. It uses the Marketplace in the URL. When the URL names no Marketplace, it uses the Marketplace that the user selected last, which Orbit keeps in the user preferences. When neither Marketplace is available, it uses the first Marketplace in name order.

The interactive overview receives that initial data and owns the later changes to the Marketplace, the search text, the sort order, the page size, and the page. When the page becomes interactive in the browser, the Client Component must reuse the Marketplace data that the server already loaded. It must not request the same data again when the page loads. Each later BFF response also contains the Marketplace list, because the server needs the list to validate the Marketplace name.

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

Option 1 loads the Marketplace names and one page of Skills in the initial server response. The interactive component starts with this data and does not request it again when the page loads.

After that, each change to the Marketplace, search text, sort order, page size, or page number sends a BFF request. The route does not render again on the server, and the controls stay on screen. The [frontend state golden path](../../frontend/docs/golden-paths/frontend-state-pattern.md) requires the same pattern for server-side filters. It states that `router.push` causes a full RSC render and a loading skeleton flash for each change. Option 2 has that cost for every browse change. Option 3 waits for browser JavaScript before it loads any Marketplace data.

The decision follows Orbit’s three architecture questions.

  - Who makes the request? The Server Component and BFF use the current user’s Orbit session. skills-service authenticates the user’s token on every request.

  - What can it access? skills-service enforces its access rules and the tenant boundaries. The backend ADR requires `marketplace:read`, but the frontend shows the Marketplace to every signed-in user (see Open Integration Questions). A Skill page gives no access to other Orbit resources.

  - How does it connect? The browser communicates with Next.js. Server Components load initial and detail data. Later browse requests pass through BFF routes. Only server-side code calls skills-service with the user’s Orbit token.

---

## Consequences

### Positive

* The server renders the initial Marketplace data and direct-linked Skill details.
* Later browse changes update the results without a new server render of the route.
* One owner coordinates interactive state and prevents inconsistent controls, results, and URLs.
* After a failed request, the page keeps the last authorized results and labels them as the results of the last successful request.
* The Marketplace reuses Orbit's security, Terra, and testing patterns.
* The server render, the BFF route, and the browser read and write the URL values with one module (`browse-params.ts`). The server render and the BFF route also share the data rules in `marketplace-server.ts`.
* A returning user sees the Marketplace that they picked last, also after a link without `marketplace`, such as the sidebar entry. The server reads the saved Marketplace, so the first render shows it with no second request.
* On a Skill page, the header Back button returns to the same Marketplace, search, sort order, page size, and page.

### Negative

* The interactive overview must coordinate debounce, cancellation, late responses, and Back and Forward navigation.
* Each browse change sends at least two requests to skills-service: one for the Marketplace list and one for the Skills. A page after the last page causes a third request.
* The history entries that the hook adds keep the Next.js router state of the first server render. After Back from a Skill detail page, Next.js shows that first render from its cache. The overview then sends one BFF request for the URL, and the old results stay on screen until the response arrives.
* When the URL names no Marketplace, the server also reads the user preferences. In the server render, the dashboard layout already read them, and React `cache()` reuses that result. In the BFF route, the read adds one request to the identity service.
* After a failed preferences read, the page does not save a pick. The next visit without `marketplace` opens the previously saved Marketplace.
* The preferences write is best-effort and waits 500 ms after the last change. If the user opens a link without `marketplace` in that time, the server can still read the previous Marketplace. A full page load in that time, such as a click on the sidebar entry, prevents the write.
* The Skill URL is longer, because it carries the browse values.
* Until skills-service is available, a mock client serves fixture data. The pages do not show real Skills yet.

### Neutral

* skills-service remains responsible for access checks, search, sort, and pagination.
* The pages use the backend slug as an opaque identifier, so a change to the slug format does not change the pages. The real client must know the format, because the backend detail endpoint takes the Marketplace name and the Skill name as separate path values.


---

## Implementation

| Area | Behavior |
| --- | --- |
| Overview (`/marketplace`) | The server loads the Marketplace names in name order and one page of Skills. The client reuses this initial data. |
| Browse requests | Marketplace selection, search, sort order, page size, and pagination call the same-origin BFF route `/api/marketplace/skills`. The route validates the input and calls skills-service through the server-only Marketplace client with the Orbit session. The response contains the Marketplace list and one page of Skills. |
| Overview state | A custom hook (`_hooks/use-marketplace.ts`) and a React Context (`_context/marketplace-context.tsx`) own the state, as the frontend state golden path describes. A new request cancels the previous request. Only the latest successful request updates the results and the URL. A cancelled request shows no error. |
| Saved Marketplace | The provider (`_context/marketplace-context.tsx`) saves a picked Marketplace in the user preferences key `marketplace.lastSelected` with `usePreferences().updatePreferences`, as the Models page saves its favourites. The key and its default `''` are in `frontend/src/app/(dashboard)/_preferences/preferences.schema.ts`. The provider saves only after the load of the picked Marketplace succeeds (`onMarketplacePicked`), and only when the name changed. A preferences PUT replaces the whole document. So the provider does not save when the preferences read failed (`usePreferences().loaded` is false). The dashboard layout gets this flag from `readPreferences`. |
| Sort | A select in the toolbar offers A to Z and Z to A by Skill name ([PRD FR-18](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). A change goes back to page 1. The BFF passes `sort` to the Marketplace client. The mock client sorts all matches before it cuts the page, so the order is correct across pages. |
| Pagination | A bar below the results shows the result range, a rows-per-page select, Previous and Next buttons, and page buttons. The page buttons show the first page, the last page, and the pages within 2 of the current page. A gap stands for each run of hidden pages. |
| URL history | A page change adds a history entry (`pushState`). Other changes replace the current entry (`replaceState`). Back and Forward inside the overview reload the results for the restored URL. If that reload fails, the URL of the results on screen comes back. Each overview response also contains the browse values of its request (`requested`). On mount, the hook compares the URL with these values and with the values that the server used. When the URL matches neither set of values, Next.js restored an old server render from its cache. The hook then loads the results for the URL. |
| Loading | During the first server load, the overview and the Skill page each show a skeleton (`loading.tsx`). During a later request, the current results stay on screen, and a notice above them says "Loading Skills…". A retry after a failed first load shows the skeleton in place of the toolbar and the results. |
| Errors | If a later request fails, the page keeps the current results and shows a notice with a retry button. The toolbar stays available, so the user can select another Marketplace. A blocking state shows only when no Marketplace data exists (see Open Frontend Questions). |
| Empty results | When no Marketplace exists, the page shows "No Marketplaces available". With no search, the page shows that the Marketplace has no Skills. With a search, the page shows that no Skills match, and it offers "Clear search". |
| Views | The user can show the results as a list or as cards ([PRD FR-16](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). The list is the default. The URL and the user preferences do not keep this choice. |
| Skill detail (`/marketplace/[...slug]`) | The server loads the Skill in the first response, and the page shows its name, Marketplace, description, and path ([PRD FR-7](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). It shows the `SKILL.md` file and each resource file as plain text ([PRD FR-9](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements), [FR-15](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). It shows a separate message for an expired session, a missing Skill, and a failed load. The failed-load message has a "Try again" button (`_components/marketplace-skill-load-error.tsx`, [PRD FR-14](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). The button renders the page on the server again (`router.refresh`) and stays disabled until that render completes. The browser sends no other request. |
| Back from a Skill | In the card view and the list view, the Skill URL carries the browse values of the results on screen (`buildSkillUrl`). For the Marketplace section, the shared `DashboardHeader` shows "Marketplace" in the breadcrumb, and its Back button keeps this query (`keepQueryOnBack`). So Back returns to the same Marketplace, search, sort order, page size, and page, also after a reload or from a shared link ([PRD FR-17](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). Other sections do not set `keepQueryOnBack`, so their Back button still opens the section root. The Skill page has no back link of its own, as on most detail pages in Orbit. The "Skill not found" message has a "Back to Skills" link, built from the validated values (`parseBrowseParams`, `buildOverviewUrl`). |
| Skill identifier | The detail route passes the full backend slug to the server. The pages do not split the slug or read display data from its parts. Only the shared header shows the last part of the URL path in its breadcrumb, as in other sections. The list view and the card view show the full slug under the Skill title. Unless the backend adds an endpoint for the full slug, the real client must split the slug (see Open Integration Questions). |
| Navigation | The Marketplace entry is visible to every signed-in user. The frontend adds no scope check. The sidebar compares the exact path for every entry, so the Marketplace entry is active on `/marketplace` but not on a Skill page. |
| Data source | Until skills-service is available, a server-only mock client (`frontend/src/server/marketplace-client.ts`) serves fixture data (`frontend/src/server/marketplace-fixtures.ts`). The fixture Skills have no resource files. The real client must use a 10-second timeout together with the caller's abort signal. It must throw `ApiError` with the upstream status for each failed response. |
| Testing | The frontend test ADR requires one happy-path test and one test for each mapped error for every BFF route. It also requires 100% path coverage for `lib/routing/api-routes.ts` and `lib/routing/routes.ts`, which contain Marketplace entries. The Marketplace tests are not in the repository yet. All of them come in one change when the skills-service API is ready, so that the test set is complete. Planned tests:<br>- MSW integration tests for the hook, including the mount check (`_hooks/use-marketplace.integration.test.ts`)<br>- MSW integration test for the saved Marketplace in the provider, including the kept preferences of other features (`_context/marketplace-context.integration.test.tsx`)<br>- MSW test for the browser fetch helper (`_services/marketplace-api.ts`)<br>- Tests for the BFF route (`app/api/marketplace/skills/route.ts`)<br>- Unit test for the Marketplace choice in `loadMarketplaceOverview` (`_services/marketplace-server.test.ts`)<br>- Unit tests for the URL rules, including the Skill URL and the back link URL (`_utils/browse-params.test.ts`)<br>- Unit test for the preference key (`_preferences/preferences.schema.test.ts`)<br>- Unit test for the `loaded` flag of the preferences read, with MSW in place of a `fetch` stub (`_preferences/preferences-server.test.ts`)<br>- Integration test for the Skill URLs in the list view and the card view (`_components/marketplace-skill-links.integration.test.tsx`)<br>- Integration test for the header Back button, for the Marketplace and for other sections (`(dashboard)/_components/dashboard-header-back.integration.test.tsx`)<br>- Marketplace entries in `lib/routing/api-routes.test.ts` and `lib/routing/routes.test.ts`. |

### Browse Parameters

The overview keeps five values in the URL. The server render, the BFF route, and the browser read and write these values with one module (`frontend/src/app/(dashboard)/marketplace/_utils/browse-params.ts`). Two rules need data from the server: the Marketplace choice and the last-page rule. `loadMarketplaceOverview` applies these two rules on the server (`frontend/src/app/(dashboard)/marketplace/_services/marketplace-server.ts`).

| Parameter | Rule | Default |
| --- | --- | --- |
| `marketplace` | A Marketplace name. When the URL has no `marketplace`, the server uses the saved Marketplace (`marketplace.lastSelected`) if it still exists. The server replaces an unknown name with the first Marketplace. | The saved Marketplace, or else the first Marketplace in name order |
| `query` | The search text without spaces at the start and end. Text with fewer than 3 characters counts as no search, and a hint below the field asks for at least 3 characters. The search starts 300 ms after the last change. skills-service matches the text against the Skill name only. | No search |
| `sort` | `name-asc` (A to Z) or `name-desc` (Z to A), by Skill name. | `name-asc` |
| `page` | A whole number from 1. The server replaces a page after the last page with the last page. | 1 |
| `pageSize` | 12, 24, or 48. All three values are below the backend maximum of 100. | 24 |

* An invalid `sort`, `page`, or `pageSize` value becomes its default.
* The URL does not contain `query`, `sort`, `page`, or `pageSize` when they have their default values. Other query keys, such as `agent`, stay in the URL.
* A Skill URL carries the same values, with the same rules, for the header Back button.
* After a successful first render, the URL always contains `marketplace` when a Marketplace exists, because the server chooses the Marketplace.
* After the first render, the browser writes the validated values to the URL. If the first load fails, the URL stays as it is, so a reload tries the same values again.
* `loadSkillsPage` in `marketplace-server.ts` converts `page` and `pageSize` to the backend `offset` and `limit`.

### Open Integration Questions

The backend ADR for skills-service (PR #224) is not merged yet. These points must agree with it before the Marketplace is complete:

* **Slug format:** the backend ADR uses `{marketplace-name}/{skill-name}`. The mock uses `{org}/{repo}/{skill-name}`. The pages use the slug as it is, so both formats work for the pages. But the backend detail endpoint is `GET /v1/marketplaces/{marketplaceName}/skills/{skillName}`, and no endpoint accepts the full slug. Either the real client splits the slug into its two parts, or the backend adds an endpoint that accepts the full slug.
* **Field names:** the frontend types use names such as `title_of_package` and a top-level `total`. The backend ADR uses `skills`, `name`, `pagination.totalItems`, `skill`, and `documents`. The real client must map these names, or the types must change.
* **Marketplace name on the detail page:** [PRD FR-7](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements) asks for the Marketplace on the Skill page, but the backend detail response has no Marketplace name. Until a decision exists, the page shows the mock value.
* **URL-safe Marketplace name:** the list path needs the URL-safe form of the Marketplace name, but `GET /v1/marketplaces` returns only the display name.
* **Scope:** the backend ADR requires `marketplace:read` for each endpoint, but the frontend shows the Marketplace to every signed-in user ([PRD FR-2](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements)). This ADR does not yet define the page for a request that skills-service refuses. `marketplace-server.ts` keeps the status and message of an `ApiError`, as the other server services do. The first render of the overview shows the upstream message. After a browse change, the BFF route passes a 4xx status, and the notice shows "Failed to load Skills" for every upstream failure. The Skill page shows its failed-load message for every failure except 404.
* **Sort:** [PRD FR-18](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements) asks for A to Z and Z to A across all pages. The backend ADR has no sort parameter. The frontend sends `sort`, and only the mock client applies it.
* **Search fields:** [PRD FR-4](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#functional-requirements) and the backend ADR search Skill names only. The search field says "Search Skills by name or description", and the mock client also matches descriptions and `*` wildcards.
* **[PRD user journey](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#user-flow):** the [PRD](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#user-flow) says that the page opens with the first Marketplace in name order. With the saved Marketplace, this is true only on a first visit. This is intended behaviour.

### Open Frontend Questions

* **Failed first load:** when the first render loads the Marketplace list but not the Skills, the page does not keep the list (`page.tsx`, `emptyOverview`). The page then shows only the error and "Try again", so the user cannot select another Marketplace. With a saved Marketplace, each visit without `marketplace` opens the same Marketplace again. This conflicts with the [PRD Reliability section](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md#non-functional-requirements).
* **Back from a Skill detail page:** a browser test on 2026-09-27 confirmed the risk under Negative consequences. The fix is in the Implementation table (URL history), and a planned hook integration test will cover it (see Testing). No E2E test covers Back from a Skill detail page yet.
* **Browser tests:** no browser test covers the saved Marketplace or the header Back button from a Skill page yet. The planned unit and integration tests will cover the same behaviors without a browser (see Testing).

### Migration Path

Marketplace is a new read-only dashboard area, so it needs no migration of existing frontend data. The new preference key `marketplace.lastSelected` needs no backend change and no data migration, because the identity service stores the preferences document as it is (see User App Preferences). Replace the mock client with the real client when the backend API is available. Use Orbit's normal deployment process. Resolve all open questions before the team marks the Marketplace as complete.

---

## Validation

### Success Metrics

* The first response of `/marketplace` contains the Marketplace names and one page of Skills. Check: an E2E test reads the first HTML response.
* A browse change loads new results with no server render of the route. Check: the browser network log shows only `/api/marketplace/skills` requests.
* A shared overview link opens the same Marketplace, search, sort order, page size, and page. Check: an E2E test.
* A Skill page sends no browser data request after the first response. Check: the browser network log.

### Monitoring

The BFF route and the server functions log each upstream failure with their name as a prefix, for example `[GET /api/marketplace/skills]` (`frontend/src/app/api/marketplace/skills/route.ts:37-39`).

---

## Related Decisions

* [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md) - Next.js App Router and frontend structure.
* [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md) - sealed server-side session and token boundary.
* [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md) - scope-aware navigation and presentation.
* [Terra Frontend Design System](2026-03-25-terra-frontend-design-system.md) - required UI foundation.
* [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md) - three-tier component placement.
* [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md) - frontend testing approach.
* [User App Preferences](2026-05-08-user-app-preferences.md) - server-side user preferences that keep the saved Marketplace (`marketplace.lastSelected`).
* [Frontend State Golden Path](../../frontend/docs/golden-paths/frontend-state-pattern.md) - custom hook, React Context, RSC page, and BFF route pattern.
* **Agent Marketplace — Cache-Aside, Tenant-Scoped Redis Keys, and Resource-Aware Skills** ([PR #224](https://github.com/clear-route/orbit/pull/224), branch `siddhant/marketplace-adr-backend`, file `docs/adr/2026-08-28-marketplace_adr_backend.md`, commit `a715373`, not merged yet) - authoritative backend API contract, Skill data model, tenant resolution, pagination, and status-code behavior.

---

## References

[Agent Marketplace PRD](https://github.com/clear-route/orbit/blob/feat/skills-marketplace/docs/prd/agent-marketplace.md): `docs/prd/agent-marketplace.md` on branch `feat/skills-marketplace`, [PR #225](https://github.com/clear-route/orbit/pull/225), commit `07841e6` (2026-09-26), not merged yet.


**Last Updated:** 2026-09-28
