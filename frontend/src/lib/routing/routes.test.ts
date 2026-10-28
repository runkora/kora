import { describe, it, expect } from 'vitest';
import { APP_ROUTES, PUBLIC_PATHS, isSafeRedirectPath, skillSlugFromPath } from './routes';
import { ApiRoute } from './api-routes';

/**
 * Unit tests for app routing constants and helpers.
 *
 * Pure functions — no DOM, no network, no MSW required.
 */
describe('APP_ROUTES', () => {
  it('overview is the bare Marketplace path', () => {
    expect(APP_ROUTES.marketplace.overview).toBe('/marketplace');
  });

  it('keeps both Skill identifiers in encoded path segments without browse parameters', () => {
    const path = APP_ROUTES.marketplace.detail('team/repo ?#', 'skill/50% ?#');
    const url = new URL(path, 'http://localhost');
    expect(url.pathname).toBe('/marketplace/team%2Frepo%20%3F%23/skill%2F50%25%20%3F%23');
    expect(url.search).toBe('');
    expect(url.hash).toBe('');
  });

  it('leaves the repeated Marketplace prefix out of the Skill segment', () => {
    expect(
      APP_ROUTES.marketplace.detail('anthropics__skills', 'anthropics__skills__academy-guide'),
    ).toBe('/marketplace/anthropics__skills/academy-guide');
  });

  it('restores the full skillSlug from a short or an older full segment', () => {
    expect(skillSlugFromPath('anthropics__skills', 'academy-guide')).toBe(
      'anthropics__skills__academy-guide',
    );
    expect(skillSlugFromPath('anthropics__skills', 'anthropics__skills__academy-guide')).toBe(
      'anthropics__skills__academy-guide',
    );
  });
  // APP_ROUTES is `as const` — TypeScript enforces value types at every call
  // site, so asserting individual string values against themselves adds no
  // signal. The only structural invariant the type system cannot enforce is
  // "every route string starts with /", which is tested here.

  it('all flat routes start with /', () => {
    for (const [key, value] of Object.entries(APP_ROUTES)) {
      if (typeof value === 'string') {
        expect(value, `${key} should start with /`).toMatch(/^\//);
      }
    }
  });

  it('all modelGateway routes start with /', () => {
    for (const [key, value] of Object.entries(APP_ROUTES.modelGateway)) {
      // Some entries (e.g. analyticsPrincipal) are dynamic route builder
      // functions rather than plain strings — only assert on the literal ones.
      if (typeof value === 'string') {
        expect(value, `modelGateway.${key} should start with /`).toMatch(/^\//);
      }
    }
  });

  it('analyticsPrincipal route builder produces a path starting with / and encodes its id', () => {
    expect(APP_ROUTES.modelGateway.analyticsPrincipal('abc-123')).toMatch(/^\//);
    expect(APP_ROUTES.modelGateway.analyticsPrincipal('abc-123')).toBe(
      '/model-gateway/analytics/principals/abc-123',
    );
    // Reserved characters (e.g. a slash in an IdP-issued subject claim) must
    // be percent-encoded so the id is treated as a single path segment.
    expect(APP_ROUTES.modelGateway.analyticsPrincipal('team/sub-id')).toBe(
      '/model-gateway/analytics/principals/team%2Fsub-id',
    );
  });
});

describe('PUBLIC_PATHS', () => {
  // Page routes — must be public or the auth flow breaks entirely.
  it('contains the login route', () => {
    expect(PUBLIC_PATHS.has(APP_ROUTES.login)).toBe(true);
  });

  it('contains the auth callback route', () => {
    expect(PUBLIC_PATHS.has(APP_ROUTES.authCallback)).toBe(true);
  });

  // API routes — omitting any of these silently breaks the middleware.
  it('contains the session API route', () => {
    // POST /api/auth/session is called during the token exchange — must be
    // reachable before a session exists.
    expect(PUBLIC_PATHS.has(ApiRoute.session.POST())).toBe(true);
  });

  it('contains the logout API route', () => {
    // An expired session must still be able to reach the logout endpoint.
    expect(PUBLIC_PATHS.has(ApiRoute.logout.POST())).toBe(true);
  });

  it('contains the health API route', () => {
    // Load balancer probes arrive without a session cookie.
    expect(PUBLIC_PATHS.has(ApiRoute.health.GET())).toBe(true);
  });

  it('contains the discover API route', () => {
    // Home Realm Discovery is called pre-auth to resolve OIDC tenant config.
    expect(PUBLIC_PATHS.has(ApiRoute.discover.PATH())).toBe(true);
  });

  // Security invariant — authenticated routes must never appear in the set.
  it('does not contain authenticated routes', () => {
    expect(PUBLIC_PATHS.has(APP_ROUTES.home)).toBe(false);
    expect(PUBLIC_PATHS.has(APP_ROUTES.modelGateway.overview)).toBe(false);
  });
});

describe('isSafeRedirectPath', () => {
  describe('valid paths', () => {
    it('accepts the root path', () => {
      // APP_ROUTES.home is '/' — the most common post-auth redirect destination.
      expect(isSafeRedirectPath('/')).toBe(true);
    });

    it('accepts a simple absolute path', () => {
      expect(isSafeRedirectPath('/dashboard')).toBe(true);
    });

    it('accepts a nested path', () => {
      expect(isSafeRedirectPath('/model-gateway/models')).toBe(true);
    });

    it('accepts a path with a query string', () => {
      expect(isSafeRedirectPath('/dashboard?tab=overview')).toBe(true);
    });

    it('accepts a path with a hash', () => {
      expect(isSafeRedirectPath('/settings#profile')).toBe(true);
    });
  });

  describe('invalid paths', () => {
    it('rejects an empty string', () => {
      expect(isSafeRedirectPath('')).toBe(false);
    });

    it('rejects a path that does not start with /', () => {
      expect(isSafeRedirectPath('dashboard')).toBe(false);
    });

    it('rejects a protocol-relative URL (//)', () => {
      expect(isSafeRedirectPath('//evil.com')).toBe(false);
    });

    it('rejects an http URL', () => {
      expect(isSafeRedirectPath('http://evil.com/path')).toBe(false);
    });

    it('rejects an https URL', () => {
      expect(isSafeRedirectPath('https://evil.com/path')).toBe(false);
    });

    it('rejects a javascript: URI', () => {
      expect(isSafeRedirectPath('javascript:alert(1)')).toBe(false);
    });

    it('rejects a path with leading whitespace', () => {
      // ' /dashboard'.startsWith('/') is false — correctly rejected by the
      // first guard, but easy to miss when trimming input upstream.
      expect(isSafeRedirectPath(' /dashboard')).toBe(false);
    });
  });

  // Note: the catch { return false } branch on line 118 of routes.ts is
  // likely dead code. new URL(path, 'http://localhost') does not throw for
  // any string input — malformed paths are normalised, not thrown. If this
  // function is ever security-reviewed, consider removing the try/catch or
  // documenting a concrete input that would exercise the catch branch.
});
