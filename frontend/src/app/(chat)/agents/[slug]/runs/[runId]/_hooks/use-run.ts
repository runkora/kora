'use client';

import { useState, useEffect, useRef } from 'react';
import type {
  RunChatState,
  Turn,
  AgentTurn,
  AgentElement,
  CanvasState,
  RunState,
  ToolIconName,
} from '../_types';

// ── In-progress turn accumulator ─────────────────────────────────────────────

interface InProgress {
  turn: AgentTurn;
  thinkingContent: string;
  // true when text_delta events were received for this turn (live streaming).
  // Used to suppress message_complete.text on live turns — the content is
  // already built from deltas. During replay, no text_delta events fire, so
  // this stays false and message_complete.text (trailing text) is used instead.
  hasSeenTextDelta: boolean;
}

// ── Event payload shapes ──────────────────────────────────────────────────────

interface TextDeltaPayload {
  delta: string;
}
interface ThinkingDeltaPayload {
  delta: string;
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
interface MessageCompletePayload {
  text: string;
  thinking?: string;
}
interface RunStatePayload {
  state: RunState;
}
interface RunErrorPayload {
  code: string;
  message: string;
}

// ── Hook return type ──────────────────────────────────────────────────────────

/**
 * Live status of the SSE connection to the run's event stream.
 *
 * - 'connecting'   — initial state and during EventSource auto-reconnect attempts.
 * - 'open'         — connection established; events flowing.
 * - 'reconnecting' — connection dropped at least once; browser is retrying.
 * - 'closed'       — terminal failure (auth rejected, run not found, network down
 *                    long enough that the browser gave up). User intervention needed.
 */
export type ConnectionStatus = 'connecting' | 'open' | 'reconnecting' | 'closed';

export interface UseRunReturn extends RunChatState {
  /** True while the current agent turn is still streaming (inProgressTurn is non-null). */
  isStreaming: boolean;
  /** True after a human message is sent and before the first agent event arrives. */
  isPending: boolean;
  /** True when runState is starting or active — covers initial navigation and mid-turn reconnects. */
  isBusy: boolean;
  /** SSE connection lifecycle status — surfaced in the UI as a banner when not 'open'. */
  connectionStatus: ConnectionStatus;
  openCanvas: (viewId: string) => void;
  closeCanvas: () => void;
  switchTab: (tabId: string) => void;
  addHumanMessage: (content: string) => void;
  interruptRun: () => void;
  /** Manually re-open the SSE stream after a terminal failure. */
  reconnect: () => void;
}

// ── useRun ────────────────────────────────────────────────────────────────────

export function useRun(runId: string, slug: string): UseRunReturn {
  const [runState, setRunState] = useState<RunState>('pending');
  const [agentName, setAgentName] = useState('Agent');
  // Empty until the run's agent type metadata resolves — the model is injected
  // from the created agent (agent_type.model_id), never hardcoded here.
  const [model, setModel] = useState('');
  const [committedTurns, setCommittedTurns] = useState<Turn[]>([]);
  const [inProgressTurn, setInProgressTurn] = useState<AgentTurn | null>(null);
  const [isPending, setIsPending] = useState(false);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting');
  // Bumped to force the SSE effect to re-run on manual reconnect.
  const [reconnectKey, setReconnectKey] = useState(0);
  const [canvasState, setCanvasState] = useState<CanvasState>({
    isOpen: false,
    tabs: [],
    activeTabId: null,
  });

  const inProgressRef = useRef<InProgress | null>(null);
  // RAF handle for batching text_delta / thinking_delta state flushes to ~60fps.
  const rafHandleRef = useRef<number | null>(null);
  // Tracks optimistically-added human messages so the echoed SSE event is skipped.
  const pendingHumanMessages = useRef<string[]>([]);

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
            setModel(body.agent_type.model_id ?? '');
          }
        },
      )
      .catch(() => undefined);
  }, [runId]);

  // Subscribe to the SSE event stream for this run.
  useEffect(() => {
    if (!runId) return;

    // The initial state is 'connecting'; reconnect() resets it before bumping
    // reconnectKey, so we don't need to setConnectionStatus here.
    const es = new EventSource(`/api/agents/runs/${runId}/events`);

    // EventSource auto-reconnects on transient drops using the Last-Event-ID
    // protocol. `onopen` fires both on first connect and on a successful
    // reconnect; `onerror` fires whenever the connection drops or fails to
    // open. Distinguish "trying to reconnect" from "terminally closed" by
    // inspecting es.readyState:
    //   - CONNECTING (0) → still trying; surface as 'reconnecting' after first open
    //   - CLOSED (2)     → browser gave up (4xx/5xx response on reconnect or
    //                       cors/network failure); user must manually retry.
    es.onopen = () => {
      setConnectionStatus('open');
    };
    es.onerror = () => {
      if (es.readyState === EventSource.CLOSED) {
        setConnectionStatus('closed');
        // Flush any in-progress turn so the chat doesn't appear stuck in
        // mid-response state when the stream is permanently lost.
        flushInProgress();
      } else {
        // CONNECTING: the browser is auto-retrying. Reflect that in the UI
        // so the user knows the stream is degraded, without nuking state.
        setConnectionStatus('reconnecting');
      }
    };

    // Cancel any pending RAF and synchronously flush ref state to React.
    // Call this before any structural change (tool events, completion, flush).
    function syncNow(): void {
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
      if (inProgressRef.current) {
        setInProgressTurn({ ...inProgressRef.current.turn });
      }
    }

    function flushInProgress(): void {
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
      if (!inProgressRef.current) return;
      const completed = inProgressRef.current.turn;
      inProgressRef.current = null;
      setCommittedTurns((prev) => [...prev, completed]);
      setInProgressTurn(null);
    }

    function ensureInProgress(): AgentTurn {
      if (!inProgressRef.current) {
        // First event for this agent turn — clear the "pending" gap indicator.
        setIsPending(false);
        const turn: AgentTurn = {
          type: 'agent',
          id: `a-${Date.now()}`,
          timestamp: new Date().toTimeString().slice(0, 8),
          elements: [],
        };
        inProgressRef.current = { turn, thinkingContent: '', hasSeenTextDelta: false };
      }
      return inProgressRef.current.turn;
    }

    // Mutate the ref immediately (zero re-render cost) and schedule a batched
    // state flush via requestAnimationFrame (~60fps cap). Pass sync=true for
    // structural events (tools, completion) that must render without delay.
    function updateInProgress(updater: (t: AgentTurn) => AgentTurn, sync = false): void {
      if (!inProgressRef.current) return;
      inProgressRef.current = {
        ...inProgressRef.current,
        turn: updater(inProgressRef.current.turn),
      };
      if (sync) {
        syncNow();
      } else if (rafHandleRef.current === null) {
        rafHandleRef.current = requestAnimationFrame(() => {
          rafHandleRef.current = null;
          if (inProgressRef.current) {
            setInProgressTurn({ ...inProgressRef.current.turn });
          }
        });
      }
    }

    es.addEventListener('text_delta', (e: MessageEvent) => {
      const payload = safeParse<TextDeltaPayload>(e.data);
      if (!payload) return;
      ensureInProgress();
      if (inProgressRef.current) inProgressRef.current.hasSeenTextDelta = true;
      updateInProgress((turn) => {
        const elements = [...turn.elements];
        const last = elements[elements.length - 1];
        if (last?.kind === 'text') {
          elements[elements.length - 1] = { kind: 'text', content: last.content + payload.delta };
        } else {
          elements.push({ kind: 'text', content: payload.delta });
        }
        return { ...turn, elements };
      });
    });

    es.addEventListener('text_block', (e: MessageEvent) => {
      const payload = safeParse<{ content: string }>(e.data);
      if (!payload?.content) return;
      ensureInProgress();
      updateInProgress(
        (turn) => ({
          ...turn,
          elements: [...turn.elements, { kind: 'text' as const, content: payload.content }],
        }),
        true,
      );
    });

    es.addEventListener('thinking_delta', (e: MessageEvent) => {
      const payload = safeParse<ThinkingDeltaPayload>(e.data);
      if (!payload) return;
      ensureInProgress();
      const newContent = (inProgressRef.current?.thinkingContent ?? '') + payload.delta;
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
      updateInProgress((turn) => ({ ...turn, elements: [...turn.elements, toolEl] }), true);
    });

    es.addEventListener('tool_result', (e: MessageEvent) => {
      const payload = safeParse<ToolResultPayload>(e.data);
      if (!payload) return;
      const resultStr =
        typeof payload.content === 'string'
          ? payload.content
          : JSON.stringify(payload.content ?? {});
      updateInProgress(
        (turn) => ({
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
        }),
        true,
      );
    });

    es.addEventListener('message_complete', (e: MessageEvent) => {
      const payload = safeParse<MessageCompletePayload>(e.data);

      // During replay, text/thinking deltas are not re-sent — only the stored
      // message_complete payload carries the content. Detect replay by checking
      // whether any text was already accumulated via text_delta events.
      if (payload) {
        // hasSeenTextDelta is true during live streaming (text_delta events built
        // the content already). During replay — both old runs (message_complete
        // carries all text) and new runs (text_block events carried pre-tool text,
        // message_complete carries trailing text) — it is false.
        const hasSeenTextDelta = inProgressRef.current?.hasSeenTextDelta ?? false;
        const hasStreamedThinking = !!inProgressRef.current?.thinkingContent;

        if (!hasSeenTextDelta && payload.text) {
          ensureInProgress();
          updateInProgress((turn) => ({
            ...turn,
            elements: [...turn.elements, { kind: 'text' as const, content: payload.text }],
          }));
        }
        if (!hasStreamedThinking && payload.thinking) {
          ensureInProgress();
          const content = payload.thinking;
          if (inProgressRef.current) inProgressRef.current.thinkingContent = content;
          updateInProgress((turn) => ({
            ...turn,
            thinking: { content, durationSeconds: 0, tokenCount: 0 },
          }));
        }
      }

      flushInProgress();
    });

    es.addEventListener('message_stop', () => {
      // message_stop signals streaming ended; message_complete flushes. No-op here.
    });

    es.addEventListener('run_state', (e: MessageEvent) => {
      const payload = safeParse<RunStatePayload>(e.data);
      if (!payload?.state) return;
      // Flush any in-progress turn when the run goes idle (e.g. after an interrupt).
      // This ensures the interrupted turn is committed before any follow-up human
      // message is appended, and clears the streaming/busy indicators.
      if (payload.state === 'idle' || payload.state === 'completed' || payload.state === 'failed') {
        flushInProgress();
      }
      setRunState(payload.state);
    });

    es.addEventListener('human_message', (e: MessageEvent) => {
      const payload = safeParse<{ content: string }>(e.data);
      if (!payload?.content) return;
      // Skip if we sent this message ourselves (already optimistically rendered).
      const idx = pendingHumanMessages.current.indexOf(payload.content);
      if (idx !== -1) {
        pendingHumanMessages.current.splice(idx, 1);
        return;
      }
      setCommittedTurns((prev) => [
        ...prev,
        {
          type: 'human' as const,
          id: `h-${Date.now()}`,
          content: payload.content,
          timestamp: new Date().toTimeString().slice(0, 8),
        },
      ]);
    });

    es.addEventListener('run_error', (e: MessageEvent) => {
      const payload = safeParse<RunErrorPayload>(e.data);
      if (!payload) return;
      flushInProgress();
      setCommittedTurns((prev) => [
        ...prev,
        {
          type: 'error' as const,
          id: `err-${Date.now()}`,
          timestamp: new Date().toTimeString().slice(0, 8),
          code: payload.code,
          message: payload.message,
        },
      ]);
      setRunState('failed');
    });

    return () => {
      es.close();
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
    };
    // reconnectKey is in the dep array so calling reconnect() (which bumps it)
    // tears down the current EventSource and opens a fresh one.
  }, [runId, reconnectKey]);

  function reconnect(): void {
    // Clear accumulated turns before opening a fresh EventSource. The new
    // connection carries no Last-Event-ID, so the backend replays the full
    // event log from seq 0; without resetting first, that replay would append
    // the entire history on top of the existing turns and duplicate every turn.
    // Wiping here lets the replay deterministically rebuild the transcript.
    // (The browser's own auto-reconnect on transient drops resumes via
    // Last-Event-ID and never calls this, so it is unaffected.)
    if (rafHandleRef.current !== null) {
      cancelAnimationFrame(rafHandleRef.current);
      rafHandleRef.current = null;
    }
    inProgressRef.current = null;
    pendingHumanMessages.current = [];
    setCommittedTurns([]);
    setInProgressTurn(null);
    setIsPending(false);
    setConnectionStatus('connecting');
    setReconnectKey((k) => k + 1);
  }

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
    pendingHumanMessages.current.push(content);
    const turn: Turn = {
      type: 'human',
      id: `h-${Date.now()}`,
      content,
      timestamp: new Date().toTimeString().slice(0, 8),
    };
    setIsPending(true);
    setCommittedTurns((prev) => [...prev, turn]);

    void fetch(`/api/agents/runs/${runId}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    }).catch(() => undefined);
  }

  function interruptRun(): void {
    void fetch(`/api/agents/runs/${runId}/interrupt`, { method: 'POST' }).catch(() => undefined);
  }

  const allTurns: Turn[] = inProgressTurn ? [...committedTurns, inProgressTurn] : committedTurns;

  return {
    runId,
    slug,
    agentName,
    model,
    runState,
    turns: allTurns,
    isStreaming: inProgressTurn !== null,
    isPending,
    isBusy: runState === 'starting' || runState === 'active',
    connectionStatus,
    contextFiles: [],
    canvasState,
    views: {},
    openCanvas,
    closeCanvas,
    switchTab,
    addHumanMessage,
    interruptRun,
    reconnect,
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

function deriveToolIcon(name: string): ToolIconName {
  const n = name.toLowerCase();
  if (
    n === 'bash' ||
    n === 'execute' ||
    n.includes('shell') ||
    n.includes('run') ||
    n.includes('command')
  )
    return 'square-terminal';
  if (n.includes('read') || n.includes('view') || n.includes('cat') || n.includes('open'))
    return 'file-text';
  if (
    n.includes('write') ||
    n.includes('edit') ||
    n.includes('create') ||
    n.includes('update') ||
    n.includes('patch')
  )
    return 'edit';
  if (
    n.includes('search') ||
    n.includes('find') ||
    n.includes('grep') ||
    n.includes('glob') ||
    n.includes('list')
  )
    return 'search';
  if (
    n.includes('database') ||
    n.includes('provision') ||
    n.includes('sql') ||
    n.includes('query') ||
    n.includes('db')
  )
    return 'database';
  if (
    n.includes('web') ||
    n.includes('fetch') ||
    n.includes('http') ||
    n.includes('url') ||
    n.includes('request')
  )
    return 'external-link';
  return 'wrench';
}
