import { http, HttpResponse } from 'msw';
import { ApiRoute } from '@runkora/lib/routing';
import { createHealthResponse } from '../../factories/health';

/**
 * Default MSW handler for the BFF health route.
 *
 * Returns a healthy response by default. Tests that need to exercise
 * loading or error states should override via `server.use(...)`.
 */
export const healthHandlers = [
  /**
   * The BFF identity-health route wraps its successful response as `{ data: HealthResponse }`.
   * This shape must match so that HealthStatus can read `body.data` correctly.
   */
  http.get(ApiRoute.identityHealth.GET(), () => {
    return HttpResponse.json({ data: createHealthResponse() });
  }),
];
