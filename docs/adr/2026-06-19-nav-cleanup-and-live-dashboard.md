# Nav Cleanup and Live Dashboard

<!-- Filename: 2026-06-19-nav-cleanup-and-live-dashboard.md -->

**Status:** Accepted  
**Date:** 2026-06-19  
**Deciders:** Platform Engineering  
**Technical Story:** Remove placeholder nav items and replace the static dashboard home with live data from existing platform APIs.

---

## Context

### Background

During initial scaffolding of the kora frontend, several navigation items and their corresponding route entries were created as forward-references — placeholders for features not yet built. These included:

- **Control Plane group**: `Event Spine` (`/events`) and `Credentials` (`/credentials`) — both `wip: true`.
- **Security group**: `Identity` (`/identity`) — the only item in the group, `wip: true`.
- **System group**: `Storage` (`/storage`) and `Settings` (`/settings`) — both `wip: true`.

The dashboard home page (`/`) was also scaffolded with entirely static mock data (hard-coded agent names, fake event entries, and fictional resource utilisation percentages) to demonstrate the layout before any backend was available.

### Problem Statement

The platform is now shipping real features and real agents run in production. The placeholder nav items create noise — they are always disabled, their group labels add visual clutter, and they signal immaturity to operators and evaluators. The static dashboard is worse: operators see fictional numbers that bear no relationship to the actual state of their deployment.

### Goals

- Remove all `wip: true` nav items from the sidebar.
- Remove the Security and System nav groups (they are entirely placeholder).
- Remove stub route keys that have no corresponding page implementation.
- Replace the static dashboard with an RSC page that fetches real data from existing platform APIs.

### Non-Goals

- Building the Identity, Storage, Settings, Event Spine, or Credentials features.
- Adding new backend endpoints or changing any existing API contract.
- Real-time / WebSocket dashboard updates.

---

## Decision Drivers

* **Signal-to-noise**: every visible nav item should navigate to a working page.
* **Accuracy**: the dashboard home is an operator tool; it must reflect real platform state.
* **Consistency**: follows the established RSC + server-fetch pattern used by Agents, Connections, and Environments pages.
* **Test integrity**: integration tests must remain meaningful after the change.
* **Minimal footprint**: the smallest change that fully solves the problem; no speculative additions.

---

## Considered Options

### Option 1: Remove wip items and replace dashboard with live RSC

**Description:** Delete all `wip: true` nav items and their groups; remove the corresponding route keys; rewrite `(dashboard)/page.tsx` as an RSC that fetches agents, connections, environments, and (where available) triggers in parallel at request time.

**Pros:**
- Clean nav with zero dead links.
- Dashboard shows accurate platform state.
- Follows the existing RSC server-fetch pattern — no new patterns introduced.
- Route-key removal is validated by TypeScript at every call site.

**Cons:**
- Removes `APP_ROUTES.identity`, `storage`, `settings`, `events`, `credentials` — any future code that tries to use these will get a compile error until the feature is built and re-added.

### Option 2: Keep wip items but gate them with a scope nobody holds

**Description:** Replace `wip: true` with a `requiredScope` value that no current user is granted, effectively hiding the items from everyone.

**Pros:**
- Easier to re-enable when features are built.

**Cons:**
- The group label still renders when zero scoped items are visible (the current sidebar only suppresses the group when `visibleItems.length === 0`, and `wip` items are excluded from `visibleItems` already — so Security and System would still appear empty if we switch to scope gating).
- Indirect — the intent "this feature does not exist" is better expressed by absence than by a permission that nobody has.
- Route keys would still pollute `APP_ROUTES` with entries that resolve to 404.

### Option 3: Mark items "coming soon" with a tooltip

**Description:** Replace disabled `wip` items with enabled nav items that open a "Coming soon" modal or navigate to a placeholder `/coming-soon` page.

**Pros:**
- Users see what is planned.

**Cons:**
- `coming-soon` pages add maintenance burden and are considered bad UX practice.
- Hades nav items do not have a built-in tooltip variant; adding one requires Hades modifications.
- Still adds noise; users want functional nav, not a roadmap.

---

## Decision

**Chosen Option:** Option 1 — Remove wip items and replace dashboard with live RSC.

### Rationale

Option 1 is the smallest, most honest change. It removes the things that don't work and replaces the one thing (the dashboard home) that exists but lies. The RSC fetch pattern is already established and tested (Agents page, Connections page, Environments page), so there is no new architecture to introduce. TypeScript enforces that removing route keys will surface any accidental dangling references at compile time rather than at runtime.

---

## Consequences

### Positive

- Navigation is clean; every item in the sidebar is actionable.
- The dashboard home shows accurate agent and run counts, giving operators genuine situational awareness.
- Route type safety: removing route keys from `APP_ROUTES` means TypeScript catches any future code that tries to navigate to a non-existent page.
- No new patterns — the live dashboard follows the same shape as `AgentsPage`, `ConnectionsPage`, etc.

### Negative

- `APP_ROUTES.identity`, `.storage`, `.settings`, `.events`, `.credentials` no longer exist. Code added in the future for these features must add them back.
- The System Health progress-bar card (which displayed fake CPU/memory metrics) is removed; operators may notice its absence. It is replaced with a quick-links card pointing to the real platform sections.

### Neutral

- The `dashboard-sidebar.integration.test.tsx` test file requires updated assertions (Security and System group assertions are removed; wip-item exclusion tests are narrowed to the remaining wip items in Control Plane — but since those are all removed too, the wip-exclusion tests are no longer needed).

---

## Implementation

### Changes

1. **`src/lib/routing/routes.ts`** — Remove `identity`, `storage`, `settings`, `events`, `credentials` from `APP_ROUTES`.

2. **`src/app/(dashboard)/_utils/nav-entries.ts`** — Delete `SECURITY_NAV`, `SYSTEM_NAV`, remove `Event Spine` and `Credentials` from `MAIN_NAV`. Remove Security and System from `NAV_GROUPS`.

3. **`src/app/(dashboard)/_components/dashboard-header.tsx`** — Remove `identity` from `SECTION_LABELS` (no longer a valid nav target).

4. **`src/app/(dashboard)/page.tsx`** — Rewrite as an async RSC. Fetch agents+runs, connections, environments, and triggers in parallel using the existing server-fetch helpers. Pass data to a new `DashboardPage` client component (or keep it fully server-rendered). Replace static mock arrays with real data.

5. **`src/app/(dashboard)/_components/dashboard-sidebar.integration.test.tsx`** — Update group assertions: Security and System group tests are removed; wip-item exclusion tests no longer apply to removed items.

6. **Stub page directories** — Delete any empty or placeholder directories under `(dashboard)` for routes being removed (if they exist).

### Data Model for Live Dashboard

The dashboard home shows:

| Stat Card | Source |
|-----------|--------|
| Agent Types | `fetchAgentsFromBFF` → `agentTypes.length` |
| Active Runs | `fetchAgentsFromBFF` → runs filtered to `state === 'running'` |
| Connections | `fetchConnections` → `rows.length` |
| Environments | `fetchEnvironments` → `rows.length` |

The agents table shows up to 10 `AgentTypeRow` entries with name, state, model, and run count. The recent runs section shows the last 10 `RunRow` entries with status, agent name, and elapsed time.

The System Health card is replaced with a **Platform Sections** quick-links card that links to Agents, Connections, Model Gateway, and Policy (when scoped).

### Migration Path

No migration needed — this is a frontend-only change with no database, API, or schema impact.

---

## Validation

### Success Metrics

- `pnpm lint && pnpm tsc --noEmit` passes with zero errors.
- All unit and integration tests pass.
- No `wip: true` items appear in the rendered sidebar.
- Dashboard stat cards render with values from the API response.

### Monitoring

- No runtime monitoring change needed; the dashboard is a standard RSC page.

---

## Amendment — Dashboard enrichment (2026-06-20)

After the initial live dashboard landed, the home page was still thin (four count cards plus a registry table). It was extended — within the same RSC + server-fetch pattern — to surface higher-signal operational data for the control plane. This amendment records what changed; the original decision (nav cleanup + live RSC) is unaffected.

### What was added

- **Inference KPIs (last 7 days)** — Cost, Requests, Tokens, and Latency cards driven by the model-gateway analytics API.
- **Usage trend** — a requests-over-time line chart for the same 7-day window.
- **Spend vs budget** — tenant spend against the configured cap, rendered with a colour-coded `Progress` bar (`healthy` < 80% → `degraded` ≥ 80% → `error` when reached) and a "no budget configured" empty state.

### What was removed (supersedes the original Consequences)

- The **Platform Sections quick-links card** that originally replaced the System Health card is itself removed — it duplicated the sidebar. Those destinations now hang off the stat cards as `View all →` footer links.
- The **backend `HealthStatus` card** is removed as low-signal for day-to-day operation.

### Data sources (extends the original Data Model table)

| Section | Source |
|---------|--------|
| Inference KPIs | `fetchAnalyticsFromGateway(token, { days: 7, granularity: 'daily' })` → `kpis` |
| Usage trend | same call → `buckets` |
| Spend vs budget | `fetchBudgetPrincipalsFromGateway(token)` → `tenant_cap` |

`fetchDashboardData` issues these in the same `Promise.all` batch as agents/connections/environments. Every sub-fetch degrades independently: when the inference gateway is unavailable, `analytics` is `null` and the Inference section + trend simply do not render; an absent cap yields the budget empty state. A failure never breaks the page.

The agents table renders up to 6 `AgentTypeRow` entries; Recent Runs shows up to 8 `RunRow` entries. Vertical spacing was tightened (`spacious` → `relaxed` section gaps).

### Component reuse and a convention deviation to resolve

The Inference KPIs and trend reuse the existing model-gateway components `AnalyticsKPICards` and `AnalyticsRequestsChart` rather than duplicating recharts code. These are Tier-3 (feature-scoped) components now consumed by a second feature (the dashboard). Per the component-tier model in [2026-05-04-frontend-component-consolidation.md](2026-05-04-frontend-component-consolidation.md), components used across ≥2 features should be promoted to Tier 2 (`src/components/`). They were left in place to keep this change focused; **promotion is a tracked follow-up** and should be done before these are reused a third time.

### Testing

- `SpendBudgetCard` is exported and unit-tested (`dashboard-page.test.tsx`) for its empty-state and the spend → variant/badge threshold logic.
- The dashboard E2E spec (`e2e/dashboard.spec.ts`) drops the obsolete `HealthStatus` assertions and instead checks the always-present stat and section cards.

---

## Related Decisions

- [2026-03-17-frontend-scaffolding-and-toolchain.md](2026-03-17-frontend-scaffolding-and-toolchain.md) — Established the Next.js app-router pattern this change follows.
- [2026-03-25-terra-frontend-design-system.md](2026-03-25-terra-frontend-design-system.md) — Hades component constraints honoured throughout.
- [2026-05-04-frontend-component-consolidation.md](2026-05-04-frontend-component-consolidation.md) — Component-tier model followed for any new sub-components.

---

## Notes

The `(dashboard)/_components/dashboard-sidebar.integration.test.tsx` test comment at the top currently reads: *"Default state (empty scopes) renders Control Plane, Security, Model Gateway, System groups."* — this comment must be updated to reflect the new group set.

**Last Updated:** 2026-06-20
