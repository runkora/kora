import { renderHook, act } from '@testing-library/react';
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
    const ev = { data: JSON.stringify(data) } as MessageEvent;
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
