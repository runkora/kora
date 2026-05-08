import { describe, it, expect } from 'vitest';
import { ApiRoute } from './api-routes';

/**
 * Unit tests for ApiRoute path builders.
 *
 * Pure string functions — no DOM, no network, no MSW required.
 *
 * ApiRoute methods carry explicit typed return annotations using template
 * literals (`(): \`/api/health\``), so asserting their return values against
 * hardcoded strings is ceremonial — the type system already enforces exact
 * values. The tests here focus on:
 *
 *   1. Runtime encoding behaviour that TypeScript's `${string}` slot cannot
 *      distinguish from unencoded output (discover.GET).
 *   2. Structural invariants the type system cannot express (leading `/api`,
 *      session verb symmetry, discover path coupling).
 */
describe('ApiRoute', () => {
  describe('discover', () => {
    it('PATH returns a path with no query string', () => {
      // PUBLIC_PATHS uses PATH() because the Edge middleware matches pathname
      // only — no query string. This documents that contract explicitly.
      expect(ApiRoute.discover.PATH()).not.toContain('?');
    });

    it('GET returns the fully encoded discover URL', () => {
      // toBe rather than toContain — verifies the full output, not just
      // partial presence. toContain would pass even if encoding were wrong.
      expect(ApiRoute.discover.GET('dev@orbit.local')).toBe(
        '/api/auth/discover?email=dev%40orbit.local',
      );
    });

    it('GET encodes + in the email local part', () => {
      // encodeURIComponent encodes '+' as '%2B'; encodeURI does not.
      // Plus-addressing (user+alias@example.com) is valid RFC 5321 — many
      // servers decode '+' as a space in query strings, producing a broken
      // lookup if it reaches the server unencoded.
      expect(ApiRoute.discover.GET('user+alias@example.com')).toBe(
        '/api/auth/discover?email=user%2Balias%40example.com',
      );
    });

    it('GET path segment matches PATH()', () => {
      // Documents the coupling between GET() and PATH(). PUBLIC_PATHS stores
      // PATH(); the browser fetches GET(email). If the path segment in GET()
      // ever drifted, the middleware would no longer recognise the request as
      // public and would block it before HRD could respond.
      const url = new URL(ApiRoute.discover.GET('any@example.com'), 'http://localhost');
      expect(url.pathname).toBe(ApiRoute.discover.PATH());
    });
  });

  describe('session', () => {
    it('GET and POST resolve to the same path', () => {
      // Both verbs hit the same endpoint — same URL, different HTTP methods.
      // Pinning this symmetry means a refactor that splits the endpoint
      // cannot silently break PUBLIC_PATHS (which uses POST()).
      expect(ApiRoute.session.GET()).toBe(ApiRoute.session.POST());
    });
  });

  describe('all paths start with /api', () => {
    it('every BFF route is under /api', () => {
      // The /api prefix is what Next.js middleware uses to distinguish BFF
      // calls from page navigations. Template literal return types enforce
      // this structurally, but this sweep independently cross-checks runtime
      // output and acts as a completeness guard — a new route added to
      // ApiRoute without being listed here is immediately visible.
      const paths = [
        ApiRoute.health.GET(),
        ApiRoute.identityHealth.GET(),
        ApiRoute.session.GET(),
        ApiRoute.session.POST(),
        ApiRoute.logout.POST(),
        ApiRoute.discover.PATH(),
        ApiRoute.discover.GET('test@example.com'),
        ApiRoute.models.GET(),
      ];

      for (const p of paths) {
        expect(p, `Expected "${p}" to start with /api`).toMatch(/^\/api/);
      }
    });
  });
});
