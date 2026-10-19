/**
 * Integration tests for DashboardSidebar.
 *
 * Strategy:
 * - `next/navigation` is vi.mock'd so `usePathname` returns a controllable value.
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   scopes without needing a live PermissionsProvider + MSW. The provider's own
 *   fetch behaviour is tested separately.
 * - `next/link` is vi.mock'd with a marker so the "client-side routing" test
 *   below can assert that nav items render through next/link rather than a
 *   plain `<a>` (regression coverage for #385). A raw `<a>` forces a full
 *   document navigation on click, which unmounts the dashboard layout and
 *   resets PermissionsProvider's RBAC state on every nav click. getByRole
 *   ('link', ...) alone can't tell the two apart since both render an <a> with
 *   the same href — this mock makes that distinction testable.
 *
 * Default state (empty scopes) renders Control Plane and Model Gateway groups.
 * Policy group requires `platform:grants:read` and is hidden by default.
 * Security and System groups have been removed (no built features behind them yet).
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@runkora/test-utils';
import { APP_ROUTES } from '@runkora/lib/routing';
import { DashboardSidebar } from './dashboard-sidebar';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({ scopes: [], user: null, loaded: false }),
}));

vi.mock('next/link', () => ({
  default: ({
    href,
    onClick,
    children,
  }: {
    href: string;
    onClick?: () => void;
    children: React.ReactNode;
  }) => (
    <a href={href} onClick={onClick} data-testid="next-link">
      {children}
    </a>
  ),
}));

import { usePathname } from 'next/navigation';
import { useSession } from './permissions-provider';

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderSidebar() {
  return renderWithProviders(<DashboardSidebar />);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('DashboardSidebar', () => {
  describe('navigation groups', () => {
    it('renders Control Plane and Model Gateway group labels by default', () => {
      renderSidebar();
      expect(screen.getByText('Control Plane')).toBeInTheDocument();
      expect(screen.getByText('Model Gateway')).toBeInTheDocument();
    });

    it('does not render Security or System group labels', () => {
      renderSidebar();
      expect(screen.queryByText('Security')).not.toBeInTheDocument();
      expect(screen.queryByText('System')).not.toBeInTheDocument();
    });

    it('hides the Policy group when the user has no scopes', () => {
      renderSidebar();
      expect(screen.queryByText('Policy')).not.toBeInTheDocument();
    });

    it('shows the Policy group when the user holds platform:grants:read', () => {
      vi.mocked(useSession).mockReturnValueOnce({
        scopes: ['platform:grants:read'],
        user: null,
        loaded: true,
      });
      renderSidebar();
      expect(screen.getByText('Policy')).toBeInTheDocument();
    });
  });

  describe('scoped nav entries', () => {
    it('hides Environments and Models when the user has no scopes', () => {
      renderSidebar();
      expect(screen.queryByRole('link', { name: /environments/i })).not.toBeInTheDocument();
      expect(screen.queryByRole('link', { name: /^models$/i })).not.toBeInTheDocument();
    });

    it('shows Environments when the user holds agents:environments:read', () => {
      vi.mocked(useSession).mockReturnValueOnce({
        scopes: ['agents:environments:read'],
        user: null,
        loaded: true,
      });
      renderSidebar();
      expect(screen.getByRole('link', { name: /environments/i })).toHaveAttribute(
        'href',
        APP_ROUTES.environments,
      );
    });

    it('shows Models when the user holds inference:list', () => {
      vi.mocked(useSession).mockReturnValueOnce({
        scopes: ['inference:list'],
        user: null,
        loaded: true,
      });
      renderSidebar();
      expect(screen.getByRole('link', { name: /^models$/i })).toBeInTheDocument();
    });
  });

  describe('nav link hrefs', () => {
    it('Dashboard link points to APP_ROUTES.home', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute(
        'href',
        APP_ROUTES.home,
      );
    });

    it('Models link points to the correct route', () => {
      vi.mocked(useSession).mockReturnValueOnce({
        scopes: ['inference:list'],
        user: null,
        loaded: true,
      });
      renderSidebar();
      expect(screen.getByRole('link', { name: /models/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.models,
      );
    });

    it('Agents link points to APP_ROUTES.agents.list', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /agents/i })).toHaveAttribute(
        'href',
        APP_ROUTES.agents.list,
      );
    });

    it('non-scoped Model Gateway links point to the correct routes', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /my usage/i })).toHaveAttribute(
        'href',
        APP_ROUTES.modelGateway.myUsage,
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
      vi.mocked(usePathname).mockReturnValue(APP_ROUTES.home);
      renderSidebar();
      expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute(
        'href',
        APP_ROUTES.home,
      );
    });
  });

  describe('client-side routing', () => {
    // Regression test for #385: nav items must render through next/link, not
    // a plain `<a>`. Both render an <a href="..."> with the same text, so
    // getByRole('link', ...).toHaveAttribute('href', ...) alone can't tell
    // them apart — a plain anchor would pass every other test in this file.
    // Only next/link preserves the dashboard layout (and PermissionsProvider's
    // RBAC state) across navigation instead of triggering a full page reload.
    // The mock above tags next/link's output with data-testid="next-link" so
    // this test can distinguish the two implementations.
    it('renders every non-wip nav item through next/link', () => {
      vi.mocked(useSession).mockReturnValueOnce({
        scopes: ['platform:grants:read', 'agents:environments:read', 'inference:list'],
        user: null,
        loaded: true,
      });
      renderSidebar();

      const links = screen.getAllByRole('link');
      expect(links.length).toBeGreaterThan(0);
      for (const link of links) {
        expect(link).toHaveAttribute('data-testid', 'next-link');
      }
    });
  });
});
