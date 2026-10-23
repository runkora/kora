/**
 * Shared E2E test fixtures.
 *
 * `injectSessionCookie` creates a valid iron-session cookie using the same
 * secret and payload shape that the Next.js app expects, then sets it in the
 * browser context before any navigation.  This bypasses the Edge middleware
 * auth redirect (which reads the cookie directly — page.route() cannot
 * intercept server-side cookie checks) so tests can navigate to protected
 * routes without going through the OIDC login flow.
 *
 * The secret must match BFF_SESSION_SECRET in playwright.config.ts.
 */
import { sealData } from 'iron-session';
import type { BrowserContext } from '@playwright/test';

const TEST_SESSION_SECRET = 'playwright-test-session-secret-32ch';
const SESSION_COOKIE_NAME = 'kora_session';
// Match the server's default maxAge (30 days). sealData takes ttl in seconds, not ms.
const TTL_S = 30 * 24 * 60 * 60;

const TEST_SESSION_PAYLOAD = {
  user: { sub: 'user-e2e', email: 'e2e@runkora.test', name: 'E2E User' },
  accessToken: 'e2e-test-access-token',
  refreshToken: 'e2e-test-refresh-token',
  expiresAt: Math.floor(Date.now() / 1000) + 3600,
  tenantId: 'test-tenant',
  permissions: { scopes: [], evaluatedAt: Math.floor(Date.now() / 1000) },
};

/**
 * Seals a test session payload and injects it as the iron-session cookie into
 * the given browser context.  Call before page.goto() on any protected route.
 */
export async function injectSessionCookie(context: BrowserContext): Promise<void> {
  const sealedSession = await sealData(TEST_SESSION_PAYLOAD, {
    password: TEST_SESSION_SECRET,
    ttl: TTL_S,
  });

  await context.addCookies([
    {
      name: SESSION_COOKIE_NAME,
      value: sealedSession,
      url: 'http://localhost:3000',
      httpOnly: true,
      sameSite: 'Strict',
    },
  ]);
}
