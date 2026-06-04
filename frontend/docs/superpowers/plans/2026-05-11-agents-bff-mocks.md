# Plan 2 of 4 — Agents UI: Mock BFF API Routes

## Goal

Implement all mock BFF API routes for the Agents UI. Routes return hardcoded realistic data so the frontend can be built and iterated on without a live backend. No network calls to the backend are made. All routes are authenticated via the existing `withAuth` wrapper.

## Architecture

```
src/app/api/agents/
├── _mock/
│   └── data.ts                         # Shared mock data — MOCK_AGENT_TYPES, MOCK_RUNS
├── types/
│   ├── route.ts                        # GET /api/agents/types, POST /api/agents/types
│   └── [id]/
│       └── route.ts                    # GET /api/agents/types/:id, DELETE /api/agents/types/:id
├── runs/
│   ├── route.ts                        # GET /api/agents/runs (all runs, tenant scope)
│   └── [runId]/
│       ├── route.ts                    # GET /api/agents/runs/:runId, DELETE
│       └── messages/
│           └── route.ts               # POST /api/agents/runs/:runId/messages
└── types/[typeId]/runs/
    └── route.ts                        # GET /api/agents/types/:typeId/runs, POST (start run)
```

Shared types (`AgentType`, `Run`, `CreateAgentTypeRequest`, `StartRunRequest`) are added to `src/shared/api.ts` — the existing file that mirrors Go backend JSON shapes.

## Tech Stack

- Next.js 16 App Router Route Handlers
- `withAuth` from `@orbit/server/route-helpers` — auth guard, identical to all other BFF routes
- `import 'server-only'` on every route file
- TypeScript strict mode — no `any`, all params typed

## Route Map

| Method | Path                               | Handler file                     |
| ------ | ---------------------------------- | -------------------------------- |
| GET    | `/api/agents/types`                | `types/route.ts`                 |
| POST   | `/api/agents/types`                | `types/route.ts`                 |
| GET    | `/api/agents/types/:id`            | `types/[id]/route.ts`            |
| DELETE | `/api/agents/types/:id`            | `types/[id]/route.ts`            |
| GET    | `/api/agents/types/:typeId/runs`   | `types/[typeId]/runs/route.ts`   |
| POST   | `/api/agents/types/:typeId/runs`   | `types/[typeId]/runs/route.ts`   |
| GET    | `/api/agents/runs`                 | `runs/route.ts`                  |
| GET    | `/api/agents/runs/:runId`          | `runs/[runId]/route.ts`          |
| DELETE | `/api/agents/runs/:runId`          | `runs/[runId]/route.ts`          |
| POST   | `/api/agents/runs/:runId/messages` | `runs/[runId]/messages/route.ts` |

---

## Task 1 — Add shared types to `src/shared/api.ts`

### Files

- `src/shared/api.ts` — append agent/run types and request types

### Steps

- [ ] 1. Read `src/shared/api.ts` to confirm the current end of file (line count, last export).

- [ ] 2. Append the following block to `src/shared/api.ts` after the existing `ListPATsResponse` interface:

```typescript
// ── Agents ────────────────────────────────────────────────────────────────────

/**
 * The runtime framework an agent type uses to execute tasks.
 * Mirrors Go: agents.Framework
 */
export type AgentFramework = 'claude-code' | 'codex' | 'opencode';

/**
 * Lifecycle state of an agent type.
 * Mirrors Go: agents.AgentTypeState
 */
export type AgentTypeState = 'provisioning' | 'active' | 'deleting';

/**
 * Lifecycle state of an individual agent run.
 * Mirrors Go: agents.RunState
 */
export type RunState = 'pending' | 'starting' | 'active' | 'completing' | 'completed' | 'failed';

/**
 * An agent type — the template from which runs are spawned.
 * Mirrors Go: agents.AgentType
 */
export interface AgentType {
  id: string;
  tenant_id: string;
  name: string;
  /** Unique per tenant, URL-safe identifier. */
  slug: string;
  description: string;
  system_prompt: string;
  framework: AgentFramework;
  /** Tool capability scopes, e.g. ["shell", "filesystem"]. */
  tool_scopes: string[];
  /** Inference/resource grant scopes, e.g. ["inference:invoke"]. */
  grant_scopes: string[];
  max_concurrent_runs_per_pod: number;
  state: AgentTypeState;
  created_at: string;
  updated_at: string;
}

/**
 * A single agent run instance.
 * Mirrors Go: agents.Run
 */
export interface Run {
  id: string;
  tenant_id: string;
  agent_type_id: string;
  state: RunState;
  /** StatefulSet pod ordinal. -1 = not yet assigned. */
  pod_ordinal: number;
  /** Principal ID of the user who created this run. */
  created_by: string;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

/** Request body for POST /api/agents/types. */
export interface CreateAgentTypeRequest {
  name: string;
  slug: string;
  description?: string;
  system_prompt?: string;
  framework?: AgentFramework;
  tool_scopes?: string[];
  grant_scopes?: string[];
  max_concurrent_runs_per_pod?: number;
}

/** Request body for POST /api/agents/types/:typeId/runs. */
export interface StartRunRequest {
  /** Optional initial message to send once the run is active. */
  initial_message?: string;
}
```

---

## Task 2 — Create shared mock data module

### Files

- `src/app/api/agents/_mock/data.ts` — new file

### Steps

- [ ] 1. Create the directory `src/app/api/agents/_mock/` (the leading underscore keeps Next.js from treating it as a route segment).

- [ ] 2. Create `src/app/api/agents/_mock/data.ts` with the following content:

```typescript
import type { AgentType, Run } from '@orbit/shared/api';

const now = Date.now();

export const MOCK_AGENT_TYPES: AgentType[] = [
  {
    id: 'at_01jwk4m2n3p5q7r8s9t0',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    name: 'Code Review Agent',
    slug: 'code-review-agent',
    description:
      'Reviews pull requests, identifies issues, and suggests improvements across TypeScript, Go, and Python codebases.',
    system_prompt:
      'You are an expert code reviewer. Analyse the provided code for bugs, security issues, performance problems, and style violations. Be specific and constructive.',
    framework: 'claude-code',
    tool_scopes: ['shell', 'filesystem'],
    grant_scopes: ['inference:invoke'],
    max_concurrent_runs_per_pod: 10,
    state: 'active',
    created_at: new Date(now - 7 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 2 * 24 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: 'at_01jwk4m2n3p5q7r8s9t1',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    name: 'Data Pipeline Fixer',
    slug: 'data-pipeline-fixer',
    description:
      'Diagnoses and repairs broken ETL pipelines, validates schemas, and fixes data quality issues.',
    system_prompt:
      'You are a data engineering specialist. Diagnose pipeline failures, fix broken transforms, and ensure data quality at every stage.',
    framework: 'claude-code',
    tool_scopes: ['shell', 'filesystem', 'network'],
    grant_scopes: ['inference:invoke', 'storage:read'],
    max_concurrent_runs_per_pod: 5,
    state: 'active',
    created_at: new Date(now - 14 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 1 * 24 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: 'at_01jwk4m2n3p5q7r8s9t2',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    name: 'Security Auditor',
    slug: 'security-auditor',
    description:
      'Scans codebases for vulnerabilities, checks dependency CVEs, and validates security configurations.',
    system_prompt:
      'You are a security engineer. Scan for vulnerabilities, misconfigurations, and dependency risks. Report findings with CVSS scores and remediation steps.',
    framework: 'codex',
    tool_scopes: ['filesystem'],
    grant_scopes: ['inference:invoke'],
    max_concurrent_runs_per_pod: 3,
    state: 'provisioning',
    created_at: new Date(now - 10 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 10 * 60 * 1000).toISOString(),
  },
  {
    id: 'at_01jwk4m2n3p5q7r8s9t3',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    name: 'Docs Writer',
    slug: 'docs-writer',
    description:
      'Generates and updates technical documentation from source code, comments, and existing docs.',
    system_prompt:
      'You are a technical writer. Generate clear, accurate documentation from code and context. Follow existing style guides.',
    framework: 'claude-code',
    tool_scopes: ['filesystem'],
    grant_scopes: ['inference:invoke'],
    max_concurrent_runs_per_pod: 10,
    state: 'active',
    created_at: new Date(now - 30 * 24 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 5 * 24 * 60 * 60 * 1000).toISOString(),
  },
];

export const MOCK_RUNS: Run[] = [
  {
    id: 'run_01jwk5a2b3c4d5e6f7g8',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t0',
    state: 'active',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 5 * 60 * 1000).toISOString(),
    completed_at: null,
    created_at: new Date(now - 5 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 30 * 1000).toISOString(),
  },
  {
    id: 'run_01jwk5a2b3c4d5e6f7g9',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t0',
    state: 'active',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 12 * 60 * 1000).toISOString(),
    completed_at: null,
    created_at: new Date(now - 12 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 2 * 60 * 1000).toISOString(),
  },
  {
    id: 'run_01jwk5a2b3c4d5e6f7h0',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t1',
    state: 'active',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 8 * 60 * 1000).toISOString(),
    completed_at: null,
    created_at: new Date(now - 8 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 60 * 1000).toISOString(),
  },
  {
    id: 'run_01jwk5a2b3c4d5e6f7h1',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t0',
    state: 'completed',
    pod_ordinal: 1,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 2 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(now - 90 * 60 * 1000).toISOString(),
    created_at: new Date(now - 2 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 90 * 60 * 1000).toISOString(),
  },
  {
    id: 'run_01jwk5a2b3c4d5e6f7h2',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t0',
    state: 'failed',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 4 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(now - 3.5 * 60 * 60 * 1000).toISOString(),
    created_at: new Date(now - 4 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 3.5 * 60 * 60 * 1000).toISOString(),
  },
  {
    id: 'run_01jwk5a2b3c4d5e6f7h3',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t3',
    state: 'completed',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 6 * 60 * 60 * 1000).toISOString(),
    completed_at: new Date(now - 5.5 * 60 * 60 * 1000).toISOString(),
    created_at: new Date(now - 6 * 60 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 5.5 * 60 * 60 * 1000).toISOString(),
  },
  // Canonical demo run used by the run chat page mock
  {
    id: 'run_demo_chat_01',
    tenant_id: 'ten_01jwk4m2n3p5q7r8s9',
    agent_type_id: 'at_01jwk4m2n3p5q7r8s9t0',
    state: 'active',
    pod_ordinal: 0,
    created_by: 'user_01jwk4m2n3p5q7r8',
    started_at: new Date(now - 3 * 60 * 1000).toISOString(),
    completed_at: null,
    created_at: new Date(now - 3 * 60 * 1000).toISOString(),
    updated_at: new Date(now - 10 * 1000).toISOString(),
  },
];
```

---

## Task 3 — Agent types BFF routes

### Files

- `src/app/api/agents/types/route.ts` — new file
- `src/app/api/agents/types/[id]/route.ts` — new file

### Steps

- [ ] 1. Create `src/app/api/agents/types/route.ts`:

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_AGENT_TYPES } from '../_mock/data';
import type { CreateAgentTypeRequest } from '@orbit/shared/api';

export const GET = withAuth(async (_req: NextRequest, _session) => {
  return NextResponse.json({ data: MOCK_AGENT_TYPES });
});

export const POST = withAuth(async (req: NextRequest, _session) => {
  const body = (await req.json()) as CreateAgentTypeRequest;
  if (!body.name || !body.slug) {
    return NextResponse.json(
      { code: 'validation_error', message: 'name and slug are required' },
      { status: 400 },
    );
  }
  const created = {
    ...MOCK_AGENT_TYPES[0],
    id: `at_${Date.now()}`,
    name: body.name,
    slug: body.slug,
    description: body.description ?? '',
    system_prompt: body.system_prompt ?? '',
    framework: body.framework ?? ('claude-code' as const),
    tool_scopes: body.tool_scopes ?? [],
    grant_scopes: body.grant_scopes ?? [],
    max_concurrent_runs_per_pod: body.max_concurrent_runs_per_pod ?? 10,
    state: 'provisioning' as const,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  };
  return NextResponse.json({ data: created }, { status: 201 });
});
```

- [ ] 2. Create `src/app/api/agents/types/[id]/route.ts`:

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_AGENT_TYPES } from '../../_mock/data';

export const GET = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ id: string }> }) => {
    const { id } = await params;
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === id || a.slug === id);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    return NextResponse.json({ data: agent });
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ id: string }> }) => {
    const { id } = await params;
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === id || a.slug === id);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    return new NextResponse(null, { status: 204 });
  },
);
```

**Note on `withAuth` signature with route params:** The `withAuth` wrapper in `src/server/route-helpers.ts` currently types `AuthedHandler` as `(req, session) => Promise<NextResponse>` — it does not forward a third `context` argument. Before writing the dynamic segment routes, check whether the current signature accepts a third argument. If it does not, the dynamic segment routes must either:

- Read `params` directly from the request URL (via `new URL(req.url).pathname.split(...)`), or
- Extend `AuthedHandler` in `route-helpers.ts` to accept an optional third `context` parameter.

Prefer extending `route-helpers.ts` since it keeps the handlers clean. The extension is backward-compatible: existing callers that do not use the third argument continue to work unchanged.

Updated `AuthedHandler` type in `src/server/route-helpers.ts`:

```typescript
export type AuthedHandler<TParams = Record<string, string>> = (
  req: NextRequest,
  session: IronSession<SessionPayload>,
  context?: { params: Promise<TParams> },
) => Promise<NextResponse>;
```

And the `withAuth` wrapper must be updated to forward the context argument from the Next.js route handler:

```typescript
export function withAuth<TParams = Record<string, string>>(
  handler: AuthedHandler<TParams>,
): (req: NextRequest, context?: { params: Promise<TParams> }) => Promise<NextResponse> {
  return async (req: NextRequest, context?: { params: Promise<TParams> }) => {
    const session = await getSession();
    if (!isSessionValid(session)) {
      return NextResponse.json(
        { code: 'unauthorized', message: 'No valid session' },
        { status: 401 },
      );
    }
    return handler(req, session, context);
  };
}
```

Check `src/server/route-helpers.test.ts` and update its tests if the signature changes.

---

## Task 4 — Runs BFF routes

### Files

- `src/app/api/agents/runs/route.ts` — new file
- `src/app/api/agents/runs/[runId]/route.ts` — new file
- `src/app/api/agents/runs/[runId]/messages/route.ts` — new file
- `src/app/api/agents/types/[typeId]/runs/route.ts` — new file

### Steps

- [ ] 1. Create `src/app/api/agents/runs/route.ts` (tenant-scoped list of all runs):

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS } from '../_mock/data';

export const GET = withAuth(async (_req: NextRequest, _session) => {
  return NextResponse.json({ data: MOCK_RUNS });
});
```

- [ ] 2. Create `src/app/api/agents/runs/[runId]/route.ts`:

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS, MOCK_AGENT_TYPES } from '../../_mock/data';

export const GET = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ runId: string }> }) => {
    const { runId } = await params;
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    const agentType = MOCK_AGENT_TYPES.find((a) => a.id === run.agent_type_id) ?? null;
    return NextResponse.json({ data: run, agent_type: agentType });
  },
);

export const DELETE = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ runId: string }> }) => {
    const { runId } = await params;
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    return new NextResponse(null, { status: 204 });
  },
);
```

- [ ] 3. Create `src/app/api/agents/runs/[runId]/messages/route.ts`:

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS } from '../../../_mock/data';

export const POST = withAuth(
  async (req: NextRequest, _session, { params }: { params: Promise<{ runId: string }> }) => {
    const { runId } = await params;
    const run = MOCK_RUNS.find((r) => r.id === runId);
    if (!run) {
      return NextResponse.json({ code: 'not_found', message: 'Run not found' }, { status: 404 });
    }
    const body = (await req.json()) as { content: string };
    if (!body.content?.trim()) {
      return NextResponse.json(
        { code: 'validation_error', message: 'content is required' },
        { status: 400 },
      );
    }
    // Mock: acknowledge receipt. Real SSE response arrives via events stream.
    return NextResponse.json({ ok: true, run_id: runId });
  },
);
```

- [ ] 4. Create `src/app/api/agents/types/[typeId]/runs/route.ts`:

```typescript
import 'server-only';
import { type NextRequest, NextResponse } from 'next/server';
import { withAuth } from '@orbit/server/route-helpers';
import { MOCK_RUNS, MOCK_AGENT_TYPES } from '../../../_mock/data';

export const GET = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ typeId: string }> }) => {
    const { typeId } = await params;
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === typeId || a.slug === typeId);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    const runs = MOCK_RUNS.filter((r) => r.agent_type_id === agent.id);
    return NextResponse.json({ data: runs });
  },
);

export const POST = withAuth(
  async (_req: NextRequest, _session, { params }: { params: Promise<{ typeId: string }> }) => {
    const { typeId } = await params;
    const agent = MOCK_AGENT_TYPES.find((a) => a.id === typeId || a.slug === typeId);
    if (!agent) {
      return NextResponse.json(
        { code: 'not_found', message: 'Agent type not found' },
        { status: 404 },
      );
    }
    // Return the canonical demo run. The UI navigates to /agents/runs/run_demo_chat_01.
    const run = {
      id: 'run_demo_chat_01',
      tenant_id: agent.tenant_id,
      agent_type_id: agent.id,
      state: 'pending' as const,
      pod_ordinal: -1,
      created_by: 'user_01jwk4m2n3p5q7r8',
      started_at: null,
      completed_at: null,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    };
    return NextResponse.json({ data: run }, { status: 201 });
  },
);
```

---

## Verification

Run both checks from the repo root's frontend directory. Both must exit zero before marking this plan complete.

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Expected: zero TypeScript errors, zero ESLint errors.

If `pnpm tsc` reports errors related to the `withAuth` signature change, fix `src/server/route-helpers.ts` and its test file `src/server/route-helpers.test.ts` first, then re-run.

---

## Commit

Once verification passes, commit all new and modified files:

```
git add \
  frontend/src/shared/api.ts \
  frontend/src/server/route-helpers.ts \
  frontend/src/server/route-helpers.test.ts \
  frontend/src/app/api/agents/_mock/data.ts \
  frontend/src/app/api/agents/types/route.ts \
  frontend/src/app/api/agents/types/\[id\]/route.ts \
  frontend/src/app/api/agents/types/\[typeId\]/runs/route.ts \
  frontend/src/app/api/agents/runs/route.ts \
  frontend/src/app/api/agents/runs/\[runId\]/route.ts \
  frontend/src/app/api/agents/runs/\[runId\]/messages/route.ts
```

Commit message:

```
feat(frontend): add mock BFF API routes for Agents UI (Plan 2 of 4)

Adds shared AgentType/Run types to src/shared/api.ts, a mock data module
under src/app/api/agents/_mock/data.ts, and all ten BFF route handlers
covering agent type CRUD and run lifecycle endpoints. All routes return
hardcoded realistic data so the UI can be built without a live backend.

Part of Orbit Agents UI implementation series.
```
