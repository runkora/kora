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
