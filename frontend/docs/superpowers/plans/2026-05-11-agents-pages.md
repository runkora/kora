# Plan 3 of 4 — Agents UI: Dashboard Pages

## Goal

Implement the three dashboard pages for the Orbit Agents UI: the agents list page (`/agents`), the create-agent form (`/agents/new`), and the agent detail page (`/agents/{slug}`). All pages live inside the `(dashboard)` route group and inherit the standard sidebar + header chrome from the existing `DashboardLayout`.

## Architecture

```
src/app/(dashboard)/agents/
├── layout.tsx                                  # Thin layout — page padding only
├── page.tsx                                    # RSC: fetches agent types + recent runs, renders AgentsShell
├── _types/
│   └── index.ts                               # AgentTypeRow, RunRow, transformers, formatDuration
├── _services/
│   ├── agents-server.ts                       # Server-only: fetchAgentTypesFromBFF, fetchRunsFromBFF
│   └── agents-api.ts                          # Client-side: fetchAgentTypes, fetchRuns, fetchStartRun, createAgentType
├── _hooks/
│   └── use-agents.ts                          # AgentsStateShape, useAgents hook
├── _context/
│   └── agents-context.tsx                     # AgentsProvider + useAgentsContext
├── _components/
│   ├── agents-shell.tsx                       # Client boundary — wraps AgentsProvider
│   ├── agents-page.tsx                        # Main list page UI
│   └── start-run-modal.tsx                    # Start-run modal
├── new/
│   ├── page.tsx                               # RSC: renders CreateAgentForm
│   └── _components/
│       └── create-agent-form.tsx              # Client form component
└── [slug]/
    ├── page.tsx                               # RSC: fetches agent + runs by slug
    ├── _services/
    │   ├── agent-detail-server.ts             # Server-only: fetchAgentDetailFromBFF
    │   └── agent-detail-api.ts                # Client: deleteAgentType, updateAgentType
    ├── _hooks/
    │   └── use-agent-detail.ts                # AgentDetailStateShape, useAgentDetail hook
    ├── _context/
    │   └── agent-detail-context.tsx           # AgentDetailProvider + useAgentDetailContext
    └── _components/
        ├── agent-detail-shell.tsx             # Client boundary — wraps AgentDetailProvider
        ├── agent-detail-page.tsx              # Tabs + header
        ├── agent-overview-tab.tsx             # Overview tab content
        ├── agent-runs-tab.tsx                 # Runs table tab
        ├── agent-settings-tab.tsx             # Settings / edit tab
        └── start-run-modal.tsx                # Reused inline (or shared via agents-page import)
```

Routes and API routes are defined in Plan 1 (`src/lib/routing/routes.ts` and `src/lib/routing/api-routes.ts`). This plan consumes those symbols; it does not modify the routing files.

## Tech Stack

- Next.js App Router RSC pages + client components
- `@orbit/terra`: `Stack`, `Grid`, `Heading`, `Text`, `Button`, `Icon`, `Badge`, `Card`, `CardHeader`, `CardTitle`, `CardDescription`, `CardContent`, `CardFooter`, `Tabs`, `TabsList`, `TabsTrigger`, `TabsContent`, `Modal`, `ModalHeader`, `ModalBody`, `ModalFooter`, `Form`, `FormField`, `Input`, `Textarea`, `NativeSelect`, `Separator`, `Alert`, `Callout`, `Skeleton`, `Toggle`, `DataTable`, `DataTableColumn`
- `@orbit/components`: `StatusBadge`, `EmptyState`, `ConfirmDialog`, `MetricCard`, `ScopeChip`
- `@orbit/server/session`: `getSession`, `isSessionValid`
- `@orbit/lib/routing`: `APP_ROUTES`, `ApiRoute`
- `@orbit/utils`: `fmtDate`, `fmtDateTime`
- `useRouter`, `useCallback`, `useState`, `useContext`, `createContext`, `useMemo` from React/Next.js

## State / view type mappings (reference throughout)

**AgentType state → StatusBadge:**
| `state` | `status` prop | `label` prop |
|---------|--------------|--------------|
| `active` | `healthy` | (default) |
| `provisioning` | `degraded` | `"Provisioning"` |
| `deleting` | `error` | `"Deleting"` |

**Run state → StatusBadge:**
| `state` | `status` prop | `label` prop |
|---------|--------------|--------------|
| `pending` | `idle` | (default) |
| `starting` | `degraded` | (default) |
| `active` | `healthy` | (default) |
| `completing` | `degraded` | (default) |
| `completed` | `idle` | `"Completed"` |
| `failed` | `error` | (default) |

**AgentFramework → Badge:**
| `framework` | display label | `variant` |
|------------|--------------|-----------|
| `claude-code` | `"Claude Code"` | `"default"` |
| `codex` | `"Codex"` | `"outline"` |
| `opencode` | `"OpenCode"` | `"secondary"` |

---

## Task 1 — Types, services, hook, and context for the agents list

### Files

- `src/app/(dashboard)/agents/_types/index.ts`
- `src/app/(dashboard)/agents/_services/agents-server.ts`
- `src/app/(dashboard)/agents/_services/agents-api.ts`
- `src/app/(dashboard)/agents/_hooks/use-agents.ts`
- `src/app/(dashboard)/agents/_context/agents-context.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/_types/index.ts`:

```typescript
// ── Display-layer types for the Agents feature ────────────────────────────────
//
// AgentTypeRow and RunRow are the shapes consumed by all UI components.
// Raw API shapes (AgentType, Run) only appear at the service layer boundary.

import type { AgentType, Run, AgentTypeState, RunState } from '@orbit/shared/api';

export type { AgentTypeState, RunState };

export interface AgentTypeRow {
  id: string;
  name: string;
  slug: string;
  description: string;
  /** Human-readable label, e.g. "Claude Code" for framework "claude-code". */
  framework: string;
  /** Raw framework value for badge variant selection. */
  frameworkKey: string;
  state: AgentTypeState;
  toolScopes: string[];
  grantScopes: string[];
  maxConcurrentRunsPerPod: number;
  systemPrompt: string;
  createdAt: string;
  updatedAt: string;
}

export interface RunRow {
  id: string;
  agentTypeId: string;
  agentTypeName: string;
  agentTypeSlug: string;
  state: RunState;
  createdBy: string;
  startedAt: string | null;
  completedAt: string | null;
  createdAt: string;
  /** Elapsed ms: completedAt - startedAt, or now - startedAt for active runs, or null if not started. */
  durationMs: number | null;
}

// ── Transformers ──────────────────────────────────────────────────────────────

const FRAMEWORK_LABELS: Record<string, string> = {
  'claude-code': 'Claude Code',
  codex: 'Codex',
  opencode: 'OpenCode',
};

export function toAgentTypeRow(a: AgentType): AgentTypeRow {
  return {
    id: a.id,
    name: a.name,
    slug: a.slug,
    description: a.description,
    framework: FRAMEWORK_LABELS[a.framework] ?? a.framework,
    frameworkKey: a.framework,
    state: a.state,
    toolScopes: a.tool_scopes,
    grantScopes: a.grant_scopes,
    maxConcurrentRunsPerPod: a.max_concurrent_runs_per_pod,
    systemPrompt: a.system_prompt,
    createdAt: a.created_at,
    updatedAt: a.updated_at,
  };
}

export function toRunRow(r: Run, agentTypes: AgentTypeRow[]): RunRow {
  const agent = agentTypes.find((a) => a.id === r.agent_type_id);
  const startedMs = r.started_at ? new Date(r.started_at).getTime() : null;
  const endMs = r.completed_at ? new Date(r.completed_at).getTime() : null;
  const durationMs = startedMs !== null ? (endMs ?? Date.now()) - startedMs : null;
  return {
    id: r.id,
    agentTypeId: r.agent_type_id,
    agentTypeName: agent?.name ?? 'Unknown',
    agentTypeSlug: agent?.slug ?? r.agent_type_id,
    state: r.state,
    createdBy: r.created_by,
    startedAt: r.started_at,
    completedAt: r.completed_at,
    createdAt: r.created_at,
    durationMs,
  };
}

// ── Utilities ─────────────────────────────────────────────────────────────────

export function formatDuration(ms: number | null): string {
  if (ms === null) return '—';
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ${s % 60}s`;
  return `${Math.floor(m / 60)}h ${m % 60}m`;
}

/**
 * Returns a human-readable relative time string for a given ISO timestamp.
 * Examples: "just now", "2 minutes ago", "3 hours ago", "4 days ago".
 */
export function formatRelativeTime(iso: string): string {
  const diffMs = Date.now() - new Date(iso).getTime();
  const diffS = Math.floor(diffMs / 1000);
  if (diffS < 60) return 'just now';
  const diffM = Math.floor(diffS / 60);
  if (diffM < 60) return `${diffM} minute${diffM !== 1 ? 's' : ''} ago`;
  const diffH = Math.floor(diffM / 60);
  if (diffH < 24) return `${diffH} hour${diffH !== 1 ? 's' : ''} ago`;
  const diffD = Math.floor(diffH / 24);
  return `${diffD} day${diffD !== 1 ? 's' : ''} ago`;
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/_services/agents-server.ts`:

```typescript
import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

// ── Result type ───────────────────────────────────────────────────────────────

export type AgentsResult =
  | { ok: true; agentTypes: AgentTypeRow[]; runs: RunRow[] }
  | { ok: false; status: number; message: string };

// ── Internal BFF fetch helper ─────────────────────────────────────────────────

async function bffFetch(
  path: string,
  accessToken: string,
): Promise<{ ok: boolean; status: number; body: unknown }> {
  const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? 'http://localhost:3000';
  let res: Response;
  try {
    res = await fetch(`${appUrl}${path}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    return { ok: false, status: isTimeout ? 504 : 502, body: null };
  }
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    return { ok: false, status: res.status, body: text };
  }
  try {
    const json = await res.json();
    return { ok: true, status: res.status, body: json };
  } catch {
    return { ok: false, status: 502, body: 'Invalid JSON response' };
  }
}

// ── Exported service ──────────────────────────────────────────────────────────

/**
 * Fetches agent types and recent runs in parallel from the BFF.
 * Used by the RSC `/agents` page for server-side initial data.
 */
export async function fetchAgentsFromBFF(accessToken: string): Promise<AgentsResult> {
  const [typesResult, runsResult] = await Promise.all([
    bffFetch('/api/agents/types', accessToken),
    bffFetch('/api/agents/runs', accessToken),
  ]);

  if (!typesResult.ok) {
    const msg = typeof typesResult.body === 'string' ? typesResult.body : 'Failed to load agents';
    console.error(`[fetchAgentsFromBFF] agent types fetch failed ${typesResult.status}: ${msg}`);
    return { ok: false, status: typesResult.status, message: msg };
  }

  const typesBody = typesResult.body as { data: AgentType[] };
  const agentTypes = (typesBody.data ?? []).map(toAgentTypeRow);

  const runsBody = runsResult.ok ? (runsResult.body as { data: Run[] }) : { data: [] };
  const runs = (runsBody.data ?? []).map((r) => toRunRow(r, agentTypes));

  return { ok: true, agentTypes, runs };
}
```

- [ ] 3. Create `src/app/(dashboard)/agents/_services/agents-api.ts`:

```typescript
// ── Agents API — client-side BFF calls ───────────────────────────────────────
//
// All functions call the BFF routes added in Plan 2. Never throws — callers
// pattern-match on the returned `error` field.

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentType, Run, CreateAgentTypeRequest, StartRunRequest } from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../_types';
import type { AgentTypeRow, RunRow } from '../_types';

// ── fetchAgentTypes ───────────────────────────────────────────────────────────

export async function fetchAgentTypes(): Promise<{ data: AgentTypeRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.list(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType[] };
  return { data: (body.data ?? []).map(toAgentTypeRow), error: null };
}

// ── fetchRuns ─────────────────────────────────────────────────────────────────

export async function fetchRuns(
  agentTypes: AgentTypeRow[],
): Promise<{ data: RunRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.list(), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Run[] };
  return { data: (body.data ?? []).map((r) => toRunRow(r, agentTypes)), error: null };
}

// ── fetchStartRun ─────────────────────────────────────────────────────────────

export async function fetchStartRun(
  agentTypeId: string,
  payload: StartRunRequest,
): Promise<{ run: RunRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.start(agentTypeId), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { run: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      run: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Run };
  const run = toRunRow(body.data, []);
  return { run, error: null };
}

// ── createAgentType ───────────────────────────────────────────────────────────

export async function createAgentType(
  payload: CreateAgentTypeRequest,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.list(), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { agentType: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      agentType: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType };
  return { agentType: toAgentTypeRow(body.data), error: null };
}
```

- [ ] 4. Create `src/app/(dashboard)/agents/_hooks/use-agents.ts`:

```typescript
'use client';

// ── useAgents ─────────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents list page. Accepts initial server-
// rendered data so the page renders immediately without a loading flash.
//
// FeatureStateShape upgrade contract: when this feature graduates to Zustand,
// the store's state + actions must satisfy the AgentsStateShape interface.

import { useState, useCallback } from 'react';
import { fetchAgentTypes, fetchRuns } from '../_services/agents-api';
import type { AgentTypeRow, RunRow } from '../_types';

export type LoadStatus = 'idle' | 'loading' | 'success' | 'error';

export interface AgentsStateShape {
  agentTypes: AgentTypeRow[];
  recentRuns: RunRow[];
  status: LoadStatus;
  error: string | null;
  refresh: () => Promise<void>;
}

export function useAgents(
  initialAgentTypes: AgentTypeRow[],
  initialRuns: RunRow[],
  initialError: string | null,
): AgentsStateShape {
  const [agentTypes, setAgentTypes] = useState<AgentTypeRow[]>(initialAgentTypes);
  const [recentRuns, setRecentRuns] = useState<RunRow[]>(initialRuns);
  const [status, setStatus] = useState<LoadStatus>(
    initialError ? 'error' : initialAgentTypes.length > 0 ? 'success' : 'idle',
  );
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async () => {
    setStatus('loading');
    setError(null);

    const typesResult = await fetchAgentTypes();
    if (typesResult.error) {
      setError(typesResult.error);
      setStatus('error');
      return;
    }

    setAgentTypes(typesResult.data);

    const runsResult = await fetchRuns(typesResult.data);
    if (!runsResult.error) {
      setRecentRuns(runsResult.data);
    }

    setStatus('success');
  }, []);

  return { agentTypes, recentRuns, status, error, refresh };
}
```

- [ ] 5. Create `src/app/(dashboard)/agents/_context/agents-context.tsx`:

```typescript
'use client';

// ── AgentsContext ──────────────────────────────────────────────────────────────
//
// Provides AgentsStateShape to any component inside the /agents page tree.
// The RSC page fetches server-side and passes initialData to AgentsProvider,
// which bootstraps useAgents synchronously — no useEffect hydration flash.

import { createContext, useContext } from 'react';
import { useAgents } from '../_hooks/use-agents';
import type { AgentsStateShape } from '../_hooks/use-agents';
import type { AgentTypeRow, RunRow } from '../_types';

const AgentsContext = createContext<AgentsStateShape | null>(null);

interface AgentsProviderProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
  children: React.ReactNode;
}

export function AgentsProvider({
  initialAgentTypes,
  initialRuns,
  initialError,
  children,
}: AgentsProviderProps) {
  const state = useAgents(initialAgentTypes, initialRuns, initialError);
  return <AgentsContext.Provider value={state}>{children}</AgentsContext.Provider>;
}

export function useAgentsContext(): AgentsStateShape {
  const ctx = useContext(AgentsContext);
  if (!ctx) {
    throw new Error('useAgentsContext must be used inside <AgentsProvider>');
  }
  return ctx;
}
```

---

## Task 2 — Agents list page: layout + RSC page

### Files

- `src/app/(dashboard)/agents/layout.tsx`
- `src/app/(dashboard)/agents/page.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/layout.tsx`:

```typescript
// Shared layout for all /agents pages.
// Provides consistent page padding — no data fetching, no providers.

export default function AgentsLayout({ children }: { children: React.ReactNode }) {
  return <main className="px-6 py-8">{children}</main>;
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/page.tsx`:

```typescript
// RSC — fetches agent types + recent runs in parallel server-side so the page
// renders with real data immediately.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentsFromBFF } from './_services/agents-server';
import { AgentsShell } from './_components/agents-shell';

export default async function AgentsPage() {
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchAgentsFromBFF(session.accessToken)
    : null;

  const initialAgentTypes = result?.ok ? result.agentTypes : [];
  const initialRuns = result?.ok ? result.runs : [];
  const initialError = result && !result.ok ? result.message : null;

  return (
    <AgentsShell
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    />
  );
}
```

---

## Task 3 — Agents list page: shell + main component

### Files

- `src/app/(dashboard)/agents/_components/agents-shell.tsx`
- `src/app/(dashboard)/agents/_components/agents-page.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/_components/agents-shell.tsx`:

```typescript
'use client';

/** @tier 3 — agents-scoped. Client boundary that owns the AgentsProvider. */

import { AgentsProvider } from '../_context/agents-context';
import { AgentsPage } from './agents-page';
import type { AgentTypeRow, RunRow } from '../_types';

interface AgentsShellProps {
  initialAgentTypes: AgentTypeRow[];
  initialRuns: RunRow[];
  initialError: string | null;
}

export function AgentsShell({ initialAgentTypes, initialRuns, initialError }: AgentsShellProps) {
  return (
    <AgentsProvider
      initialAgentTypes={initialAgentTypes}
      initialRuns={initialRuns}
      initialError={initialError}
    >
      <AgentsPage />
    </AgentsProvider>
  );
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/_components/agents-page.tsx`:

```typescript
'use client';

/** @tier 3 — agents-scoped. Main UI for the /agents list page. */

import { useMemo, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  DataTable,
  Grid,
  Heading,
  Icon,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import Link from 'next/link';
import { EmptyState, MetricCard, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { fmtDate } from '@orbit/utils';
import { useAgentsContext } from '../_context/agents-context';
import { StartRunModal } from './start-run-modal';
import { formatRelativeTime } from '../_types';
import type { AgentTypeRow } from '../_types';

// ── Helpers ───────────────────────────────────────────────────────────────────

function agentTypeMatchesSearch(row: AgentTypeRow, q: string): boolean {
  const lower = q.toLowerCase();
  return (
    row.name.toLowerCase().includes(lower) ||
    row.slug.toLowerCase().includes(lower) ||
    row.description.toLowerCase().includes(lower) ||
    row.framework.toLowerCase().includes(lower)
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

export function AgentsPage() {
  const { agentTypes, recentRuns, status, error, refresh } = useAgentsContext();
  const router = useRouter();

  const [startRunTarget, setStartRunTarget] = useState<AgentTypeRow | null>(null);

  // ── Metric card computations ───────────────────────────────────────────────

  const activeAgentCount = useMemo(
    () => agentTypes.filter((a) => a.state === 'active').length,
    [agentTypes],
  );

  const activeRunCount = useMemo(
    () =>
      recentRuns.filter((r) =>
        ['active', 'starting', 'pending', 'completing'].includes(r.state),
      ).length,
    [recentRuns],
  );

  const completedTodayCount = useMemo(() => {
    const oneDayAgo = Date.now() - 24 * 60 * 60 * 1000;
    return recentRuns.filter(
      (r) =>
        r.state === 'completed' &&
        r.completedAt !== null &&
        new Date(r.completedAt).getTime() > oneDayAgo,
    ).length;
  }, [recentRuns]);

  // ── Active run counts per agent type ──────────────────────────────────────

  const activeRunsByType = useMemo(() => {
    const counts = new Map<string, number>();
    for (const run of recentRuns) {
      if (['active', 'starting', 'pending', 'completing'].includes(run.state)) {
        counts.set(run.agentTypeId, (counts.get(run.agentTypeId) ?? 0) + 1);
      }
    }
    return counts;
  }, [recentRuns]);

  // ── Table columns ─────────────────────────────────────────────────────────

  const columns = useMemo<DataTableColumn<AgentTypeRow>[]>(
    () => [
      {
        id: 'name',
        header: 'Agent',
        locked: true,
        sortable: true,
        sortAccessor: (row) => row.name,
        cell: (row) => (
          <Stack gap="none">
            <Text size="supporting" weight="emphasis">
              {row.name}
            </Text>
            <Text mono size="caption" color="muted">
              {row.slug}
            </Text>
          </Stack>
        ),
      },
      {
        id: 'framework',
        header: 'Framework',
        cell: (row) => {
          const variantMap: Record<string, 'default' | 'outline' | 'secondary'> = {
            'claude-code': 'default',
            codex: 'outline',
            opencode: 'secondary',
          };
          return (
            <Badge variant={variantMap[row.frameworkKey] ?? 'outline'} size="sm">
              {row.framework}
            </Badge>
          );
        },
      },
      {
        id: 'state',
        header: 'Status',
        cell: (row) => {
          const labelMap: Partial<Record<string, string>> = {
            provisioning: 'Provisioning',
            deleting: 'Deleting',
          };
          const statusMap: Record<string, 'healthy' | 'degraded' | 'error'> = {
            active: 'healthy',
            provisioning: 'degraded',
            deleting: 'error',
          };
          return (
            <StatusBadge
              status={statusMap[row.state] ?? 'unknown'}
              label={labelMap[row.state]}
            />
          );
        },
      },
      {
        id: 'activeRuns',
        header: 'Active Runs',
        cell: (row) => (
          <Text mono size="supporting">
            {activeRunsByType.get(row.id) ?? 0}
          </Text>
        ),
      },
      {
        id: 'created',
        header: 'Created',
        sortable: true,
        sortAccessor: (row) => row.createdAt,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {formatRelativeTime(row.createdAt)}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '6rem',
        cell: (row) => (
          <Stack direction="horizontal" gap="tight" onClick={(e) => e.stopPropagation()}>
            <Button
              variant="ghost"
              size="icon-sm"
              title="Start run"
              aria-label={`Start run for ${row.name}`}
              onClick={() => setStartRunTarget(row)}
            >
              <Icon name="play" size="2xs" />
            </Button>
            <Button variant="ghost" size="icon-sm" aria-label={`Settings for ${row.name}`} asChild>
              <Link href={APP_ROUTES.agents.detail(row.slug)}>
                <Icon name="settings" size="2xs" />
              </Link>
            </Button>
          </Stack>
        ),
      },
    ],
    [activeRunsByType],
  );

  // ── Render ────────────────────────────────────────────────────────────────

  return (
    <Stack gap="comfortable">
      {/* Page header */}
      <Stack direction="horizontal" align="center" justify="between">
        <Stack gap="tight">
          <Heading variant="title">Agents</Heading>
          <Text size="supporting" color="muted">
            Manage agent definitions and start runs.
          </Text>
        </Stack>
        <Button asChild>
          <Link href={APP_ROUTES.agents.new}>
            <Icon name="plus" size="xs" />
            New Agent
          </Link>
        </Button>
      </Stack>

      {/* Metric cards */}
      <Grid cols={{ base: 1, sm: 2, md: 3 }} gap="comfortable">
        <MetricCard
          icon={<Icon name="bot" />}
          label="Active Agents"
          value={activeAgentCount}
        />
        <MetricCard
          icon={<Icon name="activity" />}
          label="Active Runs"
          value={activeRunCount}
        />
        <MetricCard
          icon={<Icon name="check-circle" />}
          label="Completed Today"
          value={completedTodayCount}
        />
      </Grid>

      {/* Agent types table */}
      <DataTable
        data={agentTypes}
        columns={columns}
        getRowKey={(row) => row.id}
        noun="agents"
        searchPlaceholder="Search agents…"
        searchFn={agentTypeMatchesSearch}
        defaultSortId="name"
        defaultSortDir="asc"
        error={
          status === 'error' ? (
            <Stack align="center" paddingBlock="comfortable">
              <Text size="supporting" color="muted">
                {error ?? 'Failed to load agents.'}
              </Text>
              <Button variant="outline" size="sm" onClick={() => void refresh()}>
                Try again
              </Button>
            </Stack>
          ) : undefined
        }
        emptyMessage={
          <EmptyState
            icon="bot"
            title="No agents yet"
            description="Create your first agent definition to start running AI tasks."
            action={
              <Button asChild>
                <Link href={APP_ROUTES.agents.new}>New Agent</Link>
              </Button>
            }
          />
        }
        filteredEmptyMessage="No agents match your search."
        onRowClick={(row) => router.push(APP_ROUTES.agents.detail(row.slug))}
      />

      {/* Start run modal */}
      <StartRunModal
        open={startRunTarget !== null}
        agentType={startRunTarget}
        onClose={() => setStartRunTarget(null)}
        onStarted={(runId) => {
          if (startRunTarget) {
            setStartRunTarget(null);
            router.push(APP_ROUTES.agents.run(startRunTarget.slug, runId));
          }
        }}
      />
    </Stack>
  );
}
```

---

## Task 4 — Start run modal

### Files

- `src/app/(dashboard)/agents/_components/start-run-modal.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/_components/start-run-modal.tsx`:

```typescript
'use client';

/** @tier 3 — agents-scoped. Modal to start a new run for an agent type. */

import { useCallback, useEffect, useState } from 'react';
import {
  Alert,
  Button,
  FormField,
  Icon,
  Modal,
  ModalBody,
  ModalFooter,
  ModalHeader,
  Stack,
  Text,
  Textarea,
  Toggle,
} from '@orbit/terra';
import { fetchStartRun } from '../_services/agents-api';
import type { AgentTypeRow } from '../_types';

interface StartRunModalProps {
  open: boolean;
  agentType: AgentTypeRow | null;
  onClose: () => void;
  onStarted: (runId: string) => void;
}

export function StartRunModal({ open, agentType, onClose, onStarted }: StartRunModalProps) {
  const [task, setTask] = useState('');
  const [provisionSandbox, setProvisionSandbox] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Reset form when modal opens/closes.
  useEffect(() => {
    if (!open) {
      setTask('');
      setProvisionSandbox(false);
      setError(null);
    }
  }, [open]);

  const handleSubmit = useCallback(async () => {
    if (!agentType) return;
    const trimmedTask = task.trim();
    if (!trimmedTask) {
      setError('Task description is required.');
      return;
    }

    setError(null);
    setSubmitting(true);
    try {
      const result = await fetchStartRun(agentType.id, {
        task: trimmedTask,
        provision_sandbox: provisionSandbox,
      });
      if (result.error) {
        setError(result.error);
        return;
      }
      if (result.run) {
        onStarted(result.run.id);
      }
    } catch {
      setError('Something went wrong. Please try again.');
    } finally {
      setSubmitting(false);
    }
  }, [agentType, task, provisionSandbox, onStarted]);

  return (
    <Modal open={open} onClose={onClose} size="md">
      <ModalHeader title={`Start run — ${agentType?.name ?? ''}`} />
      <ModalBody>
        <Stack gap="comfortable">
          <FormField
            id="run-task"
            label="Task"
            hint="Describe what the agent should do."
            required
          >
            <Textarea
              id="run-task"
              value={task}
              onChange={(e) => setTask(e.target.value)}
              placeholder="e.g. Review the auth module for security issues..."
              rows={4}
            />
          </FormField>

          <Stack direction="horizontal" align="center" gap="snug">
            <Toggle
              checked={provisionSandbox}
              onChange={setProvisionSandbox}
              aria-labelledby="provision-sandbox-label"
            />
            <Stack gap="tight">
              <Text id="provision-sandbox-label" size="supporting" weight="emphasis">
                Pre-provision sandbox
              </Text>
              <Text size="supporting" color="muted">
                Reserve a sandbox pod upfront to reduce latency on first tool use.
              </Text>
            </Stack>
          </Stack>

          {error && <Alert variant="error">{error}</Alert>}
        </Stack>
      </ModalBody>
      <ModalFooter>
        <Button variant="outline" size="sm" onClick={onClose} disabled={submitting}>
          Cancel
        </Button>
        <Button
          variant="default"
          size="sm"
          onClick={() => void handleSubmit()}
          disabled={submitting}
        >
          <Icon name="play" size="xs" />
          {submitting ? 'Starting…' : 'Start Run'}
        </Button>
      </ModalFooter>
    </Modal>
  );
}
```

---

## Task 5 — Create agent type form page

### Files

- `src/app/(dashboard)/agents/new/page.tsx`
- `src/app/(dashboard)/agents/new/_components/create-agent-form.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/new/page.tsx`:

```typescript
// RSC — no server data needed, just renders the create agent form.

import { CreateAgentForm } from './_components/create-agent-form';

export default function NewAgentPage() {
  return <CreateAgentForm />;
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/new/_components/create-agent-form.tsx`:

```typescript
'use client';

/** @tier 3 — agents/new-scoped. Form to create a new agent type. */

import { useCallback, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  Card,
  CardContent,
  Form,
  FormField,
  Heading,
  Icon,
  Input,
  NativeSelect,
  Separator,
  Stack,
  Text,
  Textarea,
} from '@orbit/terra';
import Link from 'next/link';
import { APP_ROUTES } from '@orbit/lib/routing';
import { createAgentType } from '../../_services/agents-api';

// ── Framework options ─────────────────────────────────────────────────────────

const FRAMEWORK_OPTIONS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
] as const;

// ── Slug derivation ───────────────────────────────────────────────────────────

function deriveSlug(name: string): string {
  return name
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-|-$/g, '');
}

// ── Component ─────────────────────────────────────────────────────────────────

export function CreateAgentForm() {
  const router = useRouter();

  const [name, setName] = useState('');
  const [slug, setSlug] = useState('');
  const [slugEdited, setSlugEdited] = useState(false);
  const [description, setDescription] = useState('');
  const [framework, setFramework] = useState<'claude-code' | 'codex' | 'opencode'>('claude-code');
  const [systemPrompt, setSystemPrompt] = useState('');
  const [maxConcurrentRuns, setMaxConcurrentRuns] = useState('10');
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<Partial<Record<string, string>>>({});

  // Auto-derive slug from name unless user has manually edited it.
  const handleNameChange = useCallback(
    (value: string) => {
      setName(value);
      if (!slugEdited) {
        setSlug(deriveSlug(value));
      }
    },
    [slugEdited],
  );

  const handleSlugChange = useCallback((value: string) => {
    setSlug(value);
    setSlugEdited(true);
  }, []);

  const validate = useCallback((): boolean => {
    const errors: Partial<Record<string, string>> = {};
    if (!name.trim()) errors.name = 'Name is required.';
    if (!slug.trim()) errors.slug = 'Slug is required.';
    else if (!/^[a-z0-9-]+$/.test(slug)) errors.slug = 'Slug may only contain lowercase letters, numbers, and hyphens.';
    if (!systemPrompt.trim()) errors.systemPrompt = 'System prompt is required.';
    const maxRuns = parseInt(maxConcurrentRuns, 10);
    if (isNaN(maxRuns) || maxRuns < 1 || maxRuns > 50)
      errors.maxConcurrentRuns = 'Must be between 1 and 50.';
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }, [name, slug, systemPrompt, maxConcurrentRuns]);

  const handleSubmit = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      setError(null);
      if (!validate()) return;

      setSubmitting(true);
      try {
        const result = await createAgentType({
          name: name.trim(),
          slug: slug.trim(),
          description: description.trim(),
          framework,
          system_prompt: systemPrompt.trim(),
          tool_scopes: [],
          grant_scopes: [],
          max_concurrent_runs_per_pod: parseInt(maxConcurrentRuns, 10),
        });
        if (result.error) {
          setError(result.error);
          return;
        }
        if (result.agentType) {
          router.push(APP_ROUTES.agents.detail(result.agentType.slug));
        }
      } catch {
        setError('Something went wrong. Please try again.');
      } finally {
        setSubmitting(false);
      }
    },
    [validate, name, slug, description, framework, systemPrompt, maxConcurrentRuns, router],
  );

  return (
    <Form onSubmit={(e) => void handleSubmit(e)} noValidate gap="comfortable">
      {/* Breadcrumb + heading */}
      <Stack gap="snug">
        <Stack direction="horizontal" gap="snug" align="center">
          <Button variant="ghost" size="sm" asChild>
            <Link href={APP_ROUTES.agents.list}>
              <Icon name="arrow-left" size="xs" />
              Agents
            </Link>
          </Button>
        </Stack>
        <Heading variant="title">New Agent</Heading>
      </Stack>

      <Card>
        <CardContent>
          <Stack gap="comfortable">
            {/* Identity section */}
            <Stack gap="snug">
              <Heading variant="section">Identity</Heading>
              <FormField id="agent-name" label="Name" required error={fieldErrors.name}>
                <Input
                  id="agent-name"
                  value={name}
                  onChange={(e) => handleNameChange(e.target.value)}
                  placeholder="e.g. Security Reviewer"
                  maxLength={128}
                />
              </FormField>
              <FormField
                id="agent-slug"
                label="Slug"
                hint="URL-safe identifier, auto-derived from name. Only lowercase letters, numbers, and hyphens."
                required
                error={fieldErrors.slug}
              >
                <Input
                  id="agent-slug"
                  value={slug}
                  onChange={(e) => handleSlugChange(e.target.value)}
                  placeholder="e.g. security-reviewer"
                  mono
                />
              </FormField>
              <FormField id="agent-description" label="Description" hint="Optional summary of what this agent does.">
                <Textarea
                  id="agent-description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  placeholder="e.g. Reviews code for security vulnerabilities and compliance issues."
                  rows={3}
                />
              </FormField>
            </Stack>

            <Separator />

            {/* Behaviour section */}
            <Stack gap="snug">
              <Heading variant="section">Behaviour</Heading>
              <FormField id="agent-framework" label="Framework" required>
                <NativeSelect
                  id="agent-framework"
                  value={framework}
                  onChange={(e) =>
                    setFramework(e.target.value as typeof framework)
                  }
                >
                  {FRAMEWORK_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </NativeSelect>
              </FormField>
              <FormField
                id="agent-system-prompt"
                label="System Prompt"
                required
                hint="Instructions that define the agent's role and behaviour."
                error={fieldErrors.systemPrompt}
              >
                <Textarea
                  id="agent-system-prompt"
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  placeholder="You are a security-focused code reviewer. Your role is to…"
                  rows={8}
                  mono
                />
              </FormField>
            </Stack>

            <Separator />

            {/* Capacity section */}
            <Stack gap="snug">
              <Heading variant="section">Capacity</Heading>
              <FormField
                id="agent-max-concurrent"
                label="Max concurrent runs per pod"
                hint="How many simultaneous runs this agent type may have on a single sandbox pod (1–50)."
                error={fieldErrors.maxConcurrentRuns}
              >
                <Input
                  id="agent-max-concurrent"
                  type="number"
                  min={1}
                  max={50}
                  value={maxConcurrentRuns}
                  onChange={(e) => setMaxConcurrentRuns(e.target.value)}
                />
              </FormField>
            </Stack>
          </Stack>
        </CardContent>
      </Card>

      {error && <Alert variant="error">{error}</Alert>}

      <Stack direction="horizontal" justify="end" gap="snug">
        <Button type="button" variant="outline" size="sm" asChild>
          <Link href={APP_ROUTES.agents.list}>Cancel</Link>
        </Button>
        <Button type="submit" variant="default" size="sm" disabled={submitting}>
          {submitting ? 'Creating…' : 'Create Agent'}
        </Button>
      </Stack>
    </Form>
  );
}
```

---

## Task 6 — Agent detail page: data layer + shell

### Files

- `src/app/(dashboard)/agents/[slug]/_services/agent-detail-server.ts`
- `src/app/(dashboard)/agents/[slug]/_services/agent-detail-api.ts`
- `src/app/(dashboard)/agents/[slug]/_hooks/use-agent-detail.ts`
- `src/app/(dashboard)/agents/[slug]/_context/agent-detail-context.tsx`
- `src/app/(dashboard)/agents/[slug]/page.tsx`
- `src/app/(dashboard)/agents/[slug]/_components/agent-detail-shell.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/[slug]/_services/agent-detail-server.ts`:

```typescript
import 'server-only';

import type { AgentType, Run } from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

export type AgentDetailResult =
  | { ok: true; agent: AgentTypeRow; runs: RunRow[] }
  | { ok: false; status: number; message: string };

async function bffFetch(
  path: string,
  accessToken: string,
): Promise<{ ok: boolean; status: number; body: unknown }> {
  const appUrl = process.env.NEXT_PUBLIC_APP_URL ?? 'http://localhost:3000';
  let res: Response;
  try {
    res = await fetch(`${appUrl}${path}`, {
      headers: { Authorization: `Bearer ${accessToken}` },
      cache: 'no-store',
      signal: AbortSignal.timeout(10_000),
    });
  } catch (err) {
    const isTimeout = err instanceof DOMException && err.name === 'TimeoutError';
    return { ok: false, status: isTimeout ? 504 : 502, body: null };
  }
  if (!res.ok) {
    const text = await res.text().catch(() => '');
    return { ok: false, status: res.status, body: text };
  }
  try {
    return { ok: true, status: res.status, body: await res.json() };
  } catch {
    return { ok: false, status: 502, body: 'Invalid JSON response' };
  }
}

/**
 * Fetches an agent type by slug + its runs in parallel from the BFF.
 * Returns status: 404 when the agent does not exist so the shell can render a
 * dedicated not-found state rather than a generic error.
 */
export async function fetchAgentDetailFromBFF(
  slug: string,
  accessToken: string,
): Promise<AgentDetailResult> {
  // First fetch agent by slug (the BFF route returns the matching agent type)
  const agentResult = await bffFetch(
    `/api/agents/types?slug=${encodeURIComponent(slug)}`,
    accessToken,
  );

  if (!agentResult.ok) {
    const msg = typeof agentResult.body === 'string' ? agentResult.body : 'Agent not found';
    console.error(`[fetchAgentDetailFromBFF] ${slug} fetch failed ${agentResult.status}: ${msg}`);
    return { ok: false, status: agentResult.status, message: msg };
  }

  // The BFF returns { data: AgentType[] }; pick first match by slug.
  const typesBody = agentResult.body as { data: AgentType[] };
  const found = (typesBody.data ?? []).find((a) => a.slug === slug);
  if (!found) {
    return { ok: false, status: 404, message: 'Agent not found' };
  }

  const agent = toAgentTypeRow(found);

  // Fetch runs for this agent type in parallel.
  const runsResult = await bffFetch(
    `/api/agents/types/${encodeURIComponent(found.id)}/runs`,
    accessToken,
  );

  let runs: RunRow[] = [];
  if (runsResult.ok) {
    const runsBody = runsResult.body as { data: Run[] };
    runs = (runsBody.data ?? []).map((r) => toRunRow(r, [agent]));
  }

  return { ok: true, agent, runs };
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/[slug]/_services/agent-detail-api.ts`:

```typescript
// ── Agent detail — client-side BFF calls ──────────────────────────────────────

import { ApiRoute } from '@orbit/lib/routing/api-routes';
import type { AgentType, Run, UpdateAgentTypeRequest } from '@orbit/shared/api';
import { toAgentTypeRow, toRunRow } from '../../_types';
import type { AgentTypeRow, RunRow } from '../../_types';

// ── deleteAgentType ───────────────────────────────────────────────────────────

export async function deleteAgentType(id: string): Promise<{ error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), { method: 'DELETE' });
  } catch {
    return { error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return { error: (body as { message?: string }).message ?? `Request failed (${res.status})` };
  }
  return { error: null };
}

// ── fetchAgentRuns ────────────────────────────────────────────────────────────

export async function fetchAgentRuns(
  agentTypeId: string,
  agentTypeRow: AgentTypeRow,
): Promise<{ data: RunRow[]; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.runs.listForType(agentTypeId), { cache: 'no-store' });
  } catch {
    return { data: [], error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      data: [],
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: Run[] };
  return { data: (body.data ?? []).map((r) => toRunRow(r, [agentTypeRow])), error: null };
}

// ── updateAgentType ───────────────────────────────────────────────────────────

export async function updateAgentType(
  id: string,
  payload: UpdateAgentTypeRequest,
): Promise<{ agentType: AgentTypeRow | null; error: string | null }> {
  let res: Response;
  try {
    res = await fetch(ApiRoute.agentTypes.detail(id), {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });
  } catch {
    return { agentType: null, error: 'Network error — could not reach the server.' };
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}) as { message?: string });
    return {
      agentType: null,
      error: (body as { message?: string }).message ?? `Request failed (${res.status})`,
    };
  }
  const body = (await res.json()) as { data: AgentType };
  return { agentType: toAgentTypeRow(body.data), error: null };
}
```

- [ ] 3. Create `src/app/(dashboard)/agents/[slug]/_hooks/use-agent-detail.ts`:

```typescript
'use client';

// ── useAgentDetail ────────────────────────────────────────────────────────────
//
// Owns all reactive state for the /agents/[slug] detail page.

import { useState, useCallback } from 'react';
import { fetchAgentRuns, deleteAgentType } from '../_services/agent-detail-api';
import { fetchStartRun } from '../../_services/agents-api';
import type { AgentTypeRow, RunRow } from '../../_types';

export type DetailStatus = 'idle' | 'loading' | 'success' | 'error' | 'not_found';

export interface AgentDetailStateShape {
  agent: AgentTypeRow | null;
  runs: RunRow[];
  status: DetailStatus;
  error: string | null;
  refresh: () => Promise<void>;
  deleteAgent: () => Promise<boolean>;
  startRun: (
    task: string,
    provisionSandbox: boolean,
  ) => Promise<{ runId: string | null; error: string | null }>;
}

export function useAgentDetail(
  initialAgent: AgentTypeRow | null,
  initialRuns: RunRow[],
  initialError: string | null,
  initialStatus: DetailStatus,
): AgentDetailStateShape {
  const [agent, setAgent] = useState<AgentTypeRow | null>(initialAgent);
  const [runs, setRuns] = useState<RunRow[]>(initialRuns);
  const [status, setStatus] = useState<DetailStatus>(initialStatus);
  const [error, setError] = useState<string | null>(initialError);

  const refresh = useCallback(async () => {
    if (!agent) return;
    setStatus('loading');
    setError(null);

    const result = await fetchAgentRuns(agent.id, agent);
    if (result.error) {
      setError(result.error);
      setStatus('error');
    } else {
      setRuns(result.data);
      setStatus('success');
    }
  }, [agent]);

  const deleteAgent = useCallback(async (): Promise<boolean> => {
    if (!agent) return false;
    const result = await deleteAgentType(agent.id);
    return result.error === null;
  }, [agent]);

  const startRun = useCallback(
    async (
      task: string,
      provisionSandbox: boolean,
    ): Promise<{ runId: string | null; error: string | null }> => {
      if (!agent) return { runId: null, error: 'No agent loaded.' };
      const result = await fetchStartRun(agent.id, { task, provision_sandbox: provisionSandbox });
      if (result.error) return { runId: null, error: result.error };
      return { runId: result.run?.id ?? null, error: null };
    },
    [agent],
  );

  return { agent, runs, status, error, refresh, deleteAgent, startRun };
}
```

- [ ] 4. Create `src/app/(dashboard)/agents/[slug]/_context/agent-detail-context.tsx`:

```typescript
'use client';

// ── AgentDetailContext ────────────────────────────────────────────────────────
//
// Provides AgentDetailStateShape to the entire /agents/[slug] tree.

import { createContext, useContext } from 'react';
import { useAgentDetail } from '../_hooks/use-agent-detail';
import type { AgentDetailStateShape, DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';

const AgentDetailContext = createContext<AgentDetailStateShape | null>(null);

interface AgentDetailProviderProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  initialError: string | null;
  initialStatus: DetailStatus;
  children: React.ReactNode;
}

export function AgentDetailProvider({
  initialAgent,
  initialRuns,
  initialError,
  initialStatus,
  children,
}: AgentDetailProviderProps) {
  const state = useAgentDetail(initialAgent, initialRuns, initialError, initialStatus);
  return <AgentDetailContext.Provider value={state}>{children}</AgentDetailContext.Provider>;
}

export function useAgentDetailContext(): AgentDetailStateShape {
  const ctx = useContext(AgentDetailContext);
  if (!ctx) {
    throw new Error('useAgentDetailContext must be used inside <AgentDetailProvider>');
  }
  return ctx;
}
```

- [ ] 5. Create `src/app/(dashboard)/agents/[slug]/page.tsx`:

```typescript
// RSC — fetches agent type + runs by slug server-side.

import { getSession, isSessionValid } from '@orbit/server/session';
import { fetchAgentDetailFromBFF } from './_services/agent-detail-server';
import { AgentDetailShell } from './_components/agent-detail-shell';

interface AgentDetailPageProps {
  params: Promise<{ slug: string }>;
}

export default async function AgentDetailPage({ params }: AgentDetailPageProps) {
  const { slug } = await params;
  const session = await getSession();
  const result = isSessionValid(session)
    ? await fetchAgentDetailFromBFF(slug, session.accessToken)
    : null;

  const initialAgent = result?.ok ? result.agent : null;
  const initialRuns = result?.ok ? result.runs : [];
  const initialError = result && !result.ok ? result.message : null;
  const initialStatus =
    result === null
      ? ('idle' as const)
      : result.ok
        ? ('success' as const)
        : result.status === 404
          ? ('not_found' as const)
          : ('error' as const);

  return (
    <AgentDetailShell
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialError={initialError}
      initialStatus={initialStatus}
    />
  );
}
```

- [ ] 6. Create `src/app/(dashboard)/agents/[slug]/_components/agent-detail-shell.tsx`:

```typescript
'use client';

/** @tier 3 — agents/[slug]-scoped. Client boundary for the agent detail page. */

import { AgentDetailProvider } from '../_context/agent-detail-context';
import { AgentDetailPage } from './agent-detail-page';
import type { DetailStatus } from '../_hooks/use-agent-detail';
import type { AgentTypeRow, RunRow } from '../../_types';

interface AgentDetailShellProps {
  initialAgent: AgentTypeRow | null;
  initialRuns: RunRow[];
  initialError: string | null;
  initialStatus: DetailStatus;
}

export function AgentDetailShell({
  initialAgent,
  initialRuns,
  initialError,
  initialStatus,
}: AgentDetailShellProps) {
  return (
    <AgentDetailProvider
      initialAgent={initialAgent}
      initialRuns={initialRuns}
      initialError={initialError}
      initialStatus={initialStatus}
    >
      <AgentDetailPage />
    </AgentDetailProvider>
  );
}
```

---

## Task 7 — Agent detail page: tabs

### Files

- `src/app/(dashboard)/agents/[slug]/_components/agent-detail-page.tsx`
- `src/app/(dashboard)/agents/[slug]/_components/agent-overview-tab.tsx`
- `src/app/(dashboard)/agents/[slug]/_components/agent-runs-tab.tsx`
- `src/app/(dashboard)/agents/[slug]/_components/agent-settings-tab.tsx`

### Steps

- [ ] 1. Create `src/app/(dashboard)/agents/[slug]/_components/agent-detail-page.tsx`:

```typescript
'use client';

/** @tier 3 — agents/[slug]-scoped. Detail page with header + tabs. */

import { useState, useCallback } from 'react';
import { useRouter } from 'next/navigation';
import {
  Badge,
  Button,
  Callout,
  Heading,
  Icon,
  Stack,
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
  Text,
} from '@orbit/terra';
import Link from 'next/link';
import { ConfirmDialog, EmptyState, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { AgentOverviewTab } from './agent-overview-tab';
import { AgentRunsTab } from './agent-runs-tab';
import { AgentSettingsTab } from './agent-settings-tab';
import { StartRunModal } from '../../../_components/start-run-modal';

export function AgentDetailPage() {
  const { agent, status, error, deleteAgent, startRun } = useAgentDetailContext();
  const router = useRouter();

  const [startRunOpen, setStartRunOpen] = useState(false);
  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    setDeleteError(null);
    const ok = await deleteAgent();
    setDeleting(false);
    if (!ok) {
      setDeleteError('Failed to delete agent. Please try again.');
      return;
    }
    setDeleteOpen(false);
    router.push(APP_ROUTES.agents.list);
  }, [deleteAgent, router]);

  // ── Not found ──────────────────────────────────────────────────────────────

  if (status === 'not_found') {
    return (
      <Stack gap="comfortable">
        <Button variant="ghost" size="sm" asChild>
          <Link href={APP_ROUTES.agents.list}>
            <Icon name="arrow-left" size="xs" />
            Agents
          </Link>
        </Button>
        <EmptyState
          icon="search-x"
          title="Agent not found"
          description="This agent definition does not exist or may have been deleted."
          action={
            <Button asChild>
              <Link href={APP_ROUTES.agents.list}>Back to Agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  // ── Error fallback ─────────────────────────────────────────────────────────

  if (status === 'error' && !agent) {
    return (
      <Stack gap="comfortable">
        <Button variant="ghost" size="sm" asChild>
          <Link href={APP_ROUTES.agents.list}>
            <Icon name="arrow-left" size="xs" />
            Agents
          </Link>
        </Button>
        <EmptyState
          icon="alert-triangle"
          title="Failed to load agent"
          description={error ?? 'An unexpected error occurred.'}
          action={
            <Button asChild>
              <Link href={APP_ROUTES.agents.list}>Back to Agents</Link>
            </Button>
          }
        />
      </Stack>
    );
  }

  if (!agent) return null;

  const statusMap: Record<string, 'healthy' | 'degraded' | 'error'> = {
    active: 'healthy',
    provisioning: 'degraded',
    deleting: 'error',
  };
  const labelMap: Partial<Record<string, string>> = {
    provisioning: 'Provisioning',
    deleting: 'Deleting',
  };
  const frameworkVariantMap: Record<string, 'default' | 'outline' | 'secondary'> = {
    'claude-code': 'default',
    codex: 'outline',
    opencode: 'secondary',
  };

  return (
    <Stack gap="comfortable">
      {/* Breadcrumb */}
      <Button variant="ghost" size="sm" asChild>
        <Link href={APP_ROUTES.agents.list}>
          <Icon name="arrow-left" size="xs" />
          Agents
        </Link>
      </Button>

      {/* Provisioning banner */}
      {agent.state === 'provisioning' && (
        <Callout variant="info">
          Agent is being provisioned, this usually takes 30–60 seconds.
        </Callout>
      )}

      {/* Page header */}
      <Stack direction="horizontal" align="center" justify="between" wrap gap="comfortable">
        <Stack gap="snug">
          <Stack direction="horizontal" align="center" gap="snug" wrap>
            <Heading variant="title">{agent.name}</Heading>
            <Badge
              variant={frameworkVariantMap[agent.frameworkKey] ?? 'outline'}
              size="sm"
            >
              {agent.framework}
            </Badge>
            <StatusBadge
              status={statusMap[agent.state] ?? 'unknown'}
              label={labelMap[agent.state]}
            />
          </Stack>
          <Text mono size="supporting" color="muted">
            {agent.slug}
          </Text>
        </Stack>
        <Stack direction="horizontal" gap="snug">
          <Button
            variant="default"
            size="sm"
            onClick={() => setStartRunOpen(true)}
            disabled={agent.state !== 'active'}
          >
            <Icon name="play" size="xs" />
            Start Run
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setDeleteOpen(true)}
          >
            <Icon name="trash-2" size="xs" />
            Delete
          </Button>
        </Stack>
      </Stack>

      {/* Tabs */}
      <Tabs defaultValue="overview">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="runs">Runs</TabsTrigger>
          <TabsTrigger value="settings">Settings</TabsTrigger>
        </TabsList>
        <TabsContent value="overview">
          <AgentOverviewTab />
        </TabsContent>
        <TabsContent value="runs">
          <AgentRunsTab onStartRun={() => setStartRunOpen(true)} />
        </TabsContent>
        <TabsContent value="settings">
          <AgentSettingsTab />
        </TabsContent>
      </Tabs>

      {/* Start run modal */}
      <StartRunModal
        open={startRunOpen}
        agentType={agent}
        onClose={() => setStartRunOpen(false)}
        onStarted={(runId) => {
          setStartRunOpen(false);
          router.push(APP_ROUTES.agents.run(agent.slug, runId));
        }}
      />

      {/* Delete confirm */}
      <ConfirmDialog
        open={deleteOpen}
        onClose={() => {
          setDeleteOpen(false);
          setDeleteError(null);
        }}
        title="Delete agent"
        description={`Permanently delete "${agent.name}"? All associated runs will be terminated. This cannot be undone.`}
        confirmLabel="Delete agent"
        onConfirm={() => void handleDelete()}
        confirming={deleting}
        error={deleteError}
      />
    </Stack>
  );
}
```

- [ ] 2. Create `src/app/(dashboard)/agents/[slug]/_components/agent-overview-tab.tsx`:

```typescript
'use client';

/** @tier 3 — agents/[slug]-scoped. Overview tab: description, system prompt, scopes. */

import { useState } from 'react';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Code,
  Separator,
  Stack,
  Text,
} from '@orbit/terra';
import { ScopeChip } from '@orbit/components';
import { fmtDate } from '@orbit/utils';
import { useAgentDetailContext } from '../_context/agent-detail-context';

export function AgentOverviewTab() {
  const { agent } = useAgentDetailContext();
  const [promptExpanded, setPromptExpanded] = useState(false);

  if (!agent) return null;

  return (
    <Stack gap="comfortable" paddingBlock="comfortable">
      {/* Description */}
      {agent.description ? (
        <Text size="supporting" color="muted">
          {agent.description}
        </Text>
      ) : (
        <Text size="supporting" color="subtle">
          No description provided.
        </Text>
      )}

      {/* System prompt */}
      <Card>
        <CardHeader compact>
          <CardTitle>System Prompt</CardTitle>
        </CardHeader>
        <CardContent>
          <Stack gap="snug">
            <Code
              className={
                !promptExpanded
                  ? 'line-clamp-4 overflow-hidden whitespace-pre-wrap break-words text-sm'
                  : 'whitespace-pre-wrap break-words text-sm'
              }
            >
              {agent.systemPrompt}
            </Code>
            <Button
              variant="ghost"
              size="sm"
              onClick={() => setPromptExpanded((prev) => !prev)}
            >
              {promptExpanded ? 'Show less' : 'Show full prompt'}
            </Button>
          </Stack>
        </CardContent>
      </Card>

      {/* Tool scopes */}
      <Stack gap="snug">
        <Text size="supporting" weight="emphasis">
          Tool Scopes
        </Text>
        {agent.toolScopes.length > 0 ? (
          <Stack direction="horizontal" gap="snug" wrap>
            {agent.toolScopes.map((scope) => (
              <ScopeChip key={scope} scope={scope} />
            ))}
          </Stack>
        ) : (
          <Text size="supporting" color="muted">
            No tool scopes configured.
          </Text>
        )}
      </Stack>

      <Separator />

      {/* Grant scopes */}
      <Stack gap="snug">
        <Text size="supporting" weight="emphasis">
          Grant Scopes
        </Text>
        {agent.grantScopes.length > 0 ? (
          <Stack direction="horizontal" gap="snug" wrap>
            {agent.grantScopes.map((scope) => (
              <ScopeChip key={scope} scope={scope} />
            ))}
          </Stack>
        ) : (
          <Text size="supporting" color="muted">
            No grant scopes configured.
          </Text>
        )}
      </Stack>

      <Separator />

      {/* Metadata */}
      <Stack gap="tight">
        <Stack direction="horizontal" align="center" justify="between">
          <Text size="supporting" color="muted">
            Created
          </Text>
          <Text size="supporting">{fmtDate(agent.createdAt)}</Text>
        </Stack>
        <Stack direction="horizontal" align="center" justify="between">
          <Text size="supporting" color="muted">
            Last updated
          </Text>
          <Text size="supporting">{fmtDate(agent.updatedAt)}</Text>
        </Stack>
        <Stack direction="horizontal" align="center" justify="between">
          <Text size="supporting" color="muted">
            Max concurrent runs / pod
          </Text>
          <Text mono size="supporting">
            {agent.maxConcurrentRunsPerPod}
          </Text>
        </Stack>
      </Stack>
    </Stack>
  );
}
```

- [ ] 3. Create `src/app/(dashboard)/agents/[slug]/_components/agent-runs-tab.tsx`:

```typescript
'use client';

/** @tier 3 — agents/[slug]-scoped. Runs table tab for the agent detail page. */

import { useMemo } from 'react';
import { useRouter } from 'next/navigation';
import {
  Button,
  DataTable,
  Icon,
  Stack,
  Text,
} from '@orbit/terra';
import type { DataTableColumn } from '@orbit/terra';
import { EmptyState, StatusBadge } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { useAgentDetailContext } from '../_context/agent-detail-context';
import { formatDuration, formatRelativeTime } from '../../_types';
import type { RunRow } from '../../_types';

// ── Status helpers ────────────────────────────────────────────────────────────

function runStatusBadge(row: RunRow) {
  const statusMap: Record<string, 'idle' | 'degraded' | 'healthy' | 'error'> = {
    pending: 'idle',
    starting: 'degraded',
    active: 'healthy',
    completing: 'degraded',
    completed: 'idle',
    failed: 'error',
  };
  const labelMap: Partial<Record<string, string>> = {
    completed: 'Completed',
  };
  return (
    <StatusBadge
      status={statusMap[row.state] ?? 'unknown'}
      label={labelMap[row.state]}
    />
  );
}

// ── Component ─────────────────────────────────────────────────────────────────

interface AgentRunsTabProps {
  onStartRun: () => void;
}

export function AgentRunsTab({ onStartRun }: AgentRunsTabProps) {
  const { agent, runs, status, error, refresh } = useAgentDetailContext();
  const router = useRouter();

  const columns = useMemo<DataTableColumn<RunRow>[]>(
    () => [
      {
        id: 'id',
        header: 'Run ID',
        locked: true,
        cell: (row) => (
          <Text mono size="supporting">
            {row.id.slice(0, 8)}
          </Text>
        ),
      },
      {
        id: 'state',
        header: 'Status',
        cell: (row) => runStatusBadge(row),
      },
      {
        id: 'started',
        header: 'Started',
        sortable: true,
        sortAccessor: (row) => row.startedAt ?? row.createdAt,
        cell: (row) => (
          <Text size="supporting" color="muted">
            {row.startedAt ? formatRelativeTime(row.startedAt) : '—'}
          </Text>
        ),
      },
      {
        id: 'duration',
        header: 'Duration',
        cell: (row) => (
          <Text mono size="supporting">
            {formatDuration(row.durationMs)}
          </Text>
        ),
      },
      {
        id: 'actions',
        header: '',
        locked: true,
        width: '5rem',
        cell: (row) => (
          <Button
            variant="ghost"
            size="sm"
            onClick={(e) => {
              e.stopPropagation();
              if (agent) {
                router.push(APP_ROUTES.agents.run(agent.slug, row.id));
              }
            }}
          >
            Open
          </Button>
        ),
      },
    ],
    [agent, router],
  );

  return (
    <Stack gap="comfortable" paddingBlock="comfortable">
      <Stack direction="horizontal" justify="end">
        <Button size="sm" onClick={onStartRun} disabled={agent?.state !== 'active'}>
          <Icon name="plus" size="xs" />
          Start New Run
        </Button>
      </Stack>

      <DataTable
        data={runs}
        columns={columns}
        getRowKey={(row) => row.id}
        noun="runs"
        searchPlaceholder="Search runs…"
        defaultSortId="started"
        defaultSortDir="desc"
        error={
          status === 'error' ? (
            <Stack align="center" paddingBlock="comfortable">
              <Text size="supporting" color="muted">
                {error ?? 'Failed to load runs.'}
              </Text>
              <Button variant="outline" size="sm" onClick={() => void refresh()}>
                Try again
              </Button>
            </Stack>
          ) : undefined
        }
        emptyMessage={
          <EmptyState
            icon="play-circle"
            title="No runs yet"
            description="Start your first run to execute tasks with this agent."
            action={
              <Button onClick={onStartRun} disabled={agent?.state !== 'active'}>
                Start your first run
              </Button>
            }
          />
        }
        filteredEmptyMessage="No runs match your search."
        onRowClick={(row) => {
          if (agent) {
            router.push(APP_ROUTES.agents.run(agent.slug, row.id));
          }
        }}
      />
    </Stack>
  );
}
```

- [ ] 4. Create `src/app/(dashboard)/agents/[slug]/_components/agent-settings-tab.tsx`:

```typescript
'use client';

/** @tier 3 — agents/[slug]-scoped. Settings / edit form tab for agent detail. */

import { useCallback, useState } from 'react';
import { useRouter } from 'next/navigation';
import {
  Alert,
  Button,
  Card,
  CardContent,
  Form,
  FormField,
  Heading,
  Icon,
  Input,
  NativeSelect,
  Separator,
  Stack,
  Textarea,
} from '@orbit/terra';
import { ConfirmDialog } from '@orbit/components';
import { APP_ROUTES } from '@orbit/lib/routing';
import { updateAgentType } from '../_services/agent-detail-api';
import { useAgentDetailContext } from '../_context/agent-detail-context';

const FRAMEWORK_OPTIONS = [
  { value: 'claude-code', label: 'Claude Code' },
  { value: 'codex', label: 'Codex' },
  { value: 'opencode', label: 'OpenCode' },
] as const;

export function AgentSettingsTab() {
  const { agent, deleteAgent } = useAgentDetailContext();
  const router = useRouter();

  const [name, setName] = useState(agent?.name ?? '');
  const [description, setDescription] = useState(agent?.description ?? '');
  const [framework, setFramework] = useState(agent?.frameworkKey ?? 'claude-code');
  const [systemPrompt, setSystemPrompt] = useState(agent?.systemPrompt ?? '');
  const [maxConcurrentRuns, setMaxConcurrentRuns] = useState(
    String(agent?.maxConcurrentRunsPerPod ?? 10),
  );

  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveSuccess, setSaveSuccess] = useState(false);

  const [deleteOpen, setDeleteOpen] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const handleSave = useCallback(
    async (e: React.FormEvent) => {
      e.preventDefault();
      if (!agent) return;
      setSaving(true);
      setSaveError(null);
      setSaveSuccess(false);

      const result = await updateAgentType(agent.id, {
        name: name.trim(),
        description: description.trim(),
        framework: framework as 'claude-code' | 'codex' | 'opencode',
        system_prompt: systemPrompt.trim(),
        max_concurrent_runs_per_pod: parseInt(maxConcurrentRuns, 10),
      });

      setSaving(false);
      if (result.error) {
        setSaveError(result.error);
      } else {
        setSaveSuccess(true);
      }
    },
    [agent, name, description, framework, systemPrompt, maxConcurrentRuns],
  );

  const handleDelete = useCallback(async () => {
    setDeleting(true);
    setDeleteError(null);
    const ok = await deleteAgent();
    setDeleting(false);
    if (!ok) {
      setDeleteError('Failed to delete agent. Please try again.');
      return;
    }
    setDeleteOpen(false);
    router.push(APP_ROUTES.agents.list);
  }, [deleteAgent, router]);

  if (!agent) return null;

  return (
    <Stack gap="comfortable" paddingBlock="comfortable">
      <Form onSubmit={(e) => void handleSave(e)} noValidate gap="comfortable">
        <Card>
          <CardContent>
            <Stack gap="comfortable">
              <Heading variant="section">Identity</Heading>
              <FormField id="settings-name" label="Name" required>
                <Input
                  id="settings-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </FormField>
              <FormField id="settings-slug" label="Slug" hint="Slug cannot be changed after creation.">
                <Input id="settings-slug" value={agent.slug} disabled mono />
              </FormField>
              <FormField id="settings-description" label="Description">
                <Textarea
                  id="settings-description"
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  rows={3}
                />
              </FormField>

              <Separator />

              <Heading variant="section">Behaviour</Heading>
              <FormField id="settings-framework" label="Framework" required>
                <NativeSelect
                  id="settings-framework"
                  value={framework}
                  onChange={(e) => setFramework(e.target.value)}
                >
                  {FRAMEWORK_OPTIONS.map((opt) => (
                    <option key={opt.value} value={opt.value}>
                      {opt.label}
                    </option>
                  ))}
                </NativeSelect>
              </FormField>
              <FormField id="settings-system-prompt" label="System Prompt" required>
                <Textarea
                  id="settings-system-prompt"
                  value={systemPrompt}
                  onChange={(e) => setSystemPrompt(e.target.value)}
                  rows={8}
                  mono
                />
              </FormField>

              <Separator />

              <Heading variant="section">Capacity</Heading>
              <FormField
                id="settings-max-concurrent"
                label="Max concurrent runs per pod"
                hint="1–50"
              >
                <Input
                  id="settings-max-concurrent"
                  type="number"
                  min={1}
                  max={50}
                  value={maxConcurrentRuns}
                  onChange={(e) => setMaxConcurrentRuns(e.target.value)}
                />
              </FormField>
            </Stack>
          </CardContent>
        </Card>

        {saveError && <Alert variant="error">{saveError}</Alert>}
        {saveSuccess && <Alert variant="success">Changes saved.</Alert>}

        <Stack direction="horizontal" justify="end">
          <Button type="submit" variant="default" size="sm" disabled={saving}>
            {saving ? 'Saving…' : 'Save Changes'}
          </Button>
        </Stack>
      </Form>

      {/* Danger zone */}
      <Separator />

      <Stack gap="snug">
        <Heading variant="section">Danger Zone</Heading>
        <Card>
          <CardContent>
            <Stack direction="horizontal" align="center" justify="between" wrap gap="comfortable">
              <Stack gap="tight">
                <Text size="supporting" weight="emphasis">
                  Delete this agent
                </Text>
                <Text size="supporting" color="muted">
                  Permanently delete this agent definition and terminate all active runs. This
                  cannot be undone.
                </Text>
              </Stack>
              <Button
                variant="destructive"
                size="sm"
                onClick={() => setDeleteOpen(true)}
              >
                <Icon name="trash-2" size="xs" />
                Delete Agent
              </Button>
            </Stack>
          </CardContent>
        </Card>
      </Stack>

      <ConfirmDialog
        open={deleteOpen}
        onClose={() => {
          setDeleteOpen(false);
          setDeleteError(null);
        }}
        title="Delete agent"
        description={`Permanently delete "${agent.name}"? All associated runs will be terminated. This cannot be undone.`}
        confirmLabel="Delete agent"
        onConfirm={() => void handleDelete()}
        confirming={deleting}
        error={deleteError}
      />
    </Stack>
  );
}
```

---

## Verification checkpoints

After completing each task group, run:

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit with zero errors before proceeding. Fix all type errors and lint violations before moving to the next task.

### Checkpoint 1 — After Task 1 (types + services + hook + context)

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Common issues to check:

- `AgentType`, `Run`, `CreateAgentTypeRequest`, `StartRunRequest`, `UpdateAgentTypeRequest` must be exported from `src/shared/api.ts` (added in Plan 1). If they are missing, add stub type exports before the checkpoint.
- `ApiRoute.agentTypes.*`, `ApiRoute.runs.*` must be present in `src/lib/routing/api-routes.ts` (added in Plan 1).
- `APP_ROUTES.agents.list`, `APP_ROUTES.agents.new`, `APP_ROUTES.agents.detail(slug)`, `APP_ROUTES.agents.run(slug, runId)` must be present in `src/lib/routing/routes.ts` (added in Plan 1).

### Checkpoint 2 — After Tasks 2–4 (list page + start run modal)

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Common issues to check:

- `DataTable`'s `emptyMessage` prop accepts `React.ReactNode` — if it only accepts `string`, pass `filteredEmptyMessage` for the filtered case and handle the primary empty state via `data.length === 0` conditional rendering.
- `APP_ROUTES.agents.list` — the spec above uses this as a property. Ensure `routes.ts` exposes `agents` as an object with `list`, `new`, `detail`, and `run` keys (Plan 1 work). If `APP_ROUTES.agents` is a flat string, update the import accordingly.

### Checkpoint 3 — After Tasks 5–7 (new form + detail page)

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Common issues to check:

- `Alert` variant `"success"` — confirm this variant exists in the Terra `Alert` component. If not, use `variant="info"` for the save-success state.
- `Input` `mono` prop — confirm this prop exists. If not, use `className="font-mono"` as a temporary workaround (check Terra source; avoid if it violates lint rules).
- All `@tier 3` JSDoc comments must be present at the top of each component file (before `'use client'` is not valid — the `@tier` comment goes after the `'use client'` directive, as the JSDoc block above the component function or as a file-level comment on the second line).

---

## Commit checkpoints

Commit at these logical boundaries only when explicitly asked:

1. After Task 1 passes `pnpm tsc --noEmit && pnpm lint`: `feat(agents): add data layer — types, services, hook, and context`
2. After Tasks 2–4 pass: `feat(agents): agents list page and start-run modal`
3. After Task 5 passes: `feat(agents): create agent type form`
4. After Tasks 6–7 pass: `feat(agents): agent detail page with overview, runs, and settings tabs`

---

## Notes and known stubs

1. **`APP_ROUTES.agents`** — Plan 1 extends `routes.ts` so that `APP_ROUTES.agents` is an object:

   ```typescript
   agents: {
     list: '/agents',
     new: '/agents/new',
     detail: (slug: string) => `/agents/${slug}`,
     run: (slug: string, runId: string) => `/agents/${slug}/runs/${runId}`,
   }
   ```

   All code in this plan references `APP_ROUTES.agents.list`, `APP_ROUTES.agents.new`, etc. If Plan 1 has not been applied yet, apply those routing changes first.

2. **`ApiRoute.agentTypes` and `ApiRoute.runs`** — Plan 1 adds these to `api-routes.ts`. This plan's services import them. If absent, add them before running verification.

3. **`StartRunModal` import in `agent-detail-page.tsx`** — the modal is created in Task 4 at `src/app/(dashboard)/agents/_components/start-run-modal.tsx`. The detail page imports it via a relative path `../../../_components/start-run-modal`. If the directory depth changes, update the import.

4. **`UpdateAgentTypeRequest` in `src/shared/api.ts`** — the settings tab calls `updateAgentType` with an `UpdateAgentTypeRequest`. Plan 1 should add this type. If missing, add it inline:

   ```typescript
   export interface UpdateAgentTypeRequest {
     name?: string;
     description?: string;
     framework?: 'claude-code' | 'codex' | 'opencode';
     system_prompt?: string;
     max_concurrent_runs_per_pod?: number;
   }
   ```

5. **`Alert variant="success"`** — verify this variant exists in the Terra `Alert` component before using it. Look at `src/packages/terra/src/components/alert/alert.tsx`. If the `success` variant is absent, use `variant="info"` with appropriate text.

6. **Mock BFF returns array on `GET /api/agents/types`** — the detail server service fetches all agent types and filters by slug locally. This is intentional for the mock stage (Plan 2 does not add a `GET /api/agents/types/:slug` route). When the real backend is wired, replace with a direct `GET /api/agents/types/{slug}` call.
