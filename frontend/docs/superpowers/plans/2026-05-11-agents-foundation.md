# Plan 1 of 4 — Agents UI: Shared Foundation

**Date:** 2026-05-11
**Goal:** Establish the shared TypeScript types, routing constants, nav wiring, and the `OrbitalLoader` Terra component that all subsequent Agents UI plans depend on. No page or feature UI is written here — this is pure infrastructure.
**Depends on:** Nothing (this is the foundation plan).
**Unlocks:** Plans 2 (Agent Types list/create), 3 (Agent Type detail / run list), 4 (Run detail / event stream).

---

## Architecture

- `src/shared/api.ts` — wire types that mirror Go JSON exactly (snake_case).
- `src/lib/routing/routes.ts` — nested `agents` object replacing the flat string.
- `src/lib/routing/api-routes.ts` — BFF proxy route registry extended with `agentTypes` and `runs` entries.
- `src/app/(dashboard)/_utils/nav-entries.ts` — activate the Agents nav entry (remove `wip` / `badge`).
- `src/packages/terra/src/components/orbital-loader/index.tsx` — new Tier-1 Terra component.
- `src/packages/terra/src/tokens/tokens.css` — `@keyframes` definitions for the orbital animation (Tailwind v4 custom keyframes live in the token CSS, not a separate file).
- `src/packages/terra/src/index.ts` — export the new component.

## Tech Stack

- TypeScript 5 / Next.js 15 App Router
- Tailwind v4 (CSS-first config via `tokens.css`)
- Terra design system (`@orbit/terra`) — `React.forwardRef`, `cn()`, no raw HTML

---

## Task 1 — Add agent types to `src/shared/api.ts`

### Files

- `src/shared/api.ts`

### Steps

- [ ] 1. Open `src/shared/api.ts`. After the closing `}` of `ListPATsResponse` (current last type, line 114), append the following block exactly:

```typescript
// ── Agents — /api/agents ──────────────────────────────────────────────────────

/**
 * Execution framework the agent type targets.
 * Mirrors Go: agents.Framework
 */
export type AgentFramework = 'claude-code' | 'codex' | 'opencode';

/**
 * Lifecycle state of an AgentType resource.
 * Mirrors Go: agents.AgentTypeState
 */
export type AgentTypeState = 'provisioning' | 'active' | 'deleting';

/**
 * Lifecycle state of a single Run.
 * Mirrors Go: agents.RunState
 */
export type RunState = 'pending' | 'starting' | 'active' | 'completing' | 'completed' | 'failed';

/**
 * An AgentType defines the configuration template for a class of agents.
 * Mirrors Go: agents.AgentType
 */
export interface AgentType {
  id: string;
  tenant_id: string;
  name: string;
  slug: string;
  description: string;
  system_prompt: string;
  framework: AgentFramework;
  tool_scopes: string[];
  grant_scopes: string[];
  max_concurrent_runs_per_pod: number;
  state: AgentTypeState;
  created_at: string;
  updated_at: string;
}

/**
 * A single execution of an AgentType.
 * Mirrors Go: agents.Run
 */
export interface Run {
  id: string;
  tenant_id: string;
  agent_type_id: string;
  state: RunState;
  pod_ordinal: number;
  created_by: string;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

/** Top-level response from GET /agents/types. Mirrors Go: agents.ListAgentTypesResponse */
export interface AgentTypesListResponse {
  data: AgentType[];
}

/** Top-level response from GET /agents/runs. Mirrors Go: agents.ListRunsResponse */
export interface RunsListResponse {
  data: Run[];
}

/** Request body for POST /agents/types. Mirrors Go: agents.CreateAgentTypeRequest */
export interface CreateAgentTypeRequest {
  name: string;
  slug: string;
  description: string;
  system_prompt: string;
  framework: AgentFramework;
  tool_scopes: string[];
  grant_scopes: string[];
  max_concurrent_runs_per_pod: number;
}

/** Request body for POST /agents/types/{id}/runs. Mirrors Go: agents.StartRunRequest */
export interface StartRunRequest {
  agent_type_id: string;
  task?: string;
  provision_sandbox?: boolean;
}
```

- [ ] 2. Verify no existing `AgentType` or `Run` symbol exists in the file (would cause a duplicate):

```bash
grep -n "AgentType\|RunState\|AgentFramework" /Users/justin/Development/orbit/orbit/frontend/src/shared/api.ts
```

Expected: only lines inside the block you just added.

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0 with zero errors before continuing.

### Commit

```bash
git add frontend/src/shared/api.ts
git commit -m "feat(agents): add AgentType, Run, and request/response wire types to shared/api.ts"
```

---

## Task 2 — Update `src/lib/routing/routes.ts`

Replace the flat `agents: '/agents'` entry with a nested object that covers the list, create, detail, and run-detail routes.

### Files

- `src/lib/routing/routes.ts`

### Steps

- [ ] 1. In `src/lib/routing/routes.ts`, locate the line:

```typescript
  agents: '/agents',
```

Replace it with:

```typescript
  agents: {
    list:   '/agents' as const,
    new:    '/agents/new' as const,
    detail: (slug: string) => `/agents/${slug}` as const,
    run:    (slug: string, runId: string) => `/agents/${slug}/runs/${runId}` as const,
  },
```

**Important:** The existing `LeafRoutes<T>` helper already handles nested objects — it recursively extracts leaf strings. However, it does **not** pick up function return values. The `AppRoute` union will therefore cover `/agents`, `/agents/new`, but not the dynamic routes. That is intentional and matches how `modelGateway` works (no dynamic routes there either). The dynamic helpers are used at call sites without needing to be in the `AppRoute` union.

Verify no other part of the file references `APP_ROUTES.agents` as a plain string (it previously was just a string):

```bash
grep -n "APP_ROUTES\.agents" /Users/justin/Development/orbit/orbit/frontend/src/lib/routing/routes.ts
```

Expected: 0 results (the only reference is the definition itself, which is now an object).

- [ ] 2. Check all other files that import `APP_ROUTES.agents` and may be using it as a plain string — they will break at compile time after this change. Run:

```bash
grep -rn "APP_ROUTES\.agents[^.]" /Users/justin/Development/orbit/orbit/frontend/src --include="*.ts" --include="*.tsx"
```

For each result:

- `nav-entries.ts`: will be fixed in Task 4.
- Any other file: update it to use `APP_ROUTES.agents.list` or the appropriate sub-key before proceeding.

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Note: TypeScript will emit an error for `nav-entries.ts` at this point because `APP_ROUTES.agents` is now an object but the nav entry still expects an `AppRoute` string. That is expected — it will be fixed in Task 4. The tsc check here is just to ensure no other files broke.

If `nav-entries.ts` is the **only** error, proceed. Otherwise fix any other broken files first.

### Commit

```bash
git add frontend/src/lib/routing/routes.ts
git commit -m "feat(agents): replace flat agents route with nested list/new/detail/run object"
```

---

## Task 3 — Update `src/lib/routing/api-routes.ts`

Add `agentTypes` and `runs` route groups to the BFF route registry and extend the `ApiRoutePath` union.

### Files

- `src/lib/routing/api-routes.ts`

### Steps

- [ ] 1. Open `src/lib/routing/api-routes.ts`. Locate the closing `} as const;` of the `ApiRoute` object (currently after the `analyticsPrincipals` entry). Insert the following two new entries **immediately before** that closing `} as const;`:

```typescript
  /**
   * `/api/agents/types`           — list all agent types for the caller's tenant.
   * `/api/agents/types`           — create a new agent type.
   * `/api/agents/types/[id]`      — get / delete a single agent type.
   */
  agentTypes: {
    list:   (): `${typeof API_BASE}/agents/types`           => `${API_BASE}/agents/types`,
    create: (): `${typeof API_BASE}/agents/types`           => `${API_BASE}/agents/types`,
    detail: (id: string): `${typeof API_BASE}/agents/types/${string}` =>
      `${API_BASE}/agents/types/${id}`,
    delete: (id: string): `${typeof API_BASE}/agents/types/${string}` =>
      `${API_BASE}/agents/types/${id}`,
  },

  /**
   * `/api/agents/runs`                       — list all runs (tenant-wide).
   * `/api/agents/types/[typeId]/runs`         — list runs for a specific type.
   * `/api/agents/types/[typeId]/runs`         — start a new run.
   * `/api/agents/runs/[runId]`                — get / terminate a single run.
   * `/api/agents/runs/[runId]/messages`       — list messages for a run.
   * `/api/agents/runs/[runId]/events`         — SSE event stream for a run.
   */
  runs: {
    list: (): `${typeof API_BASE}/agents/runs` =>
      `${API_BASE}/agents/runs`,
    listForType: (typeId: string): `${typeof API_BASE}/agents/types/${string}/runs` =>
      `${API_BASE}/agents/types/${typeId}/runs`,
    start: (typeId: string): `${typeof API_BASE}/agents/types/${string}/runs` =>
      `${API_BASE}/agents/types/${typeId}/runs`,
    detail: (runId: string): `${typeof API_BASE}/agents/runs/${string}` =>
      `${API_BASE}/agents/runs/${runId}`,
    terminate: (runId: string): `${typeof API_BASE}/agents/runs/${string}` =>
      `${API_BASE}/agents/runs/${runId}`,
    messages: (runId: string): `${typeof API_BASE}/agents/runs/${string}/messages` =>
      `${API_BASE}/agents/runs/${runId}/messages`,
    events: (runId: string): `${typeof API_BASE}/agents/runs/${string}/events` =>
      `${API_BASE}/agents/runs/${runId}/events`,
  },
```

- [ ] 2. Extend the `ApiRoutePath` union at the bottom of the file. Locate the last line of the union:

```typescript
  | string; // analyticsMe and analytics routes include dynamic query strings
```

Insert the following lines **before** that `| string;` line:

```typescript
  | ReturnType<(typeof ApiRoute)['agentTypes']['list']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['create']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['detail']>
  | ReturnType<(typeof ApiRoute)['agentTypes']['delete']>
  | ReturnType<(typeof ApiRoute)['runs']['list']>
  | ReturnType<(typeof ApiRoute)['runs']['listForType']>
  | ReturnType<(typeof ApiRoute)['runs']['start']>
  | ReturnType<(typeof ApiRoute)['runs']['detail']>
  | ReturnType<(typeof ApiRoute)['runs']['terminate']>
  | ReturnType<(typeof ApiRoute)['runs']['messages']>
  | ReturnType<(typeof ApiRoute)['runs']['events']>
```

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0 (still excepting the `nav-entries.ts` error from Task 2 if not yet fixed).

### Commit

```bash
git add frontend/src/lib/routing/api-routes.ts
git commit -m "feat(agents): add agentTypes and runs BFF route entries to api-routes.ts"
```

---

## Task 4 — Update `src/app/(dashboard)/_utils/nav-entries.ts`

Activate the Agents nav entry: point it to the new nested route, remove `wip: true` and `badge: 12`.

### Files

- `src/app/(dashboard)/_utils/nav-entries.ts`

### Steps

- [ ] 1. Locate the Agents entry in `MAIN_NAV`:

```typescript
  { label: 'Agents', href: APP_ROUTES.agents, icon: 'bot', badge: 12, wip: true },
```

Replace it with:

```typescript
  { label: 'Agents', href: APP_ROUTES.agents.list, icon: 'bot' },
```

- [ ] 2. Confirm the file compiles. `APP_ROUTES.agents.list` is `'/agents'` which is a valid `AppRoute` leaf string.

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0 with zero errors. This is the first full-green check after Tasks 1–4.

### Commit

```bash
git add frontend/src/app/\(dashboard\)/_utils/nav-entries.ts
git commit -m "feat(agents): activate Agents nav entry, remove wip flag and badge"
```

---

## Task 5 — Create `OrbitalLoader` Terra component

A branded loading animation (pulsing core dot + concentric rings + orbiting dots). Tier-1 Terra component — zero Orbit domain knowledge.

### Files (create)

- `src/packages/terra/src/components/orbital-loader/index.tsx`

### Files (modify)

- `src/packages/terra/src/tokens/tokens.css`
- `src/packages/terra/src/index.ts`

---

### Step 5a — Add `@keyframes` to `tokens.css`

Tailwind v4 uses CSS-first configuration. Custom keyframes must be defined in the same CSS file that contains `@import 'tailwindcss'` (i.e. `tokens.css`) so that the Tailwind engine picks them up and makes `animate-[...]` arbitrary-value references work.

- [ ] 1. Open `src/packages/terra/src/tokens/tokens.css`. At the very end of the file (after the closing `}` of the `@theme inline { ... }` block), append:

```css
/* ─── OrbitalLoader keyframes ────────────────────────────────────────────── */
/* Used by the OrbitalLoader component. Defined here so the Tailwind v4       */
/* engine (which owns this CSS entry point) compiles the arbitrary animate-[] */
/* class references in the component.                                          */

@keyframes orbital-core-pulse {
  0%,
  100% {
    opacity: 1;
    transform: scale(1);
  }
  50% {
    opacity: 0.6;
    transform: scale(0.75);
  }
}

@keyframes orbital-dot-sm1 {
  from {
    transform: rotate(0deg) translateX(8px) rotate(0deg);
  }
  to {
    transform: rotate(360deg) translateX(8px) rotate(-360deg);
  }
}

@keyframes orbital-dot-sm2 {
  from {
    transform: rotate(80deg) translateX(13px) rotate(-80deg);
  }
  to {
    transform: rotate(440deg) translateX(13px) rotate(-440deg);
  }
}

@keyframes orbital-dot-md1 {
  from {
    transform: rotate(0deg) translateX(10px) rotate(0deg);
  }
  to {
    transform: rotate(360deg) translateX(10px) rotate(-360deg);
  }
}

@keyframes orbital-dot-md2 {
  from {
    transform: rotate(80deg) translateX(16px) rotate(-80deg);
  }
  to {
    transform: rotate(440deg) translateX(16px) rotate(-440deg);
  }
}

@keyframes orbital-dot-md3 {
  from {
    transform: rotate(200deg) translateX(22px) rotate(-200deg);
  }
  to {
    transform: rotate(560deg) translateX(22px) rotate(-560deg);
  }
}
```

---

### Step 5b — Create the component file

- [ ] 2. Create the directory and file `src/packages/terra/src/components/orbital-loader/index.tsx` with the following content exactly:

```typescript
/** @tier 1 — Zero Orbit domain knowledge. Branded loading animation. */
'use client';

import * as React from 'react';
import { cn } from '../../utils';

export interface OrbitalLoaderProps {
  /** Visual size. `md` (default) is 44 × 44 px; `sm` is 32 × 32 px. */
  size?: 'sm' | 'md';
  className?: string;
}

const OrbitalLoader = React.forwardRef<HTMLDivElement, OrbitalLoaderProps>(
  ({ size = 'md', className }, ref) => {
    const isSm = size === 'sm';
    return (
      <div
        ref={ref}
        data-slot="orbital-loader"
        aria-label="Loading"
        role="status"
        className={cn(
          'relative inline-flex items-center justify-center shrink-0',
          isSm ? 'w-8 h-8' : 'w-11 h-11',
          className,
        )}
      >
        {/* Core pulsing dot */}
        <div
          className={cn(
            'rounded-full bg-accent relative z-10',
            isSm
              ? 'w-[5px] h-[5px] shadow-[0_0_6px_var(--color-accent)]'
              : 'w-[6px] h-[6px] shadow-[0_0_10px_color-mix(in_oklch,var(--color-accent)_60%,transparent)]',
            'animate-[orbital-core-pulse_2.4s_ease-in-out_infinite]',
          )}
        />
        {/* Ring 1 */}
        <div
          className={cn(
            'absolute rounded-full border border-accent/35 top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2',
            isSm ? 'w-4 h-4' : 'w-5 h-5',
          )}
        />
        {/* Ring 2 */}
        <div
          className={cn(
            'absolute rounded-full border top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2',
            isSm ? 'w-[26px] h-[26px] border-accent/18' : 'w-8 h-8 border-accent/20',
          )}
        />
        {/* Ring 3 (md only) */}
        {!isSm && (
          <div className="absolute rounded-full border border-accent/10 top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-11 h-11" />
        )}
        {/* Orbiting dot 1 */}
        <div
          className={cn(
            'absolute rounded-full bg-accent top-1/2 left-1/2',
            isSm
              ? 'w-[3.5px] h-[3.5px] -mt-[1.75px] -ml-[1.75px] animate-[orbital-dot-sm1_1.8s_linear_infinite]'
              : 'w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md1_1.8s_linear_infinite]',
          )}
        />
        {/* Orbiting dot 2 */}
        <div
          className={cn(
            'absolute rounded-full bg-accent top-1/2 left-1/2',
            isSm
              ? 'w-[3.5px] h-[3.5px] -mt-[1.75px] -ml-[1.75px] animate-[orbital-dot-sm2_3.2s_linear_infinite_reverse]'
              : 'w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md2_3.2s_linear_infinite_reverse]',
          )}
        />
        {/* Orbiting dot 3 (md only) */}
        {!isSm && (
          <div className="absolute rounded-full bg-accent top-1/2 left-1/2 w-1 h-1 -mt-[2px] -ml-[2px] animate-[orbital-dot-md3_5.0s_linear_infinite]" />
        )}
      </div>
    );
  },
);
OrbitalLoader.displayName = 'OrbitalLoader';

export { OrbitalLoader };
```

---

### Step 5c — Export from Terra index

- [ ] 3. Open `src/packages/terra/src/index.ts`. Locate the `// ─── Organisms ───` section at the bottom. Insert the following block immediately **before** that section (keeping the Atoms section grouping):

```typescript
// ─── Atoms: OrbitalLoader ────────────────────────────────────────────────────
export { OrbitalLoader } from './components/orbital-loader';
export type { OrbitalLoaderProps } from './components/orbital-loader';
```

The exact insertion point in the current file is after:

```typescript
export { Indicator } from './components/indicator';
export type { IndicatorProps } from './components/indicator';
```

and before:

```typescript
export { InputSurface, TypingIndicator, StreamingCursor } from './components/chat';
```

(Any position in the Atoms block is fine — keep it grouped logically near `Indicator` or `StepDot`.)

---

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0 with zero errors.

Spot-check the export is reachable:

```bash
grep -n "OrbitalLoader" /Users/justin/Development/orbit/orbit/frontend/src/packages/terra/src/index.ts
```

Expected: 2 lines (the value export and the type export).

### Commit

```bash
git add frontend/src/packages/terra/src/components/orbital-loader/index.tsx \
        frontend/src/packages/terra/src/tokens/tokens.css \
        frontend/src/packages/terra/src/index.ts
git commit -m "feat(terra): add OrbitalLoader branded loading animation component"
```

---

## Final Verification — Full foundation check

After all five tasks are complete and individually committed:

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0.

Then run `make check` from the repo root to confirm the Go side is unaffected:

```bash
cd /Users/justin/Development/orbit/orbit && make check
```

This plan is complete when:

- [ ] `pnpm tsc --noEmit` exits 0
- [ ] `pnpm lint` exits 0
- [ ] `make check` exits 0
- [ ] `OrbitalLoader` is importable from `@orbit/terra`
- [ ] `APP_ROUTES.agents.list` resolves to `'/agents'` (TypeScript confirms)
- [ ] `ApiRoute.agentTypes.list()` returns `'/api/agents/types'`
- [ ] Agents nav entry has no `wip` flag and no badge
