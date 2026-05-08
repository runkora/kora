import { setupServer } from 'msw/node';
import { defaultHandlers } from './handlers';

/**
 * MSW server instance shared across all integration tests.
 *
 * Lifecycle hooks (listen / resetHandlers / close) are wired in
 * src/test-utils/setup.ts so every test file gets them automatically
 * via the `setupFiles` Vitest option — no per-file boilerplate needed.
 *
 * To override a handler for a single test:
 *   server.use(http.get('/api/auth/session', () => HttpResponse.json({ error: 'unauthorized' }, { status: 401 })))
 *
 * The override is automatically removed after each test by `resetHandlers()`.
 */
export const server = setupServer(...defaultHandlers);
