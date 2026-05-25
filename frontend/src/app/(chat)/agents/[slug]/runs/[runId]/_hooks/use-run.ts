'use client';

import { useState, useEffect, useRef } from 'react';
import type { RunChatState, Turn, AgentTurn, AgentElement, CanvasState, RunState } from '../_types';

// ── In-progress turn accumulator ─────────────────────────────────────────────

interface InProgress {
  turn: AgentTurn;
  thinkingContent: string;
}

// ── Event payload shapes ──────────────────────────────────────────────────────

interface TextDeltaPayload {
  content: string;
}
interface ThinkingDeltaPayload {
  content: string;
}
interface ToolUsePayload {
  id: string;
  name: string;
  input?: Record<string, unknown>;
}
interface ToolResultPayload {
  tool_use_id: string;
  content?: unknown;
  is_error?: boolean;
}
interface RunStatePayload {
  state: RunState;
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
  const [runState, setRunState] = useState<RunState>('pending');
  const [agentName, setAgentName] = useState('Agent');
  const [model, setModel] = useState('anthropic/claude-sonnet-4-6');
  const [committedTurns, setCommittedTurns] = useState<Turn[]>([]);
  const [inProgressTurn, setInProgressTurn] = useState<AgentTurn | null>(null);
  const [canvasState, setCanvasState] = useState<CanvasState>({
    isOpen: false,
    tabs: [],
    activeTabId: null,
  });

  const inProgressRef = useRef<InProgress | null>(null);

  // Fetch initial run + agent type metadata.
  useEffect(() => {
    if (!runId) return;
    void fetch(`/api/agents/runs/${runId}`)
      .then((r) => r.json())
      .then(
        (body: {
          data?: { state: RunState };
          agent_type?: { name?: string; model_id?: string };
        }) => {
          if (body.data?.state) {
            setRunState(body.data.state);
          }
          if (body.agent_type) {
            setAgentName(body.agent_type.name ?? 'Agent');
            setModel(body.agent_type.model_id ?? 'anthropic/claude-sonnet-4-6');
          }
        },
      )
      .catch(() => undefined);
  }, [runId]);

  // Subscribe to the SSE event stream for this run.
  useEffect(() => {
    if (!runId) return;

    const es = new EventSource(`/api/agents/runs/${runId}/events`);

    function flushInProgress(): void {
      if (!inProgressRef.current) return;
      const completed = inProgressRef.current.turn;
      inProgressRef.current = null;
      setCommittedTurns((prev) => [...prev, completed]);
      setInProgressTurn(null);
    }

    function ensureInProgress(): AgentTurn {
      if (!inProgressRef.current) {
        const turn: AgentTurn = {
          type: 'agent',
          id: `a-${Date.now()}`,
          timestamp: new Date().toTimeString().slice(0, 8),
          elements: [],
        };
        inProgressRef.current = { turn, thinkingContent: '' };
      }
      return inProgressRef.current.turn;
    }

    function updateInProgress(updater: (t: AgentTurn) => AgentTurn): void {
      if (!inProgressRef.current) return;
      inProgressRef.current = {
        ...inProgressRef.current,
        turn: updater(inProgressRef.current.turn),
      };
      setInProgressTurn({ ...inProgressRef.current.turn });
    }

    es.addEventListener('text_delta', (e: MessageEvent) => {
      const payload = safeParse<TextDeltaPayload>(e.data);
      if (!payload) return;
      ensureInProgress();
      updateInProgress((turn) => {
        const elements = [...turn.elements];
        const last = elements[elements.length - 1];
        if (last?.kind === 'text') {
          elements[elements.length - 1] = { kind: 'text', content: last.content + payload.content };
        } else {
          elements.push({ kind: 'text', content: payload.content });
        }
        return { ...turn, elements };
      });
    });

    es.addEventListener('thinking_delta', (e: MessageEvent) => {
      const payload = safeParse<ThinkingDeltaPayload>(e.data);
      if (!payload) return;
      ensureInProgress();
      const newContent = (inProgressRef.current?.thinkingContent ?? '') + payload.content;
      if (inProgressRef.current) inProgressRef.current.thinkingContent = newContent;
      updateInProgress((turn) => ({
        ...turn,
        thinking: {
          content: newContent,
          durationSeconds: turn.thinking?.durationSeconds ?? 0,
          tokenCount: turn.thinking?.tokenCount ?? 0,
        },
      }));
    });

    es.addEventListener('tool_use', (e: MessageEvent) => {
      const payload = safeParse<ToolUsePayload>(e.data);
      if (!payload) return;
      ensureInProgress();
      const firstVal = payload.input ? Object.values(payload.input)[0] : '';
      const arg = String(firstVal ?? '').slice(0, 80);
      const toolEl: AgentElement = {
        kind: 'tool',
        tool: {
          id: payload.id,
          icon: deriveToolIcon(payload.name),
          name: payload.name,
          arg,
          status: 'running',
          result: '',
        },
      };
      updateInProgress((turn) => ({ ...turn, elements: [...turn.elements, toolEl] }));
    });

    es.addEventListener('tool_result', (e: MessageEvent) => {
      const payload = safeParse<ToolResultPayload>(e.data);
      if (!payload) return;
      const resultStr =
        typeof payload.content === 'string'
          ? payload.content
          : JSON.stringify(payload.content ?? {});
      updateInProgress((turn) => ({
        ...turn,
        elements: turn.elements.map((el) => {
          if (el.kind !== 'tool' || el.tool.id !== payload.tool_use_id) return el;
          return {
            ...el,
            tool: {
              ...el.tool,
              status: payload.is_error ? ('error' as const) : ('done' as const),
              result: resultStr.slice(0, 120),
            },
          };
        }),
      }));
    });

    es.addEventListener('message_complete', () => {
      flushInProgress();
    });

    es.addEventListener('message_stop', () => {
      // message_stop signals streaming ended; message_complete flushes. No-op here.
    });

    es.addEventListener('run_state', (e: MessageEvent) => {
      const payload = safeParse<RunStatePayload>(e.data);
      if (payload?.state) setRunState(payload.state);
    });

    return () => {
      es.close();
    };
  }, [runId]);

  function openCanvas(): void {
    // Canvas views are not yet populated from SSE — deferred to canvas feature.
  }

  function closeCanvas(): void {
    setCanvasState((prev) => ({ ...prev, isOpen: false }));
  }

  function switchTab(tabId: string): void {
    setCanvasState((prev) => ({ ...prev, activeTabId: tabId }));
  }

  function addHumanMessage(content: string): void {
    const turn: Turn = {
      type: 'human',
      id: `h-${Date.now()}`,
      content,
      timestamp: new Date().toTimeString().slice(0, 8),
    };
    setCommittedTurns((prev) => [...prev, turn]);

    void fetch(`/api/agents/runs/${runId}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    }).catch(() => undefined);
  }

  const allTurns: Turn[] = inProgressTurn ? [...committedTurns, inProgressTurn] : committedTurns;

  return {
    runId,
    slug,
    agentName,
    model,
    runState,
    turns: allTurns,
    contextFiles: [],
    canvasState,
    views: {},
    openCanvas,
    closeCanvas,
    switchTab,
    addHumanMessage,
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function safeParse<T>(data: string): T | null {
  try {
    return JSON.parse(data) as T;
  } catch {
    return null;
  }
}

function deriveToolIcon(name: string): string {
  if (name === 'bash' || name === 'execute') return '⚡';
  if (name.includes('read') || name.includes('view')) return '📄';
  if (name.includes('write') || name.includes('edit') || name.includes('create')) return '✏️';
  if (name.includes('search') || name.includes('find') || name.includes('grep')) return '🔍';
  return '🔧';
}
