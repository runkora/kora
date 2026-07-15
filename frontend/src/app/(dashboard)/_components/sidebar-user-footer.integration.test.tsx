/**
 * Integration tests for SidebarUserFooter.
 *
 * Strategy:
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   user state (loaded with user, loading with null, partial user without name).
 * - The LogoutButton is rendered as-is; its click behaviour is tested in
 *   health-status and logout integration tests.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarUserFooter } from './sidebar-user-footer';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({
    scopes: [],
    user: { sub: 'user-test-123', email: 'dev@orbit.local', name: 'Dev User' },
    loaded: true,
    devEnvironmentId: null,
  }),
}));

import { useSession } from './permissions-provider';

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderFooter() {
  return renderWithProviders(<SidebarUserFooter />);
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SidebarUserFooter', () => {
  it('renders the user email from the session', () => {
    renderFooter();
    expect(screen.getByText('dev@orbit.local')).toBeInTheDocument();
  });

  it('renders the user display name from the session', () => {
    renderFooter();
    expect(screen.getByText('Dev User')).toBeInTheDocument();
  });

  it('shows "Loading…" before the session is loaded (user is null)', () => {
    vi.mocked(useSession).mockReturnValueOnce({
      scopes: [],
      user: null,
      loaded: false,
      devEnvironmentId: null,
    });
    renderFooter();
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });

  it('renders a custom user email when provided', () => {
    vi.mocked(useSession).mockReturnValueOnce({
      scopes: [],
      user: { sub: 'custom-sub', email: 'alice@orbit.local', name: 'Alice' },
      loaded: true,
      devEnvironmentId: null,
    });
    renderFooter();
    expect(screen.getByText('alice@orbit.local')).toBeInTheDocument();
    expect(screen.getByText('Alice')).toBeInTheDocument();
  });

  it('does not render a name row when the user has no name', () => {
    vi.mocked(useSession).mockReturnValueOnce({
      scopes: [],
      user: { sub: 'anon-sub', email: 'anon@orbit.local', name: '' },
      loaded: true,
      devEnvironmentId: null,
    });
    renderFooter();
    expect(screen.getByText('anon@orbit.local')).toBeInTheDocument();
    expect(screen.queryByText('Dev User')).not.toBeInTheDocument();
  });

  it('renders the logout button', () => {
    renderFooter();
    expect(screen.getByRole('button', { name: /logout/i })).toBeInTheDocument();
  });
});
