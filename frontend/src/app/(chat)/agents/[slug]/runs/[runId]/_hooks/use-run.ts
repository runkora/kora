'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { useBrand } from '@orbit/theme/brand-provider';
import type {
  RunChatState,
  Turn,
  AgentTurn,
  AgentElement,
  CanvasState,
  CanvasView,
  RunState,
  ToolCall,
  LiveActivity,
} from '../_types';
import { describeTool } from '../_lib/tool-activity';

// ── In-progress turn accumulator ─────────────────────────────────────────────

interface InProgress {
  turn: AgentTurn;
  thinkingContent: string;
  // true when text_delta events were received for this turn (live streaming).
  // Used to suppress message_complete.text on live turns — the content is
  // already built from deltas. During replay, no text_delta events fire, so
  // this stays false and message_complete.text (trailing text) is used instead.
  hasSeenTextDelta: boolean;
  // true once the turn's time comes from a recorded event. Streamed deltas are
  // not replayed, so a turn is stamped by its first recorded event on reload;
  // live turns take the same time once that event arrives.
  stamped: boolean;
}

// Events recorded in run_events: the ones a reload replays.
const RECORDED = new Set([
  'text_block',
  'tool_use',
  'tool_result',
  'message_complete',
  'turn_stopped',
  'run_error',
  'api_retry',
  'sandbox_status',
]);

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
  parent_tool_use_id?: string;
}
interface ToolResultPayload {
  tool_use_id: string;
  parent_tool_use_id?: string;
  content?: unknown;
  is_error?: boolean;
  truncated?: boolean;
  original_bytes?: number;
}
interface ToolProgressPayload {
  tool_use_id: string;
  elapsed_seconds: number;
}
interface MessageCompletePayload {
  text: string;
  thinking?: string;
  interrupted?: boolean;
}
interface RunStatePayload {
  state: RunState;
}
interface RunErrorPayload {
  code: string;
  message: string;
}
interface ApiRetryPayload {
  attempt: number;
  max_retries: number;
  error?: string;
}
interface SandboxStatusPayload {
  phase: string;
  elapsed_ms: number;
}
interface GuardrailWarningPayload {
  kind: string;
  count: number;
  tool: string;
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
  /** True while the run is pending or starting. It takes no messages until it is active. */
  isStarting: boolean;
  /** SSE connection lifecycle status — surfaced in the UI as a banner when not 'open'. */
  connectionStatus: ConnectionStatus;
  /** What the agent is doing while nothing visible streams; null when not busy. */
  liveActivity: LiveActivity | null;
  /** Render every tool call rather than grouping them. */
  showAllCalls: boolean;
  setShowAllCalls: (show: boolean) => void;
  openCanvas: (viewId: string) => void;
  openToolInCanvas: (tool: ToolCall) => void;
  closeCanvas: () => void;
  switchTab: (tabId: string) => void;
  addHumanMessage: (content: string) => void;
  interruptRun: () => void;
  /** Manually re-open the SSE stream after a terminal failure. */
  reconnect: () => void;
}

const SANDBOX_STARTING = new Set(['provisioning', 'pod_ready']);

// ── useRun ────────────────────────────────────────────────────────────────────

export function useRun(runId: string, slug: string): UseRunReturn {
  const { productName } = useBrand();
  // Null until the API or the stream reports the run's state.
  const [runState, setRunState] = useState<RunState | null>(null);
  const [agentName, setAgentName] = useState('Agent');
  // Empty until the run's agent type metadata resolves — the model is injected
  // from the created agent (agent_type.model_id), never hardcoded here.
  const [model, setModel] = useState('');
  const [summary, setSummary] = useState<string | null>(null);
  // Who started the run — the BFF-resolved display name, falling back to the
  // raw principal id when unresolvable. Null until metadata loads.
  const [startedBy, setStartedBy] = useState<string | null>(null);
  // True when the agent is delegated: the run acts on the starter's behalf and
  // is private to them + tenant admins.
  const [delegated, setDelegated] = useState(false);
  const [committedTurns, setCommittedTurns] = useState<Turn[]>([]);
  const [inProgressTurn, setInProgressTurn] = useState<AgentTurn | null>(null);
  const [isPending, setIsPending] = useState(false);
  // Live run-usage meter. spentUsd/tokensUsed refresh as turns complete;
  // perRunLimitUsd comes from the agent's grant and is fixed for the run.
  const [spentUsd, setSpentUsd] = useState(0);
  const [tokensUsed, setTokensUsed] = useState(0);
  const [perRunLimitUsd, setPerRunLimitUsd] = useState<number | null>(null);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting');
  // Bumped to force the SSE effect to re-run on manual reconnect.
  const [reconnectKey, setReconnectKey] = useState(0);
  const [canvasState, setCanvasState] = useState<CanvasState>({
    isOpen: false,
    tabs: [],
    activeTabId: null,
  });
  const [views, setViews] = useState<Record<string, CanvasView>>({});
  const [showAllCalls, setShowAllCalls] = useState(false);
  // Status line inputs: a sandbox being started, a provider retry, and when the
  // agent last showed any sign of life (browser time).
  const [sandboxSince, setSandboxSince] = useState<number | null>(null);
  const [retry, setRetry] = useState<{
    attempt: number;
    maxRetries: number;
    error: string;
  } | null>(null);
  const [lastActivityAt, setLastActivityAt] = useState(() => Date.now());

  const inProgressRef = useRef<InProgress | null>(null);
  // RAF handle for batching text_delta / thinking_delta state flushes to ~60fps.
  const rafHandleRef = useRef<number | null>(null);
  // Tracks optimistically-added human messages so the echoed SSE event is skipped.
  const pendingHumanMessages = useRef<string[]>([]);
  // Messages sent mid-turn, by turn id, and whether a tool boundary or turn end
  // has passed since: the agent reads a queued message at the next boundary, so
  // its next output after one means the message was delivered.
  const queuedRef = useRef<Map<string, boolean>>(new Map());
  const runStateRef = useRef<RunState | null>(null);
  const humanSeq = useRef(0);
  // Monotonic ids for turns: replayed events arrive within one millisecond.
  const turnSeq = useRef(0);
  // A message sent mid-turn splits the turn so the bubble sits where it was
  // sent. What the first part already streamed is carried over, so the
  // turn's message_complete does not repeat it in the second part.
  const splitStreamed = useRef({ text: false, thinking: false });
  const lastActivityRef = useRef(0);
  // Calls a mid-turn message moved into a committed part of their turn.
  const splitToolIds = useRef<Set<string>>(new Set());

  // Refresh the live run-usage meter from the gateway (spend + tokens). Stable
  // per runId so it can be called from both the mount effect and the SSE
  // event handlers without re-subscribing the stream.
  const loadUsage = useCallback(() => {
    if (!runId) return;
    void fetch(`/api/agents/runs/${runId}/usage`)
      .then((r) => (r.ok ? r.json() : null))
      .then((body: { spend_usd?: number; total_tokens?: number } | null) => {
        if (!body) return;
        if (typeof body.spend_usd === 'number') setSpentUsd(body.spend_usd);
        if (typeof body.total_tokens === 'number') setTokensUsed(body.total_tokens);
      })
      .catch(() => undefined);
  }, [runId]);

  // Throttled wrapper around loadUsage for the high-frequency stream events. A
  // single agent turn spans many LLM calls (each tool cycle is its own gateway
  // request that writes a usage row), so spend climbs at every call boundary —
  // not just at turn end. The stream fires bursts of events (text deltas, a
  // cluster of tool_use blocks) so we coalesce: refresh at most once per
  // window, with a trailing call so the last burst is never dropped.
  const usageRefreshRef = useRef<{ last: number; timer: ReturnType<typeof setTimeout> | null }>({
    last: 0,
    timer: null,
  });
  const scheduleUsageRefresh = useCallback(() => {
    const MIN_INTERVAL_MS = 1500;
    const t = usageRefreshRef.current;
    const now = Date.now();
    const wait = t.last + MIN_INTERVAL_MS - now;
    if (wait <= 0) {
      t.last = now;
      loadUsage();
    } else if (t.timer === null) {
      t.timer = setTimeout(() => {
        t.timer = null;
        t.last = Date.now();
        loadUsage();
      }, wait);
    }
  }, [loadUsage]);

  // Adds a human message to the transcript. A message sent mid-turn splits the
  // turn so the bubble sits after what the agent had already said, live and on
  // replay alike; the agent's later output starts a new turn below it.
  const appendHuman = useCallback((turn: Turn) => {
    const inProgress = inProgressRef.current;
    if (!inProgress) {
      setCommittedTurns((prev) => [...prev, turn]);
      return;
    }
    if (rafHandleRef.current !== null) {
      cancelAnimationFrame(rafHandleRef.current);
      rafHandleRef.current = null;
    }
    splitStreamed.current = {
      text: inProgress.hasSeenTextDelta,
      thinking: !!inProgress.thinkingContent,
    };
    for (const el of inProgress.turn.elements) {
      if (el.kind === 'tool') splitToolIds.current.add(el.tool.id);
    }
    inProgressRef.current = null;
    setInProgressTurn(null);
    setCommittedTurns((prev) => [...prev, inProgress.turn, turn]);
  }, []);

  // Fetch initial run + agent type metadata, the per-run budget ceiling, and
  // the opening usage snapshot.
  useEffect(() => {
    if (!runId) return;
    void fetch(`/api/agents/runs/${runId}`)
      .then((r) => r.json())
      .then(
        (body: {
          data?: {
            state: RunState;
            summary?: string | null;
            created_by?: string;
            created_by_name?: string | null;
          };
          agent_type?: {
            name?: string;
            model_id?: string;
            identity_mode?: 'autonomous' | 'delegated';
            budget?: { per_run_limit_usd?: number };
          };
        }) => {
          if (body.data?.state) {
            runStateRef.current = body.data.state;
            setRunState(body.data.state);
          }
          if (typeof body.data?.summary === 'string') {
            setSummary(body.data.summary);
          }
          if (body.data) {
            setStartedBy(body.data.created_by_name ?? body.data.created_by ?? null);
          }
          if (body.agent_type) {
            setAgentName(body.agent_type.name ?? 'Agent');
            setModel(body.agent_type.model_id ?? '');
            setDelegated(body.agent_type.identity_mode === 'delegated');
            const limit = body.agent_type.budget?.per_run_limit_usd;
            setPerRunLimitUsd(typeof limit === 'number' && limit > 0 ? limit : null);
          }
        },
      )
      .catch(() => undefined);
    loadUsage();
  }, [runId, loadUsage]);

  // Subscribe to the SSE event stream for this run.
  useEffect(() => {
    if (!runId) return;

    // The initial state is 'connecting'; reconnect() resets it before bumping
    // reconnectKey, so we don't need to setConnectionStatus here.
    const es = new EventSource(`/api/agents/runs/${runId}/events`);
    // Capture the throttle state object for the cleanup below — the ref's
    // .current is mutated but never reassigned, so this stays the live object.
    const usageRefresh = usageRefreshRef.current;
    const queued = queuedRef.current;

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

    // Commits the in-progress turn. endRunning marks calls still running as
    // stopped: the turn ended (an error, the run settling) without their results.
    function flushInProgress(endRunning = false): void {
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
      splitStreamed.current = { text: false, thinking: false };
      if (endRunning) {
        // A turn split by a mid-turn message may hold calls in a committed part.
        setCommittedTurns((prev) =>
          prev.some((t) => t.type === 'agent' && stopRunningTools(t) !== t)
            ? prev.map((t) => (t.type === 'agent' ? stopRunningTools(t) : t))
            : prev,
        );
      }
      if (!inProgressRef.current) return;
      const turn = inProgressRef.current.turn;
      const completed = endRunning ? stopRunningTools(turn) : turn;
      inProgressRef.current = null;
      setCommittedTurns((prev) => [...prev, completed]);
      setInProgressTurn(null);
    }

    // Time of the event being handled, from the server's envelope. Turns and
    // calls take their times from it, so a reload shows when things happened.
    let eventTime = '';
    let eventAt: number | undefined;
    let eventRecorded = false;
    function read<T>(e: MessageEvent): T | null {
      const parsed = parseEnvelope<T>(e.data);
      if (!parsed) return null;
      eventTime = parsed.time;
      eventAt = parsed.at ?? undefined;
      eventRecorded = RECORDED.has(e.type);
      const current = inProgressRef.current;
      if (current && !current.stamped && eventRecorded && eventTime) {
        current.stamped = true;
        current.turn = { ...current.turn, timestamp: eventTime };
      }
      return parsed.payload;
    }

    function ensureInProgress(): AgentTurn {
      if (!inProgressRef.current) {
        // First event for this agent turn — clear the "pending" gap indicator.
        setIsPending(false);
        const turn: AgentTurn = {
          type: 'agent',
          id: `a-${++turnSeq.current}`,
          timestamp: eventTime,
          elements: [],
        };
        inProgressRef.current = {
          turn,
          thinkingContent: '',
          hasSeenTextDelta: false,
          stamped: eventRecorded,
        };
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

    function appendElement(el: AgentElement): void {
      ensureInProgress();
      updateInProgress((turn) => ({ ...turn, elements: [...turn.elements, el] }), true);
    }

    // Apply a change to one tool call, wherever its turn is: a result can land
    // after message_stop flushed the turn, or during replay.
    function patchTool(id: string, patch: (tool: ToolCall) => ToolCall): void {
      const has = (turn: AgentTurn) =>
        turn.elements.some((el) => el.kind === 'tool' && el.tool.id === id);
      const apply = (turn: AgentTurn): AgentTurn =>
        has(turn)
          ? {
              ...turn,
              elements: turn.elements.map((el) =>
                el.kind === 'tool' && el.tool.id === id ? { ...el, tool: patch(el.tool) } : el,
              ),
            }
          : turn;
      if (
        inProgressRef.current?.turn.elements.some((el) => el.kind === 'tool' && el.tool.id === id)
      ) {
        updateInProgress(apply, true);
        return;
      }
      setCommittedTurns((prev) =>
        prev.some((t) => t.type === 'agent' && has(t))
          ? prev.map((t) => (t.type === 'agent' ? apply(t) : t))
          : prev,
      );
    }

    // At most one state update a second: the status line only counts seconds,
    // and one per token would defeat the frame batching of text deltas.
    function markActivity(): void {
      const now = Date.now();
      if (now - lastActivityRef.current < 1000) return;
      lastActivityRef.current = now;
      setLastActivityAt(now);
    }

    // The agent produced output: it is alive, any provider retry is over, and
    // a queued message whose boundary has passed has been read.
    function agentActed(): void {
      markActivity();
      setIsPending(false);
      setRetry(null);
      markDelivered([...queued].filter(([, boundary]) => boundary).map(([id]) => id));
    }

    function markDelivered(delivered: string[]): void {
      if (delivered.length === 0) return;
      for (const id of delivered) queued.delete(id);
      setCommittedTurns((prev) =>
        prev.map((t) =>
          t.type === 'human' && delivered.includes(t.id) ? { ...t, delivery: 'delivered' } : t,
        ),
      );
    }

    // A tool boundary or turn end: queued messages are read before the next output.
    function boundaryPassed(): void {
      for (const id of queued.keys()) queued.set(id, true);
    }

    es.addEventListener('text_delta', (e: MessageEvent) => {
      const payload = read<TextDeltaPayload>(e);
      if (!payload) return;
      agentActed();
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
      const payload = read<{ content: string }>(e);
      if (!payload?.content) return;
      appendElement({ kind: 'text', content: payload.content });
    });

    es.addEventListener('thinking_delta', (e: MessageEvent) => {
      const payload = read<ThinkingDeltaPayload>(e);
      if (!payload) return;
      agentActed();
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
      const payload = read<ToolUsePayload>(e);
      if (!payload) return;
      if (!payload.parent_tool_use_id) agentActed();
      const el: AgentElement = {
        kind: 'tool',
        tool: {
          id: payload.id,
          name: payload.name,
          input: payload.input ?? {},
          status: 'running',
          result: '',
          parentId: payload.parent_tool_use_id || undefined,
          startedAt: eventAt,
          receivedAt: Date.now(),
        },
      };
      // A subagent whose Agent call a mid-turn message moved into a committed
      // part keeps its calls with that call, so they still nest under it.
      const parent = payload.parent_tool_use_id;
      if (parent && splitToolIds.current.has(parent)) {
        setCommittedTurns((prev) =>
          prev.map((t) =>
            t.type === 'agent' && t.elements.some((x) => x.kind === 'tool' && x.tool.id === parent)
              ? { ...t, elements: [...t.elements, el] }
              : t,
          ),
        );
      } else {
        appendElement(el);
      }
      // A tool_use block means the model just finished an LLM call (its usage
      // row is now written) — refresh the meter so spend climbs mid-turn.
      scheduleUsageRefresh();
    });

    es.addEventListener('tool_result', (e: MessageEvent) => {
      const payload = read<ToolResultPayload>(e);
      if (!payload) return;
      markActivity();
      // Only the main thread reads queued messages; a subagent's result is not its boundary.
      if (!payload.parent_tool_use_id) boundaryPassed();
      const result =
        typeof payload.content === 'string'
          ? payload.content
          : JSON.stringify(payload.content ?? '');
      const endedAt = eventAt;
      patchTool(payload.tool_use_id, (tool) => ({
        ...tool,
        status: payload.is_error ? 'error' : 'done',
        result,
        truncated: payload.truncated || undefined,
        originalBytes: payload.original_bytes,
        endedAt,
      }));
    });

    es.addEventListener('tool_progress', (e: MessageEvent) => {
      const payload = read<ToolProgressPayload>(e);
      if (!payload) return;
      patchTool(payload.tool_use_id, (tool) => ({
        ...tool,
        progressSeconds: payload.elapsed_seconds,
      }));
    });

    es.addEventListener('api_retry', (e: MessageEvent) => {
      const payload = read<ApiRetryPayload>(e);
      if (!payload) return;
      setRetry({
        attempt: payload.attempt,
        maxRetries: payload.max_retries,
        error: payload.error ?? '',
      });
    });

    es.addEventListener('sandbox_status', (e: MessageEvent) => {
      const payload = read<SandboxStatusPayload>(e);
      if (!payload) return;
      setSandboxSince(
        SANDBOX_STARTING.has(payload.phase) ? Date.now() - (payload.elapsed_ms ?? 0) : null,
      );
    });

    es.addEventListener('message_complete', (e: MessageEvent) => {
      const payload = read<MessageCompletePayload>(e);

      // During replay, text/thinking deltas are not re-sent — only the stored
      // message_complete payload carries the content. Detect replay by checking
      // whether any text was already accumulated via text_delta events.
      if (payload) {
        // hasSeenTextDelta is true during live streaming (text_delta events built
        // the content already). During replay — both old runs (message_complete
        // carries all text) and new runs (text_block events carried pre-tool text,
        // message_complete carries trailing text) — it is false.
        const hasSeenTextDelta =
          !!inProgressRef.current?.hasSeenTextDelta || splitStreamed.current.text;
        const hasStreamedThinking =
          !!inProgressRef.current?.thinkingContent || splitStreamed.current.thinking;

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
        // A partial turn stays open: the turn_stopped or run_error that follows
        // closes it, so its marker lands in the same turn as its text.
        if (payload.interrupted) return;
      }

      boundaryPassed();
      flushInProgress();
      // Turn complete — refresh the meter (throttled; the terminal/idle
      // run_state handler does a final exact true-up).
      scheduleUsageRefresh();
    });

    es.addEventListener('turn_stopped', (e: MessageEvent) => {
      const stop = read<{ reason: string }>(e);
      if (!stop) return;
      ensureInProgress();
      updateInProgress(
        (turn) => ({
          ...turn,
          elements: [
            ...turn.elements.map((el) =>
              el.kind === 'tool' && el.tool.status === 'running'
                ? { ...el, tool: { ...el.tool, status: 'stopped' as const } }
                : el,
            ),
            stop.reason === 'guardrail'
              ? { kind: 'stopped' as const, byGuardrail: true }
              : { kind: 'stopped' as const },
          ],
        }),
        true,
      );
      flushInProgress();
      setRetry(null);
      setSandboxSince(null);
    });

    es.addEventListener('guardrail_warning', (e: MessageEvent) => {
      const payload = read<GuardrailWarningPayload>(e);
      if (!payload) return;
      appendElement({ kind: 'notice', message: guardrailMessage(payload, productName) });
    });

    es.addEventListener('message_stop', () => {
      // message_stop signals streaming ended; message_complete flushes. No-op here.
    });

    es.addEventListener('run_state', (e: MessageEvent) => {
      const payload = read<RunStatePayload>(e);
      if (!payload?.state) return;
      // Flush any in-progress turn when the run goes idle (e.g. after an interrupt).
      // This ensures the interrupted turn is committed before any follow-up human
      // message is appended, and clears the streaming/busy indicators.
      if (
        payload.state === 'idle' ||
        payload.state === 'completed' ||
        payload.state === 'failed' ||
        payload.state === 'budget_exhausted'
      ) {
        // The run settled with the message in hand: a runtime that reads a
        // message within the turn (Codex, OpenCode) may produce nothing after it.
        markDelivered([...queued.keys()]);
        flushInProgress(true);
        setIsPending(false);
        setRetry(null);
        setSandboxSince(null);
        // Final true-up of the usage meter when the run settles (terminal or
        // idle between turns) — catches any spend the last turn added.
        loadUsage();
      }
      if (payload.state === 'active') markActivity();
      runStateRef.current = payload.state;
      setRunState(payload.state);
    });

    es.addEventListener('human_message', (e: MessageEvent) => {
      const payload = read<{ content: string }>(e);
      if (!payload?.content) return;
      // Skip if we sent this message ourselves (already optimistically rendered).
      const idx = pendingHumanMessages.current.indexOf(payload.content);
      if (idx !== -1) {
        pendingHumanMessages.current.splice(idx, 1);
        // The optimistic bubble takes the server's time from its echo.
        const time = eventTime;
        setCommittedTurns((prev) => {
          const at = prev.findIndex(
            (t) => t.type === 'human' && t.content === payload.content && !t.timestamp,
          );
          if (at === -1) return prev;
          const next = [...prev];
          next[at] = { ...next[at], timestamp: time } as Turn;
          return next;
        });
        return;
      }
      appendHuman({
        type: 'human',
        id: `h-${++turnSeq.current}`,
        content: payload.content,
        timestamp: eventTime,
      });
    });

    es.addEventListener('run_error', (e: MessageEvent) => {
      const payload = read<RunErrorPayload>(e);
      if (!payload) return;
      setRetry(null);
      const friendly = runErrorMessage(payload.code, productName);
      // A lost sandbox does not end the turn: the agent carries on, so the
      // error sits in the turn where it happened.
      if (TURN_CONTINUES.has(payload.code) && inProgressRef.current) {
        appendElement({ kind: 'notice', tone: 'error', message: friendly ?? payload.message });
        return;
      }
      flushInProgress(true);
      setIsPending(false);
      const time = eventTime;
      const id = `err-${++turnSeq.current}`;
      setCommittedTurns((prev) => [
        ...prev,
        {
          type: 'error' as const,
          id,
          timestamp: time,
          code: payload.code,
          message: friendly ?? payload.message,
          detail: friendly ? payload.message : undefined,
        },
      ]);
      // Run state comes only from run_state events: most errors end a turn,
      // not the run, and the backend reports the run's state separately.
    });

    return () => {
      es.close();
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
      if (usageRefresh.timer !== null) {
        clearTimeout(usageRefresh.timer);
        usageRefresh.timer = null;
      }
    };
    // reconnectKey is in the dep array so calling reconnect() (which bumps it)
    // tears down the current EventSource and opens a fresh one. loadUsage /
    // scheduleUsageRefresh are stable per runId, so they never re-subscribe the
    // stream on their own.
  }, [runId, reconnectKey, loadUsage, scheduleUsageRefresh, appendHuman, productName]);

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
    queuedRef.current.clear();
    splitToolIds.current.clear();
    setCommittedTurns([]);
    setInProgressTurn(null);
    setIsPending(false);
    setConnectionStatus('connecting');
    setReconnectKey((k) => k + 1);
  }

  function openCanvas(viewId: string): void {
    setCanvasState((prev) =>
      prev.tabs.some((t) => t.id === viewId)
        ? { ...prev, isOpen: true, activeTabId: viewId }
        : prev,
    );
  }

  function openToolInCanvas(tool: ToolCall): void {
    const view = toolCanvasView(tool);
    setViews((prev) => ({ ...prev, [view.id]: view }));
    setCanvasState((prev) => ({
      isOpen: true,
      activeTabId: view.id,
      tabs: prev.tabs.some((t) => t.id === view.id)
        ? prev.tabs
        : [...prev.tabs, { id: view.id, label: view.title }],
    }));
  }

  function closeCanvas(): void {
    setCanvasState((prev) => ({ ...prev, isOpen: false }));
  }

  function switchTab(tabId: string): void {
    setCanvasState((prev) => ({ ...prev, activeTabId: tabId }));
  }

  function addHumanMessage(content: string): void {
    pendingHumanMessages.current.push(content);
    humanSeq.current += 1;
    const id = `h-sent-${humanSeq.current}`;
    // Claude Code reads a message sent mid-turn at its next tool boundary, so
    // it is shown queued until the agent acts after one.
    const agentWorking = runStateRef.current === 'active' || inProgressRef.current !== null;
    if (agentWorking) queuedRef.current.set(id, false);
    const turn: Turn = {
      type: 'human',
      id,
      content,
      timestamp: '',
      delivery: agentWorking ? 'queued' : undefined,
    };
    setIsPending(true);
    appendHuman(turn);

    const fail = (error: string) => {
      const i = pendingHumanMessages.current.indexOf(content);
      if (i !== -1) pendingHumanMessages.current.splice(i, 1);
      queuedRef.current.delete(id);
      setIsPending(false);
      setCommittedTurns((prev) =>
        prev.map((t) => (t.id === id ? { ...t, delivery: 'failed' as const, error } : t)),
      );
    };
    void fetch(`/api/agents/runs/${runId}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ content }),
    })
      .then(async (res) => {
        if (res.ok) return;
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        fail(body.message ?? `The message was not sent (${res.status}).`);
      })
      .catch(() => fail('The message was not sent: the server could not be reached.'));
  }

  function interruptRun(): void {
    void fetch(`/api/agents/runs/${runId}/interrupt`, { method: 'POST' }).catch(() => undefined);
  }

  const allTurns: Turn[] = inProgressTurn ? [...committedTurns, inProgressTurn] : committedTurns;
  const isBusy = runState === 'starting' || runState === 'active';
  const isStarting = runState === 'pending' || runState === 'starting';

  return {
    runId,
    slug,
    agentName,
    model,
    summary,
    startedBy,
    delegated,
    runState,
    turns: allTurns,
    isStreaming: inProgressTurn !== null,
    isPending,
    isStarting,
    isBusy,
    connectionStatus,
    liveActivity: isBusy ? deriveActivity(allTurns, retry, sandboxSince, lastActivityAt) : null,
    showAllCalls,
    setShowAllCalls,
    spentUsd,
    perRunLimitUsd,
    tokensUsed,
    contextFiles: [],
    canvasState,
    views,
    openCanvas,
    openToolInCanvas,
    closeCanvas,
    switchTab,
    addHumanMessage,
    interruptRun,
    reconnect,
  };
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function stopRunningTools(turn: AgentTurn): AgentTurn {
  if (!turn.elements.some((el) => el.kind === 'tool' && el.tool.status === 'running')) return turn;
  return {
    ...turn,
    elements: turn.elements.map((el) =>
      el.kind === 'tool' && el.tool.status === 'running'
        ? { ...el, tool: { ...el.tool, status: 'stopped' as const } }
        : el,
    ),
  };
}

const TURN_CONTINUES = new Set(['sandbox_lost']);

/** The friendly text for a run error code, when there is one; the product is named by the brand. */
function runErrorMessage(code: string, productName: string): string | undefined {
  switch (code) {
    case 'sandbox_lost':
      return 'The sandbox was lost. Files in the workspace are kept; anything elsewhere in the sandbox is gone.';
    case 'loop_detected':
      return `${productName} paused this run because the agent was repeating itself. Reply to continue.`;
    default:
      return undefined;
  }
}

function guardrailMessage(p: GuardrailWarningPayload, productName: string): string {
  const what =
    p.kind === 'wait_rearm'
      ? `${p.tool} re-armed ${p.count} times with no new output`
      : `${p.tool} called ${p.count} times with the same input`;
  return `${productName} asked the agent to change approach: ${what}.`;
}

/**
 * What the status line shows, in order of what the user most needs to know: a
 * provider retry, a sandbox starting, the running main-thread tool, or thinking.
 */
export function deriveActivity(
  turns: Turn[],
  retry: { attempt: number; maxRetries: number; error: string } | null,
  sandboxSince: number | null,
  lastActivityAt: number,
): LiveActivity {
  if (retry) return { kind: 'retry', ...retry };
  if (sandboxSince !== null) return { kind: 'sandbox', since: sandboxSince };
  // Calls left running by an earlier turn are marked stopped when it settles,
  // so the latest running call is live, however many messages were sent since.
  let current: ToolCall | undefined;
  for (let i = turns.length - 1; i >= 0 && !current; i--) {
    const t = turns[i];
    if (t.type !== 'agent') continue;
    current = t.elements
      .flatMap((el) => (el.kind === 'tool' && el.tool.status === 'running' ? [el.tool] : []))
      .filter((tool) => !tool.parentId)
      .at(-1);
  }
  if (current) return { kind: 'tool', tool: current, since: callStart(current) ?? lastActivityAt };
  return { kind: 'thinking', since: lastActivityAt };
}

/**
 * When a running call started, by the server's clock, so a reload does not
 * restart its count; the browser's arrival time when the server gave none.
 */
export function callStart(tool: ToolCall): number | undefined {
  return tool.startedAt ?? tool.receivedAt;
}

/** A canvas view for a call's full result: terminal output, a diff, or text. */
function toolCanvasView(tool: ToolCall): CanvasView {
  const id = `tool-${tool.id}`;
  const d = describeTool(tool.name, tool.input);
  const title = d.target ? `${d.label} ${d.target}`.slice(0, 60) : d.label;
  if (d.kind === 'command') {
    return {
      id,
      type: 'terminal',
      title,
      command: String(tool.input.command ?? ''),
      output: tool.result
        .split('\n')
        .map((content) => ({ kind: tool.status === 'error' ? 'error' : 'normal', content })),
    };
  }
  return {
    id,
    type: 'code',
    title,
    code: prettyJson(tool.result) ?? tool.result,
    lineCount: tool.result.split('\n').length,
  };
}

/** Pretty-printed JSON, or null when the text is not a JSON object or array. */
export function prettyJson(text: string): string | null {
  const t = text.trim();
  if (!(t.startsWith('{') || t.startsWith('['))) return null;
  try {
    return JSON.stringify(JSON.parse(t), null, 2);
  } catch {
    return null;
  }
}

/**
 * Unwraps an SSE event: `{"at": <RFC 3339>, "payload": <event>}`. `at` is the
 * server's time for the event; `time` is it as local HH:MM:SS. A bare payload
 * (an orbit-agents that predates the envelope) parses with no time.
 */
export function parseEnvelope<T>(
  data: string,
): { payload: T; time: string; at: number | null } | null {
  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
  } catch {
    return null;
  }
  if (parsed && typeof parsed === 'object' && 'at' in parsed && 'payload' in parsed) {
    const env = parsed as { at: string; payload: T };
    const at = new Date(env.at).getTime();
    if (Number.isNaN(at)) return { payload: env.payload, time: '', at: null };
    return { payload: env.payload, time: new Date(at).toTimeString().slice(0, 8), at };
  }
  return { payload: parsed as T, time: '', at: null };
}
