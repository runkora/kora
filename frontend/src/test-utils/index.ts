/**
 * Test utilities barrel.
 *
 * Aliased to `@runkora/test-utils` via `resolve.alias` in both Vitest configs.
 *
 * Re-exports everything a test file needs in one import line:
 *
 *   import { renderWithProviders, server, createSessionUser, … } from '@runkora/test-utils';
 */

// Rendering
export { renderWithProviders, ALL_TEST_SCOPES } from './render';

// MSW server — exposed so tests can call `server.use(...)` for overrides
export { server } from './msw/server';

// Factories
export {
  createSessionUser,
  createSessionPayload,
  createBackendSessionResponse,
} from './factories/session';
export { createHealthResponse } from './factories/health';
