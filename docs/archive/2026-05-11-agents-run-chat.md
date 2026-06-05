# Plan 4 of 4 — Agents UI: Run Chat Page

**Date:** 2026-05-11
**Goal:** Build the full-viewport real-time agent run chat page at `/agents/{slug}/runs/{runId}`. This is a purpose-built coding-agent interface modelled after the `terra-agent-chat-v2.html` mockup and the `agent-chat-ui-spec.md` spec. It lives **outside** the `(dashboard)` route group to strip the sidebar/header chrome, renders a hardcoded static conversation, and provides a complete canvas panel for viewing diffs, code, and terminal output.
**Depends on:** Plan 1 (foundation — OrbitalLoader Terra component, shared `AgentType`/`Run` wire types, routing constants). OrbitalLoader must be exported from `@orbit/terra` before this plan runs.
**Unlocks:** Nothing — this is the final plan.

---

## Architecture

```
src/app/(chat)/                                  ← new route group (no sidebar chrome)
├── layout.tsx                                   ← passthrough shell
└── agents/
    └── [slug]/
        └── runs/
            └── [runId]/
                ├── layout.tsx                   ← CSS custom property tokens + full-viewport wrapper
                ├── page.tsx                     ← RSC, reads params, renders RunShell
                └── _components/
                    ├── run-shell.tsx            ← 'use client' — mounts RunProvider, renders page layout
                    ├── run-topbar.tsx           ← 46px chrome: logo, model pill, ctx meter, canvas btn
                    ├── context-strip.tsx        ← 32px file chip strip
                    ├── chat-thread.tsx          ← scrollable turn renderer
                    ├── human-turn.tsx           ← right-aligned bubble
                    ├── agent-turn.tsx           ← byline + content flow
                    ├── agent-text-block.tsx     ← prose + inline code parsing
                    ├── tool-row.tsx             ← the core tool call row with pop-out
                    ├── thinking-block.tsx       ← collapsible extended-thinking section
                    ├── approval-prompt.tsx      ← approve/reject/edit action card
                    ├── canvas-panel.tsx         ← 0→520px slide-in canvas
                    └── input-area.tsx           ← slash commands + textarea + send
                └── _context/
                    └── run-context.tsx          ← RunProvider + useRunContext()
                └── _hooks/
                    └── use-run.ts               ← static mock data + canvas state
                └── _types/
                    └── index.ts                 ← all data model types
```

The `(chat)` route group is Next.js App Router convention: the parens are stripped from the URL, so `/agents/{slug}/runs/{runId}` resolves correctly. The group has its own layout that provides **no** sidebar, header, or permissions provider — just the children.

---

## Tech Stack

- **TypeScript 5 / Next.js 15 App Router** — RSC page + client shell pattern
- **Tailwind v4** — only for layout utilities (`h-dvh`, `flex`, `overflow-hidden`, `flex-1`). All brand colours use `var(--chat-*)` CSS custom properties set on the layout wrapper.
- **Terra (`@orbit/terra`)** — `Stack`, `Grid`, `Text`, `Heading`, `Button`, `Icon`, `OrbitalLoader` (from Plan 1)
- **React context** — canvas open/close and tab state shared across the component tree
- **No live SSE** — entire conversation is hardcoded in `use-run.ts`; canvas state is managed with `useState`

### Terra usage note

This page has its own dense visual language. Terra layout primitives (`Stack`) are used where possible, but components that require the `var(--chat-*)` design tokens (ToolRow borders, approval prompts, canvas panel) use `style={{ ... }}` inline objects with CSS variable references. This is intentional and acceptable for this specialised page — it does not violate the Terra rules because the raw HTML primitives (`<div>`, `<button>`, `<p>`) are replaced by Terra components wherever structurally appropriate; `className` and `style` props on Terra components are only used here for the chat-specific token overrides that are genuinely not expressible through Terra's prop API.

**Exception:** ToolRow, CanvasPanel, and InputArea contain a handful of raw `<div>` and `<button>` elements for the dense compound layouts (tool badge inline-spinner, diff line columns, tab bar close button) where Terra's component boundaries would add structural noise with no benefit. These are tier-3 feature-scoped components — the `no-raw-*` lint rules apply only to pages in `src/app`, not to feature-private `_components` directories. Confirm this against `.eslintrc` before opening a PR; if lint fails, wrap raw elements in `Stack asChild` or extract a mini Terra component.

---

## Design Tokens Reference

All tokens are scoped as CSS custom properties on the layout `div`. Reference them everywhere as `var(--chat-*)`.

```
--chat-deep:    oklch(13% 0.005 248)   canvas bg, terminal bg
--chat-bg:      oklch(18% 0.005 248)   main background
--chat-surf:    oklch(22% 0.005 248)   topbar, input area
--chat-surf2:   oklch(27% 0.005 248)   hover states, chips
--chat-surf3:   oklch(32% 0.005 248)   scrollbar thumbs
--chat-fg:      oklch(96% 0.005 248)   primary text
--chat-muted:   oklch(72% 0.005 248)   secondary text
--chat-subtle:  oklch(58% 0.005 248)   de-emphasised text
--chat-bd:      oklch(52% 0.005 248)   prominent borders (input focus)
--chat-bds:     oklch(28% 0.005 248)   standard borders
--chat-acc:     oklch(72% 0.17 8)      accent foreground
--chat-acc-h:   oklch(82% 0.17 8)      accent hover
--chat-acc-dim: oklch(72% 0.17 8 / 0.10)  accent bg fill
--chat-acc-bdr: oklch(72% 0.17 8 / 0.30)  accent border
--chat-healthy: #9ce37d
--chat-degraded:#ffd046
--chat-error:   #ef4444
```

---

## Task 1 — Route group + layouts + global animation CSS

### Files

- `src/app/(chat)/layout.tsx`
- `src/app/(chat)/agents/[slug]/runs/[runId]/layout.tsx`

### Steps

- [ ] 1. Create the chat route group passthrough layout. This layout is a Next.js App Router requirement — without it the route group has no layout boundary. It must not import `server-only` (it is a client-side boundary passthrough):

**`src/app/(chat)/layout.tsx`**

```typescript
export default function ChatLayout({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}
```

- [ ] 2. Create the run-level layout. This sets every `--chat-*` CSS custom property on a full-viewport wrapper div. Because CSS custom properties cascade, every child component can reference `var(--chat-acc)` etc. without any import:

**`src/app/(chat)/agents/[slug]/runs/[runId]/layout.tsx`**

```typescript
export default function RunLayout({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="h-dvh flex flex-col overflow-hidden text-[13.5px] leading-relaxed"
      style={
        {
          '--chat-deep': 'oklch(13% 0.005 248)',
          '--chat-bg': 'oklch(18% 0.005 248)',
          '--chat-surf': 'oklch(22% 0.005 248)',
          '--chat-surf2': 'oklch(27% 0.005 248)',
          '--chat-surf3': 'oklch(32% 0.005 248)',
          '--chat-fg': 'oklch(96% 0.005 248)',
          '--chat-muted': 'oklch(72% 0.005 248)',
          '--chat-subtle': 'oklch(58% 0.005 248)',
          '--chat-bd': 'oklch(52% 0.005 248)',
          '--chat-bds': 'oklch(28% 0.005 248)',
          '--chat-acc': 'oklch(72% 0.17 8)',
          '--chat-acc-h': 'oklch(82% 0.17 8)',
          '--chat-acc-dim': 'oklch(72% 0.17 8 / 0.10)',
          '--chat-acc-bdr': 'oklch(72% 0.17 8 / 0.30)',
          '--chat-healthy': '#9ce37d',
          '--chat-degraded': '#ffd046',
          '--chat-error': '#ef4444',
          background: 'var(--chat-bg)',
          color: 'var(--chat-fg)',
          fontFamily: 'ui-sans-serif, system-ui, -apple-system, sans-serif',
        } as React.CSSProperties
      }
    >
      {children}
    </div>
  );
}
```

- [ ] 3. Verify the directory structure was created correctly:

```bash
ls src/app/(chat)/agents/[slug]/runs/[runId]/
# Expected: layout.tsx
ls src/app/(chat)/
# Expected: layout.tsx  agents/
```

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0.

### Commit

```bash
git add frontend/src/app/\(chat\)
git commit -m "feat(agents/run-chat): add (chat) route group + layout with design token CSS vars"
```

---

## Task 2 — Data model types

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_types/index.ts`

### Steps

- [ ] 1. Create the types file. These types drive the entire page — every component imports from this file:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_types/index.ts`**

```typescript
// ── Tool / run status enums ───────────────────────────────────────────────────

export type ToolStatus = 'running' | 'done' | 'error';

export type RunState = 'pending' | 'starting' | 'active' | 'completing' | 'completed' | 'failed';

// ── Turn types ────────────────────────────────────────────────────────────────

export interface HumanTurn {
  type: 'human';
  id: string;
  content: string;
  timestamp: string;
}

export interface ToolCall {
  id: string;
  /** Emoji icon: '📄' | '✏️' | '⚡' | '🔍' */
  icon: string;
  name: string;
  /** Truncated display argument (path, command snippet, etc.) */
  arg: string;
  status: ToolStatus;
  /** Display result: '✓ 142 lines' | '✗ 3 errors' | '+12 / -3' — empty when running */
  result: string;
  /** When set, the pop-out button (⤢) opens this canvas view. */
  canvasViewId?: string;
}

export interface ThinkingBlock {
  content: string;
  durationSeconds: number;
  tokenCount: number;
}

export interface AgentTurn {
  type: 'agent';
  id: string;
  timestamp: string;
  thinking?: ThinkingBlock;
  elements: AgentElement[];
}

export type AgentElement =
  | { kind: 'text'; content: string }
  | { kind: 'tool'; tool: ToolCall }
  | { kind: 'approval'; id: string; title: string; description: string; command: string };

export type Turn = HumanTurn | AgentTurn;

// ── Canvas types ──────────────────────────────────────────────────────────────

export interface CanvasTab {
  id: string;
  label: string;
  ext?: string; // 'tsx' | 'ts' | undefined — shown in accent colour
  modified?: boolean; // shows ● dot (degraded colour) when true
}

export type CanvasViewType = 'code' | 'diff' | 'terminal';

export interface DiffLine {
  kind: 'add' | 'del' | 'ctx';
  oldNum?: number;
  newNum?: number;
  content: string;
}

export interface TerminalLine {
  kind: 'normal' | 'error' | 'success' | 'dim';
  content: string;
}

export interface CanvasView {
  id: string;
  type: CanvasViewType;
  title: string;
  subtitle?: string;
  // Code view fields
  code?: string;
  language?: string;
  lineCount?: number;
  // Diff view fields
  diffLines?: DiffLine[];
  addCount?: number;
  delCount?: number;
  // Terminal view fields
  command?: string;
  exitCode?: number;
  output?: TerminalLine[];
}

// ── Top-level run state ───────────────────────────────────────────────────────

export interface CanvasState {
  isOpen: boolean;
  tabs: CanvasTab[];
  activeTabId: string | null;
}

export interface RunChatState {
  runId: string;
  slug: string;
  agentName: string;
  model: string;
  runState: RunState;
  turns: Turn[];
  contextFiles: string[];
  canvasState: CanvasState;
  views: Record<string, CanvasView>;
}
```

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit
```

---

## Task 3 — `useRun` hook with mock data

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_hooks/use-run.ts`

### Steps

- [ ] 1. Create the hook. The hook initialises canvas state with `useState` and provides `openCanvas`, `closeCanvas`, and `switchTab` mutations. All conversation content is hardcoded — no network calls:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_hooks/use-run.ts`**

```typescript
'use client';

import { useState } from 'react';
import type { RunChatState, Turn, CanvasView, CanvasTab, CanvasState } from '../_types';

// ── Mock turns ────────────────────────────────────────────────────────────────

const MOCK_TURNS: Turn[] = [
  {
    type: 'human',
    id: 'h1',
    content:
      "Fix the type errors in the auth module and add proper error handling for the login flow. There are a few places where we're not handling network failures.",
    timestamp: '09:41:00',
  },
  {
    type: 'agent',
    id: 'a1',
    timestamp: '09:41:03',
    thinking: {
      content:
        "Auth module: type-safe state init, null-check on login() result, and the fetch wrapper needs to throw on non-2xx. I'll read all three files then run tsc to confirm the exact errors before touching anything.",
      durationSeconds: 8,
      tokenCount: 1247,
    },
    elements: [
      { kind: 'text', content: "I'll read the auth files and run the type checker first." },
      {
        kind: 'tool',
        tool: {
          id: 't1',
          icon: '📄',
          name: 'read_file',
          arg: 'src/auth/login.tsx',
          status: 'done',
          result: '✓ 142 lines',
          canvasViewId: 'cv-login',
        },
      },
      {
        kind: 'tool',
        tool: {
          id: 't2',
          icon: '📄',
          name: 'read_file',
          arg: 'src/auth/types.ts',
          status: 'done',
          result: '✓ 28 lines',
          canvasViewId: 'cv-types',
        },
      },
      {
        kind: 'tool',
        tool: {
          id: 't3',
          icon: '⚡',
          name: 'bash',
          arg: 'npx tsc --noEmit 2>&1 | head -40',
          status: 'error',
          result: '✗ 3 errors',
          canvasViewId: 'cv-tsc-err',
        },
      },
      {
        kind: 'text',
        content:
          'Three issues. `useState(null)` needs an explicit generic, the submit handler needs try/catch, and the fetch wrapper silently returns `undefined` on non-2xx instead of throwing.',
      },
      {
        kind: 'tool',
        tool: {
          id: 't4',
          icon: '✏️',
          name: 'write_file',
          arg: 'src/auth/login.tsx',
          status: 'done',
          result: '✓ +12 / -3',
          canvasViewId: 'cv-diff-login',
        },
      },
      {
        kind: 'tool',
        tool: {
          id: 't5',
          icon: '✏️',
          name: 'write_file',
          arg: 'src/lib/api-client.ts',
          status: 'done',
          result: '✓ +4 / -1',
          canvasViewId: 'cv-diff-api',
        },
      },
      {
        kind: 'tool',
        tool: {
          id: 't6',
          icon: '⚡',
          name: 'bash',
          arg: 'npx tsc --noEmit',
          status: 'done',
          result: '✓ exit 0',
          canvasViewId: 'cv-tsc-ok',
        },
      },
      {
        kind: 'text',
        content:
          'Type errors resolved. `login.tsx` is now typed correctly with try/catch for network failures. `api-client.ts` throws `ApiError` on non-2xx so callers always get something meaningful to catch.',
      },
      {
        kind: 'tool',
        tool: {
          id: 't7',
          icon: '⚡',
          name: 'bash',
          arg: 'npm test src/auth -- --coverage',
          status: 'running',
          result: '',
        },
      },
      {
        kind: 'approval',
        id: 'appr1',
        title: 'Run integration tests against local server?',
        description: 'Verify network error paths end-to-end. Starts a test server on :3001.',
        command: 'npm run test:integration -- --env=local --port=3001',
      },
    ],
  },
];

// ── Mock context files ────────────────────────────────────────────────────────

const MOCK_CONTEXT_FILES = ['src/auth/login.tsx', 'src/auth/types.ts', 'src/lib/api-client.ts'];

// ── Mock canvas views ─────────────────────────────────────────────────────────

const MOCK_CANVAS_VIEWS: Record<string, CanvasView> = {
  'cv-login': {
    id: 'cv-login',
    type: 'code',
    title: 'login.tsx',
    subtitle: 'src/auth/',
    lineCount: 142,
    language: 'tsx',
    code: `import { useState } from 'react'
import { login } from '../lib/api-client'
import type { User } from './types'

export function LoginForm() {
  const [user, setUser] = useState(null)
  const [error, setError] = useState('')

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault()
    const result = await login(email, password)
    setUser(result.user) // ← result may be undefined
  }
}`,
  },
  'cv-types': {
    id: 'cv-types',
    type: 'code',
    title: 'types.ts',
    subtitle: 'src/auth/',
    lineCount: 28,
    language: 'ts',
    code: `export interface User {
  id: string
  email: string
  role: 'admin' | 'viewer'
}

// LoginResult missing — api-client returns this implicitly`,
  },
  'cv-tsc-err': {
    id: 'cv-tsc-err',
    type: 'terminal',
    title: 'terminal',
    command: 'npx tsc --noEmit',
    exitCode: 1,
    output: [
      {
        kind: 'error',
        content:
          "src/auth/login.tsx(6,28): error TS2345: Argument of type 'null' is not assignable...",
      },
      {
        kind: 'normal',
        content: "  of type 'SetStateAction<User | undefined>'.",
      },
      {
        kind: 'error',
        content: "src/auth/login.tsx(12,5): error TS18048: 'result' is possibly 'undefined'.",
      },
      {
        kind: 'error',
        content: "src/lib/api-client.ts(31,10): error TS2322: Type 'Response | undefined'...",
      },
      { kind: 'dim', content: 'Found 3 errors in 2 files.' },
    ],
  },
  'cv-diff-login': {
    id: 'cv-diff-login',
    type: 'diff',
    title: 'login.tsx',
    subtitle: 'src/auth/',
    addCount: 12,
    delCount: 3,
    diffLines: [
      { kind: 'ctx', oldNum: 5, newNum: 5, content: 'export function LoginForm() {' },
      { kind: 'del', oldNum: 6, content: '  const [user, setUser] = useState(null)' },
      { kind: 'del', oldNum: 7, content: "  const [error, setError] = useState('')" },
      {
        kind: 'add',
        newNum: 6,
        content: '  const [user, setUser] = useState<User | undefined>(undefined)',
      },
      {
        kind: 'add',
        newNum: 7,
        content: '  const [error, setError] = useState<string | null>(null)',
      },
      {
        kind: 'ctx',
        oldNum: 9,
        newNum: 9,
        content: '  const handleSubmit = async (e: FormEvent) => {',
      },
      { kind: 'del', oldNum: 11, content: '    const result = await login(email, password)' },
      { kind: 'del', oldNum: 12, content: '    setUser(result.user)' },
      { kind: 'add', newNum: 11, content: '    try {' },
      { kind: 'add', newNum: 12, content: '      const result = await login(email, password)' },
      { kind: 'add', newNum: 13, content: '      setUser(result.user)' },
      { kind: 'add', newNum: 14, content: '      setError(null)' },
      { kind: 'add', newNum: 15, content: '    } catch (err) {' },
      {
        kind: 'add',
        newNum: 16,
        content: "      setError(err instanceof Error ? err.message : 'Login failed')",
      },
      { kind: 'add', newNum: 17, content: '    }' },
      { kind: 'ctx', oldNum: 13, newNum: 18, content: '  }' },
    ],
  },
  'cv-diff-api': {
    id: 'cv-diff-api',
    type: 'diff',
    title: 'api-client.ts',
    subtitle: 'src/lib/',
    addCount: 4,
    delCount: 1,
    diffLines: [
      { kind: 'ctx', oldNum: 27, newNum: 27, content: '  const res = await fetch(url, opts)' },
      { kind: 'del', oldNum: 28, content: '  if (!res.ok) return undefined' },
      { kind: 'add', newNum: 28, content: '  if (!res.ok) {' },
      { kind: 'add', newNum: 29, content: '    const body = await res.json().catch(() => ({}))' },
      {
        kind: 'add',
        newNum: 30,
        content: "    throw new ApiError(res.status, body.message ?? 'Request failed')",
      },
      { kind: 'add', newNum: 31, content: '  }' },
      { kind: 'ctx', oldNum: 29, newNum: 32, content: '  return res.json() as Promise<T>' },
    ],
  },
  'cv-tsc-ok': {
    id: 'cv-tsc-ok',
    type: 'terminal',
    title: 'terminal',
    command: 'npx tsc --noEmit',
    exitCode: 0,
    output: [{ kind: 'success', content: '✓ No type errors found.' }],
  },
};

// ── Helper: derive CanvasTab from CanvasView ──────────────────────────────────

function viewToTab(view: CanvasView): CanvasTab {
  return {
    id: view.id,
    label: view.title,
    ext: view.language ?? (view.type === 'terminal' ? undefined : undefined),
    modified: view.type === 'diff',
  };
}

// ── Hook return type ──────────────────────────────────────────────────────────

export interface UseRunReturn extends RunChatState {
  openCanvas: (viewId: string) => void;
  closeCanvas: () => void;
  switchTab: (tabId: string) => void;
  addHumanMessage: (content: string) => void;
}

// ── useRun ────────────────────────────────────────────────────────────────────

export function useRun(runId: string, slug: string): UseRunReturn {
  const [turns, setTurns] = useState<Turn[]>(MOCK_TURNS);
  const [canvasState, setCanvasState] = useState<CanvasState>({
    isOpen: false,
    tabs: [],
    activeTabId: null,
  });

  function openCanvas(viewId: string): void {
    const view = MOCK_CANVAS_VIEWS[viewId];
    if (!view) return;

    setCanvasState((prev) => {
      const alreadyOpen = prev.tabs.some((t) => t.id === viewId);
      const tabs = alreadyOpen ? prev.tabs : [...prev.tabs, viewToTab(view)];
      return { isOpen: true, tabs, activeTabId: viewId };
    });
  }

  function closeCanvas(): void {
    setCanvasState((prev) => ({ ...prev, isOpen: false }));
  }

  function switchTab(tabId: string): void {
    setCanvasState((prev) => ({ ...prev, activeTabId: tabId }));
  }

  function addHumanMessage(content: string): void {
    const now = new Date();
    const ts = now.toTimeString().slice(0, 8);
    const turn: Turn = {
      type: 'human',
      id: `h-${Date.now()}`,
      content,
      timestamp: ts,
    };
    setTurns((prev) => [...prev, turn]);
  }

  return {
    runId,
    slug,
    agentName: 'Orbit AI',
    model: 'claude-sonnet-4-6',
    runState: 'active',
    turns,
    contextFiles: MOCK_CONTEXT_FILES,
    canvasState,
    views: MOCK_CANVAS_VIEWS,
    openCanvas,
    closeCanvas,
    switchTab,
    addHumanMessage,
  };
}
```

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit
```

---

## Task 4 — Run context provider

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_context/run-context.tsx`

### Steps

- [ ] 1. Create the context. The provider calls `useRun` and makes the full return value available to all children:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_context/run-context.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped. Provides RunChatState + canvas mutations to the
 * entire run chat page tree. Promote only if a second feature needs it.
 */

import { createContext, useContext } from 'react';
import { useRun, type UseRunReturn } from '../_hooks/use-run';

const RunContext = createContext<UseRunReturn | null>(null);

export function RunProvider({
  slug,
  runId,
  children,
}: {
  slug: string;
  runId: string;
  children: React.ReactNode;
}) {
  const value = useRun(runId, slug);
  return <RunContext.Provider value={value}>{children}</RunContext.Provider>;
}

export function useRunContext(): UseRunReturn {
  const ctx = useContext(RunContext);
  if (!ctx) {
    throw new Error('useRunContext must be used inside <RunProvider>');
  }
  return ctx;
}
```

---

## Task 5 — RSC page + RunShell client component

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/page.tsx`
- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-shell.tsx`

### Steps

- [ ] 1. Create the RSC page. Next.js 15 dynamic params are async — always `await params`:

**`src/app/(chat)/agents/[slug]/runs/[runId]/page.tsx`**

```typescript
import { RunShell } from './_components/run-shell';

export default async function RunPage({
  params,
}: {
  params: Promise<{ slug: string; runId: string }>;
}) {
  const { slug, runId } = await params;
  return <RunShell slug={slug} runId={runId} />;
}
```

- [ ] 2. Create the client shell. This is the only component that bridges the RSC boundary — it mounts the context provider and renders the four page regions:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-shell.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped. Client boundary shell: mounts RunProvider and
 * renders the four layout regions (topbar, context strip, body, input).
 */

import { RunProvider } from '../_context/run-context';
import { RunTopbar } from './run-topbar';
import { ContextStrip } from './context-strip';
import { ChatThread } from './chat-thread';
import { CanvasPanel } from './canvas-panel';
import { InputArea } from './input-area';

function RunPageLayout() {
  return (
    <>
      <RunTopbar />
      <ContextStrip />
      {/* Layout body: chat + canvas side by side */}
      <div className="flex flex-1 overflow-hidden min-h-0">
        <div className="flex flex-col flex-1 min-w-0 overflow-hidden">
          <ChatThread />
          <InputArea />
        </div>
        {/* Canvas panel hidden below md breakpoint */}
        <div className="hidden md:flex flex-shrink-0">
          <CanvasPanel />
        </div>
      </div>
    </>
  );
}

export function RunShell({ slug, runId }: { slug: string; runId: string }) {
  return (
    <RunProvider slug={slug} runId={runId}>
      <RunPageLayout />
    </RunProvider>
  );
}
```

Note: `RunPageLayout` is defined inside `run-shell.tsx` (not a separate file) because it has no meaningful use outside this shell — it only makes sense with a `RunProvider` ancestor.

### Verification

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

### Commit

```bash
git add frontend/src/app/\(chat\)
git commit -m "feat(agents/run-chat): add types, useRun hook, RunContext, RSC page, and RunShell"
```

---

## Task 6 — RunTopbar

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-topbar.tsx`

### Steps

- [ ] 1. Create the topbar. All colours use `var(--chat-*)` references via `style` props. The Canvas button reflects `canvasState.isOpen`:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-topbar.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 46px topbar: back nav, logo, model pill, session pill, ctx meter, canvas toggle.
 */

import Link from 'next/link';
import { APP_ROUTES } from '@/lib/routing/routes';
import { Stack, Text } from '@orbit/terra';
import { useRunContext } from '../_context/run-context';

// Orbital logomark SVG — inline, 10×10 viewBox
function OrbitalMark({ size = 10 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle cx="6" cy="6" r="5" stroke="currentColor" strokeWidth="1.2" fill="none" opacity=".5" />
    </svg>
  );
}

// Canvas split-pane icon
function CanvasIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <rect x="3" y="3" width="18" height="18" rx="2" />
      <line x1="12" y1="3" x2="12" y2="21" />
    </svg>
  );
}

// Session log document icon
function DocIcon() {
  return (
    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" aria-hidden>
      <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
      <polyline points="14 2 14 8 20 8" />
    </svg>
  );
}

export function RunTopbar() {
  const { slug, model, runId, canvasState, openCanvas } = useRunContext();

  const canvasIsOpen = canvasState.isOpen;
  const hasCanvasTabs = canvasState.tabs.length > 0;

  function handleCanvasToggle() {
    if (hasCanvasTabs && canvasState.activeTabId) {
      openCanvas(canvasState.activeTabId);
    } else if (hasCanvasTabs) {
      openCanvas(canvasState.tabs[0].id);
    }
    // If no tabs open yet, the button does nothing — user must click a tool row first
  }

  // Truncate runId for display (show last 8 chars)
  const displayRunId = runId.length > 8 ? runId.slice(-8) : runId;

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 10,
        padding: '0 16px',
        height: 46,
        background: 'var(--chat-surf)',
        borderBottom: '1px solid var(--chat-bds)',
        flexShrink: 0,
        zIndex: 10,
      }}
    >
      {/* Back navigation */}
      <Link
        href={APP_ROUTES.agents.detail(slug)}
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          width: 27,
          height: 27,
          borderRadius: 4,
          color: 'var(--chat-subtle)',
          textDecoration: 'none',
          fontSize: 14,
        }}
        aria-label="Back to agent"
      >
        ←
      </Link>

      {/* Logo + brand */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 7, fontWeight: 600, fontSize: 13 }}>
        <div
          style={{
            width: 20,
            height: 20,
            background: 'var(--chat-acc-dim)',
            border: '1.5px solid var(--chat-acc-bdr)',
            borderRadius: 4,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--chat-acc)',
          }}
          aria-hidden
        >
          <OrbitalMark size={10} />
        </div>
        <Text as="span" style={{ color: 'var(--chat-fg)', fontWeight: 600, fontSize: 13 }}>
          Orbit AI
        </Text>
      </div>

      {/* Separator */}
      <span style={{ color: 'var(--chat-bds)', fontSize: 14 }}>/</span>

      {/* Model pill */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 5,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 20,
          padding: '2px 9px',
          fontSize: 11.5,
          color: 'var(--chat-muted)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        }}
      >
        {/* Live dot */}
        <span
          style={{
            width: 5,
            height: 5,
            borderRadius: '50%',
            background: 'var(--chat-healthy)',
            flexShrink: 0,
            animation: 'pulse 2s ease-in-out infinite',
          }}
          aria-label="Connected"
        />
        {model}
      </div>

      {/* Separator */}
      <span style={{ color: 'var(--chat-bds)', opacity: 0.3, fontSize: 14 }}>/</span>

      {/* Session pill */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 5,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 20,
          padding: '2px 9px',
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        }}
      >
        {displayRunId}
      </div>

      {/* Spacer */}
      <div style={{ flex: 1 }} />

      {/* Context meter */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 7,
          fontSize: 11,
          color: 'var(--chat-subtle)',
        }}
      >
        <span>ctx</span>
        <div
          style={{
            width: 72,
            height: 3,
            background: 'var(--chat-surf3)',
            borderRadius: 2,
            overflow: 'hidden',
          }}
        >
          <div
            style={{
              height: '100%',
              width: '21%',
              background: 'var(--chat-acc)',
              borderRadius: 2,
            }}
          />
        </div>
        <span>42k / 200k</span>
      </div>

      {/* Session log icon button */}
      <button
        type="button"
        style={{
          width: 27,
          height: 27,
          background: 'transparent',
          border: '1px solid transparent',
          borderRadius: 4,
          color: 'var(--chat-subtle)',
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
        title="Session log"
        aria-label="Session log"
      >
        <DocIcon />
      </button>

      {/* Canvas toggle */}
      <button
        type="button"
        onClick={handleCanvasToggle}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          padding: '4px 10px',
          background: canvasIsOpen ? 'var(--chat-acc-dim)' : 'transparent',
          border: `1px solid ${canvasIsOpen ? 'var(--chat-acc-bdr)' : 'var(--chat-bds)'}`,
          borderRadius: 4,
          color: canvasIsOpen ? 'var(--chat-acc)' : 'var(--chat-subtle)',
          fontSize: 12,
          cursor: 'pointer',
        }}
        aria-label={canvasIsOpen ? 'Close canvas' : 'Open canvas'}
        aria-pressed={canvasIsOpen}
      >
        <CanvasIcon />
        Canvas
      </button>

      {/* Keyframe for live dot — injected once */}
      <style>{`@keyframes pulse { 0%,100%{opacity:1} 50%{opacity:0.4} }`}</style>
    </div>
  );
}
```

---

## Task 7 — ContextStrip

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/context-strip.tsx`

### Steps

- [ ] 1. Create the context file strip. Each chip maps a file path to a canvas view by matching `view.subtitle + view.title` against the file path:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/context-strip.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * 32px horizontal strip of file chips representing the agent's context window.
 */

import { useRunContext } from '../_context/run-context';

function getExt(path: string): string {
  const parts = path.split('.');
  return parts.length > 1 ? parts[parts.length - 1] : '';
}

function getFilename(path: string): string {
  return path.split('/').pop() ?? path;
}

function getDir(path: string): string {
  const parts = path.split('/');
  return parts.slice(0, -1).join('/');
}

export function ContextStrip() {
  const { contextFiles, views, openCanvas } = useRunContext();

  function handleFileClick(filePath: string) {
    // Find a view matching this path
    const filename = getFilename(filePath);
    const matchingView = Object.values(views).find(
      (v) => v.title === filename || (v.subtitle && filePath.startsWith(v.subtitle))
    );
    if (matchingView) {
      openCanvas(matchingView.id);
    }
  }

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 7,
        padding: '5px 16px',
        background: 'var(--chat-surf)',
        borderBottom: '1px solid var(--chat-bds)',
        flexShrink: 0,
        overflowX: 'auto',
      }}
    >
      {/* "Context" label */}
      <span
        style={{
          fontSize: 10.5,
          color: 'var(--chat-subtle)',
          textTransform: 'uppercase',
          letterSpacing: '0.06em',
          flexShrink: 0,
        }}
      >
        Context
      </span>

      {/* File chips */}
      {contextFiles.map((filePath) => {
        const ext = getExt(filePath);
        const filename = getFilename(filePath);
        const dir = getDir(filePath);

        return (
          <button
            key={filePath}
            type="button"
            onClick={() => handleFileClick(filePath)}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 4,
              padding: '2px 8px',
              background: 'var(--chat-surf2)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 20,
              fontSize: 11.5,
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              color: 'var(--chat-muted)',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
            title={filePath}
          >
            {ext && (
              <span style={{ color: 'var(--chat-acc)', opacity: 0.75 }}>{ext}</span>
            )}
            {dir && <span style={{ color: 'var(--chat-subtle)' }}>{dir}/</span>}
            <strong style={{ color: 'var(--chat-fg)' }}>{filename}</strong>
          </button>
        );
      })}

      {/* Add chip */}
      <button
        type="button"
        style={{
          padding: '2px 8px',
          background: 'transparent',
          border: '1px dashed var(--chat-bds)',
          borderRadius: 20,
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          cursor: 'pointer',
          whiteSpace: 'nowrap',
        }}
      >
        + add file
      </button>
    </div>
  );
}
```

---

## Task 8 — ChatThread + session divider

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/chat-thread.tsx`

### Steps

- [ ] 1. Create the scrollable thread. Auto-scroll to bottom on mount using `useEffect`. Uses a smart auto-scroll heuristic — only scrolls if within 100px of bottom before new content arrives:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/chat-thread.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Scrollable container that renders the full conversation turn list.
 */

import { useEffect, useRef } from 'react';
import { useRunContext } from '../_context/run-context';
import { HumanTurnItem } from './human-turn';
import { AgentTurnItem } from './agent-turn';

export function ChatThread() {
  const { turns } = useRunContext();
  const containerRef = useRef<HTMLDivElement>(null);

  // Auto-scroll to bottom on initial mount
  useEffect(() => {
    const el = containerRef.current;
    if (el) el.scrollTop = el.scrollHeight;
  }, []);

  // Auto-scroll when turns change, but only if already near bottom
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 100;
    if (nearBottom) el.scrollTop = el.scrollHeight;
  }, [turns]);

  return (
    <div
      ref={containerRef}
      style={{
        flex: 1,
        overflowY: 'auto',
        padding: '20px 0 12px',
        scrollBehavior: 'smooth',
      }}
    >
      {/* Custom scrollbar */}
      <style>{`
        .chat-thread-scroll::-webkit-scrollbar { width: 5px; }
        .chat-thread-scroll::-webkit-scrollbar-thumb {
          background: var(--chat-surf3); border-radius: 3px;
        }
      `}</style>

      {/* Session divider */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 10,
          padding: '8px 20px 6px',
          fontSize: 10.5,
          textTransform: 'uppercase',
          letterSpacing: '0.07em',
          color: 'var(--chat-bds)',
        }}
      >
        Session started · Today · 09:41
        <span
          style={{
            flex: 1,
            height: 1,
            background: 'var(--chat-bds)',
            opacity: 0.4,
          }}
        />
      </div>

      {/* Turn list */}
      {turns.map((turn) =>
        turn.type === 'human' ? (
          <HumanTurnItem key={turn.id} turn={turn} />
        ) : (
          <AgentTurnItem key={turn.id} turn={turn} />
        ),
      )}
    </div>
  );
}
```

---

## Task 9 — HumanTurn

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/human-turn.tsx`

### Steps

- [ ] 1. Create the right-aligned human bubble:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/human-turn.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Right-aligned message bubble for the human side of the conversation.
 */

import type { HumanTurn } from '../_types';

export function HumanTurnItem({ turn }: { turn: HumanTurn }) {
  return (
    <div
      style={{
        display: 'flex',
        justifyContent: 'flex-end',
        padding: '4px 20px',
      }}
    >
      <div
        style={{
          maxWidth: '68%',
          background: 'var(--chat-acc-dim)',
          border: '1px solid var(--chat-acc-bdr)',
          borderRadius: '10px 10px 3px 10px',
          padding: '9px 14px',
          color: 'var(--chat-fg)',
          lineHeight: 1.55,
        }}
      >
        {turn.content}
      </div>
    </div>
  );
}
```

---

## Task 10 — AgentTextBlock (inline code parser)

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-text-block.tsx`

### Steps

- [ ] 1. Create the text block with inline backtick parsing:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-text-block.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Renders agent prose paragraphs with inline `code` backtick spans highlighted
 * in accent colour.
 */

import type { ReactNode } from 'react';

// Splits text on backtick pairs and returns a mix of text nodes + <code> spans.
function parseInlineCode(text: string): ReactNode[] {
  const parts = text.split('`');
  return parts.map((part, i) => {
    if (i % 2 === 1) {
      // Odd indices are inside backtick pairs → render as code
      return (
        <code
          key={i}
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 12,
            color: 'var(--chat-acc)',
            background: 'var(--chat-acc-dim)',
            border: '1px solid var(--chat-bds)',
            borderRadius: 3,
            padding: '1px 5px',
          }}
        >
          {part}
        </code>
      );
    }
    return part;
  });
}

export function AgentTextBlock({ content }: { content: string }) {
  const paragraphs = content.split('\n\n').filter(Boolean);

  return (
    <div
      style={{
        color: 'var(--chat-fg)',
        lineHeight: 1.65,
        marginBottom: 10,
      }}
    >
      {paragraphs.map((para, i) => (
        <p
          key={i}
          style={{ marginTop: i > 0 ? 6 : 0 }}
        >
          {parseInlineCode(para)}
        </p>
      ))}
    </div>
  );
}
```

---

## Task 11 — ThinkingBlock

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/thinking-block.tsx`

### Steps

- [ ] 1. Create the collapsible thinking section. Uses `OrbitalLoader` from `@orbit/terra` (requires Plan 1 to be merged):

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/thinking-block.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Collapsible extended-thinking section with orbital loader toggle.
 */

import { useState } from 'react';
import { OrbitalLoader } from '@orbit/terra';
import type { ThinkingBlock as ThinkingBlockType } from '../_types';

export function ThinkingBlock({ thinking }: { thinking: ThinkingBlockType }) {
  const [expanded, setExpanded] = useState(false);

  return (
    <div
      style={{
        margin: '4px 0 10px',
        borderLeft: '2px solid var(--chat-bds)',
        paddingLeft: 12,
      }}
    >
      {/* Toggle row */}
      <button
        type="button"
        onClick={() => setExpanded((v) => !v)}
        aria-expanded={expanded}
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 6,
          cursor: 'pointer',
          userSelect: 'none',
          fontSize: 11.5,
          color: 'var(--chat-subtle)',
          padding: '2px 0',
          background: 'none',
          border: 'none',
          width: '100%',
          textAlign: 'left',
        }}
      >
        <span style={{ opacity: 0.7, flexShrink: 0 }}>
          <OrbitalLoader size="sm" />
        </span>
        <span>
          Thought for {thinking.durationSeconds} seconds
        </span>
        <span
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 10.5,
            opacity: 0.35,
            marginLeft: 6,
          }}
        >
          {thinking.tokenCount.toLocaleString()} tokens
        </span>
        <span
          style={{
            fontSize: 9,
            marginLeft: 'auto',
            transition: 'transform 0.2s',
            transform: expanded ? 'rotate(90deg)' : 'rotate(0deg)',
          }}
          aria-hidden
        >
          ▶
        </span>
      </button>

      {/* Body (conditional) */}
      {expanded && (
        <div
          style={{
            marginTop: 6,
            fontSize: 12,
            color: 'var(--chat-subtle)',
            fontStyle: 'italic',
            lineHeight: 1.65,
          }}
        >
          {thinking.content}
        </div>
      )}
    </div>
  );
}
```

**Note:** If `OrbitalLoader` is not yet exported from `@orbit/terra` (Plan 1 not merged), replace the import with a local inline orbital div as a temporary stub. See Plan 1's Task 6 for the full OrbitalLoader implementation. Once Plan 1 merges, the stub can be removed.

Temporary stub (delete after Plan 1 merges):

```typescript
// Temporary stub — replace with OrbitalLoader from @orbit/terra after Plan 1 merges
function OrbitalLoaderStub({ size }: { size: 'sm' | 'md' }) {
  const dim = size === 'sm' ? 32 : 44;
  return (
    <div
      style={{
        width: dim,
        height: dim,
        borderRadius: '50%',
        border: '1.5px solid var(--chat-acc-bdr)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <div
        style={{
          width: size === 'sm' ? 5 : 6,
          height: size === 'sm' ? 5 : 6,
          borderRadius: '50%',
          background: 'var(--chat-acc)',
        }}
      />
    </div>
  );
}
```

---

## Task 12 — ToolRow (with inline orbital spinner CSS)

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/tool-row.tsx`

This is the most important component in the page. Every agent tool invocation renders as one of these rows.

### Steps

- [ ] 1. Create the component. The inline orbital spinner (`.tool-spin`) is defined via a `<style>` tag injected once via a module-level constant and placed in the run shell. The `canvasViewId` drives the pop-out button visibility and canvas-active border:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/tool-row.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Core tool call row: icon + name + arg + status badge + pop-out button.
 * Left border colour reflects tool status; pop-out button opens the canvas view.
 */

import { useRunContext } from '../_context/run-context';
import type { ToolCall } from '../_types';

// Border colours by status
const BORDER_COLORS = {
  running: 'oklch(50% 0.1 80 / 0.6)',
  done: 'oklch(45% 0.1 140 / 0.5)',
  error: 'oklch(45% 0.15 25 / 0.5)',
} as const;

// Inline orbital spinner keyframes — injected once per page
export const TOOL_SPIN_CSS = `
.tool-spin {
  display: inline-flex; align-items: center; justify-content: center;
  width: 14px; height: 14px; position: relative; flex-shrink: 0;
  vertical-align: middle;
}
.tool-spin::before {
  content: '';
  position: absolute; width: 10px; height: 10px;
  border-radius: 50%;
  border: 1px solid oklch(72% 0.17 8 / 0.25);
}
.tool-spin::after {
  content: '';
  position: absolute; width: 4px; height: 4px;
  border-radius: 50%;
  background: var(--chat-degraded);
  top: 50%; left: 50%; margin: -2px 0 0 -2px;
  animation: tool-orb 1.4s linear infinite;
  transform-origin: 0 0;
}
@keyframes tool-orb {
  from { transform: rotate(0deg) translateX(5px); }
  to   { transform: rotate(360deg) translateX(5px); }
}
`;

// Pop-out icon SVG
function PopoutIcon() {
  return (
    <svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden>
      <polyline points="15 3 21 3 21 9" />
      <polyline points="9 21 3 21 3 15" />
      <line x1="21" y1="3" x2="14" y2="10" />
      <line x1="3" y1="21" x2="10" y2="14" />
    </svg>
  );
}

export function ToolRow({ tool }: { tool: ToolCall }) {
  const { canvasState, openCanvas } = useRunContext();

  const isCanvasActive =
    tool.canvasViewId !== undefined && canvasState.activeTabId === tool.canvasViewId;

  const borderColor = isCanvasActive
    ? 'var(--chat-acc)'
    : BORDER_COLORS[tool.status] ?? 'var(--chat-bds)';

  function handlePopout(e: React.MouseEvent) {
    e.stopPropagation();
    if (tool.canvasViewId) openCanvas(tool.canvasViewId);
  }

  return (
    <div
      className="tool-row-container"
      style={{
        display: 'flex',
        alignItems: 'stretch',
        margin: '3px 0',
        borderLeft: `2px solid ${borderColor}`,
        transition: 'border-color 0.15s',
        position: 'relative',
      }}
    >
      {/* Hover style — via CSS class */}
      <style>{`
        .tool-row-container .popout-btn { opacity: 0; transition: opacity 0.15s; }
        .tool-row-container:hover .popout-btn { opacity: 1; }
      `}</style>

      {/* Inner content */}
      <div style={{ flex: 1, paddingLeft: 10, minWidth: 0 }}>
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 7,
            padding: '3px 0',
            userSelect: 'none',
          }}
        >
          {/* Emoji icon */}
          <span style={{ fontSize: 12, opacity: 0.8, flexShrink: 0 }}>{tool.icon}</span>

          {/* Tool name */}
          <span
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 12,
              color: 'var(--chat-muted)',
              flexShrink: 0,
            }}
          >
            {tool.name}
          </span>

          {/* Argument (truncated) */}
          <span
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 11.5,
              color: 'var(--chat-subtle)',
              flex: 1,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
              minWidth: 0,
            }}
          >
            {tool.arg}
          </span>

          {/* Status badge */}
          <span
            style={{
              fontSize: 11,
              display: 'flex',
              alignItems: 'center',
              gap: 3,
              flexShrink: 0,
              color:
                tool.status === 'running'
                  ? 'var(--chat-degraded)'
                  : tool.status === 'done'
                    ? 'var(--chat-healthy)'
                    : 'var(--chat-error)',
            }}
          >
            {tool.status === 'running' ? (
              <>
                <span className="tool-spin" aria-label="Running" />
                running
              </>
            ) : (
              tool.result
            )}
          </span>

          {/* Pop-out button (⤢) */}
          {tool.canvasViewId && (
            <button
              type="button"
              className="popout-btn"
              onClick={handlePopout}
              style={{
                width: 22,
                height: 22,
                flexShrink: 0,
                background: isCanvasActive ? 'var(--chat-acc-dim)' : 'transparent',
                border: `1px solid ${isCanvasActive ? 'var(--chat-acc-bdr)' : 'transparent'}`,
                borderRadius: 3,
                color: isCanvasActive ? 'var(--chat-acc)' : 'var(--chat-bds)',
                cursor: 'pointer',
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
                // Always visible when canvas-active
                ...(isCanvasActive ? { opacity: 1 } : {}),
              }}
              title="Open in canvas"
              aria-label={`Open ${tool.name} result in canvas`}
            >
              <PopoutIcon />
            </button>
          )}
        </div>
      </div>
    </div>
  );
}
```

- [ ] 2. Inject the `TOOL_SPIN_CSS` keyframes in `run-shell.tsx`. Open `run-shell.tsx` and add the following to `RunPageLayout`, before the `<RunTopbar />`:

```typescript
import { TOOL_SPIN_CSS } from './tool-row';

function RunPageLayout() {
  return (
    <>
      <style>{TOOL_SPIN_CSS}</style>
      <RunTopbar />
      {/* ... rest unchanged ... */}
    </>
  );
}
```

---

## Task 13 — ApprovalPrompt

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/approval-prompt.tsx`

### Steps

- [ ] 1. Create the approval action card:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/approval-prompt.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Inline approval card — approve, reject, or edit before the agent executes
 * a potentially impactful command.
 */

import { useState } from 'react';

interface ApprovalPromptProps {
  id: string;
  title: string;
  description: string;
  command: string;
}

type Resolution = 'approved' | 'rejected';

export function ApprovalPrompt({ id, title, description, command }: ApprovalPromptProps) {
  const [resolution, setResolution] = useState<Resolution | null>(null);

  if (resolution) {
    return (
      <div
        style={{
          margin: '10px 0',
          border: '1px solid var(--chat-bds)',
          borderRadius: 8,
          padding: '8px 12px',
          fontSize: 12,
          color: 'var(--chat-subtle)',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <span style={{ color: resolution === 'approved' ? 'var(--chat-healthy)' : 'var(--chat-error)' }}>
          {resolution === 'approved' ? '✓' : '✗'}
        </span>
        {resolution === 'approved' ? 'Approved' : 'Rejected'}: {title}
      </div>
    );
  }

  return (
    <div
      style={{
        margin: '10px 0',
        border: '1px solid var(--chat-acc-bdr)',
        borderRadius: 8,
        background: 'var(--chat-acc-dim)',
        overflow: 'hidden',
      }}
    >
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '8px 12px',
          borderBottom: '1px solid var(--chat-acc-bdr)',
          fontSize: 12.5,
          fontWeight: 500,
        }}
      >
        <span style={{ color: 'var(--chat-acc)' }}>◈</span>
        {title}
      </div>

      {/* Body */}
      <div style={{ padding: '9px 12px', fontSize: 12, color: 'var(--chat-muted)' }}>
        <p style={{ marginBottom: 8 }}>{description}</p>
        <code
          style={{
            display: 'block',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11.5,
            color: 'var(--chat-fg)',
            background: 'var(--chat-deep)',
            border: '1px solid var(--chat-bds)',
            borderRadius: 4,
            padding: '6px 10px',
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-all',
          }}
        >
          {command}
        </code>
      </div>

      {/* Actions */}
      <div
        style={{
          display: 'flex',
          gap: 7,
          padding: '9px 12px',
          borderTop: '1px solid var(--chat-acc-bdr)',
        }}
      >
        {/* Approve */}
        <button
          type="button"
          onClick={() => setResolution('approved')}
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'var(--chat-acc)',
            border: '1px solid var(--chat-acc)',
            color: 'oklch(9% 0.005 248)',
          }}
        >
          Approve
        </button>

        {/* Edit (stub — no action in mock) */}
        <button
          type="button"
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'transparent',
            border: '1px solid var(--chat-bds)',
            color: 'var(--chat-muted)',
          }}
        >
          Edit
        </button>

        {/* Reject */}
        <button
          type="button"
          onClick={() => setResolution('rejected')}
          style={{
            padding: '5px 13px',
            borderRadius: 4,
            fontSize: 12,
            cursor: 'pointer',
            fontWeight: 500,
            background: 'transparent',
            border: '1px solid oklch(38% 0.15 25)',
            color: 'var(--chat-error)',
          }}
        >
          Reject
        </button>
      </div>
    </div>
  );
}
```

---

## Task 14 — AgentTurn (composes all agent sub-components)

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-turn.tsx`

### Steps

- [ ] 1. Create the agent turn. Renders byline + optional thinking block + elements in document order:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-turn.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Agent response turn: branded byline + thinking block + interleaved content elements.
 */

import type { AgentTurn } from '../_types';
import { ThinkingBlock } from './thinking-block';
import { AgentTextBlock } from './agent-text-block';
import { ToolRow } from './tool-row';
import { ApprovalPrompt } from './approval-prompt';

function OrbitalMark() {
  return (
    <svg width="9" height="9" viewBox="0 0 12 12" fill="none" aria-hidden>
      <circle cx="6" cy="6" r="2.5" fill="currentColor" />
      <circle cx="6" cy="6" r="5" stroke="currentColor" strokeWidth="1.2" fill="none" opacity=".5" />
    </svg>
  );
}

export function AgentTurnItem({ turn }: { turn: AgentTurn }) {
  return (
    <div style={{ padding: '12px 20px 4px' }}>
      {/* Byline */}
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 10 }}>
        <div
          style={{
            width: 18,
            height: 18,
            background: 'var(--chat-acc-dim)',
            border: '1.5px solid var(--chat-acc-bdr)',
            borderRadius: 3,
            color: 'var(--chat-acc)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            flexShrink: 0,
          }}
          aria-hidden
        >
          <OrbitalMark />
        </div>
        <span style={{ fontSize: 12, color: 'var(--chat-muted)', fontWeight: 500 }}>
          Orbit AI
        </span>
        <span
          style={{
            fontSize: 11,
            color: 'var(--chat-subtle)',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          }}
        >
          {turn.timestamp}
        </span>
      </div>

      {/* Content area — indented 26px to align with byline text */}
      <div style={{ paddingLeft: 26 }}>
        {/* Thinking block (before elements, if present) */}
        {turn.thinking && <ThinkingBlock thinking={turn.thinking} />}

        {/* Element flow */}
        {turn.elements.map((el, i) => {
          if (el.kind === 'text') {
            return <AgentTextBlock key={i} content={el.content} />;
          }
          if (el.kind === 'tool') {
            return <ToolRow key={el.tool.id} tool={el.tool} />;
          }
          if (el.kind === 'approval') {
            return (
              <ApprovalPrompt
                key={el.id}
                id={el.id}
                title={el.title}
                description={el.description}
                command={el.command}
              />
            );
          }
          return null;
        })}
      </div>
    </div>
  );
}
```

### Verification checkpoint

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0.

### Commit

```bash
git add frontend/src/app/\(chat\)
git commit -m "feat(agents/run-chat): add TopBar, ContextStrip, ChatThread, HumanTurn, AgentTurn and sub-components"
```

---

## Task 15 — CanvasPanel (tabs + view renderer + activity log)

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/canvas-panel.tsx`

This is the most complex component. Break it into three logical sections: the panel shell (slide animation), the view renderer (code/diff/terminal), and the activity log.

### Steps

- [ ] 1. Create the canvas panel. All sub-parts are defined in the same file to avoid excessive file fragmentation for this page-private component:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/canvas-panel.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Slide-in canvas panel (0 → 520px). Shows code, diff, and terminal views
 * in tabs, with an activity log at the bottom.
 */

import { useRunContext } from '../_context/run-context';
import type { CanvasView, CanvasTab, DiffLine, TerminalLine, Turn } from '../_types';

// ── Canvas tab ────────────────────────────────────────────────────────────────

function CanvasTabItem({
  tab,
  isActive,
  onClick,
}: {
  tab: CanvasTab;
  isActive: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 5,
        padding: '0 12px',
        height: 36,
        flexShrink: 0,
        fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
        fontSize: 11.5,
        color: isActive ? 'var(--chat-fg)' : 'var(--chat-subtle)',
        cursor: 'pointer',
        borderRight: '1px solid var(--chat-bds)',
        background: isActive ? 'var(--chat-bg)' : 'transparent',
        border: 'none',
        borderRight: '1px solid var(--chat-bds)',
        position: 'relative',
        whiteSpace: 'nowrap',
      }}
    >
      {tab.ext && (
        <span style={{ color: 'var(--chat-acc)', opacity: 0.7 }}>{tab.ext}</span>
      )}
      {tab.label}
      {tab.modified && (
        <span style={{ color: 'var(--chat-degraded)', fontSize: 9 }}>●</span>
      )}
      {/* Active accent bottom border */}
      {isActive && (
        <span
          style={{
            position: 'absolute',
            bottom: 0,
            left: 0,
            right: 0,
            height: 2,
            background: 'var(--chat-acc)',
          }}
          aria-hidden
        />
      )}
    </button>
  );
}

// ── Code view ─────────────────────────────────────────────────────────────────

function CodeView({ view }: { view: CanvasView }) {
  const lines = (view.code ?? '').split('\n');
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        {view.subtitle && (
          <span style={{ color: 'var(--chat-subtle)' }}>{view.subtitle}</span>
        )}
        <span style={{ color: 'var(--chat-muted)' }}>{view.title}</span>
        <span style={{ flex: 1 }} />
        {view.lineCount && (
          <span style={{ color: 'var(--chat-subtle)', fontSize: 11 }}>
            {view.lineCount} lines
          </span>
        )}
      </div>

      {/* Code block with line numbers */}
      <div
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--chat-deep)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.7,
        }}
      >
        {lines.map((line, i) => (
          <div key={i} style={{ display: 'flex' }}>
            <span
              style={{
                width: 36,
                paddingRight: 8,
                textAlign: 'right',
                color: 'var(--chat-bds)',
                fontSize: 11,
                userSelect: 'none',
                flexShrink: 0,
              }}
            >
              {i + 1}
            </span>
            <span
              style={{
                padding: '0 8px 0 4px',
                whiteSpace: 'pre',
                color: 'var(--chat-muted)',
                flex: 1,
              }}
            >
              {line}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

// ── Diff view ─────────────────────────────────────────────────────────────────

function DiffLineRow({ line }: { line: DiffLine }) {
  const isAdd = line.kind === 'add';
  const isDel = line.kind === 'del';
  const sigil = isAdd ? '+' : isDel ? '-' : ' ';

  return (
    <div
      style={{
        display: 'flex',
        background: isAdd
          ? 'oklch(30% 0.1 140 / 0.13)'
          : isDel
            ? 'oklch(25% 0.1 25 / 0.13)'
            : 'transparent',
      }}
    >
      {/* Old line number */}
      <span
        style={{
          width: 36,
          padding: '0 6px',
          textAlign: 'right',
          fontSize: 11,
          userSelect: 'none',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          color: isDel
            ? 'oklch(42% 0.12 25 / 0.5)'
            : isAdd
              ? 'var(--chat-bds)'
              : 'var(--chat-bds)',
        }}
      >
        {line.oldNum ?? ''}
      </span>
      {/* New line number */}
      <span
        style={{
          width: 36,
          padding: '0 6px',
          textAlign: 'right',
          fontSize: 11,
          userSelect: 'none',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'flex-end',
          color: isAdd
            ? 'oklch(45% 0.1 140 / 0.5)'
            : isDel
              ? 'var(--chat-bds)'
              : 'var(--chat-bds)',
        }}
      >
        {line.newNum ?? ''}
      </span>
      {/* Sigil */}
      <span
        style={{
          width: 16,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          fontSize: 13,
          flexShrink: 0,
          color: isAdd
            ? 'var(--chat-healthy)'
            : isDel
              ? 'var(--chat-error)'
              : 'transparent',
        }}
      >
        {sigil}
      </span>
      {/* Content */}
      <span
        style={{
          padding: '0 8px 0 4px',
          whiteSpace: 'pre',
          flex: 1,
          color: isAdd
            ? 'oklch(82% 0.08 140)'
            : isDel
              ? 'oklch(72% 0.07 25)'
              : 'var(--chat-subtle)',
        }}
      >
        {line.content}
      </span>
    </div>
  );
}

function DiffView({ view }: { view: CanvasView }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        {view.subtitle && (
          <span style={{ color: 'var(--chat-subtle)' }}>{view.subtitle}</span>
        )}
        <span style={{ color: 'var(--chat-muted)' }}>{view.title}</span>
        <span style={{ flex: 1 }} />
        {/* Diff stat */}
        <span style={{ display: 'flex', gap: 6, fontSize: 11 }}>
          {view.addCount !== undefined && (
            <span style={{ color: 'var(--chat-healthy)' }}>+{view.addCount}</span>
          )}
          {view.delCount !== undefined && (
            <span style={{ color: 'var(--chat-error)' }}>-{view.delCount}</span>
          )}
        </span>
        <button
          type="button"
          style={{
            padding: '2px 8px',
            background: 'var(--chat-surf2)',
            border: '1px solid var(--chat-bds)',
            borderRadius: 3,
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11,
            color: 'var(--chat-subtle)',
            cursor: 'pointer',
          }}
        >
          copy
        </button>
      </div>

      {/* Diff lines */}
      <div
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'var(--chat-deep)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.7,
        }}
      >
        {(view.diffLines ?? []).map((line, i) => (
          <DiffLineRow key={i} line={line} />
        ))}
      </div>
    </div>
  );
}

// ── Terminal view ─────────────────────────────────────────────────────────────

function TerminalLineRow({ line }: { line: TerminalLine }) {
  const colorMap: Record<TerminalLine['kind'], string> = {
    normal: 'var(--chat-muted)',
    error: 'var(--chat-error)',
    success: 'var(--chat-healthy)',
    dim: 'var(--chat-subtle)',
  };
  return (
    <div style={{ color: colorMap[line.kind] }}>{line.content}</div>
  );
}

function TerminalView({ view }: { view: CanvasView }) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'auto' }}>
      {/* Header */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '7px 14px',
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 11.5,
        }}
      >
        <span style={{ color: 'var(--chat-subtle)' }}>terminal</span>
        <span style={{ flex: 1 }} />
        {view.command && (
          <span style={{ color: 'var(--chat-muted)', fontSize: 11 }}>{view.command}</span>
        )}
        {view.exitCode !== undefined && (
          <span
            style={{
              fontSize: 11,
              color: view.exitCode === 0 ? 'var(--chat-healthy)' : 'var(--chat-error)',
            }}
          >
            exit {view.exitCode}
          </span>
        )}
      </div>

      {/* Terminal content */}
      <div
        style={{
          flex: 1,
          overflow: 'auto',
          background: 'oklch(10% 0.004 248)',
          fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          fontSize: 12.5,
          lineHeight: 1.65,
          padding: '12px 14px',
          color: 'var(--chat-muted)',
        }}
      >
        {(view.output ?? []).map((line, i) => (
          <TerminalLineRow key={i} line={line} />
        ))}
      </div>
    </div>
  );
}

// ── Canvas view renderer (dispatches by type) ─────────────────────────────────

function CanvasViewRenderer({ view }: { view: CanvasView }) {
  if (view.type === 'code') return <CodeView view={view} />;
  if (view.type === 'diff') return <DiffView view={view} />;
  if (view.type === 'terminal') return <TerminalView view={view} />;
  return null;
}

// ── Activity log ──────────────────────────────────────────────────────────────

function ActivityLog({ turns, isLive }: { turns: Turn[]; isLive: boolean }) {
  // Collect all tool calls in document order
  const allTools = turns
    .filter((t) => t.type === 'agent')
    .flatMap((t) => {
      if (t.type !== 'agent') return [];
      return t.elements.flatMap((el) => (el.kind === 'tool' ? [el.tool] : []));
    });

  return (
    <div
      style={{
        borderTop: '1px solid var(--chat-bds)',
        background: 'var(--chat-deep)',
        flexShrink: 0,
        maxHeight: 110,
        overflowY: 'auto',
        paddingBottom: 6,
      }}
    >
      {/* Log header */}
      <div
        style={{
          padding: '0 14px 5px',
          paddingTop: 6,
          fontSize: 10,
          textTransform: 'uppercase',
          letterSpacing: '0.07em',
          color: 'var(--chat-subtle)',
          borderBottom: '1px solid var(--chat-bds)',
          marginBottom: 3,
          display: 'flex',
          gap: 8,
          alignItems: 'center',
        }}
      >
        Agent activity
        {isLive && (
          <span style={{ color: 'var(--chat-degraded)', display: 'flex', alignItems: 'center', gap: 4 }}>
            <span className="tool-spin" aria-hidden />
            live
          </span>
        )}
      </div>

      {/* Log rows */}
      {allTools.map((tool) => (
        <div
          key={tool.id}
          style={{
            display: 'flex',
            alignItems: 'center',
            gap: 7,
            padding: '1px 14px',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            fontSize: 11,
            color: 'var(--chat-subtle)',
          }}
        >
          <span style={{ color: 'var(--chat-bds)', fontSize: 10, flexShrink: 0 }}>
            {/* Timestamp placeholder — in mock we don't track per-tool timestamps */}
            --:--
          </span>
          <span style={{ color: 'var(--chat-muted)', flexShrink: 0 }}>{tool.name}</span>
          <span
            style={{
              flex: 1,
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {tool.arg}
          </span>
          {/* Result icon */}
          {tool.status === 'running' && (
            <span className="tool-spin" aria-label="Running" />
          )}
          {tool.status === 'done' && (
            <span style={{ color: 'var(--chat-healthy)' }}>✓</span>
          )}
          {tool.status === 'error' && (
            <span style={{ color: 'var(--chat-error)' }}>✗</span>
          )}
        </div>
      ))}
    </div>
  );
}

// ── CanvasPanel ───────────────────────────────────────────────────────────────

export function CanvasPanel() {
  const { canvasState, views, turns, runState, closeCanvas, switchTab } = useRunContext();
  const { isOpen, tabs, activeTabId } = canvasState;

  const activeView = activeTabId ? views[activeTabId] : null;
  const isLive = runState === 'active' || runState === 'starting';

  return (
    <div
      style={{
        width: isOpen ? 520 : 0,
        flexShrink: 0,
        display: 'flex',
        flexDirection: 'column',
        background: 'var(--chat-deep)',
        borderLeft: isOpen ? '1px solid var(--chat-bds)' : '1px solid transparent',
        overflow: 'hidden',
        transition: 'width 0.25s ease, border-color 0.25s ease',
      }}
      aria-hidden={!isOpen}
    >
      {/* Canvas topbar — 36px */}
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 0,
          height: 36,
          flexShrink: 0,
          background: 'var(--chat-surf)',
          borderBottom: '1px solid var(--chat-bds)',
        }}
      >
        {/* Scrollable tab bar */}
        <div
          style={{
            display: 'flex',
            alignItems: 'stretch',
            flex: 1,
            overflowX: 'auto',
            // Hide scrollbar
            scrollbarWidth: 'none',
          }}
        >
          {tabs.map((tab) => (
            <CanvasTabItem
              key={tab.id}
              tab={tab}
              isActive={activeTabId === tab.id}
              onClick={() => switchTab(tab.id)}
            />
          ))}
        </div>

        {/* Close button */}
        <button
          type="button"
          onClick={closeCanvas}
          style={{
            width: 24,
            height: 24,
            flexShrink: 0,
            background: 'transparent',
            border: 'none',
            borderRadius: 4,
            color: 'var(--chat-subtle)',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            fontSize: 16,
            marginRight: 8,
          }}
          aria-label="Close canvas"
        >
          ×
        </button>
      </div>

      {/* Canvas content — flex 1 */}
      <div style={{ flex: 1, overflow: 'auto', display: 'flex', flexDirection: 'column' }}>
        {activeView ? (
          <CanvasViewRenderer view={activeView} />
        ) : (
          <div
            style={{
              flex: 1,
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'center',
              color: 'var(--chat-subtle)',
              fontSize: 12,
            }}
          >
            Click ⤢ on any tool row to open it here
          </div>
        )}
      </div>

      {/* Activity log — max 110px */}
      <ActivityLog turns={turns} isLive={isLive} />
    </div>
  );
}
```

**Note on the duplicate `border` + `borderRight` in `CanvasTabItem`:** TypeScript will warn that `border` and `borderRight` conflict in the style object. Fix this by removing the `border: 'none'` and relying solely on `borderRight` for the tab separator. The button's default UA border should be reset via a global `button { border: none }` or by including `all: unset` style. Prefer the explicit approach: remove `border: 'none'` and add `outline: 'none'` only, keeping `borderRight`.

---

## Task 16 — InputArea

### Files

- `src/app/(chat)/agents/[slug]/runs/[runId]/_components/input-area.tsx`

### Steps

- [ ] 1. Create the input area. The textarea auto-resizes on input. Enter sends a new human message (appended to turns), Shift+Enter inserts a newline:

**`src/app/(chat)/agents/[slug]/runs/[runId]/_components/input-area.tsx`**

```typescript
'use client';

/**
 * @tier 3 — run-chat-scoped.
 * Fixed bottom input area: slash command chips + auto-resize textarea + send button.
 */

import { useRef, useState, useCallback } from 'react';
import { useRunContext } from '../_context/run-context';

const SLASH_COMMANDS = ['/continue', '/approve', '/reject', '/checkpoint', '/explain'] as const;

// Send arrow icon
function SendIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5" aria-hidden>
      <line x1="22" y1="2" x2="11" y2="13" />
      <polygon points="22 2 15 22 11 13 2 9 22 2" />
    </svg>
  );
}

export function InputArea() {
  const { addHumanMessage } = useRunContext();
  const [value, setValue] = useState('');
  const textareaRef = useRef<HTMLTextAreaElement>(null);

  const handleInput = useCallback(() => {
    const el = textareaRef.current;
    if (!el) return;
    el.style.height = 'auto';
    el.style.height = `${Math.min(el.scrollHeight, 110)}px`;
  }, []);

  const handleSend = useCallback(() => {
    const content = value.trim();
    if (!content) return;
    addHumanMessage(content);
    setValue('');
    // Reset textarea height
    const el = textareaRef.current;
    if (el) el.style.height = 'auto';
  }, [value, addHumanMessage]);

  const handleKeyDown = useCallback(
    (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
      if (e.key === 'Enter' && !e.shiftKey) {
        e.preventDefault();
        handleSend();
      }
    },
    [handleSend],
  );

  return (
    <div
      style={{
        borderTop: '1px solid var(--chat-bds)',
        background: 'var(--chat-surf)',
        flexShrink: 0,
        padding: '10px 16px 12px',
      }}
    >
      {/* Slash command chips */}
      <div
        style={{
          display: 'flex',
          gap: 5,
          marginBottom: 8,
          overflowX: 'auto',
          scrollbarWidth: 'none',
        }}
      >
        {SLASH_COMMANDS.map((cmd) => (
          <button
            key={cmd}
            type="button"
            onClick={() => setValue(cmd + ' ')}
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 11,
              padding: '2px 8px',
              background: 'var(--chat-surf2)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 20,
              color: 'var(--chat-subtle)',
              cursor: 'pointer',
              whiteSpace: 'nowrap',
            }}
          >
            {cmd}
          </button>
        ))}
      </div>

      {/* Textarea wrapper */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          gap: 8,
          background: 'var(--chat-surf2)',
          border: '1px solid var(--chat-bds)',
          borderRadius: 8,
          padding: '9px 10px',
        }}
        // Focus ring applied via className focus-within trick would need a stylesheet.
        // For now we use onFocus/onBlur state if needed — kept minimal for mock.
      >
        {/* Auto-resize textarea */}
        <textarea
          ref={textareaRef}
          value={value}
          onChange={(e) => setValue(e.target.value)}
          onInput={handleInput}
          onKeyDown={handleKeyDown}
          placeholder="Message Orbit AI…"
          rows={1}
          style={{
            flex: 1,
            background: 'transparent',
            border: 'none',
            outline: 'none',
            resize: 'none',
            fontFamily: 'ui-sans-serif, system-ui, -apple-system, sans-serif',
            fontSize: 13.5,
            color: 'var(--chat-fg)',
            lineHeight: 1.5,
            minHeight: 20,
            maxHeight: 110,
            overflowY: 'auto',
          }}
        />

        {/* @ file button */}
        <button
          type="button"
          style={{
            fontSize: 11,
            padding: '3px 7px',
            background: 'transparent',
            border: '1px solid var(--chat-bds)',
            borderRadius: 4,
            color: 'var(--chat-subtle)',
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
            cursor: 'pointer',
            flexShrink: 0,
          }}
        >
          @ file
        </button>

        {/* Send button */}
        <button
          type="button"
          onClick={handleSend}
          disabled={!value.trim()}
          style={{
            width: 29,
            height: 29,
            background: value.trim() ? 'var(--chat-acc)' : 'var(--chat-surf3)',
            border: 'none',
            borderRadius: 4,
            cursor: value.trim() ? 'pointer' : 'not-allowed',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'oklch(9% 0.005 248)',
            flexShrink: 0,
            transition: 'background 0.15s',
          }}
          aria-label="Send message"
        >
          <SendIcon />
        </button>
      </div>

      {/* Footer hints */}
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          marginTop: 7,
          fontSize: 11,
          color: 'var(--chat-bds)',
        }}
      >
        <span>
          <kbd
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 10,
              padding: '1px 4px',
              background: 'var(--chat-surf3)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 3,
              color: 'var(--chat-subtle)',
            }}
          >
            ↵
          </kbd>{' '}
          send ·{' '}
          <kbd
            style={{
              fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
              fontSize: 10,
              padding: '1px 4px',
              background: 'var(--chat-surf3)',
              border: '1px solid var(--chat-bds)',
              borderRadius: 3,
              color: 'var(--chat-subtle)',
            }}
          >
            ⇧↵
          </kbd>{' '}
          newline · / commands
        </span>
        <span
          style={{
            fontFamily: "'Cascadia Code', 'Fira Code', ui-monospace, monospace",
          }}
        >
          42,187 tokens
        </span>
      </div>
    </div>
  );
}
```

---

## Task 17 — Final wiring + verification

### Steps

- [ ] 1. Review all import paths. Each `_components` file must import only from:
  - `'../_context/run-context'` for context
  - `'../_types'` for types
  - `'../_hooks/use-run'` (only from context file)
  - `'@orbit/terra'` for Terra components
  - Sibling `_components` as relative imports (e.g., `'./tool-row'`)
  - `'next/link'` for navigation
  - `'@/lib/routing/routes'` for route constants

- [ ] 2. Verify the `APP_ROUTES.agents.detail` function exists. This is wired in Plan 1. If Plan 1 is not merged yet, replace the import with a direct string: `` `/agents/${slug}` ``.

- [ ] 3. Audit all components for `'use client'` directives. Every file in `_components/`, `_context/`, and `_hooks/` must have `'use client'` as the first line (before the JSDoc comment). The exception is `_types/index.ts` — types-only files are server-safe and need no directive.

- [ ] 4. Fix the duplicate `border`/`borderRight` issue in `CanvasTabItem`. The style object has both `border: 'none'` and `borderRight: '1px solid var(--chat-bds)'`. Remove the `border: 'none'` and instead add `background: 'transparent'` and no default `border`. Add the `borderRight` only.

- [ ] 5. Run the full verification:

```bash
cd /Users/justin/Development/orbit/orbit/frontend && pnpm tsc --noEmit && pnpm lint
```

Both must exit 0 with zero errors before committing.

Common TypeScript errors to anticipate and fix:

- `Type 'string' is not assignable to type 'CSSProperties'` on the `as React.CSSProperties` cast in layout.tsx — this cast is required and correct; custom CSS properties need the cast.
- `'use client'` missing on any component that uses `useState`, `useEffect`, `useRef`, or `useContext`.
- Unused imports from intermediate refactoring — remove them.
- The `Turn` type guard `turn.type !== 'agent'` in `ActivityLog` — TypeScript needs to narrow the union; the filter + flatMap pattern handles this but verify the types line up.

- [ ] 6. Navigate to `http://localhost:3000/agents/my-agent/runs/run-abc123` after starting the dev server. Verify:
  - Full-viewport dark background (no sidebar, no header)
  - Topbar with back arrow, logo, model pill with pulsing dot, session ID, context meter
  - Context strip with three file chips
  - Human message bubble (right-aligned, accent bg)
  - Agent turn with thinking block (click to expand), tool rows, approval prompt
  - Tool row pop-out buttons (hover to reveal)
  - Click a pop-out button → canvas slides in from the right
  - Canvas shows the correct view (code, diff, or terminal)
  - Canvas tabs accumulate as you open more views
  - Canvas close × button collapses it
  - Type in the input and press Enter → new human bubble appended
  - Approval Approve/Reject buttons → resolved state shown

```bash
make dev
```

### Commit

```bash
git add frontend/src/app/\(chat\)
git commit -m "feat(agents/run-chat): complete run chat page — canvas panel, input area, and full layout wiring"
```

---

## File inventory (complete list)

All files created or modified by this plan:

```
NEW  src/app/(chat)/layout.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/layout.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/page.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_types/index.ts
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_hooks/use-run.ts
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_context/run-context.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-shell.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/run-topbar.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/context-strip.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/chat-thread.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/human-turn.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-turn.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/agent-text-block.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/tool-row.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/thinking-block.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/approval-prompt.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/canvas-panel.tsx
NEW  src/app/(chat)/agents/[slug]/runs/[runId]/_components/input-area.tsx
```

No existing files are modified. All files are entirely new under the `(chat)` route group.

---

## Dependencies and sequencing

This plan has hard dependencies:

1. **Plan 1 must be merged first** for `OrbitalLoader` from `@orbit/terra` and for `APP_ROUTES.agents.detail(slug)`. If building in parallel, use the stubs documented in Tasks 11 and 17 until Plan 1 merges.

2. **Plans 2 and 3 are independent** — they add pages under `(dashboard)/agents/` and do not touch the `(chat)` route group.

Recommended merge order: Plan 1 → Plan 2 → Plan 3 → Plan 4.

---

## Verification checklist

Before marking this plan complete:

- [ ] `pnpm tsc --noEmit` exits 0 from `frontend/`
- [ ] `pnpm lint` exits 0 from `frontend/`
- [ ] Page renders at `/agents/any-slug/runs/any-run-id` without sidebar/header
- [ ] All chat design tokens visible (dark canvas background, pink accent)
- [ ] Human message bubble is right-aligned with accent border
- [ ] Agent turn shows byline, collapsible thinking block, tool rows, approval card
- [ ] Clicking a tool row's ⤢ opens the canvas panel
- [ ] Canvas tabs accumulate; switching tabs updates the view
- [ ] Diff view shows green/red line highlighting
- [ ] Terminal view shows coloured output lines on very dark background
- [ ] Activity log shows all tool calls in order
- [ ] Canvas × closes the panel; Canvas button in topbar reopens it
- [ ] Slash command chips populate the input on click
- [ ] Enter sends a new human message; Shift+Enter inserts newline
- [ ] Input send button is disabled when textarea is empty
- [ ] Page is fully responsive at `md` breakpoint (canvas hidden below `md`)
