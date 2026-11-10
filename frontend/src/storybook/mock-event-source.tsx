'use client';

/**
 * EventSource stand-in for page stories. Pages that stream from the BFF open an
 * EventSource; this replays a fixed list of events to every stream opened while
 * the story is mounted, as a backend replays a run's log on a fresh connection.
 */

import * as React from 'react';
import type { Decorator } from '@storybook/react';

/** One SSE event: its name and the JSON body the server would send as `data`. */
export interface MockStreamEvent {
  type: string;
  data: unknown;
}

type Listener = (event: MessageEvent) => void;

function fakeEventSource(events: MockStreamEvent[]) {
  return class FakeEventSource {
    static readonly CONNECTING = 0;
    static readonly OPEN = 1;
    static readonly CLOSED = 2;

    readonly url: string;
    readyState = FakeEventSource.CONNECTING;
    onopen: (() => void) | null = null;
    onerror: (() => void) | null = null;
    private listeners: Record<string, Listener[]> = {};

    constructor(url: string | URL) {
      this.url = String(url);
      // Replayed after the opening effect has attached its listeners.
      setTimeout(() => {
        if (this.readyState === FakeEventSource.CLOSED) return;
        this.readyState = FakeEventSource.OPEN;
        this.onopen?.();
        for (const { type, data } of events) {
          if (this.readyState === FakeEventSource.CLOSED) return;
          const event = new MessageEvent(type, { data: JSON.stringify(data) });
          (this.listeners[type] ?? []).forEach((listener) => listener(event));
        }
      }, 0);
    }

    addEventListener(type: string, listener: Listener) {
      (this.listeners[type] ??= []).push(listener);
    }

    removeEventListener(type: string, listener: Listener) {
      this.listeners[type] = (this.listeners[type] ?? []).filter((l) => l !== listener);
    }

    close() {
      this.readyState = FakeEventSource.CLOSED;
    }
  };
}

function install(events: MockStreamEvent[]): () => void {
  const original = window.EventSource;
  window.EventSource = fakeEventSource(events) as unknown as typeof EventSource;
  return () => {
    window.EventSource = original;
  };
}

function Installed({ events, children }: { events: MockStreamEvent[]; children: React.ReactNode }) {
  // Installed during the first render so the page's mount effects see it.
  const restore = React.useRef<(() => void) | null>(null);
  if (restore.current === null) restore.current = install(events);
  React.useEffect(
    () => () => {
      restore.current?.();
      restore.current = null;
    },
    [],
  );
  return <>{children}</>;
}

/** Story decorator that answers every EventSource with `events`. */
export function mockEventSource(events: MockStreamEvent[]): Decorator {
  const WithMockEventSource: Decorator = (Story) => (
    <Installed events={events}>
      <Story />
    </Installed>
  );
  return WithMockEventSource;
}
