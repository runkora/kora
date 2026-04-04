# PR Review: feat/engineer-model-gateway

**Branch:** `feat/engineer-model-gateway` → `main`  
**Commits:** 8 commits  
**Files changed:** 53 files, +5,969 / −72 lines

---

## Summary

This PR introduces the **Model Gateway** section of the Orbit frontend: three new dashboard pages (Models, Setup, Usage), a documented golden-path state pattern, and extensions to the Terra design system (Chart, Popover, Tabs components). The architecture is clean and well-thought-out. The code quality is generally high. However, there are several issues — ranging from a functional bug that will cause charts to show empty data, to mock data being shipped without any indication it is mock — that need addressing before this can be merged for real users.

---

## Critical Issues

### 1. Mock data dates are in the past — charts render empty by default

**File:** `frontend/src/app/(dashboard)/model-gateway/_data/mock.ts:25–56`  
**File:** `frontend/src/app/(dashboard)/model-gateway/usage/_components/utils/table-utils.ts:33–40`

`DAILY_SPEND`, `DAILY_TOKENS`, and `DAILY_REQUESTS` all have dates in June 2025. `filterByPeriod` computes a cutoff from `new Date()` (today). With the current date being April 2026, the cutoff for the default `30d` period is around March 2026 — all mock data pre-dates it and is filtered out. Charts will show completely empty content for every period except `365d`.

```ts
// table-utils.ts:39 — cutoff calculated from today
const cutoff = toDateStr(subtractDays(today, PERIOD_DAYS[period]));
return rows.filter((r) => r.date >= cutoff); // June 2025 rows fail this for 7d–180d
```

**Fix:** Either use relative dates in the mock (rolling 30 days back from `new Date()`) or update the static dates to within the last year.

---

### 2. API key generation is a client-side mock — not a real key management system

**File:** `frontend/src/app/(dashboard)/model-gateway/setup/_components/api-key-card.tsx:19–24, 54–61`

The "Generate new key" flow calls `Math.random()` locally and simulates a 600 ms delay with `setTimeout`. There is no backend call, no persistence, and the key disappears on page refresh. If a user copies this key and tries to use it against the inference gateway, it will fail silently.

```ts
// The key is entirely fictional
function generateApiKey(): string {
  const chars = 'abcdef0123456789';
  ...
  return `sk-orbit-${segment(8)}-${segment(8)}-${segment(8)}`;
}

// And the "async" generation is just a timeout
setTimeout(() => {
  setApiKey(generateApiKey()); // comment even says: "Simulate async keygen"
  setGenerating(false);
}, 600);
```

**Fix:** Either connect this to a real key-creation API endpoint, or clearly label the entire card as "Coming soon" / hide it behind a feature flag. As-is it's a trap for users.

---

### 3. `toModelRow` always sets `status: 'available'`

**File:** `frontend/src/app/(dashboard)/model-gateway/_types/index.ts:58`

Every model is unconditionally mapped to `status: 'available'`, regardless of what the backend returns. The `ModelObject` interface has no `status` field at all, meaning availability is never communicated from the Go backend to the UI. The "Unavailable" filter option in the models table and the red availability dot are therefore dead UI — they can never trigger.

```ts
export function toModelRow(m: ModelObject): ModelRow {
  return {
    ...
    status: 'available', // ← always; no backend field drives this
  };
}
```

**Fix:** Either add a `status` or `available: boolean` field to `ModelObject` (and the Go backend's JSON response), or remove the status filter/dot until availability data is wired up.

---

## Major Issues

### 4. `handleSort` calls a state setter inside another state updater

**File:** `frontend/src/app/(dashboard)/model-gateway/models/_components/models-table.tsx:228–237`

`setSortDir` is called inside the updater function passed to `setSortKey`. React may invoke state updaters more than once in Strict Mode, causing `setSortDir` to fire multiple times per click and producing incorrect sort direction state.

```ts
const handleSort = useCallback((key: ColumnId) => {
  setSortKey((prev) => {
    if (prev === key) {
      setSortDir((d) => ...); // ← side effect inside updater: not safe
      return prev;
    }
    setSortDir('asc'); // ← fine
    return key;
  });
}, []);
```

**Fix:** Merge `sortKey` and `sortDir` into a single state object, or use a `useReducer`, so the transition is atomic:

```ts
const [sort, setSort] = useState<{ key: SortKey; dir: SortDir }>({ key: 'name', dir: 'asc' });

const handleSort = useCallback((key: ColumnId) => {
  setSort((prev) => {
    if (prev.key !== key) return { key, dir: 'asc' };
    const dir = prev.dir === 'asc' ? 'desc' : prev.dir === 'desc' ? null : 'asc';
    return { key, dir };
  });
}, []);
```

---

### 5. Usage page and stat cards import mock data directly — no indication it's placeholder

**Files:**  
- `frontend/src/app/(dashboard)/model-gateway/usage/_components/stat-cards.tsx` (receives `STATS` from `mock.ts`)  
- `frontend/src/app/(dashboard)/model-gateway/setup/_components/usage-overview-card.tsx:11–13`  
- `frontend/src/app/(dashboard)/model-gateway/usage/page.tsx`

The entire usage dashboard — spend/token/request charts, stat cards, and the usage overview card — displays hardcoded June 2025 data with no disclaimer. `UsageOverviewCard` even hardcodes a tier (`Pro`), a spend limit (`$500`), and a reset date (`2025-07-01`), all imported directly from `_data/mock`:

```ts
// usage-overview-card.tsx:48–57 — fully hardcoded
const MOCK_SETUP = {
  tier: 'Pro' as const,
  spendLimit: 500,
  spent: STATS.spend.period,
  resetDate: new Date('2025-07-01').toLocaleDateString(...),
};
```

A user looking at their "Usage & Limits" page will see fabricated data. This is a trust issue.

**Fix:** At minimum, wrap mock-driven pages in a clearly visible "Preview — live data coming soon" banner, or gate them behind a feature flag. Ideally, wire up a real API endpoint.

---

### 6. `DashboardSidebar` fetches session on every mount instead of using SSR data

**File:** `frontend/src/app/(dashboard)/_components/dashboard-sidebar.tsx:25–35`

The sidebar is a `'use client'` component that issues a `fetch('/api/auth/session')` on every mount to display the user's name and email. Since the sidebar is part of the persistent shell layout, this fires on every client-side navigation. The session data is already available server-side — passing it via props from the RSC layout would eliminate the waterfall fetch and the `Loading…` flash.

```ts
useEffect(() => {
  fetch(ApiRoute.session.GET())      // ← new HTTP request on every page visit
    .then((res) => res.ok ? res.json() : null)
    .then((data) => { setUserEmail(...); setUserName(...); })
    .catch(() => {});
}, []);
```

**Fix:** Pass `{ email, name }` as props from the RSC layout (where `getSession()` is already called), or use a shared server context.

---

### 7. Sidebar uses `<a href>` instead of Next.js `<Link>`

**File:** `frontend/src/app/(dashboard)/_components/dashboard-sidebar.tsx:85–87`

```tsx
<a href={href}>{itemLabel}</a>  // ← causes a full-page reload on every nav click
```

This bypasses Next.js client-side routing, forcing a full browser navigation (and a re-fetch of RSC payloads) on every sidebar click. All other navigation in a Next.js app shell should use `<Link>` from `next/link`.

**Fix:**
```tsx
import Link from 'next/link';
// ...
<Link href={href}>{itemLabel}</Link>
```

---

## Minor Issues

### 8. Empty-state row `colSpan` is wrong when columns are hidden

**File:** `frontend/src/app/(dashboard)/model-gateway/models/_components/models-table.tsx:361`

```tsx
<TableCell colSpan={ALL_COLUMNS.length}>  // ← 8, but fewer columns may be visible
```

When columns are hidden via the column toggle, the table renders fewer `<th>` elements than `ALL_COLUMNS.length`. The empty state cell should span only visible columns:

```tsx
<TableCell colSpan={visible.size}>
```

---

### 9. `CopyRow` in `ModelDetailModal` provides no copy feedback

**File:** `frontend/src/app/(dashboard)/model-gateway/models/_components/model-detail-modal.tsx:18–42`

`CopyRow` silently writes to the clipboard with no visual confirmation. `ApiKeyCard` shows a checkmark for 2 seconds after copying — `CopyRow` should do the same. The footer hint ("Click any identity row to copy") makes this interaction even more prominent, so the lack of feedback is jarring.

---

### 10. `ModelObject.display_name` typed as non-optional but treated as potentially falsy

**File:** `frontend/src/shared/api.ts:50` and `frontend/src/app/(dashboard)/model-gateway/_types/index.ts:51`

The interface declares `display_name: string` (required), but the transformer guards against it:

```ts
name: m.display_name || m.id,  // guard implies display_name may be empty/absent
```

If the Go backend genuinely omits `display_name` for some models, the TypeScript type is a lie. If it never omits it, the guard is unnecessary noise.

**Fix:** Change `display_name: string` to `display_name?: string` in `ModelObject` to accurately reflect the backend contract.

---

### 11. `fmtUsd` tooltip formatter uses 4 decimal places; Y-axis uses 2

**File:** `frontend/src/app/(dashboard)/model-gateway/usage/_components/spend-chart.tsx:72–78`

```ts
function fmtUsd(n: number): string { return `$${n.toFixed(4)}`; }  // tooltip
function fmtUsdAxis(n: number): string { return `$${n.toFixed(2)}`; } // axis
```

Hovering the chart shows `$5.8700` while the axis shows `$5.87`. Use consistent precision (2 decimal places) in both places.

---

### 12. Favourites are not persisted across sessions

**File:** `frontend/src/app/(dashboard)/model-gateway/_hooks/use-models.ts:66`

`favourites` is stored only in React state and is lost on page refresh or navigation away from the models page. Users who build up a favourites list will lose it every time. Consider persisting to `localStorage` (or a user preferences API endpoint) as a follow-up.

---

### 13. Version badge is hardcoded in the sidebar

**File:** `frontend/src/app/(dashboard)/_components/dashboard-sidebar.tsx:54–56`

```tsx
<Badge variant="outline" size="sm">v0.4.0</Badge>
```

This will go stale immediately. Drive it from `package.json` or an environment variable, or remove it.

---

### 14. `DEFAULT_FAVOURITES` is a module-level singleton `Set`

**File:** `frontend/src/app/(dashboard)/model-gateway/_hooks/use-models.ts:48`

```ts
const DEFAULT_FAVOURITES = new Set<string>();
```

While React's `useState` only reads the initial value once per component instance, it's safer (and idiomatic) to use an initializer function:

```ts
const [favourites, setFavourites] = useState<Set<string>>(() => new Set());
```

This avoids any risk of the shared instance being mutated before state is initialized.

---

### 15. No tests for utility functions or hooks

The golden-path document mentions test coverage as a verification step, but no tests are included in this PR. The pure utility functions in `table-utils.ts` (`filterByPeriod`, `aggregateByWeek`, `aggregateByMonth`) are ideal candidates for unit tests — they have well-defined inputs/outputs and no side effects. The mock data bug in issue #1 would have been caught by a test that asserts non-empty chart output for the default `30d` period.

---

## Positive Highlights

- **Golden-path documentation** (`frontend/docs/golden-paths/frontend-state-pattern.md`) is outstanding. It covers the full pattern end-to-end with a concrete file map, the Zustand upgrade contract, and a verification checklist. This is exactly the kind of living documentation that prevents architectural drift.

- **`fetchModels` never throws** (`_services/models-api.ts`). The pattern of returning a discriminated `{ models, error }` object rather than throwing eliminates try/catch boilerplate at every call site and makes error handling explicit. This should be the standard across all service functions.

- **Type-safe route registry** (`api-routes.ts`, `routes.ts`). Using const objects with template literal return types means dead links are compile errors, not runtime 404s. The `isSafeRedirectPath` guard is a correct and concise open-redirect prevention.

- **RSC + client shell pattern** (`models/page.tsx` → `models-client-shell.tsx`). Fetching server-side and passing `initialData` to the client shell eliminates the loading flash without requiring a separate loading state or suspense boundary. The pattern is clean and well-commented.

- **Terra Chart/Popover/Tabs components** are well-implemented. Using Radix UI primitives for Tabs and Popover gives correct accessibility semantics (keyboard navigation, ARIA roles) for free. The `ChartContainer` CSS variable injection pattern (`--color-{key}`) keeps chart colors in sync with the design token system.

- **BFF route handler** (`/api/models/route.ts`) correctly uses `server-only`, validates the session before forwarding, handles upstream errors with a structured response, and disables caching. No security issues with the proxy pattern.

- **`config.ts` server isolation** — `import 'server-only'` at the top and the lazy `sessionSecret` getter are good defensive patterns.

---

## Summary Table

| # | Severity | File | Issue |
|---|----------|------|-------|
| 1 | Critical | `_data/mock.ts`, `table-utils.ts` | Mock dates in past → charts empty for default periods |
| 2 | Critical | `setup/.../api-key-card.tsx` | API key is `Math.random()` mock, not real key management |
| 3 | Critical | `_types/index.ts` | `toModelRow` hardcodes `status: 'available'` for all models |
| 4 | Major | `models-table.tsx` | `setSortDir` called inside `setSortKey` updater (Strict Mode unsafe) |
| 5 | Major | `usage/...`, `setup/...` | Mock data presented as real usage data with no disclaimer |
| 6 | Major | `dashboard-sidebar.tsx` | Session fetch on every mount instead of SSR prop |
| 7 | Major | `dashboard-sidebar.tsx` | `<a href>` causes full-page reload instead of `<Link>` |
| 8 | Minor | `models-table.tsx:361` | `colSpan={ALL_COLUMNS.length}` ignores hidden columns |
| 9 | Minor | `model-detail-modal.tsx` | No copy feedback in `CopyRow` |
| 10 | Minor | `shared/api.ts`, `_types/index.ts` | `display_name` typed required but guarded as optional |
| 11 | Minor | `spend-chart.tsx` | Inconsistent USD precision (4dp tooltip vs 2dp axis) |
| 12 | Minor | `use-models.ts` | Favourites lost on refresh (no persistence) |
| 13 | Minor | `dashboard-sidebar.tsx` | Hardcoded version badge `v0.4.0` |
| 14 | Minor | `use-models.ts` | Module-level `Set` singleton as default state |
| 15 | Minor | — | No tests for `table-utils.ts` or hook logic |
