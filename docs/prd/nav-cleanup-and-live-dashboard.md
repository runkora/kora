# Nav Cleanup and Live Dashboard - Product Requirements Document

**Status:** Approved  
**Created:** 2026-06-19  
**Last Updated:** 2026-06-20  
**Owner:** Platform Engineering  
**Stakeholders:** Platform Engineering, Product

---

## Executive Summary

The Orbit dashboard currently contains placeholder navigation items and stub pages that were created as scaffolding during initial development. These items represent unbuilt features and create a misleading, cluttered navigation experience. At the same time the dashboard home page shows entirely static mock data. This PRD covers removing the noise and replacing the dashboard home with real, live data derived from the platform's existing features.

---

## Problem Statement

### Current State

The left-hand navigation sidebar has five groups: **Control Plane**, **Security**, **Policy**, **Model Gateway**, and **System**. Of these:

- The **Security** group contains only one entry — _Identity_ — marked `wip: true`, meaning it renders as a disabled, unclickable item. It adds a labelled group with a single dead link.
- The **System** group contains two entries — _Storage_ and _Settings_ — both `wip: true`. Same result.
- The **Control Plane** group contains two `wip: true` entries — _Event Spine_ and _Credentials_ — alongside three real links and two real scoped links.

Additionally, `APP_ROUTES` and the routing type union include entries (`/events`, `/credentials`, `/identity`, `/storage`, `/settings`) that do not correspond to any existing page file.

The dashboard home page (`/`) renders entirely static, hard-coded mock data (fake agent names, fake events, fake resource utilisation percentages).

### User Pain Points

- Navigation is cluttered with disabled items that lead nowhere, signalling immaturity to evaluators and new users.
- The Security and System groupings with only disabled entries add label noise with zero signal.
- The dashboard home gives a false sense of the actual platform state — an operator monitoring real agents sees fictional data.

### Business Impact

- Reduces product credibility during demos and evaluations.
- Operators cannot trust the dashboard, so it is ignored — defeating the purpose of a control plane overview.

---

## Goals and Objectives

### Primary Goals

1. Remove all placeholder/stub navigation items that are not backed by working pages.
2. Remove the Security and System nav groups entirely (their contents are all stubs).
3. Remove the corresponding stub route entries from `APP_ROUTES` and any associated page files.
4. Replace the dashboard home page's static mock data with real data fetched from the platform's existing APIs (agents, connections, environments, triggers, model-gateway analytics).

### Success Metrics

- Zero disabled (`wip: true`) nav items visible to any user after this change.
- Zero nav group labels rendered when all items in the group are hidden.
- Dashboard home page stat cards show counts derived from real API data.
- The existing integration test suite passes with updated assertions.

### Non-Goals

- Building the Identity, Storage, Settings, Event Spine, or Credentials features.
- Redesigning the dashboard layout or adding new chart components.
- Real-time streaming on the dashboard (polling or SSR is sufficient).
- Changing any backend APIs.

---

## User Stories

### Primary User Stories

**As a** platform operator,  
**I want to** open the Orbit dashboard and see only navigation items I can actually use,  
**So that** I can navigate confidently without encountering dead ends.

**Acceptance Criteria:**
- [ ] No disabled (`wip`) nav items appear in the sidebar.
- [ ] No empty nav group labels appear.
- [ ] All visible nav items navigate to a working page.

---

**As a** platform operator,  
**I want to** open the dashboard home and see real counts of my agents, connections, environments, and recent runs,  
**So that** I have an accurate at-a-glance view of platform state without navigating to each section.

**Acceptance Criteria:**
- [ ] Stat cards show live agent type count, active run count, connection count, and environment count.
- [ ] Agents table shows real agent types from the API (not hard-coded names).
- [ ] Recent runs section shows real run data with live status.
- [ ] Page handles empty state gracefully (zero counts, no runs yet).

---

### Secondary User Stories

**As a** developer evaluating Orbit,  
**I want to** see a clean, complete-feeling navigation,  
**So that** placeholder items don't undermine confidence in the platform's maturity.

---

## Proposed Solution

### High-Level Approach

1. **Nav cleanup**: Delete `SECURITY_NAV`, `SYSTEM_NAV`, and the two `wip: true` entries from `MAIN_NAV`. Remove their route entries from `APP_ROUTES`. Delete any stub page files for those routes. Update the `dashboard-sidebar.integration.test.tsx` assertions accordingly.

2. **Live dashboard**: Convert `src/app/(dashboard)/page.tsx` from a static component to an RSC that fetches agents, connections, environments, and triggers in parallel at request time. The existing component structure (stat cards, agent table, events feed, system health section) is preserved but populated with real data. Where an API is unavailable or errors, the section degrades to an empty/error state rather than crashing.

### User Experience

The sidebar becomes shorter and cleaner — only the Control Plane, Policy, and Model Gateway groups remain. The dashboard home page loads with the same visual layout it has today but with real numbers.

#### User Flow

1. User logs in → sidebar shows Control Plane (Dashboard, Agents, Connections, Environments, Triggers), Policy (scoped), Model Gateway.
2. User lands on `/` → dashboard fetches agents + runs + connections + environments in parallel.
3. Stat cards display: total agent types, active/running runs, connection count, environment count.
4. Agent types table lists real registered agents.
5. Recent runs feed shows the last N runs with live status badges.

### Key Components

- `nav-entries.ts` — remove Security/System groups and wip items.
- `routes.ts` — remove stub route keys.
- `(dashboard)/page.tsx` — rewrite as RSC using server-side fetch helpers.
- `(dashboard)/_components/dashboard-sidebar.integration.test.tsx` — update group/link assertions.

---

## Scope

### In Scope

- Removal of Security and System nav groups.
- Removal of `wip: true` items from Control Plane (Event Spine, Credentials).
- Removal of corresponding route keys from `APP_ROUTES`.
- Deletion of any stub page files for removed routes (if they exist).
- Live data on dashboard home: agent type count, active run count, connection count, environment count, agent types table, recent runs.
- Updated/passing integration tests.

### Out of Scope

- Building Identity, Storage, Settings, Event Spine, or Credentials pages.
- Replacing the System Health card with real infrastructure metrics (no backend endpoint exists).
- Real-time dashboard updates (WebSocket / SSE polling).
- Dashboard-specific backend endpoints.

### Future Considerations

- Re-introduce Security and System groups when the underlying features are built.
- Add a real-time event feed sourced from the event spine once it is implemented.
- Add infrastructure health metrics from a future platform-health endpoint.

---

## Requirements

### Functional Requirements

**Must Have (P0):**
- [ ] No `wip: true` nav items visible in the sidebar.
- [ ] Security nav group removed entirely.
- [ ] System nav group removed entirely.
- [ ] `APP_ROUTES` no longer exports `identity`, `storage`, `settings`, `events`, `credentials` keys.
- [ ] Dashboard home stat cards derive values from real API responses.
- [ ] Dashboard home agent table shows real agent types.
- [ ] Dashboard home handles API errors gracefully (shows empty state, not crash).
- [ ] All existing tests pass.

**Should Have (P1):**
- [ ] Recent runs section shows the last 10 runs with status and timing.
- [ ] Dashboard home shows a loading skeleton during server fetch.

**Nice to Have (P2):**
- [ ] System Health card replaced with a "Quick links to platform sections" card while infrastructure metrics are unavailable.

### Non-Functional Requirements

**Performance:**
- Dashboard home server fetch completes within 1 s under normal conditions (parallel fetches).

**Reliability:**
- Individual API failures degrade gracefully; dashboard does not throw.

---

## Dependencies

### Internal Dependencies

- `agents-server.ts` — already exists, fetches agent types and runs.
- `connections-server.ts` — already exists, fetches connections.
- Environments and triggers follow the same RSC pattern.

### External Dependencies

- No new backend APIs required.

---

## Risks and Mitigations

| Risk | Impact | Probability | Mitigation |
|------|--------|-------------|------------|
| Removing route keys from `APP_ROUTES` causes TypeScript errors at call sites | Medium | Low | Search for all usages before deletion; fix any remaining references |
| Integration tests hard-code Security/System group names | Low | High | Update tests as part of the same PR |
| Live dashboard fetch adds latency vs static mock | Low | Low | Fetches are parallel and server-side (RSC); no client-side waterfall |

---

## Alternatives Considered

### Alternative 1: Keep wip items but mark them differently

**Description:** Replace `wip: true` disabled nav items with tooltip-annotated "coming soon" entries.

**Why not chosen:**
- Still adds clutter and signals incomplete features to users.
- Tooltip affordance for nav items is not in the Terra design system.

### Alternative 2: Keep Security and System groups but hide all items via scope guards

**Description:** Gate every Security/System item behind a scope no user currently holds.

**Why not chosen:**
- The group label still renders when zero items are visible (current code guards items but not the group label separately when all items are wip).
- Obscures intent — these features simply don't exist yet.

---

## Open Questions

1. **System Health card replacement:** Replace with a "Quick links" card or simply remove the bottom-right panel?  
   - **Status:** Resolved — replace with a "Platform sections" quick-links card pointing to Agents, Connections, Model Gateway, and Policy.

---

## Timeline and Milestones

**Phase 1: Nav cleanup + live dashboard** (Target: 2026-06-19)
- Remove stub nav items and routes.
- Implement live RSC dashboard page.
- Update tests, pass CI.

### Key Milestones

- [x] PRD Approved: 2026-06-19
- [x] ADR Created: 2026-06-19
- [x] Implementation Started: 2026-06-19
- [x] Dashboard enrichment (Phase 2): 2026-06-20
- [ ] CI Green: 2026-06-20

---

## Addendum — Dashboard enrichment (Phase 2, 2026-06-20)

The Phase 1 dashboard met the goal of showing real data, but in practice it was still thin — four count cards and a registry table. Phase 2 makes it a genuinely useful operator landing page by surfacing usage and spend, which are the questions operators actually open the dashboard to answer. This stays within the original RSC + server-fetch approach; no new backend work.

### Added

- **Inference activity (last 7 days):** cost, request volume (with success/fail split), token consumption, and latency — so operators see whether agents are actually being used and what it costs.
- **Usage trend:** a requests-over-time chart for the same window.
- **Spend vs budget:** tenant spend against the configured cap, with a colour-coded utilisation bar and an at-a-glance "within budget / limit reached" state. This partially realises the "spend visibility" theme without waiting for a dedicated billing surface.

### Changed / removed

- The "Quick links to platform sections" card (Open Question, resolved in Phase 1) is **removed** — it duplicated the sidebar. Those links now live on the stat cards.
- The backend health card is **removed** as low day-to-day signal.

### Notes

- All new sections degrade independently: if the inference gateway or budget data is unavailable, the affected section hides (or shows an empty state) rather than failing the page.
- The Phase 1 "Future Considerations" item *"add infrastructure health metrics"* remains future work; this phase adds **inference/usage** metrics, not host-level infra metrics.

---

## Appendix

### Related Documents

- [ADR: Nav Cleanup and Live Dashboard](../adr/2026-06-19-nav-cleanup-and-live-dashboard.md)
- [ADR: Frontend Scaffolding and Toolchain](../adr/2026-03-17-frontend-scaffolding-and-toolchain.md)
- [ADR: Terra Design System](../adr/2026-03-25-terra-frontend-design-system.md)
