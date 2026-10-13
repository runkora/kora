/**
 * Integration tests for SidebarUserFooter.
 *
 * Strategy:
 * - `./permissions-provider` is vi.mock'd so `useSession` returns controllable
 *   user state. `next/navigation`'s `useRouter` is mocked so "View profile"
 *   navigation is assertable without a real router.
 * - The component picks its shell (desktop side flyout vs. mobile
 *   `BottomSheet`) via `window.matchMedia('(min-width: 768px)')`. The global
 *   test stub (`src/test-utils/setup.ts`) always reports `matches: false`, so
 *   by default every test here exercises the *mobile* sheet — matching most
 *   of this suite. A `installDesktopMatchMedia` helper (mirroring the
 *   pattern in `sidebar-state.test.tsx`) drives the desktop flyout tests.
 */
import { describe, it, expect, vi, afterEach } from 'vitest';
import { screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { APP_ROUTES } from '@orbit/lib/routing/routes';
import { SidebarUserFooter } from './sidebar-user-footer';

// ── Module mocks ──────────────────────────────────────────────────────────────

const { DEFAULT_SESSION, pushMock } = vi.hoisted(() => ({
  DEFAULT_SESSION: {
    scopes: [],
    user: { sub: 'user-test-123', email: 'dev@orbit.local', name: 'Dev User' },
    loaded: true,
  },
  pushMock: vi.fn(),
}));

vi.mock('./permissions-provider', () => ({
  useSession: vi.fn().mockReturnValue(DEFAULT_SESSION),
}));

vi.mock('next/navigation', () => ({
  useRouter: () => ({ push: pushMock }),
}));

import { useSession } from './permissions-provider';

afterEach(() => {
  vi.mocked(useSession).mockReturnValue(DEFAULT_SESSION);
  pushMock.mockClear();
});

// ── Helpers ───────────────────────────────────────────────────────────────────

function renderFooter() {
  return renderWithProviders(<SidebarUserFooter />);
}

/** Installs a controllable `matchMedia` so the desktop flyout renders instead
 *  of the mobile sheet. Restores the original afterwards. */
function installDesktopMatchMedia() {
  const original = window.matchMedia;
  window.matchMedia = ((query: string) => ({
    matches: query === '(min-width: 768px)',
    media: query,
    onchange: null,
    addEventListener: () => {},
    removeEventListener: () => {},
    addListener: () => {},
    removeListener: () => {},
    dispatchEvent: () => false,
  })) as unknown as typeof window.matchMedia;
  return () => {
    window.matchMedia = original;
  };
}

// ── Tests ─────────────────────────────────────────────────────────────────────

describe('SidebarUserFooter', () => {
  it('renders the user name and email in the trigger', () => {
    renderFooter();
    expect(screen.getByText('Dev User')).toBeInTheDocument();
    expect(screen.getByText('dev@orbit.local')).toBeInTheDocument();
  });

  it('shows "Loading…" before the session is loaded (user is null)', () => {
    vi.mocked(useSession).mockReturnValueOnce({ scopes: [], user: null, loaded: false });
    renderFooter();
    // Both the name and email fall back to "Loading…" when there's no user.
    expect(screen.getAllByText('Loading…').length).toBeGreaterThan(0);
  });

  it('does not duplicate the email when the user has no display name', async () => {
    vi.mocked(useSession).mockReturnValue({
      scopes: [],
      user: { sub: 'anon-sub', email: 'anon@orbit.local', name: '' },
      loaded: true,
    });
    const user = userEvent.setup();
    renderFooter();

    // The email renders with a zero-width space after '@' (soft wrap hint),
    // so match loosely rather than on the literal string.
    const emailPattern = /anon@​?orbit\.local/;

    // Trigger: a single email line, no separate bold name row.
    expect(screen.getAllByText(emailPattern)).toHaveLength(1);

    // Identity panel: same — "Signed in as" + email only, no name row.
    await user.click(screen.getByRole('button', { name: emailPattern }));
    expect(screen.getByText('Signed in as')).toBeInTheDocument();
    expect(screen.getAllByText(emailPattern)).toHaveLength(2);
  });

  describe('mobile (BottomSheet)', () => {
    it('opens the sheet with identity and menu items when the trigger is clicked', async () => {
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));

      expect(screen.getByText('Signed in as')).toBeInTheDocument();
      expect(screen.getByRole('menuitem', { name: /View profile/ })).toBeInTheDocument();
      expect(screen.getByRole('menuitem', { name: /Log out/ })).toBeInTheDocument();
    });

    it('navigates to the profile page and closes when View profile is selected', async () => {
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));
      await user.click(screen.getByRole('menuitem', { name: /View profile/ }));

      expect(pushMock).toHaveBeenCalledWith(APP_ROUTES.profile);
      await waitFor(() => {
        expect(screen.queryByText('Signed in as')).not.toBeInTheDocument();
      });
    });

    it('shows a confirm step before logging out, cancellable', async () => {
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));
      await user.click(screen.getByRole('menuitem', { name: /^Log out$/ }));

      expect(screen.getByText('Log out of kora?')).toBeInTheDocument();
      const dialog = screen.getByRole('dialog');
      await user.click(within(dialog).getByRole('button', { name: 'Cancel' }));

      expect(screen.queryByText('Log out of kora?')).not.toBeInTheDocument();
      expect(screen.getByRole('menuitem', { name: /View profile/ })).toBeInTheDocument();
    });
  });

  describe('desktop (side flyout)', () => {
    it('opens a non-modal menu with role="menu" when the trigger is clicked', async () => {
      const restore = installDesktopMatchMedia();
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));

      const menu = screen.getByRole('menu', { name: 'Account' });
      expect(within(menu).getByRole('menuitem', { name: /View profile/ })).toBeInTheDocument();
      expect(within(menu).getByRole('menuitem', { name: /Log out/ })).toBeInTheDocument();
      restore();
    });

    it('closes on outside click', async () => {
      const restore = installDesktopMatchMedia();
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));
      expect(screen.getByRole('menu', { name: 'Account' })).toBeInTheDocument();

      await user.click(document.body);
      expect(screen.queryByRole('menu', { name: 'Account' })).not.toBeInTheDocument();
      restore();
    });

    it('closes on Escape', async () => {
      const restore = installDesktopMatchMedia();
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));
      expect(screen.getByRole('menu', { name: 'Account' })).toBeInTheDocument();

      await user.keyboard('{Escape}');
      expect(screen.queryByRole('menu', { name: 'Account' })).not.toBeInTheDocument();
      restore();
    });

    it('drops menu semantics and focuses Cancel when entering the confirm step', async () => {
      const restore = installDesktopMatchMedia();
      const user = userEvent.setup();
      renderFooter();
      await user.click(screen.getByRole('button', { name: /Dev User/ }));
      await user.click(screen.getByRole('menuitem', { name: /^Log out$/ }));

      // A confirm step isn't menu navigation — the container stops being a
      // role="menu" and the actions are plain buttons, not menuitems.
      expect(screen.queryByRole('menu', { name: 'Account' })).not.toBeInTheDocument();
      expect(screen.queryByRole('menuitem')).not.toBeInTheDocument();

      const cancelButton = screen.getByRole('button', { name: 'Cancel' });
      await waitFor(() => expect(cancelButton).toHaveFocus());
      restore();
    });
  });
});
