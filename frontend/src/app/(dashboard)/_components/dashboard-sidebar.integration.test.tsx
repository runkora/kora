/**
 * Integration tests for DashboardSidebar.
 *
 * Strategy:
 * - `next/navigation` is vi.mock'd so `usePathname` returns a controllable value.
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   scopes without needing a live PermissionsProvider + MSW. The provider's own
 *   fetch behaviour is tested separately.
 *
 * Default state (empty scopes) renders Control Plane and Model Gateway groups.
 * Policy group requires `platform:grants:read` and is hidden by default.
 * Security and System groups have been removed (no built features behind them yet).
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { APP_ROUTES } from '@orbit/lib/routing';
import { DashboardSidebar } from './dashboard-sidebar';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({ scopes: [], user: null, loaded: false }),
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

  describe('nav link hrefs', () => {
    it('Dashboard link points to APP_ROUTES.home', () => {
      renderSidebar();
      expect(screen.getByRole('link', { name: /dashboard/i })).toHaveAttribute(
        'href',
        APP_ROUTES.home,
      );
    });

    it('Models link points to the correct route', () => {
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
});
