/**
 * Vitest global setup file.
 *
 * Loaded via `setupFiles` in both vitest.config.unit.ts and
 * vitest.config.integration.ts. Runs once per worker before any test suite.
 *
 * Responsibilities:
 *   1. Extend `expect` with @testing-library/jest-dom matchers.
 *   2. Wire MSW server lifecycle so every test file gets request interception
 *      without any per-file boilerplate.
 *   3. Stub `window.matchMedia`, which jsdom does not implement.
 */
import '@testing-library/jest-dom/vitest';
import { beforeAll, afterEach, afterAll } from 'vitest';
import { cleanup } from '@testing-library/react';
import { server } from './msw/server';

// jsdom does not implement `window.matchMedia`. Provide a minimal, inert stub so
// components that read a media query on mount (e.g. SidebarStateProvider, which
// closes the mobile drawer at >= md) don't throw. Tests that need to *drive*
// media-query changes install their own controllable mock and restore afterwards.
if (typeof window !== 'undefined' && typeof window.matchMedia !== 'function') {
  window.matchMedia = (query: string): MediaQueryList =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}

// Start the MSW server before any test runs and require that every outbound
// fetch is matched by a handler — unhandled requests surface as test failures
// rather than silently succeeding or hanging.
beforeAll(() => {
  server.listen({ onUnhandledRequest: 'error' });
});

// Unmount any React trees rendered in the previous test and remove per-test
// MSW handler overrides. Explicit cleanup is required because globals: false
// in both Vitest configs means RTL's auto-cleanup listener is never registered.
afterEach(() => {
  cleanup();
  server.resetHandlers();
});

// Tear down the server after the entire suite has finished.
afterAll(() => {
  server.close();
});
