# Golden Path: Data-Driven Pages with the Custom Hook + Context Pattern

**Audience:** Frontend engineers building new data-driven dashboard pages.  
**Pattern version:** 1.1, custom hook + React Context (see [Zustand upgrade path](#zustand-upgrade-path) for when to graduate).  
**Reference implementation:** `src/app/(dashboard)/model-gateway/models/`: the Models page is the canonical use of this pattern.

---

## Before You Start

This guide walks you through every layer required to wire a backend endpoint to a dashboard page. By the end you will have:

- A **server-side service module** that is the only code that talks to the Go backend, shared by the RSC page and the BFF route.
- A **BFF route handler** that proxies authenticated client-initiated requests to that service.
- A **client-side service module** that is the only code that knows the BFF URL.
- A **custom hook** that owns all state and orchestrates API calls.
- A **Context provider** that distributes that state to every component without prop drilling.
- A **React Server Component page** that fetches the initial data server-side so the page renders fully on first paint.
- A **tab shell** that bridges the RSC page and the hook.
- **Feature components** that only know about context: no fetch calls, no API knowledge.

The example throughout this guide uses a fictional `Widget` resource. Replace every instance of `Widget` / `widget` / `widgets` with your actual resource name.

### Prerequisites

- You have a backend endpoint to connect to (e.g. `GET /v1/widgets`).
- You know the approximate JSON response shape from the Go backend.
- Orbit is running (`make up`). Frontend changes ship with `make redeploy SERVICE=frontend`.

---

## Step 1: Add Types

**Two locations, two purposes:**

| Location                                        | What goes here                                                                                     |
| ----------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `src/shared/api.ts`                             | Types that mirror the raw Go backend JSON response: used by the server-side service and the BFF    |
| `src/app/(dashboard)/{feature}/_types/index.ts` | Feature-local view types (e.g. `WidgetRow`) that may reshape or extend the raw response for UI use |

### `src/shared/api.ts`: raw backend shapes

```typescript
// ---------------------------------------------------------------------------
// Widget domain  (internal/widgets)
// ---------------------------------------------------------------------------

/** Envelope returned by GET /v1/widgets */
export interface WidgetsResponse {
  items: WidgetRaw[];
  total: number;
}

/** Raw widget as returned by the Go backend */
export interface WidgetRaw {
  id: string;
  name: string;
  status: string;
  created_at: string;
}
```

### `_types/index.ts`: feature-local view type

```typescript
import type { WidgetRaw } from '@orbit/shared/api';

/** Possible lifecycle states a Widget can be in. */
export type WidgetStatus = 'active' | 'inactive' | 'pending';

/** UI-ready widget row — mapped from the raw backend response. */
export interface WidgetRow {
  id: string;
  name: string;
  status: WidgetStatus;
  createdAt: string; // ISO 8601 — convert to Date at display time
}

export function toWidgetRow(raw: WidgetRaw): WidgetRow {
  return {
    id: raw.id,
    name: raw.name,
    status: raw.status as WidgetStatus,
    createdAt: raw.created_at,
  };
}
```

> **Tip:** If the backend response doesn't need reshaping, you can skip the mapping function and use the raw type directly as the view type.

---

## Step 2: Create the Server-Side Service Module

**Why a dedicated server service?** This module is the single point of contact with the Go backend. It is imported by _both_ the RSC page (for server-side rendering) and the BFF route handler (for client-initiated refetches). Centralising the upstream fetch here means error handling, timeout configuration, and response parsing live in exactly one place.

```
src/app/(dashboard)/{your-feature}/_services/widgets-server.ts
```

```typescript
import 'server-only';

import { config } from '@orbit/server/config';
import type { WidgetsResponse } from '@orbit/shared/api';
import { toWidgetRow } from '../_types';
import type { WidgetRow } from '../_types';

// Returns a discriminated union so each caller can handle errors in the way
// that makes sense for its context:
//   - RSC page: fall back to [] and pass initialError to the shell
//   - BFF route: return an appropriate HTTP status to the client
export type GatewayWidgetsResult =
  { ok: true; widgets: WidgetRow[] } | { ok: false; status: number; message: string };

export async function fetchWidgetsFromGateway(accessToken: string): Promise<GatewayWidgetsResult> {
  let res: Response;
  try {
    res = await fetch(`${config.orbitApiUrl}/v1/widgets`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    console.error(`[fetchWidgetsFromGateway] ${isTimeout ? 'timeout' : 'network error'}:`, err);
    return {
      ok: false,
      status: isTimeout ? 504 : 502,
      message: isTimeout ? 'Request timed out' : 'Failed to reach backend',
    };
  }

  if (!res.ok) {
    const body = await res.text().catch(() => '');
    console.error(`[fetchWidgetsFromGateway] backend responded ${res.status}`);
    return { ok: false, status: res.status, message: body || res.statusText };
  }

  let body: WidgetsResponse;
  try {
    body = (await res.json()) as WidgetsResponse;
  } catch {
    console.error('[fetchWidgetsFromGateway] failed to parse response as JSON');
    return { ok: false, status: 502, message: 'Invalid response from backend' };
  }

  return { ok: true, widgets: (body.items ?? []).map(toWidgetRow) };
}
```

> **Security rules:**
>
> - Always import `'server-only'`: the compile-time guard that prevents this module from being bundled into client code.
> - The `accessToken` is used here to forward to the backend. It must never appear in a response body.

---

## Step 3: Add a BFF Route Handler

**Why a BFF route?** The Go backend requires an authenticated session token. Browser code must never hold that token directly. A Next.js Route Handler runs server-side, reads the token from the sealed session cookie, calls the server service, and forwards the result. The token never leaves the server.

```
src/app/api/widgets/route.ts
```

```typescript
/**
 * /api/widgets — BFF proxy for the Widgets resource.
 *
 * GET — returns widgets for the authenticated user's session.
 *
 *       Response: 200 { data: WidgetRow[] }
 *                 401 { code: 'unauthorized', message: string }
 *                 502 { code: 'upstream_error', message: string }
 */
import 'server-only';

import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { fetchWidgetsFromGateway } from '../../(dashboard)/{feature}/_services/widgets-server';

export const GET = withAuth(async (_req: NextRequest, session) => {
  const result = await fetchWidgetsFromGateway(session.accessToken);

  if (!result.ok) {
    console.error(`[GET /api/widgets] upstream error ${result.status}: ${result.message}`);
    const clientStatus = result.status >= 500 ? 502 : result.status;
    return NextResponse.json(
      { code: 'upstream_error', message: 'Failed to load widgets' },
      { status: clientStatus },
    );
  }

  return NextResponse.json({ data: result.widgets });
});
```

> **Notes:**
>
> - `withAuth` handles the session check and returns `401` automatically, with no manual `getSession()` / `isSessionValid()` call needed in the handler body.
> - `withAuth` lives in `src/server/route-helpers.ts` and is importable as `@orbit/server/route-helpers`.
> - Use `NextResponse.json` (not the web-standard `Response.json`), consistent with the rest of the app.
> - Error responses use `{ code, message }` shape, not `{ error }`.
> - The handler delegates all upstream logic to the server service. Keep handlers thin.
> - Do **not** add this route to `PUBLIC_PATHS` in `src/proxy.ts` unless it is genuinely unauthenticated.

---

## Step 4: Register the Route in `api-routes.ts`

**Why a registry?** Every BFF URL is defined once in `src/lib/routing/api-routes.ts`. TypeScript enforces that all call sites use the registry function. Raw string literals for API paths are banned. Renaming a route is a one-file change; broken references are compile errors.

Open `frontend/src/lib/routing/api-routes.ts` and add an entry:

```typescript
widgets: {
  GET: (): `${typeof API_BASE}/widgets` => `${API_BASE}/widgets`,
},
```

Also extend the `ApiRoutePath` union at the bottom of the file:

```typescript
export type ApiRoutePath =
  // ... existing entries ...
  ReturnType<(typeof ApiRoute)['widgets']['GET']>; // ← add this line
```

---

## Step 5: Create the Client-Side Service Module

**Why a separate client service?** The hook needs to call the BFF for client-initiated refetches. The client service is the only place in browser code that knows BFF endpoint paths. It wraps `fetch` in a typed function that the hook calls by name. Components never call it directly.

```
src/app/(dashboard)/{your-feature}/_services/widgets-api.ts
```

```typescript
// Client-side fetch helper. Calls the BFF route at /api/widgets.
// Never calls the Go backend directly — that is server-only.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { WidgetRow } from '../_types';

export interface FetchWidgetsResult {
  widgets: WidgetRow[];
  error: string | null;
}

/**
 * Fetch all widgets from the BFF proxy route.
 *
 * Returns a normalised result object — never throws — so callers can
 * pattern-match on `error` without a try/catch at every call site.
 */
export async function fetchWidgets(): Promise<FetchWidgetsResult> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.widgets.GET(), { cache: 'no-store' });
  } catch {
    return { widgets: [], error: 'Network error — could not reach the server.' };
  }

  if (!res.ok) {
    let message = `Request failed (${res.status})`;
    try {
      const body = (await res.json()) as { message?: string };
      if (body.message) message = body.message;
    } catch {
      // ignore parse errors; keep the status-code message
    }
    return { widgets: [], error: message };
  }

  const data = (await res.json()) as { data: WidgetRow[] };
  return { widgets: data.data ?? [], error: null };
}
```

Rules for client service modules:

- Pure async functions only: no React, no hooks, no JSX.
- Import routes from `ApiRoute`, never from string literals.
- **Return a result object, never throw**: the hook pattern-matches on `result.error`.
- Always pass `cache: 'no-store'`: user data must never be served stale from the Next.js fetch cache.

---

## Step 6: Define the State Interface and Create the Hook

This is the architectural core of the pattern. The **interface** is the upgrade contract. Both the current hook and any future Zustand store must satisfy it, so components never need to change.

```
src/app/(dashboard)/{your-feature}/_hooks/use-widgets.ts
```

```typescript
'use client';

import { useState, useCallback } from 'react';
import { fetchWidgets } from '../_services/widgets-api';
import type { WidgetRow } from '../_types';

// ── Types ─────────────────────────────────────────────────────────────────────

/** Load state for any async data fetch. */
export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

/**
 * The complete state and actions interface for the Widgets feature.
 *
 * Both the current useWidgets hook and any future Zustand store must satisfy
 * this type exactly. Components interact only with this interface — they have
 * no knowledge of how state is managed underneath.
 */
export interface WidgetsStateShape {
  // ── Server state
  widgets: WidgetRow[];
  status: LoadStatus;
  error: string | null;

  // ── Actions
  refresh(): Promise<void>;
}

// ── Hook ──────────────────────────────────────────────────────────────────────

/**
 * useWidgets
 *
 * Manages the full state for the Widgets feature. Accepts `initialData` and
 * `initialError` from the RSC parent — the RSC fetches on the server and
 * passes the result down so the client renders immediately without a loading
 * flash.
 *
 * Calling `refresh()` triggers a fresh client-side fetch via the BFF.
 */
export function useWidgets(
  initialData: WidgetRow[],
  initialError: string | null = null,
): WidgetsStateShape {
  const [widgets, setWidgets] = useState<WidgetRow[]>(initialData);
  const [status, setStatus] = useState<LoadStatus>(
    // Order matters: check initialError first, then data presence
    initialError ? 'error' : initialData.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async () => {
    setStatus('loading');
    setError(null);

    const result = await fetchWidgets();

    if (result.error) {
      setError(result.error);
      setStatus('error');
    } else {
      setWidgets(result.widgets);
      setStatus('success');
    }
  }, []);

  return { widgets, status, error, refresh };
}
```

> **Why no try/catch in `refresh()`?** The client service (`fetchWidgets`) never throws. It always returns `{ widgets, error }`. The hook pattern-matches on `result.error` instead of catching exceptions. This keeps the async flow linear and easy to follow.

> **Why `useCallback` on actions?** The context provider passes the hook's return value directly as the context value. Stable action references prevent unnecessary re-renders of every consumer when unrelated state changes.

---

## Step 7: Create the Context Provider and Consumer Hook

```
src/app/(dashboard)/{your-feature}/_context/widgets-context.tsx
```

```tsx
'use client';

import { createContext, useContext } from 'react';
import { useWidgets } from '../_hooks/use-widgets';
import type { WidgetsStateShape } from '../_hooks/use-widgets';
import type { WidgetRow } from '../_types';

// ── Context ───────────────────────────────────────────────────────────────────

const WidgetsContext = createContext<WidgetsStateShape | null>(null);

// ── Provider ──────────────────────────────────────────────────────────────────

interface WidgetsProviderProps {
  initialData: WidgetRow[];
  initialError: string | null;
  children: React.ReactNode;
}

/**
 * Mounts the WidgetsContext provider, initialising the hook synchronously
 * from server-fetched data — no useEffect hydration flash.
 *
 * Place this in the tab shell, not directly in the RSC page.
 */
export function WidgetsProvider({ initialData, initialError, children }: WidgetsProviderProps) {
  const state = useWidgets(initialData, initialError);
  return <WidgetsContext.Provider value={state}>{children}</WidgetsContext.Provider>;
}

// ── Consumer hook ─────────────────────────────────────────────────────────────

/**
 * Returns the current WidgetsStateShape from context.
 *
 * Throws if called outside a WidgetsProvider — intentional.
 * Components using this hook must be inside the provider tree.
 */
export function useWidgetsContext(): WidgetsStateShape {
  const ctx = useContext(WidgetsContext);
  if (!ctx) {
    throw new Error('useWidgetsContext must be used inside <WidgetsProvider>');
  }
  return ctx;
}
```

> **Components import `useWidgetsContext`, never `useWidgets`.**  
> The hook is an implementation detail of the provider. Components interact only with the context.

---

## Step 8: Create the Tab Shell

The tab shell is the `'use client'` boundary. It receives server-fetched data as props, mounts the provider, and renders the feature component tree. It contains no state, no logic, no effects.

```
src/app/(dashboard)/{your-feature}/_components/widgets-tab-shell.tsx
```

```tsx
'use client';

import { WidgetsProvider } from '../_context/widgets-context';
import { WidgetsTab } from './widgets-tab';
import type { WidgetRow } from '../_types';

interface WidgetsTabShellProps {
  initialWidgets: WidgetRow[];
  initialError: string | null;
}

/**
 * Client boundary for the widgets tab.
 *
 * Receives server-prefetched data as props, bootstraps WidgetsProvider, and
 * renders the tab. The shell itself has no state — it is a thin wiring layer.
 *
 * Naming convention: XTabShell (not XClientShell).
 * Location: feature-level _components/, not nested inside the route folder.
 */
export function WidgetsTabShell({ initialWidgets, initialError }: WidgetsTabShellProps) {
  return (
    <WidgetsProvider initialData={initialWidgets} initialError={initialError}>
      <WidgetsTab />
    </WidgetsProvider>
  );
}
```

> **Prop naming:** The shell accepts `initialX` props (matching the RSC page's variable names), but passes them to the provider as `initialData` / `initialError`. This keeps the provider interface consistent across features while the shell props remain descriptive.

---

## Step 9: Convert the Page to a React Server Component

The RSC page fetches initial data server-side so the HTML delivered to the browser already contains real content. A co-located `loading.tsx` provides the Suspense skeleton automatically, so no `<Suspense>` wrapper is needed in the page itself.

```
src/app/(dashboard)/{your-feature}/widgets/page.tsx
```

```tsx
// RSC — fetches widgets server-side so the page renders with real data.
// The co-located loading.tsx shows a skeleton while this fetch completes.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchWidgetsFromGateway } from '../_services/widgets-server';
import { WidgetsTabShell } from '../_components/widgets-tab-shell';

export default async function WidgetsPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchWidgetsFromGateway(session.accessToken)
    : null;

  const initialWidgets = result?.ok ? result.widgets : [];
  const initialError = result && !result.ok ? result.message : null;

  return <WidgetsTabShell initialWidgets={initialWidgets} initialError={initialError} />;
}
```

> **RSC rules:**
>
> - No `'use client'` directive.
> - No React hooks.
> - Always call `isSessionValid(session)` before fetching. Do not assume a session exists.
> - Use the shared server service (`widgets-server.ts`). Never inline the upstream fetch in the page.
> - On failure, pass empty data + an error message. Let the hook render the error state client-side.
> - Do **not** wrap the shell in `<Suspense>`. Next.js App Router uses `loading.tsx` for this automatically.

### Co-locate a `loading.tsx`

```
src/app/(dashboard)/{your-feature}/widgets/loading.tsx
```

The `loading.tsx` skeleton is required for any page whose components call `useSearchParams()` (which requires a Suspense boundary) or simply to improve perceived performance. Mirror the real layout exactly so the skeleton matches the loaded state.

---

## Step 10: Write Components That Only Know About Context

Feature components call `useWidgetsContext()` for data and actions. They accept no data props. They contain no `fetch()` calls.

```tsx
'use client';

import { useMemo } from 'react';
import { useWidgetsContext } from '../../_context/widgets-context';

export function WidgetsTab() {
  const { widgets, status, error, refresh } = useWidgetsContext();

  // Derived data: compute filtered/sorted list with useMemo.
  // Never store computed values in hook or component state.
  const visibleWidgets = useMemo(() => {
    return widgets.filter((w) => w.status === 'active');
  }, [widgets]);

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
          <tr key={w.id}>
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
- Compute derived data (filtered/sorted lists) with `useMemo`. Never store computed values in hook state.
- Call action functions returned by the context for user interactions.
- Render `status === 'error'` with a retry button that calls `refresh()`.
- Accept no data props: the context is the sole data source.

### Server-side filter columns

When a `DataTable` column has `mode: 'server'`, selecting a filter value must trigger a BFF re-fetch rather than an RSC navigation. **Do not use `router.push()`** for filter changes: it causes a full RSC re-render and a loading skeleton flash on every dropdown selection.

Use the following pattern instead:

```tsx
'use client';

import { useCallback } from 'react';
import { useSearchParams } from 'next/navigation';
import { useWidgetsContext } from '../../_context/widgets-context';

export function WidgetsTab() {
  const { records, refresh } = useWidgetsContext();
  const searchParams = useSearchParams();

  // Server-side filter: update the URL for shareability, then re-fetch
  // client-side via the BFF — no RSC navigation, no skeleton flash.
  const handleFilterChange = useCallback(
    (filterId: string) => {
      const params = new URLSearchParams(searchParams.toString());
      if (filterId) {
        params.set('filter', filterId);
      } else {
        params.delete('filter');
      }
      // Silently update the URL so deep-links remain shareable.
      window.history.replaceState(null, '', `?${params.toString()}`);
      // Re-fetch via the BFF, passing the new filter value as a param.
      refresh(filterId || undefined);
    },
    [searchParams, refresh],
  );

  // ...column definition using mode: 'server':
  // filter: {
  //   mode: 'server',
  //   options: filterOptions,
  //   selected: new Set(selectedFilterId ? [selectedFilterId] : []),
  //   onToggle: (val) => handleFilterChange(val === selectedFilterId ? '' : val),
  //   onClear: () => handleFilterChange(''),
  // },
}
```

For this to work, the hook's `refresh` action must accept the filter value as an optional parameter and forward it to the client service:

```typescript
// In use-widgets.ts — extend the state shape and hook signature:
export interface WidgetsStateShape {
  // ...
  refresh(filter?: string): Promise<void>;
}

// In useWidgets — the refresh callback forwards the param to fetchWidgets:
const refresh = useCallback(async (filter?: string) => {
  setStatus('loading');
  setError(null);
  const result = await fetchWidgets(filter);
  // ...
}, []);
```

**Why `window.history.replaceState` and not `router.push`?**

| Approach                     | URL update | RSC navigation      | Skeleton flash       |
| ---------------------------- | ---------- | ------------------- | -------------------- |
| `router.push('?filter=x')`   | ✅         | ✅ (full re-render) | ✅ (every selection) |
| `replaceState` + `refresh()` | ✅         | ❌                  | ❌                   |

The RSC page reads `?filter=` on first load (deep-link support, zero flash on first render). After that, all filter changes go client-side through the BFF. The two paths are intentionally separate: SSR for initial load, BFF fetch for subsequent interactions.

---

## Complete File Map

For a feature named `widgets` under a route group `my-feature`:

| File                                                               | Purpose                                                              |
| ------------------------------------------------------------------ | -------------------------------------------------------------------- |
| `src/shared/api.ts`                                                | Raw backend response types (`WidgetRaw`, `WidgetsResponse`)          |
| `src/server/route-helpers.ts`                                      | `withAuth` HOF: session guard shared by all BFF route handlers       |
| `src/lib/routing/api-routes.ts`                                    | Add `ApiRoute.widgets.GET()` + `ApiRoutePath` union                  |
| `src/app/(dashboard)/my-feature/_types/index.ts`                   | Feature-local view type (`WidgetRow`, `WidgetStatus`, `toWidgetRow`) |
| `src/app/(dashboard)/my-feature/_services/widgets-server.ts`       | Server-only: fetches from Go backend; shared by RSC page + BFF       |
| `src/app/api/widgets/route.ts`                                     | BFF route handler: proxies client fetches via the server service     |
| `src/app/(dashboard)/my-feature/_services/widgets-api.ts`          | Client-only: calls BFF via route registry; returns result object     |
| `src/app/(dashboard)/my-feature/_hooks/use-widgets.ts`             | `WidgetsStateShape` interface + `useWidgets` hook                    |
| `src/app/(dashboard)/my-feature/_context/widgets-context.tsx`      | `WidgetsProvider` + `useWidgetsContext()`                            |
| `src/app/(dashboard)/my-feature/_components/widgets-tab-shell.tsx` | `'use client'` shell: mounts provider, renders tab                   |
| `src/app/(dashboard)/my-feature/widgets/page.tsx`                  | RSC page: validates session, calls server service, renders shell     |
| `src/app/(dashboard)/my-feature/widgets/loading.tsx`               | Suspense skeleton: mirrors real layout                               |
| `src/app/(dashboard)/my-feature/_components/widgets-tab.tsx`       | Feature component: reads from context only                           |

---

## Verification Checklist

Use this checklist before opening a PR. Every item must be true.

### Types

- [ ] Raw backend response types added to `src/shared/api.ts`.
- [ ] Feature view types live in `_types/index.ts`, not in `src/shared/api.ts`.
- [ ] `WidgetStatus` is a string union, not a plain `string`.

### Server Service (`widgets-server.ts`)

- [ ] File starts with `import 'server-only'`.
- [ ] Returns a discriminated union `{ ok: true; ... } | { ok: false; status; message }`, never throws.
- [ ] `AbortSignal.timeout(10_000)` is set on the upstream fetch.
- [ ] `console.error` logs include the function name as a prefix `[fetchWidgetsFromGateway]`.
- [ ] `accessToken` is never included in a return value.

### BFF Route Handler

- [ ] File starts with `import 'server-only'`.
- [ ] Uses `NextResponse.json(...)`, not `Response.json(...)`.
- [ ] Route is wrapped with `withAuth` from `@orbit/server/route-helpers`, with no manual `getSession()` / `isSessionValid()` call in the handler body.
- [ ] Error responses use `{ code, message }` shape.
- [ ] Route is registered in `api-routes.ts` and `ApiRoutePath` union is extended.
- [ ] Route is **not** added to `PUBLIC_PATHS` in `src/proxy.ts`.

### Client Service (`widgets-api.ts`)

- [ ] Calls `ApiRoute.widgets.GET()`: no raw URL string literals.
- [ ] **Returns** `{ widgets, error }`, never throws.
- [ ] `cache: 'no-store'` is set on the fetch call.
- [ ] Contains no React imports, no hooks, no JSX.
- [ ] Never imported directly by a component file.

### Hook (`use-widgets.ts`)

- [ ] Has `'use client'` directive.
- [ ] `WidgetsStateShape` is a standalone exported interface.
- [ ] `LoadStatus` is a standalone exported type.
- [ ] Hook signature is `(initialData: WidgetRow[], initialError: string | null = null)`: positional params, not an options object.
- [ ] Status initialises as: `initialError ? 'error' : initialData.length > 0 ? 'success' : 'idle'`.
- [ ] `refresh()` checks `result.error` on the result object: no try/catch.
- [ ] All action functions wrapped with `useCallback`.

### Context (`widgets-context.tsx`)

- [ ] Has `'use client'` directive.
- [ ] Context default value is `null`, not a partial state object.
- [ ] `useWidgetsContext()` throws if called outside the provider.
- [ ] Provider calls `useWidgets(initialData, initialError)`: one hook call, one provider render.

### Tab Shell

- [ ] Has `'use client'` directive.
- [ ] Named `WidgetsTabShell` (not `WidgetsClientShell`).
- [ ] Lives at feature-level `_components/`, not nested inside the route folder.
- [ ] Shell props are `initialWidgets` / `initialError`; these are passed to the provider as `initialData` / `initialError`.
- [ ] Does not call `useWidgetsContext()`.
- [ ] Contains no business logic, state, or effects.

### RSC Page (`page.tsx`)

- [ ] No `'use client'` directive.
- [ ] No React hooks.
- [ ] Calls `isSessionValid(session)` before fetching. Does not assume session exists.
- [ ] Imports and calls the server service: no inline fetch logic.
- [ ] On failure, passes empty data + error message to the shell.
- [ ] No `<Suspense>` wrapper, relies on co-located `loading.tsx`.

### `loading.tsx`

- [ ] Co-located in the same route folder as `page.tsx`.
- [ ] Skeleton mirrors the real layout column-for-column.

### Feature Components

- [ ] No `fetch()` calls.
- [ ] No imports from service modules.
- [ ] No data props: data comes exclusively from `useWidgetsContext()`.
- [ ] Filtered/sorted lists computed with `useMemo`, not stored in state.
- [ ] `status === 'error'` is rendered with a retry button calling `refresh()`.

### Server-side filter columns (if applicable)

- [ ] Filter changes use `window.history.replaceState(...)` to update the URL, **not** `router.push(...)`.
- [ ] `refresh(filterId)` is called immediately after `replaceState` to trigger a BFF re-fetch.
- [ ] `handleFilterChange` is wrapped in `useCallback` with `[searchParams, refresh]` as deps.
- [ ] `useRouter` is **not** imported: it is unused when this pattern is applied.
- [ ] The hook's `refresh` signature accepts the filter param (`refresh(filter?: string)`) and forwards it to the client service.

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

If none of these apply, stay with the hook pattern: it is simpler and has zero extra dependencies.

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
    widgets: [],
    status:  'idle',
    error:   null,

    refresh: async () => {
      set({ status: 'loading', error: null });
      const result = await fetchWidgets();
      if (result.error) {
        set({ status: 'error', error: result.error });
      } else {
        set({ widgets: result.widgets, status: 'success' });
      }
    },
  }));

Step 3 — Update the context provider to use the store:
  // In widgets-context.tsx, replace:
  const state = useWidgets(initialData, initialError);

  // With:
  const state = useWidgetsStore();
  useEffect(() => {
    if (initialData.length > 0) {
      useWidgetsStore.setState({ widgets: initialData, status: 'success' });
    }
    if (initialError) {
      useWidgetsStore.setState({ error: initialError, status: 'error' });
    }
  }, []);

Step 4 — Delete use-widgets.ts once the store is verified.

Step 5 (optional) — Remove the Context layer entirely:
  Components can call useWidgetsStore() directly instead of useWidgetsContext().
  The WidgetsStateShape interface is unchanged, so component logic is identical.
  Do a safe find-and-replace across the feature directory:
    useWidgetsContext()  →  useWidgetsStore()
  Delete widgets-context.tsx and the provider from the tab shell.
  This is optional — the Context layer adds negligible overhead for most features.
```

No component files change in steps 1–4. The `WidgetsStateShape` interface is the contract that makes this safe: both the hook and the store satisfy it, so swapping the implementation is transparent to every consumer.

---

## Reference Implementation

The Models page (`src/app/(dashboard)/model-gateway/models/`) is the canonical feature built with this pattern. Read it alongside this guide to see every layer in production code.

| This guide                          | Models page equivalent                       |
| ----------------------------------- | -------------------------------------------- |
| `WidgetRaw` in `src/shared/api.ts`  | `ModelRow` raw fields in `src/shared/api.ts` |
| `WidgetRow` in `_types/index.ts`    | `ModelRow` in `_types/index.ts`              |
| `_services/widgets-server.ts`       | `_services/models-server.ts`                 |
| `src/app/api/widgets/route.ts`      | `src/app/api/models/route.ts`                |
| `ApiRoute.widgets.GET()`            | `ApiRoute.models.GET()`                      |
| `_services/widgets-api.ts`          | `_services/models-api.ts`                    |
| `_hooks/use-widgets.ts`             | `_hooks/use-models.ts`                       |
| `_context/widgets-context.tsx`      | `_context/models-context.tsx`                |
| `_components/widgets-tab-shell.tsx` | `_components/models-tab-shell.tsx`           |
| `widgets/page.tsx`                  | `models/page.tsx`                            |
| `widgets/loading.tsx`               | `models/loading.tsx`                         |
| `_components/widgets-tab.tsx`       | `_components/models-tab.tsx`                 |

The Logs page (`src/app/(dashboard)/model-gateway/logs/`) is the second feature built with this pattern and demonstrates the same structure for a paginated endpoint with multiple filter parameters.

---

_Guide owned by: Frontend Engineering_  
_Last updated: 2026-04-16_  
_Source PRD: `docs/prd/model-gateway-state-architecture.md`_
