# Golden Path: Data-Driven Pages with the Custom Hook + Context Pattern

**Audience:** Frontend engineers building new data-driven dashboard pages.  
**Pattern version:** 1.0 — custom hook + React Context (see [Zustand upgrade path](#zustand-upgrade-path) for when to graduate).  
**Reference implementation:** `src/app/(dashboard)/model-gateway/models/` — the Models page is the first concrete use of this pattern.

---

## Before You Start

This guide walks you through every layer required to wire a backend endpoint to a dashboard page. By the end you will have:

- A **BFF route handler** that proxies authenticated requests to the Go backend.
- A **service module** that is the only code that knows the BFF URL.
- A **custom hook** that owns all state and orchestrates API calls.
- A **Context provider** that distributes that state to every component without prop drilling.
- A **React Server Component page** that fetches the initial data server-side so the page renders fully on first paint.
- A **client shell** that bridges the RSC page and the hook.
- **Feature components** that only know about context — no fetch calls, no API knowledge.

The example throughout this guide uses a fictional `Widget` resource. Replace every instance of `Widget` / `widget` / `widgets` with your actual resource name.

### Prerequisites

- You have a backend endpoint to connect to (e.g. `GET /v1/widgets`).
- You know the approximate JSON response shape from the Go backend.
- The Orbit frontend dev environment runs (`pnpm dev` from `frontend/`).

---

## Step 1 — Add Types to `src/shared/api.ts`

**Why this file?** `src/shared/api.ts` is the single source of truth for shapes that mirror the Go backend's JSON responses. Both the BFF route handler (server-side) and the client service module import from here — they never define types independently.

Open `frontend/src/shared/api.ts` and add your resource types:

```typescript
// ---------------------------------------------------------------------------
// Widget domain  (internal/widgets)
// ---------------------------------------------------------------------------

/** Possible lifecycle states a Widget can be in. */
export type WidgetStatus = 'active' | 'inactive' | 'pending';

/** A single widget returned by GET /v1/widgets */
export interface WidgetRow {
  id: string;
  name: string;
  status: WidgetStatus;
  createdAt: string; // ISO 8601 — convert to Date at display time
  description: string;
}

/** Envelope returned by GET /v1/widgets */
export interface WidgetsResponse {
  items: WidgetRow[];
  total: number;
}
```

> **Tip:** If the backend response shape is not yet finalised, use the best guess you have and mark the field with `// TODO: confirm with backend`. The type lives in one file so updating it later is a one-line change.

---

## Step 2 — Add a BFF Route Handler

**Why a BFF route?** The Go backend requires an authenticated session token. Client-side code (browser) must never hold or forward that token directly. A Next.js Route Handler runs server-side, reads the token from the sealed iron-session cookie, and proxies the request — the browser never sees the token.

Create the handler file:

```
src/app/api/widgets/route.ts
```

```typescript
/**
 * /api/widgets — BFF proxy for the Widgets resource.
 *
 * GET — fetches all widgets from the Go backend on behalf of the
 *       authenticated user. The session access token is forwarded
 *       in the Authorization header; it is never exposed to the browser.
 *
 *       Response: 200 WidgetsResponse
 *                 401 { error: 'unauthorized' }
 *                 502 { error: 'upstream_error' }
 */
import 'server-only';

import { getSession, isSessionValid } from '@orbit/server/session';
import { config } from '@orbit/server/config';
import type { WidgetsResponse } from '@orbit/shared/api';

export async function GET(): Promise<Response> {
  const session = await getSession();

  if (!isSessionValid(session)) {
    return Response.json({ error: 'unauthorized' }, { status: 401 });
  }

  let upstream: Response;
  try {
    upstream = await fetch(`${config.orbitApiUrl}/v1/widgets`, {
      headers: {
        Authorization: `Bearer ${session.accessToken}`,
        'Content-Type': 'application/json',
      },
      signal: AbortSignal.timeout(10_000),
    });
  } catch {
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  if (!upstream.ok) {
    if (upstream.status === 401) {
      return Response.json({ error: 'unauthorized' }, { status: 401 });
    }
    return Response.json({ error: 'upstream_error' }, { status: 502 });
  }

  const data = (await upstream.json()) as WidgetsResponse;
  return Response.json(data);
}
```

> **Security rules:**
>
> - Always import `'server-only'` at the top — this is a compile-time guard that prevents this file from being bundled into client code.
> - Never include `accessToken` in the response body.
> - Never import `orbitClient` from client-side code — it is server-only.

---

## Step 3 — Register the Route in `api-routes.ts`

**Why a registry?** Every BFF URL is defined once in `src/lib/routing/api-routes.ts`. TypeScript enforces that all call sites use the registry function — string literals for API paths are banned. This means renaming a route is a one-file change, and broken references are a compile error.

Open `frontend/src/lib/routing/api-routes.ts` and add an entry:

```typescript
// In the ApiRoute object, add alongside the existing entries:

/**
 * `/api/widgets`
 *
 * GET — returns all widgets for the current user's session.
 */
widgets: {
  GET: (): `${typeof API_BASE}/widgets` => `${API_BASE}/widgets`,
},
```

Also extend the `ApiRoutePath` union at the bottom of the file:

```typescript
export type ApiRoutePath =
  | ReturnType<(typeof ApiRoute)['health']['GET']>
  | ReturnType<(typeof ApiRoute)['logout']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['POST']>
  | ReturnType<(typeof ApiRoute)['session']['GET']>
  | ReturnType<(typeof ApiRoute)['discover']['PATH']>
  | `${typeof AUTH_BASE}/discover?email=${string}`
  | ReturnType<(typeof ApiRoute)['widgets']['GET']>; // ← add this line
```

> **Do not add new routes to `PUBLIC_PATHS` in `src/proxy.ts`** unless the route is genuinely unauthenticated. Feature data routes require a valid session.

---

## Step 4 — Create the Service Module

**Why a dedicated service module?** The hook needs to call the BFF. If the hook calls `fetch('/api/widgets')` directly, the URL is a magic string inside business logic. The service module is the only place in client code that knows BFF endpoint paths — it wraps `fetch` in a typed function that the hook calls by name.

Create:

```
src/app/(dashboard)/{your-feature}/_services/widgets-api.ts
```

```typescript
import { ApiRoute } from '@orbit/lib/routing';
import type { WidgetsResponse } from '@orbit/shared/api';

/**
 * Fetch all widgets from the BFF route handler.
 *
 * Called by the useWidgets hook. Never called directly by components.
 */
export async function fetchWidgets(): Promise<WidgetsResponse> {
  const res = await fetch(ApiRoute.widgets.GET(), {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
  });

  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string };
    throw new Error(body.error ?? `widgets fetch failed: ${res.status}`);
  }

  return res.json() as Promise<WidgetsResponse>;
}
```

Rules for service modules:

- Pure async functions only — no React, no hooks, no JSX.
- Import routes from `ApiRoute`, never from string literals.
- Throw a descriptive `Error` on failure — the hook catches it and sets `status: 'error'`.

---

## Step 5 — Define the State Interface and Create the Hook

This is the architectural core of the pattern. There are two parts: the **interface** and the **implementation**.

Create:

```
src/app/(dashboard)/{your-feature}/_hooks/use-widgets.ts
```

### 5a — Define `WidgetsStateShape` (the upgrade contract)

```typescript
import type { WidgetRow, WidgetStatus } from '@orbit/shared/api';

// ── UI-only types ─────────────────────────────────────────────────────────────

export interface WidgetFilterState {
  search: string;
  statuses: Set<WidgetStatus>;
}

// ── The upgrade contract ──────────────────────────────────────────────────────

/**
 * The complete state and actions interface for the Widgets feature.
 *
 * Both the current useWidgets hook and any future Zustand store must
 * satisfy this type exactly. Components interact only with this interface —
 * they have no knowledge of how state is managed underneath.
 */
export interface WidgetsStateShape {
  // ── Server state
  widgets: WidgetRow[];
  status: 'idle' | 'loading' | 'success' | 'error';
  error: string | null;

  // ── UI state
  filters: WidgetFilterState;
  selectedWidgetId: string | null;

  // ── Actions
  refresh(): Promise<void>;
  setFilter(update: Partial<WidgetFilterState>): void;
  setSelectedWidget(id: string | null): void;
}
```

### 5b — Implement `useWidgets`

```typescript
import { useState, useCallback } from 'react';
import { fetchWidgets } from '../_services/widgets-api';

const DEFAULT_FILTERS: WidgetFilterState = {
  search: '',
  statuses: new Set(),
};

interface UseWidgetsOptions {
  initialWidgets?: WidgetRow[];
}

/**
 * Custom hook that owns all state for the Widgets feature.
 *
 * - Accepts `initialWidgets` from the RSC page (server-fetched data).
 *   When provided, initialises to `status: 'success'` immediately —
 *   no on-mount fetch, no loading spinner on first paint.
 * - Exposes a `refresh()` action for client-side refetches.
 * - Returns a value that satisfies `WidgetsStateShape`.
 */
export function useWidgets({ initialWidgets = [] }: UseWidgetsOptions = {}): WidgetsStateShape {
  const [widgets, setWidgets] = useState<WidgetRow[]>(initialWidgets);
  const [status, setStatus] = useState<WidgetsStateShape['status']>(
    initialWidgets.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(null);
  const [filters, setFiltersState] = useState<WidgetFilterState>(DEFAULT_FILTERS);
  const [selectedWidgetId, setSelectedWidgetId] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setStatus('loading');
    setError(null);
    try {
      const result = await fetchWidgets();
      setWidgets(result.items);
      setStatus('success');
    } catch (e) {
      setStatus('error');
      setError(e instanceof Error ? e.message : 'Unknown error');
    }
  }, []);

  const setFilter = useCallback((update: Partial<WidgetFilterState>) => {
    setFiltersState((prev) => ({ ...prev, ...update }));
  }, []);

  const setSelectedWidget = useCallback((id: string | null) => {
    setSelectedWidgetId(id);
  }, []);

  return {
    widgets,
    status,
    error,
    filters,
    selectedWidgetId,
    refresh,
    setFilter,
    setSelectedWidget,
  };
}
```

> **Why `useCallback` on every action?** The context provider memoises the value object. Actions that are stable references (via `useCallback`) prevent unnecessary re-renders of every consumer when unrelated state changes.

---

## Step 6 — Create the Context Provider and Consumer Hook

Create:

```
src/app/(dashboard)/{your-feature}/_context/widgets-context.tsx
```

```tsx
'use client';

import { createContext, useContext, type ReactNode } from 'react';
import { useWidgets } from '../_hooks/use-widgets';
import type { WidgetsStateShape } from '../_hooks/use-widgets';
import type { WidgetRow } from '@orbit/shared/api';

// ── Context ───────────────────────────────────────────────────────────────────

const WidgetsContext = createContext<WidgetsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface WidgetsProviderProps {
  initialWidgets?: WidgetRow[];
  children: ReactNode;
}

/**
 * Renders the WidgetsContext provider, initialising the hook with
 * server-fetched data so the first render is fully populated.
 *
 * Place this in the client shell, not directly in the RSC page.
 */
export function WidgetsProvider({ initialWidgets, children }: WidgetsProviderProps) {
  const value = useWidgets({ initialWidgets });
  return <WidgetsContext.Provider value={value}>{children}</WidgetsContext.Provider>;
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

/**
 * Returns the current WidgetsStateShape from context.
 *
 * Throws if called outside a WidgetsProvider — this is intentional.
 * Components that use this hook must be rendered inside the provider tree.
 */
export function useWidgetsContext(): WidgetsStateShape {
  const ctx = useContext(WidgetsContext);
  if (!ctx) {
    throw new Error('useWidgetsContext must be used within a WidgetsProvider');
  }
  return ctx;
}
```

> **Components import `useWidgetsContext`, never `useWidgets`.**  
> The hook is an implementation detail of the provider — components interact only with the context.

---

## Step 7 — Convert the Page to a React Server Component

The page fetches the initial data server-side, so the HTML delivered to the browser already contains the full model list. No loading spinner, no layout shift.

```
src/app/(dashboard)/{your-feature}/widgets/page.tsx
```

```tsx
import { Suspense } from 'react';
import { getSession } from '@orbit/server/session';
import { config } from '@orbit/server/config';
import type { WidgetsResponse, WidgetRow } from '@orbit/shared/api';
import { WidgetsClientShell } from './_components/widgets-client-shell';

/**
 * Server Component — runs on the server only.
 *
 * Fetches widgets directly from the Go backend (no BFF round-trip —
 * server-side code has direct session access). Passes the result to
 * the client shell as props. If the fetch fails, passes an empty array
 * so the hook can show an error state rather than crashing.
 */
async function fetchInitialWidgets(): Promise<WidgetRow[]> {
  try {
    const session = await getSession();
    const res = await fetch(`${config.orbitApiUrl}/v1/widgets`, {
      headers: { Authorization: `Bearer ${session.accessToken}` },
      signal: AbortSignal.timeout(10_000),
    });
    if (!res.ok) return [];
    const data = (await res.json()) as WidgetsResponse;
    return data.items;
  } catch {
    return [];
  }
}

export default async function WidgetsPage() {
  const initialWidgets = await fetchInitialWidgets();

  return (
    <Suspense fallback={null}>
      <WidgetsClientShell initialWidgets={initialWidgets} />
    </Suspense>
  );
}
```

> **RSC rules:**
>
> - No `'use client'` directive — this file is a Server Component by default.
> - No React hooks (`useState`, `useEffect`, etc.).
> - Can import `server-only` modules (`session.ts`, `config.ts`, raw `fetch` against the Go backend).
> - Return an empty array on failure — the hook handles the error state client-side.

---

## Step 8 — Create the Client Shell

The client shell bridges the RSC page and the hook. It is the only `'use client'` component that the page renders directly.

```
src/app/(dashboard)/{your-feature}/widgets/_components/widgets-client-shell.tsx
```

```tsx
'use client';

import { WidgetsProvider } from '../../_context/widgets-context';
import { WidgetsTable } from './widgets-table';
import type { WidgetRow } from '@orbit/shared/api';

interface Props {
  initialWidgets: WidgetRow[];
}

/**
 * The entry point for the client-side component tree.
 *
 * - Receives server-fetched data as props.
 * - Wraps children in WidgetsProvider, initialising the hook synchronously.
 * - Does NOT call useWidgetsContext() itself — it only sets up the provider.
 * - Does NOT contain any business logic.
 */
export function WidgetsClientShell({ initialWidgets }: Props) {
  return (
    <WidgetsProvider initialWidgets={initialWidgets}>
      <WidgetsTable />
      {/* Add more feature components here */}
    </WidgetsProvider>
  );
}
```

The shell has one job: mount the provider and compose children. Keep it thin — no state, no logic.

---

## Step 9 — Write Components That Only Know About Context

Feature components call `useWidgetsContext()` for data and actions. They accept no data props. They contain no `fetch()` calls.

```tsx
'use client';

import { useMemo } from 'react';
import { useWidgetsContext } from '../../_context/widgets-context';

export function WidgetsTable() {
  const { widgets, status, error, filters, refresh, setSelectedWidget } = useWidgetsContext();

  // Derived data: compute filtered list with useMemo — do not store this in hook state
  const visibleWidgets = useMemo(() => {
    let result = widgets;
    if (filters.search) {
      const q = filters.search.toLowerCase();
      result = result.filter(
        (w) => w.name.toLowerCase().includes(q) || w.id.toLowerCase().includes(q),
      );
    }
    if (filters.statuses.size > 0) {
      result = result.filter((w) => filters.statuses.has(w.status));
    }
    return result;
  }, [widgets, filters]);

  if (status === 'loading') return <div>Loading…</div>;
  if (status === 'error')
    return (
      <div>
        Error: {error} <button onClick={refresh}>Retry</button>
      </div>
    );
  if (status === 'idle') return null;

  return (
    <table>
      <tbody>
        {visibleWidgets.map((w) => (
          <tr key={w.id} onClick={() => setSelectedWidget(w.id)}>
            <td>{w.name}</td>
            <td>{w.status}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}
```

**Component rules:**

- Read state from `useWidgetsContext()`.
- Compute derived data (filtered/sorted lists) with `useMemo` — never store computed values in hook state.
- Call action functions returned by the context for user interactions.
- Render `status === 'error'` and provide a `retry` button that calls `refresh()`.
- Accept no data props — the context is the data source.

---

## Complete File Map

For a feature named `widgets` under a route group `my-feature`:

| File                                                                          | Purpose                                            |
| ----------------------------------------------------------------------------- | -------------------------------------------------- |
| `src/shared/api.ts`                                                           | Add `WidgetRow`, `WidgetStatus`, `WidgetsResponse` |
| `src/lib/routing/api-routes.ts`                                               | Add `ApiRoute.widgets.GET()`                       |
| `src/app/api/widgets/route.ts`                                                | BFF route handler — proxies to `/v1/widgets`       |
| `src/app/(dashboard)/my-feature/_services/widgets-api.ts`                     | `fetchWidgets()` — calls BFF via route registry    |
| `src/app/(dashboard)/my-feature/_hooks/use-widgets.ts`                        | `WidgetsStateShape` interface + `useWidgets` hook  |
| `src/app/(dashboard)/my-feature/_context/widgets-context.tsx`                 | `WidgetsProvider` + `useWidgetsContext()`          |
| `src/app/(dashboard)/my-feature/widgets/page.tsx`                             | RSC page — server-fetches initial data             |
| `src/app/(dashboard)/my-feature/widgets/_components/widgets-client-shell.tsx` | `'use client'` shell — mounts provider             |
| `src/app/(dashboard)/my-feature/widgets/_components/widgets-table.tsx`        | Feature component — reads from context             |

---

## Verification Checklist

Use this checklist before opening a PR. Every item must be true.

### Types (`src/shared/api.ts`)

- [ ] Resource type mirrors the Go backend JSON shape exactly.
- [ ] Response envelope type is defined (`WidgetsResponse`).
- [ ] Types are exported and importable from `@orbit/shared/api`.

### BFF Route Handler

- [ ] File starts with `import 'server-only'`.
- [ ] Session is validated before the upstream fetch — returns 401 if invalid.
- [ ] `accessToken` is forwarded in `Authorization: Bearer` — never in the response body.
- [ ] Upstream errors are caught and return 502, not an unhandled exception.
- [ ] Route is registered in `api-routes.ts` and `ApiRoutePath` union is extended.
- [ ] Route is **not** added to `PUBLIC_PATHS` in `src/proxy.ts`.

### Service Module

- [ ] Calls `ApiRoute.widgets.GET()` — no raw URL string literals.
- [ ] Throws a descriptive `Error` on non-OK response.
- [ ] Contains no React imports, no hooks, no JSX.
- [ ] Is never imported directly by a component file.

### Hook (`use-widgets.ts`)

- [ ] `WidgetsStateShape` interface is a standalone exported type — not inlined in the hook return.
- [ ] Hook accepts `initialWidgets` as an optional parameter.
- [ ] When `initialWidgets` is provided, `status` initialises to `'success'` — no on-mount fetch fires.
- [ ] `refresh()` sets `status: 'loading'` before calling the service function.
- [ ] `refresh()` sets `status: 'error'` and populates `error` on failure.
- [ ] All action functions are wrapped with `useCallback`.
- [ ] Hook return value satisfies `WidgetsStateShape`.

### Context (`widgets-context.tsx`)

- [ ] File has `'use client'` directive.
- [ ] Context default value is `null` (not a partial state object).
- [ ] `useWidgetsContext()` throws if called outside the provider.
- [ ] Provider renders `useWidgets({ initialWidgets })` — one hook call, one provider render.

### RSC Page (`page.tsx`)

- [ ] No `'use client'` directive.
- [ ] No React hooks.
- [ ] Server-side fetch failure returns `[]` — never throws.
- [ ] Passes `initialWidgets` as a prop to the client shell.
- [ ] Shell is wrapped in `<Suspense>`.

### Client Shell

- [ ] Has `'use client'` directive.
- [ ] Renders `<WidgetsProvider initialWidgets={initialWidgets}>`.
- [ ] Does not call `useWidgetsContext()`.
- [ ] Contains no business logic, no state, no effects.

### Feature Components

- [ ] No `fetch()` calls.
- [ ] No imports from service modules.
- [ ] No data props — data comes exclusively from `useWidgetsContext()`.
- [ ] Filtered/sorted lists computed with `useMemo`, not stored in local state.
- [ ] `status === 'error'` is rendered with a retry button calling `refresh()`.

---

## Zustand Upgrade Path

### When to upgrade

The hook + context pattern is sufficient for most features. Upgrade to Zustand when **any** of the following is true:

| Signal                                      | Example                                                                       |
| ------------------------------------------- | ----------------------------------------------------------------------------- |
| State must be shared across multiple routes | Widgets selected on page A must be visible on page B                          |
| Zustand devtools are needed                 | Complex state transitions that are hard to trace with React DevTools          |
| Middleware is needed                        | `persist` for localStorage, `immer` for deeply nested updates                 |
| Fine-grained subscriptions matter           | A component subscribes to a single field; Context causes full-tree re-renders |

If none of these apply, stay with the hook pattern — it is simpler and has zero extra dependencies.

### How to upgrade (per feature, not global)

The upgrade is mechanical. Components do not change in steps 1–4.

```
Step 1 — Install Zustand once for the project:
  pnpm add zustand

Step 2 — Create a store file alongside the existing hook:
  src/app/(dashboard)/{feature}/_hooks/use-widgets.store.ts

  import { create } from 'zustand';
  import type { WidgetsStateShape } from './use-widgets';
  import { fetchWidgets } from '../_services/widgets-api';

  export const useWidgetsStore = create<WidgetsStateShape>((set) => ({
    widgets:          [],
    status:           'idle',
    error:            null,
    filters:          { search: '', statuses: new Set() },
    selectedWidgetId: null,

    refresh: async () => {
      set({ status: 'loading', error: null });
      try {
        const result = await fetchWidgets();
        set({ widgets: result.items, status: 'success' });
      } catch (e) {
        set({ status: 'error', error: (e as Error).message });
      }
    },

    setFilter: (update) =>
      set((s) => ({ filters: { ...s.filters, ...update } })),

    setSelectedWidget: (id) =>
      set({ selectedWidgetId: id }),
  }));

Step 3 — Update the context provider to use the store:
  // In widgets-context.tsx, replace:
  const value = useWidgets({ initialWidgets });

  // With:
  const value = useWidgetsStore();
  useEffect(() => {
    if (initialWidgets && initialWidgets.length > 0) {
      useWidgetsStore.setState({ widgets: initialWidgets, status: 'success' });
    }
  }, []);
  // (Add useEffect to the imports.)

Step 4 — Delete use-widgets.ts once the store is verified.

Step 5 (optional) — Remove the Context layer entirely:
  Components can call useWidgetsStore() directly instead of useWidgetsContext().
  The WidgetsStateShape interface is unchanged, so component logic is identical.
  Do a safe find-and-replace across the feature directory:
    useWidgetsContext()  →  useWidgetsStore()
  Delete widgets-context.tsx and the provider from the client shell.
  This is optional — the Context layer adds negligible overhead for most features.
```

No component files change in steps 1–4. The `WidgetsStateShape` interface is the contract that makes this safe — both the hook and the store satisfy it, so swapping the implementation is transparent to every consumer.

---

## Reference Implementation

The Models page (`src/app/(dashboard)/model-gateway/models/`) is the first feature built with this pattern. Read it alongside this guide to see every layer in production code.

| This guide                     | Models page equivalent            |
| ------------------------------ | --------------------------------- |
| `WidgetRow`                    | `ModelRow` in `src/shared/api.ts` |
| `ApiRoute.widgets.GET()`       | `ApiRoute.models.GET()`           |
| `src/app/api/widgets/route.ts` | `src/app/api/models/route.ts`     |
| `_services/widgets-api.ts`     | `_services/models-api.ts`         |
| `_hooks/use-widgets.ts`        | `_hooks/use-models.ts`            |
| `_context/widgets-context.tsx` | `_context/models-context.tsx`     |
| `widgets/page.tsx`             | `models/page.tsx`                 |
| `widgets-client-shell.tsx`     | `models-client-shell.tsx`         |

---

_Guide owned by: Frontend Engineering_  
_Last updated: 2025-07-14_  
_Source PRD: `docs/prd/model-gateway-state-architecture.md`_
