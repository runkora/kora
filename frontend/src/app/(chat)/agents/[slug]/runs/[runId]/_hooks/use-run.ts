'use client';

import { useState, useEffect, useRef, useCallback } from 'react';
import { useBrand } from '@runkora/theme/brand-provider';
import { useSession } from '@runkora/app/(dashboard)/_components/permissions-provider';
import type {
  RunChatState,
  Turn,
  AgentTurn,
  AgentElement,
  RunState,
  ToolCall,
  LiveActivity,
} from '../_types';
import { loadErrorMessage } from '@runkora/lib/load-error';
import { ApiRoute } from '@runkora/lib/routing';
import type {
  AgentType,
  HumanMessagePayload,
  RunBudgetStop,
  RunStatePayload,
  RunWithAttribution,
  RunsPageResponse,
  SandboxEnvironmentPayload,
  TriggerMessagePayload,
} from '@runkora/shared/api';

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
  'sandbox_environment',
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
interface MessageCompletePayload {
  text: string;
  thinking?: string;
  interrupted?: boolean;
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
  addHumanMessage: (content: string) => void;
  interruptRun: () => void;
  /** Manually re-open the SSE stream after a terminal failure. */
  reconnect: () => void;
}

const SANDBOX_STARTING = new Set(['provisioning', 'pod_ready']);

// ── useRun ────────────────────────────────────────────────────────────────────

export function useRun(runId: string, slug: string): UseRunReturn {
  const { productName } = useBrand();
  // The signed-in person, recorded as the sender of a message sent from this page until its echo.
  const { user } = useSession();
  // The stream's handlers read the viewer through a ref, so the session loading does not reopen it.
  const userRef = useRef(user);
  useEffect(() => {
    userRef.current = user;
  }, [user]);
  // Null until the API or the stream reports the run's state.
  const [runState, setRunState] = useState<RunState | null>(null);
  const [run, setRun] = useState<RunWithAttribution | null>(null);
  const [agentType, setAgentType] = useState<AgentType | null>(null);
  const [earlierRuns, setEarlierRuns] = useState<RunWithAttribution[]>([]);
  const [earlierRunsError, setEarlierRunsError] = useState<string | null>(null);
  const [budgetStop, setBudgetStop] = useState<RunBudgetStop | null>(null);
  const [sandbox, setSandbox] = useState<SandboxEnvironmentPayload | null>(null);
  const [traceUrl, setTraceUrl] = useState<string | null>(null);
  const [canSteer, setCanSteer] = useState<boolean | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [stopError, setStopError] = useState<string | null>(null);
  const [committedTurns, setCommittedTurns] = useState<Turn[]>([]);
  const [inProgressTurn, setInProgressTurn] = useState<AgentTurn | null>(null);
  const [isPending, setIsPending] = useState(false);
  // Live run spend; refreshes as turns complete.
  const [spentUsd, setSpentUsd] = useState(0);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('connecting');
  // Bumped to force the SSE effect to re-run on manual reconnect.
  const [reconnectKey, setReconnectKey] = useState(0);
  // Status line inputs: a sandbox being started, a provider retry, and when the
  // agent last showed any sign of life (browser time).
  const [sandboxStarting, setSandboxStarting] = useState(false);
  const [retry, setRetry] = useState<{
    attempt: number;
    maxRetries: number;
    error: string;
  } | null>(null);

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
  // Calls a mid-turn message moved into a committed part of their turn.
  const splitToolIds = useRef<Set<string>>(new Set());

  // Refresh the live run-usage meter from the gateway (spend + tokens). Stable
  // per runId so it can be called from both the mount effect and the SSE
  // event handlers without re-subscribing the stream.
  const loadUsage = useCallback(() => {
    if (!runId) return;
    void fetch(`/api/agents/runs/${runId}/usage`)
      .then((r) => (r.ok ? r.json() : null))
      .then((body: { spend_usd?: number } | null) => {
        if (typeof body?.spend_usd === 'number') setSpentUsd(body.spend_usd);
      })
      .catch(() => undefined);
  }, [runId]);

  // Reads the run and its agent. Called on mount, with each usage refresh and when the run
  // ends, so its sources, end time and token totals stay current.
  const loadedRef = useRef(false);
  const loadRun = useCallback(() => {
    if (!runId) return;
    // A failed re-read keeps the page it already drew; only a failed first read replaces it.
    const fail = (message: string) => {
      if (!loadedRef.current) setLoadError(message);
    };
    void fetch(ApiRoute.runs.detail(runId))
      .then(async (r) => {
        if (r.ok) return r.json();
        // A run the caller may not read answers 404, so its existence does not leak.
        fail(
          r.status === 404
            ? 'This run does not exist, or you do not have access to it.'
            : loadErrorMessage(r.status, 'this run'),
        );
        return {};
      })
      .then(
        (body: {
          can_steer?: boolean | null;
          trace_url?: string | null;
          data?: RunWithAttribution;
          agent_type?: AgentType | null;
        }) => {
          if (!body.data) return;
          loadedRef.current = true;
          setCanSteer(typeof body.can_steer === 'boolean' ? body.can_steer : null);
          setTraceUrl(body.trace_url ?? null);
          setRun(body.data);
          if (body.data.state && runStateRef.current === null) {
            runStateRef.current = body.data.state;
            setRunState(body.data.state);
          }
          if (body.agent_type) setAgentType(body.agent_type);
        },
      )
      .catch(() => fail('Could not load this run: the server could not be reached.'));
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
    const refresh = () => {
      t.last = Date.now();
      loadUsage();
      loadRun();
    };
    if (wait <= 0) refresh();
    else if (t.timer === null) {
      t.timer = setTimeout(() => {
        t.timer = null;
        refresh();
      }, wait);
    }
  }, [loadUsage, loadRun]);

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

  // Fetch the run, its opening usage snapshot and the agent's earlier runs.
  useEffect(() => {
    if (!runId) return;
    loadRun();
    loadUsage();
    if (!slug) return;
    const failed = 'Other runs could not be loaded.';
    void fetch(`${ApiRoute.runs.listForType(encodeURIComponent(slug))}?limit=8`)
      .then(async (r) => {
        if (!r.ok) {
          setEarlierRunsError(failed);
          return;
        }
        const body = (await r.json()) as RunsPageResponse;
        setEarlierRuns((body.data ?? []).filter((x) => x.id !== runId));
      })
      .catch(() => setEarlierRunsError(failed));
  }, [runId, slug, loadRun, loadUsage]);

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

    // The agent produced output: it is alive, any provider retry is over, and
    // a queued message whose boundary has passed has been read.
    function agentActed(): void {
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
      setSandboxStarting(SANDBOX_STARTING.has(payload.phase));
    });

    es.addEventListener('sandbox_environment', (e: MessageEvent) => {
      const payload = read<SandboxEnvironmentPayload>(e);
      if (payload?.image) setSandbox(payload);
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
            thinking: { content },
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
      setSandboxStarting(false);
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
        payload.state === 'budget_exhausted' ||
        payload.state === 'terminated'
      ) {
        // The run settled with the message in hand: a runtime that reads a
        // message within the turn (Codex, OpenCode) may produce nothing after it.
        markDelivered([...queued.keys()]);
        flushInProgress(true);
        setIsPending(false);
        setRetry(null);
        setSandboxStarting(false);
        // Final true-up of the usage meter when the run settles (terminal or
        // idle between turns) — catches any spend the last turn added.
        loadUsage();
        // An ended run has its end time and final totals; re-read them.
        if (payload.state !== 'idle') loadRun();
      }
      if (payload.state === 'budget_exhausted') setBudgetStop(payload.budget ?? null);
      runStateRef.current = payload.state;
      setRunState(payload.state);
    });

    es.addEventListener('human_message', (e: MessageEvent) => {
      const payload = read<HumanMessagePayload>(e);
      if (!payload?.content) return;
      // Skip if we sent this message ourselves (already optimistically rendered).
      // Only this viewer's own echo replaces their pending bubble; the same words from someone else are theirs.
      const mine = !payload.sender || payload.sender.id === userRef.current?.sub;
      const idx = mine ? pendingHumanMessages.current.indexOf(payload.content) : -1;
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
          const sent = next[at] as Extract<Turn, { type: 'human' }>;
          next[at] = { ...sent, timestamp: time, sender: payload.sender ?? sent.sender };
          return next;
        });
        return;
      }
      appendHuman({
        type: 'human',
        id: `h-${++turnSeq.current}`,
        content: payload.content,
        timestamp: eventTime,
        sender: payload.sender,
        event: payload.event,
      });
    });

    // A routed trigger sends a later delivery to a run already working on it.
    es.addEventListener('trigger_message', (e: MessageEvent) => {
      const payload = read<TriggerMessagePayload>(e);
      if (!payload?.content) return;
      appendHuman({
        type: 'human',
        id: `h-${++turnSeq.current}`,
        content: payload.content,
        timestamp: eventTime,
        fromTrigger: payload.trigger_name || payload.trigger_id || 'a trigger',
        event: payload.event,
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
  }, [runId, reconnectKey, loadUsage, loadRun, scheduleUsageRefresh, appendHuman, productName]);

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
      sender: user ? { id: user.sub, type: 'USER', name: user.name || user.email } : undefined,
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
    setStopError(null);
    void fetch(`/api/agents/runs/${runId}/interrupt`, { method: 'POST' })
      .then(async (res) => {
        if (res.ok) return;
        const body = (await res.json().catch(() => ({}))) as { message?: string };
        setStopError(
          res.status === 403
            ? 'You cannot stop this run.'
            : (body.message ?? `The run was not stopped (${res.status}).`),
        );
      })
      .catch(() => setStopError('The run was not stopped: the server could not be reached.'));
  }

  const allTurns: Turn[] = inProgressTurn ? [...committedTurns, inProgressTurn] : committedTurns;
  const isBusy = runState === 'starting' || runState === 'active';
  const isStarting = runState === 'pending' || runState === 'starting';

  return {
    runId,
    slug,
    run,
    agentType,
    earlierRuns,
    earlierRunsError,
    budgetStop,
    sandbox,
    canSteer,
    loadError,
    stopError,
    traceUrl,
    runState,
    turns: allTurns,
    isStreaming: inProgressTurn !== null,
    isPending,
    isStarting,
    isBusy,
    connectionStatus,
    liveActivity: isBusy ? deriveActivity(allTurns, retry, sandboxStarting) : null,
    spentUsd,
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

/** The friendly text for a run error code, if it has one. */
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
  sandboxStarting: boolean,
): LiveActivity {
  if (retry) return { kind: 'retry', ...retry };
  if (sandboxStarting) return { kind: 'sandbox' };
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
  return current ? { kind: 'tool', tool: current } : { kind: 'thinking' };
}

/**
 * Unwraps an SSE event: `{"at": <RFC 3339>, "payload": <event>}`. `at` is the
 * server's time for the event; `time` is it as local HH:MM:SS. A bare payload
 * (a kora-agents that predates the envelope) parses with no time.
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
