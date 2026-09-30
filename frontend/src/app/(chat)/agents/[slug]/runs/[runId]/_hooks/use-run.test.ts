import { renderHook, act, waitFor } from '@testing-library/react';
import { http, HttpResponse } from 'msw';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { server } from '@orbit/test-utils/msw/server';
import { useRun } from './use-run';

// Minimal controllable EventSource stand-in. jsdom ships no EventSource, and we
// need to drive onopen/onerror/named events deterministically and inspect how
// many instances the hook created across a reconnect.
class FakeEventSource {
  static readonly CONNECTING = 0;
  static readonly OPEN = 1;
  static readonly CLOSED = 2;
  static instances: FakeEventSource[] = [];

  url: string;
  readyState = FakeEventSource.CONNECTING;
  onopen: (() => void) | null = null;
  onerror: (() => void) | null = null;
  private listeners: Record<string, ((e: MessageEvent) => void)[]> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type: string, cb: (e: MessageEvent) => void) {
    (this.listeners[type] ??= []).push(cb);
  }
  removeEventListener() {}
  close() {
    this.readyState = FakeEventSource.CLOSED;
  }

  // ── test helpers ──
  open() {
    this.readyState = FakeEventSource.OPEN;
    this.onopen?.();
  }
  emit(type: string, data: unknown) {
    const ev = { data: JSON.stringify(data), type } as MessageEvent;
    (this.listeners[type] ?? []).forEach((cb) => cb(ev));
  }
  fail({ closed }: { closed: boolean }) {
    this.readyState = closed ? FakeEventSource.CLOSED : FakeEventSource.CONNECTING;
    this.onerror?.();
  }
}

function replayOneExchange(es: FakeEventSource) {
  // Mirrors what the backend replays on a fresh (no Last-Event-ID) connection:
  // the persisted human message followed by the completed agent turn.
  es.emit('human_message', { content: 'hello' });
  es.emit('message_complete', { text: 'hi there' });
}

describe('useRun — manual reconnect', () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    // Flush rAF-batched delta updates synchronously so assertions are deterministic.
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {});
    // The hook's initial metadata fetch must resolve against a handler
    // (MSW is configured to error on unhandled requests).
    server.use(
      http.get('/api/agents/runs/:runId', () =>
        HttpResponse.json({
          data: { state: 'active' },
          agent_type: { name: 'Test', model_id: 'm' },
        }),
      ),
      // The hook also refreshes the run usage meter on mount and on turn
      // completion — handle it so MSW does not error on the request.
      http.get('/api/agents/runs/:runId/usage', () =>
        HttpResponse.json({ run_id: 'run-1', spend_usd: 0, total_tokens: 0 }),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('rebuilds rather than duplicating turns when reconnect() replays full history', async () => {
    const { result } = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });

    // First connection: replay one human + one agent turn.
    act(() => {
      const es1 = FakeEventSource.instances[0];
      es1.open();
      replayOneExchange(es1);
    });
    expect(result.current.turns).toHaveLength(2);

    // Connection drops terminally → user clicks Reconnect.
    act(() => {
      FakeEventSource.instances[0].fail({ closed: true });
    });
    expect(result.current.connectionStatus).toBe('closed');

    act(() => {
      result.current.reconnect();
    });

    // The fresh EventSource carries no Last-Event-ID, so the backend replays the
    // SAME full history. Pre-fix this appended on top of the existing turns
    // (→ 4); the reset in reconnect() makes the replay rebuild to the same 2.
    expect(FakeEventSource.instances).toHaveLength(2);
    act(() => {
      const es2 = FakeEventSource.instances[1];
      es2.open();
      replayOneExchange(es2);
    });

    expect(result.current.turns).toHaveLength(2);
    expect(result.current.connectionStatus).toBe('open');
  });
});

describe('useRun — server event envelope', () => {
  beforeEach(() => {
    FakeEventSource.instances = [];
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {});
    server.use(
      http.get('/api/agents/runs/:runId', () =>
        HttpResponse.json({
          data: { state: 'active' },
          agent_type: { name: 'Test', model_id: 'm' },
        }),
      ),
      http.get('/api/agents/runs/:runId/usage', () =>
        HttpResponse.json({ run_id: 'run-1', spend_usd: 0, total_tokens: 0 }),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const at = '2026-09-29T01:02:03Z';
  const local = new Date(at).toTimeString().slice(0, 8);
  const later = '2026-09-29T01:07:09Z';
  const laterLocal = new Date(later).toTimeString().slice(0, 8);

  it('stamps replayed turns with the server time, not the reload time', async () => {
    const { result } = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });
    act(() => {
      const es = FakeEventSource.instances[0];
      es.open();
      es.emit('human_message', { at, payload: { content: 'hello' } });
      es.emit('message_complete', { at: later, payload: { text: 'hi there' } });
      es.emit('run_error', { at: later, payload: { code: 'agent_error', message: 'boom' } });
    });
    expect(result.current.turns.map((t) => t.timestamp)).toEqual([local, laterLocal, laterLocal]);
  });

  it('stamps a sent message with the time of its echo, and reads a bare payload', async () => {
    server.use(
      http.post('/api/agents/runs/:runId/messages', () => new HttpResponse(null, { status: 202 })),
    );
    const { result } = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });
    act(() => {
      FakeEventSource.instances[0].open();
      result.current.addHumanMessage('hello');
    });
    expect(result.current.turns[0].timestamp).toBe('');
    act(() => {
      FakeEventSource.instances[0].emit('human_message', { at, payload: { content: 'hello' } });
      FakeEventSource.instances[0].emit('message_complete', { text: 'from an older server' });
    });
    expect(result.current.turns.map((t) => t.timestamp)).toEqual([local, '']);
  });

  it('stamps a streamed turn with its first recorded event, as a reload does', async () => {
    const { result } = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });
    act(() => {
      const es = FakeEventSource.instances[0];
      es.open();
      es.emit('text_delta', { at, payload: { delta: 'Half' } });
      es.emit('message_complete', { at: later, payload: { text: 'Half', interrupted: true } });
      es.emit('turn_stopped', { at: later, payload: { reason: 'user' } });
    });
    expect(result.current.turns.at(-1)?.timestamp).toBe(laterLocal);
  });

  it('keeps the run state on run_error until a run_state event says otherwise', async () => {
    const { result } = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });
    act(() => {
      const es = FakeEventSource.instances[0];
      es.open();
      es.emit('run_state', { at, payload: { state: 'active' } });
      es.emit('run_error', { at, payload: { code: 'agent_error', message: 'boom' } });
    });
    expect(result.current.runState).toBe('active');
    expect(result.current.turns.at(-1)).toMatchObject({ type: 'error', message: 'boom' });

    act(() => {
      FakeEventSource.instances[0].emit('run_state', { at, payload: { state: 'idle' } });
    });
    expect(result.current.runState).toBe('idle');
  });
});

describe('useRun — activity, delivery and stops', () => {
  const at = '2026-09-29T01:02:03Z';
  let messagesStatus = 202;

  beforeEach(() => {
    FakeEventSource.instances = [];
    messagesStatus = 202;
    vi.stubGlobal('EventSource', FakeEventSource);
    vi.stubGlobal('requestAnimationFrame', (cb: FrameRequestCallback) => {
      cb(0);
      return 1;
    });
    vi.stubGlobal('cancelAnimationFrame', () => {});
    server.use(
      http.get('/api/agents/runs/:runId', () =>
        HttpResponse.json({
          data: { state: 'active' },
          agent_type: { name: 'Test', model_id: 'm' },
        }),
      ),
      http.get('/api/agents/runs/:runId/usage', () =>
        HttpResponse.json({ run_id: 'run-1', spend_usd: 0, total_tokens: 0 }),
      ),
      http.post('/api/agents/runs/:runId/messages', () =>
        messagesStatus === 202
          ? new HttpResponse(null, { status: 202 })
          : HttpResponse.json(
              { message: 'run is not accepting messages' },
              { status: messagesStatus },
            ),
      ),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  async function start() {
    const hook = renderHook(() => useRun('run-1', 'slug'));
    await act(async () => {
      await Promise.resolve();
    });
    const es = FakeEventSource.instances[0];
    const emit = (type: string, payload: unknown) =>
      act(() => {
        es.emit(type, { at, payload });
      });
    act(() => es.open());
    emit('run_state', { state: 'active' });
    return { ...hook, emit };
  }

  const lastHuman = (turns: ReturnType<typeof useRun>['turns']) =>
    turns.filter((t) => t.type === 'human').at(-1);

  it('shows a mid-turn message queued until the agent acts after a tool boundary', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Bash', input: { command: 'sleep 8' } });
    await act(async () => {
      result.current.addHumanMessage('stop after this');
      await Promise.resolve();
    });
    expect(lastHuman(result.current.turns)).toMatchObject({ delivery: 'queued' });

    emit('tool_progress', { tool_use_id: 't1', elapsed_seconds: 5 });
    expect(lastHuman(result.current.turns)).toMatchObject({ delivery: 'queued' });
    emit('tool_result', { tool_use_id: 't1', content: '', is_error: false });
    expect(lastHuman(result.current.turns)).toMatchObject({ delivery: 'queued' });
    emit('text_delta', { delta: 'Stopping here.' });
    expect(lastHuman(result.current.turns)).toMatchObject({ delivery: 'delivered' });
  });

  it('places a mid-turn message after what the agent already said, without repeating it', async () => {
    const { result, emit } = await start();
    emit('text_delta', { delta: 'Working on it. ' });
    await act(async () => {
      result.current.addHumanMessage('also do X');
    });
    emit('text_delta', { delta: 'Done.' });
    emit('message_complete', { text: 'Working on it. Done.' });
    const shape = result.current.turns.map((t) =>
      t.type === 'agent'
        ? t.elements.map((el) => (el.kind === 'text' ? el.content : el.kind))
        : t.type,
    );
    expect(shape).toEqual([['Working on it. '], 'human', ['Done.']]);
  });

  it('clears the pending loader and ends running calls when a turn fails', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Bash', input: {} });
    await act(async () => {
      result.current.addHumanMessage('hurry');
    });
    emit('run_error', { code: 'agent_error', message: 'API Error: 500' });
    expect(result.current.isPending).toBe(false);
    const agent = result.current.turns.find((t) => t.type === 'agent');
    expect(agent?.type === 'agent' && agent.elements[0]).toMatchObject({
      tool: { id: 't1', status: 'stopped' },
    });
  });

  it('on replay, places a message sent mid-turn after the calls made before it', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Bash', input: { command: 'sleep 8' } });
    emit('human_message', { content: 'stop after this' });
    emit('tool_result', { tool_use_id: 't1', content: '' });
    emit('message_complete', { text: 'DONE-EARLY' });
    expect(result.current.turns.map((t) => t.type)).toEqual(['agent', 'human', 'agent']);
    const first = result.current.turns[0];
    expect(first.type === 'agent' && first.elements[0]).toMatchObject({
      tool: { id: 't1', status: 'done' },
    });
  });

  it('on replay, places a message sent mid-prose after the text recorded before it', async () => {
    const { result, emit } = await start();
    emit('text_block', { content: 'Working on it. ' });
    emit('human_message', { content: 'also do X' });
    emit('message_complete', { text: 'Done.' });
    const shape = result.current.turns.map((t) =>
      t.type === 'agent'
        ? t.elements.map((el) => (el.kind === 'text' ? el.content : el.kind))
        : t.type,
    );
    expect(shape).toEqual([['Working on it. '], 'human', ['Done.']]);
  });

  it('marks a queued message delivered when the run settles with no later output', async () => {
    const { result, emit } = await start();
    emit('text_delta', { delta: 'Writing' });
    await act(async () => {
      result.current.addHumanMessage('end with a summary');
    });
    emit('text_delta', { delta: ' and a summary.' });
    emit('message_complete', { text: 'Writing and a summary.' });
    emit('run_state', { state: 'idle' });
    expect(result.current.turns.filter((t) => t.type === 'human').at(-1)).toMatchObject({
      delivery: 'delivered',
    });
  });

  it('surfaces a send the server refused', async () => {
    messagesStatus = 409;
    const { result } = await start();
    await act(async () => {
      result.current.addHumanMessage('hello');
    });
    await waitFor(() =>
      expect(lastHuman(result.current.turns)).toMatchObject({
        delivery: 'failed',
        error: 'run is not accepting messages',
      }),
    );
  });

  it('keeps the partial text of a stopped turn with a stop marker', async () => {
    const { result, emit } = await start();
    emit('text_delta', { delta: 'Half an ans' });
    emit('message_complete', { text: 'Half an ans', interrupted: true });
    emit('turn_stopped', { reason: 'user' });
    const turn = result.current.turns.at(-1);
    expect(turn).toMatchObject({
      type: 'agent',
      elements: [{ kind: 'text', content: 'Half an ans' }, { kind: 'stopped' }],
    });
  });

  it('marks a turn the loop guard stopped as stopped by Orbit', async () => {
    const { result, emit } = await start();
    emit('text_delta', { delta: 'Trying again' });
    emit('message_complete', { text: 'Trying again', interrupted: true });
    emit('turn_stopped', { reason: 'guardrail' });
    expect(result.current.turns.at(-1)).toMatchObject({
      elements: [{ kind: 'text' }, { kind: 'stopped', byGuardrail: true }],
    });
  });

  it('reports what the agent is doing on the status line', async () => {
    const { result, emit } = await start();
    emit('sandbox_status', { phase: 'provisioning', elapsed_ms: 0 });
    expect(result.current.liveActivity?.kind).toBe('sandbox');
    emit('sandbox_status', { phase: 'egress_ready', elapsed_ms: 20000 });
    emit('api_retry', { attempt: 3, max_retries: 10, error: 'server_error' });
    expect(result.current.liveActivity).toMatchObject({
      kind: 'retry',
      attempt: 3,
      maxRetries: 10,
    });
    emit('tool_use', { id: 't1', name: 'Bash', input: { command: 'go test ./...' } });
    expect(result.current.liveActivity).toMatchObject({ kind: 'tool', tool: { id: 't1' } });
  });

  it('shows guardrail notices and a lost sandbox inside the turn', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Monitor', input: {} });
    emit('guardrail_warning', { kind: 'wait_rearm', count: 6, tool: 'Monitor' });
    emit('run_error', { code: 'sandbox_lost', message: 'pod gone' });
    const turn = result.current.turns.at(-1);
    expect(turn?.type === 'agent' && turn.elements.slice(1)).toEqual([
      {
        kind: 'notice',
        message:
          'Orbit asked the agent to change approach: Monitor re-armed 6 times with no new output.',
      },
      expect.objectContaining({ kind: 'notice', tone: 'error' }),
    ]);
    expect(result.current.runState).toBe('active');

    emit('run_error', { code: 'loop_detected', message: 'paused' });
    expect(result.current.turns.at(-1)).toMatchObject({
      type: 'error',
      message: expect.stringContaining('Reply to continue'),
      detail: 'paused',
    });
  });

  it('keeps the running call on the status line after a mid-turn message', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Bash', input: { command: 'sleep 8' } });
    await act(async () => {
      result.current.addHumanMessage('then stop');
    });
    expect(result.current.liveActivity).toMatchObject({ kind: 'tool', tool: { id: 't1' } });
  });

  it('keeps the running call on the status line after several mid-turn messages', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'Bash', input: { command: 'sleep 60' } });
    for (const m of ['one', 'two', 'three']) {
      await act(async () => {
        result.current.addHumanMessage(m);
      });
    }
    expect(result.current.liveActivity).toMatchObject({ kind: 'tool', tool: { id: 't1' } });
  });

  it('nests a subagent call under its Agent call after a mid-turn message split the turn', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 'a1', name: 'Agent', input: {} });
    await act(async () => {
      result.current.addHumanMessage('meanwhile');
    });
    emit('text_delta', { delta: 'Noted.' });
    emit('tool_use', { id: 'c1', name: 'Bash', input: {}, parent_tool_use_id: 'a1' });
    const first = result.current.turns[0];
    expect(
      first.type === 'agent' && first.elements.map((e) => e.kind === 'tool' && e.tool.id),
    ).toEqual(['a1', 'c1']);
  });

  it('counts a replayed running call from its server start, not the reload', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 't1', name: 'bash', input: { command: 'make' } });
    const activity = result.current.liveActivity;
    expect(activity).toMatchObject({ kind: 'tool', since: Date.parse(at) });
  });

  it('attaches a subagent call to its Agent call and records durations from server time', async () => {
    const { result, emit } = await start();
    emit('tool_use', { id: 'a1', name: 'Agent', input: {} });
    emit('tool_use', { id: 'c1', name: 'Bash', input: {}, parent_tool_use_id: 'a1' });
    const turn = result.current.turns.at(-1);
    expect(turn?.type === 'agent' && turn.elements[1]).toMatchObject({
      kind: 'tool',
      tool: { id: 'c1', parentId: 'a1', startedAt: Date.parse(at) },
    });
  });
});
