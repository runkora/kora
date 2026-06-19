/**
 * Integration tests for DashboardHeader's mobile hamburger.
 *
 * `next/navigation` is mocked so `usePathname` is controllable. The header is
 * wrapped in a real `SidebarStateProvider` so the hamburger's wiring to the
 * shared open/close state is exercised end to end (toggle, aria-expanded, and
 * the accessible-label swap).
 */
import { describe, it, expect, vi } from 'vitest';
import { screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { renderWithProviders } from '@orbit/test-utils';
import { SidebarStateProvider } from './sidebar-state';
import { DashboardHeader } from './dashboard-header';

vi.mock('next/navigation', () => ({
  usePathname: vi.fn().mockReturnValue('/'),
}));

function renderHeader() {
  return renderWithProviders(
    <SidebarStateProvider>
      <DashboardHeader />
    </SidebarStateProvider>,
  );
}

describe('DashboardHeader hamburger', () => {
  it('renders the hamburger collapsed by default', () => {
    renderHeader();
    const button = screen.getByRole('button', { name: 'Open navigation' });
    expect(button).toHaveAttribute('aria-expanded', 'false');
  });

  it('opens the drawer state on click and swaps the accessible label', async () => {
    const user = userEvent.setup();
    renderHeader();

    await user.click(screen.getByRole('button', { name: 'Open navigation' }));

    const button = screen.getByRole('button', { name: 'Close navigation' });
    expect(button).toHaveAttribute('aria-expanded', 'true');
  });

  it('toggles back to closed on a second click', async () => {
    const user = userEvent.setup();
    renderHeader();

    await user.click(screen.getByRole('button', { name: 'Open navigation' }));
    await user.click(screen.getByRole('button', { name: 'Close navigation' }));

    expect(screen.getByRole('button', { name: 'Open navigation' })).toHaveAttribute(
      'aria-expanded',
      'false',
    );
  });
});
