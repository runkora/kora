/**
 * Integration tests for SidebarUserFooter.
 *
 * Strategy:
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   user state (loaded with user, loading with null, partial user without name).
 * - The popover menu's logout action is exercised by opening the menu; the
 *   logout fetch behaviour itself is covered by logout-button.integration.test.
 */
import { describe, it, expect, vi } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarUserFooter } from './sidebar-user-footer';

// ── Module mocks ──────────────────────────────────────────────────────────────

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue({
    scopes: [],
    user: { sub: 'user-test-123', email: 'dev@orbit.local', name: 'Dev User' },
    loaded: true,
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
    vi.mocked(useSession).mockReturnValueOnce({ scopes: [], user: null, loaded: false });
    renderFooter();
    expect(screen.getByText('Loading…')).toBeInTheDocument();
  });

  it('renders a custom user email when provided', () => {
    vi.mocked(useSession).mockReturnValueOnce({
      scopes: [],
      user: { sub: 'custom-sub', email: 'alice@orbit.local', name: 'Alice' },
      loaded: true,
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
    });
    renderFooter();
    expect(screen.getByText('anon@orbit.local')).toBeInTheDocument();
    expect(screen.queryByText('Dev User')).not.toBeInTheDocument();
  });

  it('opens a menu with View Profile and Log out when the user trigger is clicked', async () => {
    renderFooter();
    await userEvent.click(screen.getByRole('button', { name: /user menu/i }));
    expect(await screen.findByText('View Profile')).toBeInTheDocument();
    expect(screen.getByText('Log out')).toBeInTheDocument();
  });

  it('closes the menu when View Profile is clicked', async () => {
    renderFooter();
    await userEvent.click(screen.getByRole('button', { name: /user menu/i }));
    const profileLink = await screen.findByText('View Profile');
    await userEvent.click(profileLink);
    await waitFor(() => {
      expect(screen.queryByText('View Profile')).not.toBeInTheDocument();
    });
  });
});
