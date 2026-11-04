import { http, HttpResponse } from 'msw';
import { ApiRoute } from '@runkora/lib/routing';
import { createSessionPayload } from '../../factories/session';

/**
 * Default MSW handlers for BFF auth routes.
 *
 * These provide happy-path responses for every auth endpoint so that
 * integration tests render into a "logged-in" state by default.
 * Individual tests that need to exercise error states should call
 * `server.use(...)` to override these handlers for that test only.
 */
export const authHandlers = [
  /**
   * GET /api/auth/session — returns the public session fields.
   * Client components call this to render the logged-in user.
   */
  http.get(ApiRoute.session.GET(), () => {
    const payload = createSessionPayload();
    return HttpResponse.json({
      user: payload.user,
      expiresAt: payload.expiresAt,
    });
  }),

  /**
   * POST /api/auth/session — accepts { id_token, tenant_id } and returns {}.
   * The real handler seals an iron-session cookie; in tests we just 200 OK.
   */
  http.post(ApiRoute.session.POST(), () => {
    return HttpResponse.json({});
  }),

  /**
   * POST /api/auth/logout — always succeeds.
   */
  http.post(ApiRoute.logout.POST(), () => {
    return HttpResponse.json({});
  }),

  /**
   * GET /api/auth/discover — returns a minimal tenant config.
   */
  http.get(ApiRoute.discover.PATH(), () => {
    return HttpResponse.json({
      tenant_id: 'test-tenant',
      issuer_url: 'https://auth.example.com',
      client_id: 'test-client',
      scopes: ['openid', 'profile', 'email'],
    });
  }),
];
