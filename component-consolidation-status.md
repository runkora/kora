# Frontend Component Consolidation — Status & Recommendations

_Assessed against current implementation. Reference: `docs/prd/frontend-component-consolidation.md` + `docs/adr/0043-frontend-component-consolidation.md`_

---

## What is complete

- **Phase 0 + Phase 1 (Terra / Tier 1):** All planned primitives and Modal/FormField work is done and in use across the app.
- **Phase 2 (Tier 2 barrel):** `EmptyState`, `StatusBadge`, `ConfirmDialog`, `MetricCard`, `EventFeedItem` are implemented in `frontend/src/components/`, exported from the `@orbit/components` barrel, and actively consumed across `page.tsx`, `model-gateway`, and `policy`.
- **`@orbit/utils` date/cost formatters:** `fmtDate`, `fmtDateTime`, `fmtCost`, `fmtNum`, `fmtTime` are implemented and in use. The previous duplicate `formatDate` local functions in `grants-tab`, `roles-tab`, and `overview-tab` are gone.

---

## Remaining issues

### 1. Cross-feature import violation — `ScopeChip` + `ScopePicker` (blocking)

`ScopeChip` and `ScopePicker` live in `policy/_components/` but are imported directly by three `model-gateway` files:

| Importer | Import path |
|---|---|
| `model-gateway/_components/keys-tab.tsx` | `../../policy/_components/scope-chip` |
| `model-gateway/_components/create-token-modal.tsx` | `../../policy/_components/scope-picker` |
| `model-gateway/_components/edit-scopes-modal.tsx` | `../../policy/_components/scope-picker` |

This is a cross-feature dependency — the ADR's feature-independence rule (`Features must not import each other`) is violated. Both components encode Orbit domain knowledge (the `resource:action` scope token format) but are used in 2+ features, which puts them squarely in **Tier 2**.

**Action:** Move `scope-chip.tsx` and `scope-picker.tsx` to `frontend/src/components/`, add `@tier 2` annotations, export from the barrel, and update all import sites to `@orbit/components`.

---

### 2. `HealthStatus` and `LogoutButton` bypass the barrel

```ts
// current — deep imports that skip the barrel
import { HealthStatus } from '@orbit/components/health-status';
import { LogoutButton } from '@orbit/components/logout-button';
```

Neither component is exported from `frontend/src/components/index.ts`. All other Tier 2 components go through the barrel (`@orbit/components`).

**Action:** Add both to `index.ts` and update the two import sites to use the barrel. If they are intentionally excluded (e.g. they carry side-effects or are too app-shell-specific), add a comment in `index.ts` explaining why.

---

### 3. `formatDateAxis` duplicated in two chart files

Identical function defined in both:
- `model-gateway/_components/overview-tab.tsx:49`
- `model-gateway/usage/_components/latency-chart.tsx:44`

Same body: parses a `YYYY-MM-DD` string, returns `"Mon D"` via `toLocaleDateString`.

**Action:** Either add `fmtDateAxis` to `frontend/src/utils/format.ts` (and export via `@orbit/utils`) or extract a shared `_utils/chart-formatters.ts` inside `model-gateway/` and import from there. Given that `fmtDate` already lives in `@orbit/utils`, adding `fmtDateAxis` there is the most consistent choice.

---

## Not in scope / correctly deferred

- `filter-chip.tsx`, `principal-badge.tsx`, `quick-link-card.tsx`, `view-toggle.tsx` — all confined to `policy/_components/`. Tier 3 placement is correct; no action needed unless a second feature needs them.
- Phase 3 Tier 3 extractions (chat sub-components, model-gateway chart sub-components) — not started, not blocking anything.

---

## Priority order

1. **Move `ScopeChip` + `ScopePicker` to Tier 2** — only blocking issue; violates the ADR's feature-independence rule today.
2. **Add `HealthStatus` + `LogoutButton` to the barrel** — low effort, restores consistency.
3. **Extract `formatDateAxis` to `@orbit/utils`** — minor cleanup, removes the only remaining formatter duplication.
