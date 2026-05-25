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
