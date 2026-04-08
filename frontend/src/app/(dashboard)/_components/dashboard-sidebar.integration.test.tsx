/**
 * Integration tests for DashboardSidebar.
 *
 * Strategy:
 * - `next/navigation` is vi.mock'd so `usePathname` returns a controllable value.
 * - MSW's default auth handlers provide the GET /api/auth/session response so
 *   the user's email and name appear in the sidebar footer without real network calls.
 * - We assert on nav group labels, nav item roles/hrefs, link presence for wip vs
 *   non-wip items, and the session-driven user identity in the footer.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { http, HttpResponse, delay } from 'msw';
import { createSessionPayload, renderWithProviders, server } from '@orbit/test-utils';
import { APP_ROUTES } from '@orbit/lib/routing';
import { DashboardSidebar } from './dashboard-sidebar';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

import { usePathname } from 'next/navigation';

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderSidebar() {
  return renderWithProviders(<DashboardSidebar />);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('DashboardSidebar', () => {
  describe('branding', () => {
    it('renders the Orbit logo text', () => {
      renderSidebar();
      expect(screen.getByText('Orbit')).toBeInTheDocument();
    });

    it('renders a semver version badge', () => {
      renderSidebar();
      // Assert shape (semver pattern) rather than the exact string so this test
      // does not fail on every version bump.
      expect(screen.getByText(/^v\d+\.\d+\.\d+/)).toBeInTheDocument();
    });
  });

  describe('navigation groups', () => {
    it('renders all four nav group labels', () => {
      renderSidebar();
      expect(screen.getByText('Control Plane')).toBeInTheDocument();
      expect(screen.getByText('Security')).toBeInTheDocument();
      expect(screen.getByText('Model Gateway')).toBeInTheDocument();
      expect(screen.getByText('System')).toBeInTheDocument();
    });

    it('wip items across all groups do not render as links', () => {
      // This exercises the wip ? <NavItem state="disabled"> : <NavItem asChild><a>
      // branch in the component. The href block covers link presence for non-wip
      // items — this test owns only the disabled/wip side of that branch.
      renderSidebar();
      const linkTexts = screen.getAllByRole('link').map((l) => l.textContent?.trim());
      // Control Plane wip
      expect(linkTexts).not.toContain('Agents');
      expect(linkTexts).not.toContain('Event Spine');
      expect(linkTexts).not.toContain('Credentials');
      // Security — entirely wip
      expect(linkTexts).not.toContain('Policy Engine');
      expect(linkTexts).not.toContain('Identity');
      // System — entirely wip
      expect(linkTexts).not.toContain('Storage');
      expect(linkTexts).not.toContain('Settings');
    });
  });

  describe('nav link hrefs', () => {
    // These tests guard against route regressions — a swap of two href constants
    // in nav-entries.ts would be invisible to text-only presence tests.

    it('Dashboard link points to APP_ROUTES.home', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute(
        'href',
        APP_ROUTES.home,
      );
    });

    it('all Model Gateway links point to the correct routes', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /overview/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.overview,
      );
      expect(screen.getByRole('link', { name: /models/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.models,
      );
      expect(screen.getByRole('link', { name: /usage/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.usage,
      );
      expect(screen.getByRole('link', { name: /logs/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.logs,
      );
      expect(screen.getByRole('link', { name: /api keys/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.keys,
      );
      expect(screen.getByRole('link', { name: /setup/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.setup,
      );
    });
  });

  describe('active state', () => {
    it('Dashboard link has the correct href when pathname is /', () => {
      // The active-state prop on NavItem changes visual styling — not testable
      // without inspecting NavItem internals. What we can pin is the href, which
      // guards against route constant regressions in nav-entries.ts.
      vi.mocked(usePathname).mockReturnValue(APP_ROUTES.home);
      renderSidebar();
      expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute(
        'href',
        APP_ROUTES.home,
      );
    });

    it('Overview link has the correct href when pathname matches the model gateway overview', () => {
      vi.mocked(usePathname).mockReturnValue(APP_ROUTES.modelGateway.overview);
      renderSidebar();
      expect(screen.getByRole('link', { name: /overview/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.overview,
      );
    });

    it('does not render a link for wip items regardless of pathname', () => {
      // Pathname value does not affect wip rendering — wip items always render
      // as disabled NavItems with no <a> element, regardless of current route.
      vi.mocked(usePathname).mockReturnValue(APP_ROUTES.home);
      renderSidebar();

      const links = screen.getAllByRole('link');
      const linkTexts = links.map((l) => l.textContent?.trim());
      expect(linkTexts).not.toContain('Agents');
    });
  });

  describe('footer — session user', () => {
    it('renders the user email from the session response', async () => {
      renderSidebar();
      await screen.findByText('dev@orbit.local');
    });

    it('renders the user display name from the session response', async () => {
      renderSidebar();
      await screen.findByText('Dev User');
    });

    it('renders "Loading…" before the session fetch resolves', () => {
      // delay('infinite') guarantees the handler never resolves for the duration
      // of this test — the component stays in the initial loading state with
      // certainty. A real setTimeout would be a race in a fast CI environment
      // and leaves a dangling timer after the test completes.
      server.use(
        http.get('/api/auth/session', async () => {
          await delay('infinite');
          return new HttpResponse(null, { status: 200 }); // unreachable
        }),
      );

      renderSidebar();
      expect(screen.getByText('Loading…')).toBeInTheDocument();
    });

    it('renders a custom user email when the session returns different data', async () => {
      const payload = createSessionPayload({
        user: { sub: 'custom-sub', email: 'alice@orbit.local', name: 'Alice' },
      });
      server.use(
        http.get('/api/auth/session', () =>
          HttpResponse.json({ user: payload.user, expiresAt: payload.expiresAt }),
        ),
      );

      renderSidebar();

      await screen.findByText('alice@orbit.local');
      await screen.findByText('Alice');
    });

    it('does not render a name row when the session returns no name', async () => {
      const payload = createSessionPayload({
        user: { sub: 'anon-sub', email: 'anon@orbit.local', name: '' },
      });
      server.use(
        http.get('/api/auth/session', () =>
          HttpResponse.json({ user: payload.user, expiresAt: payload.expiresAt }),
        ),
      );

      renderSidebar();

      // Wait for the fetch to settle — after findByText resolves the component
      // has re-rendered with session data and userName will not change again.
      await screen.findByText('anon@orbit.local');
      // The conditional {userName && <Text>} branch must not render
      expect(screen.queryByText('Dev User')).not.toBeInTheDocument();
    });

    it('shows "Loading…" when the session endpoint returns 401', async () => {
      // A 401 is a well-formed HTTP response — res.ok is false, the component
      // guards with `.then(res => res.ok ? res.json() : null)`, and the
      // if (data?.user) check is skipped. This is a distinct code path from
      // the network-error test: the .catch() branch is never entered.
      server.use(
        http.get('/api/auth/session', () =>
          HttpResponse.json({ error: 'unauthorized' }, { status: 401 }),
        ),
      );

      renderSidebar();

      await waitFor(() => {
        expect(screen.getByText('Loading…')).toBeInTheDocument();
      });
    });

    it('silently ignores a failed session fetch and shows "Loading…"', async () => {
      server.use(
        http.get('/api/auth/session', () => HttpResponse.error()),
      );

      renderSidebar();

      // After the network error the email state stays '' — falls back to 'Loading…'
      await waitFor(() => {
        expect(screen.getByText('Loading…')).toBeInTheDocument();
      });
    });
  });
});
