'use client';

/**
 * Fetch mock for page stories. Pages call the BFF under `/api/...`; a story
 * lists the responses it needs and everything else answers 404, so a missing
 * mock shows up in the console instead of silently hanging.
 */

import * as React from 'react';
import type { Decorator } from '@storybook/react';

export interface MockRequest {
  url: URL;
  method: string;
  body: unknown;
}

/** A JSON value, or a function that builds one (or a full Response) from the request. */
export type MockHandler = unknown | ((request: MockRequest) => unknown);

/** Keys are `"GET /api/agents"`. A `:name` segment matches any value. */
export type MockRoutes = Record<string, MockHandler>;

function matches(pattern: string, path: string): boolean {
  const a = pattern.split('/');
  const b = path.split('/');
  return a.length === b.length && a.every((part, i) => part.startsWith(':') || part === b[i]);
}

function toResponse(value: unknown): Response {
  if (value instanceof Response) return value;
  return new Response(JSON.stringify(value), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function install(routes: MockRoutes): () => void {
  const original = window.fetch;
  window.fetch = async (input, init) => {
    const raw = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const url = new URL(raw, window.location.origin);
    const method = (
      init?.method ?? (input instanceof Request ? input.method : 'GET')
    ).toUpperCase();
    if (!url.pathname.startsWith('/api/')) return original(input, init);

    const key = Object.keys(routes).find((candidate) => {
      const [m, pattern] = candidate.split(' ');
      return m === method && pattern !== undefined && matches(pattern, url.pathname);
    });
    if (key === undefined) {
      console.warn(`[mock-api] no mock for ${method} ${url.pathname}`);
      return new Response(
        JSON.stringify({ code: 'not_mocked', message: `${method} ${url.pathname}` }),
        {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }
    const handler = routes[key];
    let body: unknown = undefined;
    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body);
      } catch {
        body = init.body;
      }
    }
    const value = typeof handler === 'function' ? await handler({ url, method, body }) : handler;
    return toResponse(value);
  };
  return () => {
    window.fetch = original;
  };
}

function Installed({ routes, children }: { routes: MockRoutes; children: React.ReactNode }) {
  // Installed during the first render so child effects that fetch on mount see it.
  const restore = React.useRef<(() => void) | null>(null);
  if (restore.current === null) restore.current = install(routes);
  React.useEffect(
    () => () => {
      restore.current?.();
      restore.current = null;
    },
    [],
  );
  return <>{children}</>;
}

/** Story decorator that answers `/api/...` calls from `routes`. */
export function mockApi(routes: MockRoutes): Decorator {
  const WithMockApi: Decorator = (Story) => (
    <Installed routes={routes}>
      <Story />
    </Installed>
  );
  return WithMockApi;
}
