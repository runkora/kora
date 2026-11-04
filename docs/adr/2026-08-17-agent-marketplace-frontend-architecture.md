# Agent Marketplace Frontend Architecture

**Status:** Proposed, accepted amendments cover browse state (2026-10-12), backend types (2026-10-13), and server-rendered Skill routing (2026-10-16).

**Date:** 2026-08-17

**Deciders:** Frontend Engineering, Product Owner, Security Review (TBD)

**Technical Story:** [Agent Marketplace PRD](../prd/agent-marketplace.md)

## Context

### Background

The Kora Marketplace is a read-only catalogue where users browse, search, and review Skills from GitHub repositories approved for their tenant. Skills are never installed or executed from this surface.

Marketplace is part of the Kora dashboard, the web interface signed-in users use to manage agents, identities, access permissions, and connections. It appears under **Control Plane → Marketplace** in the sidebar. Control Plane is the navigation group containing operational pages such as Agents, Connections, and MCP Servers.

The frontend owns the Marketplace overview, Skill detail pages, browsing controls, and loading and error feedback. It follows the dashboard's existing Next.js Server Component and React patterns, using the same sign-in, navigation, and page layout.

Hades (`@runkora/hades`) provides shared layouts and controls. The authenticated Backend-for-Frontend (BFF) requests backend data on behalf of the browser. Repository access, credentials, search, caching, and authorization remain backend responsibilities.

### Problem Statement

The frontend must provide a consistent experience for browsing and reviewing Skills within the Kora dashboard, using the user’s existing access rights. Valid Marketplace and Skill links must work directly and after refresh. The overview must support source selection, search, sorting, and numbered pages while sending only one result page to the browser. Users must be able to visit a Skill and return to their previous search, sort order, and result page within Marketplace.

Slow or failed browse updates must provide clear feedback and retry while keeping previous results available. Responses from older requests must not overwrite a newer selection.

### Goals

- Reuse dashboard authentication, navigation, and Hades components.
- Support shareable Marketplace and Skill pages with predictable browse state.
- Keep browsing responsive and repository content read-only, with clear loading, empty, and failure states.

### Non-Goals

Skill installation or execution, agent assignment, source management, and changes to backend authorization or credential brokering.

## Decision Drivers

- Consistency with existing dashboard rendering, authentication, and UI patterns.
- Predictable results when browse requests overlap or fail.
- Shareable pages with temporary browse controls retained during Skill visits.
- Safe display of repository content without exposing credentials or executing it.

## Considered Options

### Option 1: Server initial loads with BFF browse updates

**Description:** Server Components load the initial Marketplace overview and Skill pages. The browser then requests overview updates through the authenticated BFF when users change the Marketplace, search, sort order, or page.

**Pros:** Provides initial content with the page and keeps browsing interactive, using existing dashboard patterns. Previous results can remain visible while updates load.

**Cons:** Initial loads and later updates need shared validation and consistent error handling. The browser must cancel outdated requests and ignore responses from older requests.

### Option 2: Server navigation for every browse change

**Description:** Each change to the Marketplace, search, sort order, or page updates the address and requests a server-rendered page with those settings.

**Pros:** One page-loading model handles initial visits and browse changes. Shared links can preserve search, sort order, and page.

**Cons:** Every browse change becomes a navigation. Search, sort, and page become part of shared links and survive refresh, changing the agreed temporary behaviour for these controls.

### Option 3: Client-only loading

**Description:** The page initially shows its layout and loading state. The browser then requests Marketplace and Skill data through the authenticated BFF.

**Pros:** One browser loading model handles initial data and later updates. The BFF keeps authenticated service access on the server.

**Cons:** Initial Marketplace and Skill content must wait for browser JavaScript and a separate data request. Users see a loading state before that content is available.

## Decision

**Chosen Option:** Option 1: server initial loads with BFF browse updates.

Build the Marketplace interface with the existing Hades components. Keep Marketplace components within this feature, and put files used only by Skill detail pages under `[marketplaceSlug]/[skillName]`.

The Skill detail route uses `marketplaceSlug` and `skillName` as its two path parameters. Its page is `[marketplaceSlug]/[skillName]/page.tsx`. A one-segment path under `/marketplace` opens the selected Marketplace, a two-segment path opens a Skill, and longer paths return 404.

The dashboard layout's `PageGutter` owns outer page spacing for both Marketplace routes. Marketplace uses that shared layout directly, following the dashboard convention that feature pages do not add their own outer padding.

### Rationale

Option 1 follows the existing dashboard pattern and provides initial page content while keeping frequent browse changes interactive. Dedicated server-rendered Skill pages support direct links and refresh independently of prior browse state. Shared loading logic keeps server entry and browser updates consistent, with authenticated service access on the server.

### Preferences and request coordination

After an explicit source selection loads successfully, `MarketplaceProvider` updates the shared preferences document with the selected slug. It preserves unrelated preferences, skips a redundant value, and never saves defaults over a failed preferences read. `PreferencesProvider` owns the debounced `PUT /api/preferences`. Saving is best effort, failed writes are ignored.

Each new browse request aborts its predecessor and receives a newer request ID. Only the latest active response can update the view. Pending and failed later requests retain previous results, and retry repeats the failed intent. If only the initial Skill-list load fails, the successfully loaded Marketplace list remains available for choosing another source.

### Identity, policy, and connections

- **Who is this agent?** The dashboard reuses its verified user session. Server Components validate the session, and BFF routes retain `withAuth`. The upstream request uses that session's platform token.
- **What can it access?** skills-service remains responsible for authorization and tenant-scoped source access. The frontend does not substitute local policy checks or use the saved source selection as authority.
- **How does it connect?** All upstream calls and credentials stay server-side. The browser navigates to the detail Server Component or calls the authenticated browse BFF. Repository bodies are displayed as plain text through Hades `CodeBlock` without executing Markdown or HTML.

## Consequences

### Positive

- Dedicated Skill URLs work independently of previously selected browse state and survive refresh.
- Browse controls retain their request cancellation, retained-results, retry, and preference behavior.
- The selected Marketplace is shareable through its overview path, returning from a Skill restores search, sort, and page while navigating within Marketplace.
- Catalogue ordering, counts, and display pages cover every matching result batch.

### Negative

- Keeping server initial loads and browser browse updates requires shared validation and loading rules at both entry points.
- Search, sort, and page are not shared through navigation URLs. Refreshing or leaving Marketplace clears these controls.
- Server work grows with the matching result set. Fetching the Skill batches uses a shared ten-second timeout.

### Neutral

- Skill detail loading stays behind the server page and its private helper, the authenticated browse BFF remains the browser API for Marketplace updates.
- Source management, Skill installation, agent assignment, backend authorization, and credential brokering retain their existing scope and ownership.

## Validation

Browse stories mock the browse BFF and preferences writes. Their sample Marketplace slugs are distinct from display labels. The card-view story checks that Skill links are visible, and Vitest checks their destinations. Detail stories supply read-only Skill props directly, and the retry story uses Storybook's mocked App Router.

Vitest covers loaders, sessions and failure states, exact identifier forwarding, link encoding, browse request coordination, preferences, file contents, and retry transitions. Cursor tests cover catalogues above 200 Skills, global sorting and counts, page clamping, cancellation, the shared deadline, failed later batches, and repeated cursors. Playwright exercises the actual Next.js overview/detail routes using a test-only HTTP skills-service fixture for server fetches and browser route mocks for later browse requests. No live Go backend, Postgres, Redis, or production fixture path is required.

Run frontend lint and TypeScript checks, and run the root quality gate before committing.

### Success Metrics

The following criteria must be met before release:

- Valid Marketplace and Skill links load the requested content, including after refresh, missing Skills and failed loads have distinct states.
- Only the newest active browse response updates the view, a failed update retains previous results and can be retried.
- Search relevance, alphabetical ordering, totals, and fixed pages of at most 24 Skills remain correct across result batches.
- Returning from a Skill restores search, sort, and page within Marketplace, refreshing or leaving clears them.
- Skill files remain literal text with working preview and copy controls, keyboard use and responsive layouts pass the browser accessibility and layout checks.

Validation remains incomplete. The Chromium run on 2026-11-02 passed all 24 checks. Manual screen-reader assessment remains pending.

### Monitoring

Use existing frontend server error logs to investigate failed initial loads and browse requests. Review large-catalogue loading during release validation.

## Related Decisions

- [Frontend Scaffolding and Toolchain](2026-03-17-frontend-scaffolding-and-toolchain.md)
- [Browser-Based Authentication Flow and BFF Session Architecture](2026-04-01-browser-auth-flow-and-bff-session-architecture.md)
- [Frontend Policy Projection via BFF](2026-04-14-frontend-policy-projection.md)
- [Frontend Design System](2026-03-25-terra-frontend-design-system.md)
- [Frontend Component Consolidation](2026-05-04-frontend-component-consolidation.md)
- [Marketplace backend](2026-08-28-marketplace-adr-backend.md)
- [Frontend Test Strategy and Toolchain](2026-05-08-frontend-test-strategy-and-toolchain.md)

## References

- [Agent Marketplace PRD](../prd/agent-marketplace.md): product scope and requirements.

**Last Updated:** 2026-11-02
